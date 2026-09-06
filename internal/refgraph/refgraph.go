// Package refgraph compiles the file-level reference graph.
//
// Every markdown file inside a skill becomes a node with in/out counts, and
// every reference it writes becomes a file edge -- including links between
// two docs of the same skill, which carry no node-level relationship but do
// carry documentation weight (a rename breaks them). Quoted candidates
// (inline-code paths, from refparse) count only when they resolve; an
// unresolvable one stays silent instead of becoming a broken ref.
//
// Layer position: refparse -> refgraph -> edges (node-level projection).
// The broken/external reference facts are produced here, once, per physical
// file.
package refgraph

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/williamfzc/skill-compiler/internal/collect"
	"github.com/williamfzc/skill-compiler/internal/paths"
	"github.com/williamfzc/skill-compiler/internal/refparse"
)

// FileNode is one markdown file inside a skill.
type FileNode struct {
	ID       string `json:"id"` // realpath; equals the map key
	Owner    string `json:"owner"` // owning skill's realpath
	Ext      string `json:"ext"`
	Bytes    int    `json:"bytes"`
	OutCount int    `json:"out_count"`
	InCount  int    `json:"in_count"`
}

// FileEdge is one reference from one file to one resolved target. ToSkill is
// the owning skill of the target, "" when it resolves outside any skill.
type FileEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	FromSkill string `json:"from_skill"`
	ToSkill   string `json:"to_skill"`
	Raw       string `json:"raw"`
	Quoted    bool   `json:"quoted,omitempty"`
}

// BrokenRef is a reference that did not resolve to any existing path. One row
// per physical file.
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

// Result is the file-level pass output: nodes, edges, and the broken /
// external facts. Edges are sorted; the sort order is part of determinism.
type Result struct {
	Files    map[string]*FileNode
	Edges    []FileEdge
	Broken   []BrokenRef
	External []ExternalRef
}

// Build runs the file-level pass over every skill.
func Build(nodes map[string]*collect.Node, roots []collect.Root) Result {
	ownerOf := collect.OwnerIndex(nodes)
	repoRootsByRoot := map[string][]string{}
	for _, r := range roots {
		repoRootsByRoot[r.Path] = paths.FindRepoRoots(r.Path)
	}

	res := Result{Files: map[string]*FileNode{}}
	nodeIDs := make([]string, 0, len(nodes))
	for nid := range nodes {
		nodeIDs = append(nodeIDs, nid)
	}
	sort.Strings(nodeIDs)
	scanned := map[string]bool{}
	for _, nid := range nodeIDs {
		node := nodes[nid]
		if node.BrokenSymlink {
			continue
		}
		access := accessibleDir(node)
		repoRoots := repoRootsByRoot[node.Provenance[0].Root]

		for _, fp := range mdFiles(access) {
			rp := paths.RealPath(fp)
			if scanned[rp] {
				continue
			}
			scanned[rp] = true
			src := ownerOf(fp) // deepest skill owning this md file
			if src == "" {
				continue
			}
			content := paths.ReadBytes(fp)
			fromFile := fromFileRel(src, rp, fp)
			res.Files[rp] = &FileNode{
				ID:    rp,
				Owner: src,
				Ext:   filepath.Ext(fp),
				Bytes: len(content),
			}
			for _, t := range refparse.Extract(content) {
				edge, br, ext := classify(t, src, fromFile, rp, fp, repoRoots, ownerOf)
				if br != nil {
					res.Broken = append(res.Broken, *br)
					continue
				}
				if ext != nil {
					res.External = append(res.External, *ext)
				}
				if edge != nil {
					res.Edges = append(res.Edges, *edge)
				}
			}
		}
	}

	sort.Slice(res.Edges, func(i, j int) bool {
		a, b := res.Edges[i], res.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Raw < b.Raw
	})
	// Counts: out per source file; in per target file that is itself a node.
	for _, e := range res.Edges {
		if fn := res.Files[e.From]; fn != nil {
			fn.OutCount++
		}
		if fn := res.Files[e.To]; fn != nil {
			fn.InCount++
		}
	}
	return res
}

// classify resolves one candidate into a file edge and/or a broken/external
// fact. A quoted candidate that does not resolve stays silent; a prose one
// becomes a broken ref.
func classify(t refparse.Target, src, fromFile, realFrom, fp string,
	repoRoots []string, ownerOf func(string) string) (*FileEdge, *BrokenRef, *ExternalRef) {
	pp, resolvedPath := refparse.Resolve(t.Raw, filepath.Dir(fp), src, repoRoots)
	if pp == "" || strings.HasSuffix(pp, "/") {
		return nil, nil, nil
	}
	if resolvedPath == "" {
		if t.Quoted {
			return nil, nil, nil // an example filename, not a fault
		}
		return nil, &BrokenRef{
			From:     src,
			FromFile: fromFile,
			Raw:      t.Raw,
			Resolved: filepath.Clean(filepath.Join(filepath.Dir(fp), pp)),
			RealFrom: realFrom,
		}, nil
	}
	tgtOwner := ownerOf(resolvedPath)
	edge := &FileEdge{
		From:      realFrom,
		To:        paths.RealPath(resolvedPath),
		FromSkill: src,
		ToSkill:   tgtOwner,
		Raw:       t.Raw,
		Quoted:    t.Quoted,
	}
	if tgtOwner == "" {
		if t.Quoted {
			return edge, nil, nil // real reference to a doc outside any skill
		}
		return edge, nil, &ExternalRef{
			From:     src,
			FromFile: fromFile,
			Raw:      t.Raw,
			Resolved: paths.RealPath(resolvedPath),
		}
	}
	return edge, nil, nil
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

// mdFiles lists the markdown files under dir, sorted.
func mdFiles(dir string) []string {
	var out []string
	paths.WalkFollow(dir, func(d string, names []string) bool {
		for _, n := range names {
			if strings.HasSuffix(n, ".md") {
				full := filepath.Join(d, n)
				if info, err := os.Lstat(full); err == nil && !info.IsDir() {
					out = append(out, full)
				}
			}
		}
		return false
	})
	sort.Strings(out)
	return out
}

// fromFileRel renders the writing file relative to its skill root (a bare
// basename when it sits directly in the root).
func fromFileRel(src, rp, fp string) string {
	if filepath.Dir(rp) == src {
		return filepath.Base(fp)
	}
	rel, _ := filepath.Rel(src, rp)
	return rel
}
