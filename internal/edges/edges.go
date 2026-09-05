// Package edges derives reference and nesting relationships between skill
// nodes.
//
// Two edge kinds, both derived purely from the nodes and the filesystem:
//
//	ref      an md file inside a skill points at another skill (or a file in one)
//	contains a skill's directory is the nearest-ancestor of another's (nesting)
//
// Symlinks form no edge: a symlinked directory shares its target's realpath and
// is already one node, so the fact lives in provenance / multi_mounted, not
// here.
package edges

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"skillscope/internal/collect"
	"skillscope/internal/paths"
)

// Edge is one relationship between two node ids. Ref edges additionally carry
// from_file / raw / target_is_skill_md; contains edges leave them unset.
type Edge struct {
	Kind            string `json:"kind"`
	From            string `json:"from"`
	To              string `json:"to"`
	FromFile        string `json:"from_file,omitempty"`
	Raw             string `json:"raw,omitempty"`
	TargetIsSkillMD *bool  `json:"target_is_skill_md,omitempty"`
}

// BrokenRef is a reference that did not resolve to any existing path.
type BrokenRef struct {
	From     string `json:"from"`
	FromFile string `json:"from_file"`
	Raw      string `json:"raw"`
	Resolved string `json:"resolved"`
	RealFrom string `json:"real_from"`
}

// ExternalRef is a reference resolving to an existing file outside any skill.
type ExternalRef struct {
	From     string `json:"from"`
	FromFile string `json:"from_file"`
	Raw      string `json:"raw"`
	Resolved string `json:"resolved"`
}

// A relative path or bare .md target inside a markdown link.
var reMDLink = regexp.MustCompile(`\]\((\.\.?/[^)\s]+?|[A-Za-z0-9_][^)\s]*?\.md)\)`)
var reAnyMDLink = regexp.MustCompile(`!?\[[^\]]*\]\([^)]*\)`)
var reBacktick = regexp.MustCompile("`[^`]*`")

// reFenceMarker matches a fence delimiter line: optional indentation then
// three-or-more backticks (capture group 1 = the backtick run).
var reFenceMarker = regexp.MustCompile(`^\s*(` + "```" + `+)`)
var reFence = regexp.MustCompile("(?s)```.*?```")

// After backticks/links are stripped, bare relative paths in prose pointing at
// a skill's internal resources. The leading (?:^|[^\w`]) stands in for the
// Python version's negative lookbehind, which RE2 does not support.
var reBare = regexp.MustCompile(`(?:^|[^\w` + "`" + `])(\.\.?/[A-Za-z0-9_./-]+\.(?:md|sh|py|json|ya?ml|txt))`)

// Suites often write skills/<name>/SKILL.md (relative to the current file's dir).
var reSuite = regexp.MustCompile(`(?:^|[\s` + "`" + `(|])((?:[A-Za-z0-9_.-]+/)*skills/[A-Za-z0-9_.-]+/SKILL\.md)`)

// Build produces ref + contains edges, plus broken and external refs. Ref
// attribution: a file belongs to "the deepest skill that contains it".
func Build(nodes map[string]*collect.Node, roots []collect.Root) ([]Edge, []BrokenRef, []ExternalRef) {
	ownerOf := collect.OwnerIndex(nodes)
	nodeDirs := map[string]bool{}
	for d := range nodes {
		nodeDirs[d] = true
	}
	repoRootsByRoot := map[string][]string{}
	for _, r := range roots {
		repoRootsByRoot[r.Path] = paths.FindRepoRoots(r.Path)
	}

	var edges []Edge
	var broken []BrokenRef
	var external []ExternalRef
	refEdges(nodes, ownerOf, repoRootsByRoot, &edges, &broken, &external)
	containsEdges(nodeDirs, &edges)
	return edges, broken, external
}

// accessibleDir picks a really-existing path to scan (prefer provenance, fall
// back to id).
func accessibleDir(node *collect.Node) string {
	for _, prov := range node.Provenance {
		cand := filepath.Join(prov.Root, prov.Rel)
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			return cand
		}
	}
	return node.ID
}

// proseWithoutCode returns the markdown document with all code removed:
// fenced code blocks (``` ... ```, honoring fence depth so a ````-fenced
// sample enclosing ``` lines is dropped as one block) and inline-code spans
// (`...`). A reference only counts in real prose -- a path inside code is an
// example, command, or sample content, not a document the agent is pointed
// at, so it must never produce a ref edge or a broken-ref diagnostic.
func proseWithoutCode(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	depth := 0
	for _, l := range lines {
		if m := reFenceMarker.FindStringSubmatch(l); m != nil {
			n := len(m[1])
			if depth == 0 {
				depth = n
				continue
			}
			if n >= depth && strings.TrimSpace(strings.TrimLeft(l, " \t")[n:]) == "" {
				depth = 0
			}
			continue
		}
		if depth > 0 {
			continue // body of a fenced code block
		}
		out = append(out, reBacktick.ReplaceAllString(l, ""))
	}
	return strings.Join(out, "\n")
}

// extractTargets returns the reference target strings a markdown document
// contains in its prose. All matching runs on code-stripped text: markdown
// links, bare relative paths, and suite-relative SKILL.md paths inside code
// samples are examples, not references, and are excluded.
func extractTargets(content string) map[string]bool {
	targets := map[string]bool{}
	prose := proseWithoutCode(content)
	for _, m := range reMDLink.FindAllStringSubmatch(prose, -1) {
		targets[m[1]] = true
	}
	stripped := reAnyMDLink.ReplaceAllString(prose, "")
	for _, m := range reBare.FindAllStringSubmatch(stripped, -1) {
		targets[m[1]] = true
	}
	for _, m := range reSuite.FindAllStringSubmatch(stripped, -1) {
		targets[m[1]] = true
	}
	return targets
}

