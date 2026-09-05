// Package edges derives skill-node relationships: reference edges between
// skills and containment edges from directory nesting.
//
// This is the node-level projection. The per-file extraction pass lives in
// refgraph; here each ref is attributed to the deepest skill owning the file
// that wrote it (collect.OwnerIndex), and a link to a file inside the
// authoring skill's own dir produces no edge (it is a self reference, not a
// relationship between skills).
//
// Symlinks form no edge: a symlinked directory shares its target's realpath and
// is already one node, so the fact lives in provenance / multi_mounted, not
// here.
package edges

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"skillscope/internal/collect"
	"skillscope/internal/paths"
	"skillscope/internal/refparse"
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

// Build produces ref + contains edges, plus broken and external refs.
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

// refEdges walks every markdown file under every skill and attributes its
// references to the owning skill.
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
			fromFile := filepath.Base(fp)
			if filepath.Dir(rp) != src {
				fromFile, _ = filepath.Rel(src, rp)
			}
			for _, t := range refparse.Extract(paths.ReadBytes(fp)) {
				if t.Quoted {
					continue // counted only at the file level (refgraph)
				}
				edge, br, ext, ok := refEdge(t.Raw, src, fromFile, rp, fp, repoRoots, ownerOf)
				if br != nil {
					*broken = append(*broken, *br)
				}
				if ext != nil {
					*external = append(*external, *ext)
				}
				if ok {
					*edges = append(*edges, edge)
				}
			}
		}
	}
}

// refEdge resolves one raw target and classifies it: broken (unresolvable),
// external (resolves outside any skill), a node edge, or nothing (a resolved
// reference inside the authoring skill's own dir is a self reference, not a
// relationship between skills).
func refEdge(raw, src, fromFile, realFrom, fromFileAbs string,
	repoRoots []string, ownerOf func(string) string) (Edge, *BrokenRef, *ExternalRef, bool) {
	pp, resolvedPath := refparse.Resolve(raw, filepath.Dir(fromFileAbs), src, repoRoots)
	if pp == "" || strings.HasSuffix(pp, "/") {
		return Edge{}, nil, nil, false
	}
	if resolvedPath == "" {
		return Edge{}, &BrokenRef{
			From:     src,
			FromFile: fromFile,
			Raw:      raw,
			Resolved: filepath.Clean(filepath.Join(filepath.Dir(fromFileAbs), pp)),
			RealFrom: realFrom,
		}, nil, false
	}
	tgtOwner := ownerOf(resolvedPath)
	if tgtOwner == "" {
		return Edge{}, nil, &ExternalRef{
			From:     src,
			FromFile: fromFile,
			Raw:      raw,
			Resolved: paths.RealPath(resolvedPath),
		}, false
	}
	if tgtOwner == src {
		return Edge{}, nil, nil, false
	}
	isSkillMD := filepath.Base(resolvedPath) == "SKILL.md"
	return Edge{
		Kind:            "ref",
		From:            src,
		To:              tgtOwner,
		FromFile:        fromFile,
		Raw:             raw,
		TargetIsSkillMD: &isSkillMD,
	}, nil, nil, true
}

// containsEdges connects each node to its nearest ancestor node.
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
