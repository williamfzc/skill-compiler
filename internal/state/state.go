// Package state compiles the pieces into one deterministic state graph.
//
// This is the application layer: it orchestrates discovery -> collection ->
// edges -> analysis -> diagnostics into the single artifact every consumer
// reads. It owns no scanning logic of its own, only the assembly and the
// summary.
package state

import (
	"time"

	"github.com/williamfzc/skill-compiler/internal/analysis"
	"github.com/williamfzc/skill-compiler/internal/collect"
	"github.com/williamfzc/skill-compiler/internal/diagnostics"
	"github.com/williamfzc/skill-compiler/internal/edges"
	"github.com/williamfzc/skill-compiler/internal/refgraph"
	"github.com/williamfzc/skill-compiler/internal/roots"
)

// Schema is the state format version. diff refuses two files whose Schema
// differs.
const Schema = 4

// RootMeta is one scanned root in the emitted state.
type RootMeta struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	SkillCount int    `json:"skill_count"`
}

// Summary holds scalar counts for a one-line health read.
type Summary struct {
	RootCount              int            `json:"root_count"`
	SkillCount             int            `json:"skill_count"`
	ResidentDescChars      int            `json:"resident_desc_chars"`
	EdgeCount              int            `json:"edge_count"`
	EdgesByKind            map[string]int `json:"edges_by_kind"`
	FileCount              int            `json:"file_count"`
	FileEdgeCount          int            `json:"file_edge_count"`
	BrokenRefCount         int            `json:"broken_ref_count"`
	ExternalRefCount       int            `json:"external_ref_count"`
	BrokenSymlinkCount     int            `json:"broken_symlink_count"`
	MultiMountedGroups     int            `json:"multi_mounted_groups"`
	DuplicateContentGroups int            `json:"duplicate_content_groups"`
	NameCollisionCount     int            `json:"name_collision_count"`
	ErrorCount             int            `json:"error_count"`
	WarnCount              int            `json:"warn_count"`
}

// Graph is the compiled state: the single artifact every consumer reads.
type Graph struct {
	Schema         int                      `json:"schema"`
	GeneratedAt    string                   `json:"generated_at"`
	ScanSeconds    float64                  `json:"scan_seconds"`
	Roots          []RootMeta               `json:"roots"`
	Skills         map[string]*collect.Node `json:"skills"`
	Files          map[string]*refgraph.FileNode `json:"files"`
	FileEdges      []refgraph.FileEdge      `json:"file_edges"`
	Edges          []edges.Edge             `json:"edges"`
	BrokenRefs     []refgraph.BrokenRef     `json:"broken_refs"`
	ExternalRefs   []refgraph.ExternalRef   `json:"external_refs"`
	Identities     analysis.Identities      `json:"identities"`
	NameCollisions []analysis.Collision     `json:"name_collisions"`
	Diagnostics    []diagnostics.Diagnostic `json:"diagnostics"`
	Summary        Summary                  `json:"summary"`
}

// Compile discovers roots and assembles the full state graph.
func Compile(extraRoots []string, onlyRoots []string) *Graph {
	t0 := time.Now()
	discovered := roots.Discover(extraRoots, onlyRoots)
	nodes := collect.CollectSkills(discovered)
	fg := refgraph.Build(nodes, discovered)
	edgelist := edges.Build(nodes, fg)
	analysis.ComputeRefsIn(nodes, edgelist)
	identities := analysis.GroupIdentities(nodes)
	collisions := analysis.DetectNameCollisions(nodes)
	diags := diagnostics.Diagnose(nodes, fg.Broken, collisions)

	rootsMeta := make([]RootMeta, 0, len(discovered))
	for _, r := range discovered {
		cnt := 0
		for _, n := range nodes {
			for _, p := range n.Provenance {
				if p.Root == r.Path {
					cnt++
					break
				}
			}
		}
		rootsMeta = append(rootsMeta, RootMeta{Path: r.Path, Kind: r.Kind, SkillCount: cnt})
	}

	elapsed := time.Since(t0).Seconds()
	if edgelist == nil {
		edgelist = []edges.Edge{}
	}
	if fg.Edges == nil {
		fg.Edges = []refgraph.FileEdge{}
	}
	if fg.Files == nil {
		fg.Files = map[string]*refgraph.FileNode{}
	}
	if fg.Broken == nil {
		fg.Broken = []refgraph.BrokenRef{}
	}
	if fg.External == nil {
		fg.External = []refgraph.ExternalRef{}
	}
	if collisions == nil {
		collisions = []analysis.Collision{}
	}
	if diags == nil {
		diags = []diagnostics.Diagnostic{}
	}
	g := &Graph{
		Schema:         Schema,
		GeneratedAt:    t0.Format("2006-01-02T15:04:05"),
		ScanSeconds:    float64(int(elapsed*100+0.5)) / 100,
		Roots:          rootsMeta,
		Skills:         nodes,
		Files:          fg.Files,
		FileEdges:      fg.Edges,
		Edges:          edgelist,
		BrokenRefs:     fg.Broken,
		ExternalRefs:   fg.External,
		Identities:     identities,
		NameCollisions: collisions,
		Diagnostics:    diags,
	}
	g.Summary = Summarize(rootsMeta, nodes, edgelist, fg, identities, collisions, diags)
	return g
}

// DiscoverRoots exposes root discovery for the `roots` command.
func DiscoverRoots(extra, only []string) []collect.Root {
	return roots.Discover(extra, only)
}

// Summarize computes the scalar health counts.
func Summarize(rmeta []RootMeta, nodes map[string]*collect.Node, edgelist []edges.Edge,
	fg refgraph.Result,
	identities analysis.Identities, collisions []analysis.Collision,
	diags []diagnostics.Diagnostic) Summary {
	ek := map[string]int{}
	for _, e := range edgelist {
		ek[e.Kind]++
	}
	errs, warns := 0, 0
	descChars, brokenSymlinks := 0, 0
	for _, n := range nodes {
		descChars += n.DescChars
		if n.BrokenSymlink {
			brokenSymlinks++
		}
	}
	for _, d := range diags {
		switch d.Severity {
		case "error":
			errs++
		case "warn":
			warns++
		}
	}
	return Summary{
		RootCount:              len(rmeta),
		SkillCount:             len(nodes),
		ResidentDescChars:      descChars,
		EdgeCount:              len(edgelist),
		EdgesByKind:            ek,
		FileCount:              len(fg.Files),
		FileEdgeCount:          len(fg.Edges),
		BrokenRefCount:         len(fg.Broken),
		ExternalRefCount:       len(fg.External),
		BrokenSymlinkCount:     brokenSymlinks,
		MultiMountedGroups:     len(identities.MultiMounted),
		DuplicateContentGroups: len(identities.SameContent),
		NameCollisionCount:     len(collisions),
		ErrorCount:             errs,
		WarnCount:              warns,
	}
}
