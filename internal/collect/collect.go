// Package collect turns discovered roots into deduplicated skill nodes.
//
// One SKILL.md becomes one node, keyed by realpath so that the same skill
// reached through several roots or symlinks is a single node with several
// provenance entries. This package owns the node shape; edges and diagnostics
// read it but do not build it.
package collect

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"skillscope/internal/frontmatter"
	"skillscope/internal/paths"
)

// Provenance records one arrival path: the load root a skill was reached
// through, that root's kind, and the skill's path relative to it.
type Provenance struct {
	Root     string `json:"root"`
	RootKind string `json:"root_kind"`
	Rel      string `json:"rel"`
}

// Node is one skill (one SKILL.md), keyed by the realpath of its directory.
type Node struct {
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	NameField              string       `json:"name_field"`
	Description            string       `json:"description"`
	DescChars              int          `json:"desc_chars"`
	HasFrontmatter         bool         `json:"has_frontmatter"`
	FrontmatterKeys        []string     `json:"frontmatter_keys"`
	DisableModelInvocation bool         `json:"disable_model_invocation"`
	SkillMDBytes           int          `json:"skill_md_bytes"`
	ContentHash            string       `json:"content_hash"`
	IsSymlink              bool         `json:"is_symlink"`
	SymlinkTarget          *string      `json:"symlink_target"`
	SymlinkStyle           *string      `json:"symlink_style"`
	SymlinkOK              *bool        `json:"symlink_ok"`
	BrokenSymlink          bool         `json:"broken_symlink,omitempty"`
	Provenance             []Provenance `json:"provenance"`

	RefsInCount int      `json:"refs_in_count"`
	RefsOut     []string `json:"refs_out"`
	Contains    []string `json:"contains"`
	ContainedBy []string `json:"contained_by"`
}

// Root is one load root: a directory and the kind of root it is.
type Root struct {
	Path string
	Kind string
}

// CollectSkills recursively finds every SKILL.md under each root, deduping
// into nodes by realpath. When one entity is reachable via several
// roots/symlinks, provenance records all arrival paths. The returned map is
// keyed by the realpath of the skill directory.
func CollectSkills(roots []Root) map[string]*Node {
	nodes := map[string]*Node{}
	for _, r := range roots {
		walkRoot(nodes, r.Path, r.Kind)
		recordBrokenSymlinks(nodes, r.Path, r.Kind)
	}
	return nodes
}

// walkRoot descends the root, following symlinked directories. followlinks is
// needed because the same batch of skills is often symlinked into several
// roots (.claude/x -> .agents/x); without following, those mount points are
// missed and the multi-mount fact vanishes. A realpath set guards cycles: each
// real directory is descended once, but every arrival path is recorded.
func walkRoot(nodes map[string]*Node, root, kind string) {
	paths.WalkFollow(root, func(dirpath string, names []string) bool {
		hasSkill := false
		for _, n := range names {
			if n == "SKILL.md" {
				hasSkill = true
				break
			}
		}
		if !hasSkill {
			return false
		}
		rel, err := filepath.Rel(root, dirpath)
		if err != nil {
			rel = dirpath
		}
		prov := Provenance{Root: root, RootKind: kind, Rel: rel}
		nid := paths.RealPath(dirpath)
		if n, ok := nodes[nid]; ok {
			found := false
			for _, p := range n.Provenance {
				if p == prov {
					found = true
					break
				}
			}
			if !found {
				n.Provenance = append(n.Provenance, prov)
			}
			return false
		}
		nodes[nid] = makeNode(dirpath, nid, prov)
		return false
	})
}

func makeNode(dirpath, nid string, prov Provenance) *Node {
	text := paths.ReadText(filepath.Join(dirpath, "SKILL.md"))
	raw := []byte(text)
	fm, hasFM := frontmatter.Parse(text)

	keys := make([]string, 0, len(fm))
	for k := range fm {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	n := &Node{
		ID:                     nid,
		Name:                   filepath.Base(dirpath),
		NameField:              fm["name"],
		Description:            fm["description"],
		DescChars:              len([]rune(fm["description"])),
		HasFrontmatter:         hasFM,
		FrontmatterKeys:        keys,
		DisableModelInvocation: isTruthy(fm["disable-model-invocation"]),
		SkillMDBytes:           len(raw),
		ContentHash:            contentHash(raw),
		Provenance:             []Provenance{prov},
		RefsOut:                []string{},
		Contains:               []string{},
		ContainedBy:            []string{},
	}

	if li, err := os.Lstat(dirpath); err == nil && li.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(dirpath)
		n.IsSymlink = true
		n.SymlinkTarget = &target
		style := "rel"
		if filepath.IsAbs(target) {
			style = "abs"
		}
		n.SymlinkStyle = &style
		ok := paths.Exists(dirpath)
		n.SymlinkOK = &ok
	}
	return n
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "1":
		return true
	}
	return false
}

func contentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

// recordBrokenSymlinks scans one level of the root for dangling symlinks.
// os.walk cannot enter them and the SKILL.md is unreadable, so they would be
// missed entirely; record the fact "this symlink is broken" as a node.
func recordBrokenSymlinks(nodes map[string]*Node, root, kind string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink == 0 || paths.Exists(p) {
			continue
		}
		target, _ := os.Readlink(p)
		nid := paths.RealPath(p)
		if _, ok := nodes[nid]; ok {
			continue
		}
		style := "rel"
		if filepath.IsAbs(target) {
			style = "abs"
		}
		ok := false
		nodes[nid] = &Node{
			ID:              nid,
			Name:            e.Name(),
			NameField:       "",
			FrontmatterKeys: []string{},
			IsSymlink:       true,
			SymlinkTarget:   &target,
			SymlinkStyle:    &style,
			SymlinkOK:       &ok,
			BrokenSymlink:   true,
			Provenance:      []Provenance{{Root: root, RootKind: kind, Rel: e.Name()}},
			RefsOut:         []string{},
			Contains:        []string{},
			ContainedBy:     []string{},
		}
	}
}

// OwnerIndex maps any file to "the deepest skill that contains it". Nested
// skills share a subtree, so a file goes to the deepest owner, never counted
// against the parent. The returned function maps an absolute path to a node id
// or "".
func OwnerIndex(nodes map[string]*Node) func(abspath string) string {
	dirs := make([]string, 0, len(nodes))
	for d := range nodes {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	return func(abspath string) string {
		rp := paths.RealPath(abspath)
		for _, d := range dirs {
			if rp == d || strings.HasPrefix(rp, d+string(os.PathSeparator)) {
				return d
			}
		}
		return ""
	}
}
