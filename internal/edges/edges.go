// Package edges derives skill-node relationships from the file-level
// reference graph and directory nesting.
//
// This is the node-level projection. The per-file extraction pass lives in
// refgraph; here each file edge is attributed to the deepest skill owning the
// file that wrote it (collect.OwnerIndex). A reference whose target sits in
// the authoring skill's own dir is a self reference, not a relationship
// between skills, so it produces no node edge.
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

	"github.com/williamfzc/skill-compiler/internal/collect"
	"github.com/williamfzc/skill-compiler/internal/refgraph"
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

// Build projects the file-level reference graph onto skill nodes and adds
// contains edges from directory nesting.
func Build(nodes map[string]*collect.Node, fg refgraph.Result) []Edge {
	var edges []Edge
	refEdges(fg, &edges)
	nodeDirs := map[string]bool{}
	for d := range nodes {
		nodeDirs[d] = true
	}
	containsEdges(nodeDirs, &edges)
	return edges
}

// refEdges folds file edges into node edges, deduped per writing file. The
// input is sorted, so the output order is deterministic.
func refEdges(fg refgraph.Result, edges *[]Edge) {
	seen := map[string]bool{}
	for _, fe := range fg.Edges {
		if fe.ToSkill == "" || fe.ToSkill == fe.FromSkill {
			continue // outside any skill, or a self reference
		}
		fromFile := fromFileRel(fg, fe)
		key := fe.FromSkill + "\x00" + fromFile + "\x00" + fe.Raw
		if seen[key] {
			continue
		}
		seen[key] = true
		isSkillMD := filepath.Base(fe.To) == "SKILL.md"
		*edges = append(*edges, Edge{
			Kind:            "ref",
			From:            fe.FromSkill,
			To:              fe.ToSkill,
			FromFile:        fromFile,
			Raw:             fe.Raw,
			TargetIsSkillMD: &isSkillMD,
		})
	}
}

// fromFileRel renders the writing file relative to its owning skill root.
func fromFileRel(fg refgraph.Result, fe refgraph.FileEdge) string {
	fn := fg.Files[fe.From]
	if fn == nil {
		return filepath.Base(fe.From)
	}
	if fn.Owner == filepath.Dir(fe.From) {
		return filepath.Base(fe.From)
	}
	rel, _ := filepath.Rel(fn.Owner, fe.From)
	return rel
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