// resolve resolves one raw link to an existing path, or "". It tries
// file-relative first, then skill-root-relative, then repo-root-relative
// (skills write links all three ways). It returns the path component (anchors
// and queries stripped) and the resolved path.
func resolve(raw, fromDir, src string, repoRoots []string) (string, string) {
	pp := raw
	if i := strings.Index(pp, "#"); i >= 0 {
		pp = pp[:i]
	}
	if i := strings.Index(pp, "?"); i >= 0 {
		pp = pp[:i]
	}
	if pp == "" || strings.HasSuffix(pp, "/") {
		return pp, ""
	}
	resolved := filepath.Clean(filepath.Join(fromDir, pp))
	if !paths.Exists(resolved) && !strings.HasPrefix(pp, "..") {
		alt1 := filepath.Clean(filepath.Join(src, pp))
		if paths.Exists(alt1) {
			return pp, alt1
		}
		for _, rr := range repoRoots {
			alt := filepath.Clean(filepath.Join(rr, pp))
			if paths.Exists(alt) {
				return pp, alt
			}
		}
	}
	if paths.Exists(resolved) {
		return pp, resolved
	}
	return pp, ""
}

func refEdges(nodes map[string]*collect.Node, ownerOf func(string) string,
	repoRootsByRoot map[string][]string, edges *[]Edge, broken *[]BrokenRef,
	external *[]ExternalRef) {
	scanned := map[string]bool{}
	nodeIDs := make([]string, 0, len(nodes))
	for nid := range nodes {
		nodeIDs = append(nodeIDs, nid)
	}
	sort.Strings(nodeIDs)
	for _, nid := range nodeIDs {
		node := nodes[nid]
		if node.BrokenSymlink {
			continue
		}
		access := accessibleDir(node)
		repoRoots := repoRootsByRoot[node.Provenance[0].Root]

		var mdFiles []string
		paths.WalkFollow(access, func(dir string, names []string) bool {
			for _, n := range names {
				if strings.HasSuffix(n, ".md") {
					full := filepath.Join(dir, n)
					if info, err := os.Lstat(full); err == nil && !info.IsDir() {
						mdFiles = append(mdFiles, full)
					}
				}
			}
			return false
		})
		sort.Strings(mdFiles)

		for _, fp := range mdFiles {
			rp := paths.RealPath(fp)
			if scanned[rp] {
				continue
			}
			scanned[rp] = true
			src := ownerOf(fp) // deepest skill owning this md file
			if src == "" {
				continue
			}
			content := paths.ReadText(fp)
			fromFile := filepath.Base(fp)
			if filepath.Dir(rp) != src {
				fromFile, _ = filepath.Rel(src, rp)
			}

			raws := make([]string, 0)
			for t := range extractTargets(content) {
				raws = append(raws, t)
			}
			sort.Strings(raws)
			for _, raw := range raws {
				pp, resolvedPath := resolve(raw, filepath.Dir(fp), src, repoRoots)
				if pp == "" || strings.HasSuffix(pp, "/") {
					continue
				}
				if resolvedPath == "" {
					*broken = append(*broken, BrokenRef{
						From:     src,
						FromFile: fromFile,
						Raw:      raw,
						Resolved: filepath.Clean(filepath.Join(filepath.Dir(fp), pp)),
						RealFrom: rp,
					})
					continue
				}
				tgtOwner := ownerOf(resolvedPath)
				if tgtOwner == "" {
					*external = append(*external, ExternalRef{
						From:     src,
						FromFile: fromFile,
						Raw:      raw,
						Resolved: paths.RealPath(resolvedPath),
					})
					continue
				}
				if tgtOwner == src {
					continue // references a file inside its own dir; no edge
				}
				isSkillMD := filepath.Base(resolvedPath) == "SKILL.md"
				*edges = append(*edges, Edge{
					Kind:            "ref",
					From:            src,
					To:              tgtOwner,
					FromFile:        fromFile,
					Raw:             raw,
					TargetIsSkillMD: &isSkillMD,
				})
			}
		}
		_ = nid
	}
}

func containsEdges(nodeDirs map[string]bool, edges *[]Edge) {
	dirs := make([]string, 0, len(nodeDirs))
	for d := range nodeDirs {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if len(dirs[i]) != len(dirs[j]) {
			return len(dirs[i]) < len(dirs[j])
		}
		return dirs[i] < dirs[j]
	})
	sep := string(os.PathSeparator)
	for i, a := range dirs {
		for _, b := range dirs[i+1:] {
			if !strings.HasPrefix(b, a+sep) {
				continue
			}
			// a is an ancestor of b; connect only the nearest ancestor so a
			// grandparent does not also draw an edge. If another node sits
			// between a and b, skip and let that nearer one connect.
			parent := filepath.Dir(b)
			intervening := false
			for parent != "" && parent != a && strings.HasPrefix(parent, a+sep) {
				if nodeDirs[parent] {
					intervening = true
					break
				}
				parent = filepath.Dir(parent)
			}
			if !intervening {
				*edges = append(*edges, Edge{Kind: "contains", From: a, To: b})
			}
		}
	}
}
