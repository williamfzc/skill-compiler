// catalogue_test.go -- characterization tests over real, well-known skills
// from the skills.sh registry, vendored under testdata/skills-sh.
//
// The synthetic-tree tests prove each mechanism in isolation; these prove the
// engine behaves correctly on the shape real skills actually take: flat skill
// collections, cross-skill markdown links, blast-radius counts, and genuine
// cross-references that resolve nowhere because their target lives in another
// package. Facts are pinned as observed at the vendored revisions (see
// testdata/skills-sh/SOURCES.md); when fixtures are refreshed on purpose, the
// pinned counts get updated in the same change.
package skillscope_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/williamfzc/skill-compiler/internal/state"
)

func corpusRoot(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "skills-sh", rel))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("vendored corpus missing: %v", err)
	}
	return abs
}

func compileRoots(t *testing.T, roots ...string) *state.Graph {
	t.Helper()
	return state.Compile(nil, roots)
}

func nodeNames(g *state.Graph) map[string]string {
	names := map[string]string{}
	for id, n := range g.Skills {
		names[filepath.Base(id)] = n.NameField
		if names[filepath.Base(id)] == "" {
			names[filepath.Base(id)] = n.Name
		}
	}
	return names
}

// The obra/superpowers collection: a real, widely installed flat skill suite
// from skills.sh (1.3M+ installs for its family).
func TestCatalogueSuperpowersSuite(t *testing.T) {
	root := corpusRoot(t, "obra-superpowers/skills")
	g := compileRoots(t, root)
	s := g.Summary

	if s.SkillCount != 14 {
		t.Fatalf("superpowers suite: 14 skills, got %d", s.SkillCount)
	}
	if s.BrokenSymlinkCount != 0 || s.NameCollisionCount != 0 ||
		s.MultiMountedGroups != 0 || s.DuplicateContentGroups != 0 {
		t.Fatalf("real suite should have no symlinks/collisions/copies: %+v", s)
	}
	if s.EdgesByKind["contains"] != 0 {
		t.Fatalf("superpowers is a flat layout, got %d contains edges", s.EdgesByKind["contains"])
	}

	// Every shipped skill is well-formed enough to load: frontmatter plus a
	// description, no name mismatches against the directory.
	names := nodeNames(g)
	for _, want := range []string{
		"test-driven-development", "systematic-debugging", "brainstorming",
		"writing-plans", "subagent-driven-development", "using-superpowers",
		"writing-skills", "requesting-code-review",
	} {
		if names[want] != want {
			t.Fatalf("missing or misnamed well-known skill %q (got %q)", want, names[want])
		}
	}
	for id, n := range g.Skills {
		if !n.HasFrontmatter || n.Description == "" {
			t.Fatalf("shipped skill %s lacks frontmatter/description", filepath.Base(id))
		}
		if n.NameField != "" && n.NameField != n.Name {
			t.Fatalf("shipped skill %s has name field %q", n.Name, n.NameField)
		}
	}
	// References are read only from real prose; the unshipped document
	// bundles appear in code samples, not prose links, so the suite carries
	// no broken-reference diagnostics.
	if s.ErrorCount != 0 || s.WarnCount != 0 || s.BrokenRefCount != 0 {
		t.Fatalf("real prose carries no unresolvable links: %+v", s)
	}
}

// Cross-skill references inside the suite become real ref edges, and blast
// radius (refs_in_count) is derived from them.
func TestCatalogueCrossSkillEdgesAndBlastRadius(t *testing.T) {
	root := corpusRoot(t, "obra-superpowers/skills")
	g := compileRoots(t, root)

	refs := map[string]map[string]bool{} // from dir -> set(to dir)
	for _, e := range g.Edges {
		if e.Kind != "ref" {
			continue
		}
		from, to := filepath.Base(e.From), filepath.Base(e.To)
		if refs[from] == nil {
			refs[from] = map[string]bool{}
		}
		refs[from][to] = true
	}
	if !refs["subagent-driven-development"]["requesting-code-review"] {
		t.Fatal("subagent-driven-development should reference requesting-code-review via code-reviewer.md")
	}
	if !refs["writing-skills"]["using-superpowers"] {
		t.Fatal("writing-skills should reference using-superpowers' runtime-tools references")
	}
	if len(refs) != 2 {
		t.Fatalf("two skills carry outbound ref edges, got %v", refs)
	}

	byName := map[string]string{}
	for id := range g.Skills {
		byName[filepath.Base(id)] = id
	}
	if g.Skills[byName["using-superpowers"]].RefsInCount != 2 {
		t.Fatal("using-superpowers is referenced twice (codex-tools + gemini-tools links)")
	}
	if g.Skills[byName["requesting-code-review"]].RefsInCount != 1 {
		t.Fatal("requesting-code-review is referenced once")
	}
}

// Links to the unshipped anthropic document bundles all appear inside code
// samples (fenced SKILL.md examples and command snippets), not in prose.
// Code content is an example, not a reference, so writing-skills carries no
// broken-ref diagnostics -- the whole superpowers suite is reference-clean.
func TestCatalogueCodeSampleLinksAreNotRefs(t *testing.T) {
	root := corpusRoot(t, "obra-superpowers/skills")
	g := compileRoots(t, root)

	if g.Summary.BrokenRefCount != 0 {
		var raws []string
		for _, b := range g.BrokenRefs {
			raws = append(raws, b.From+":"+b.Raw)
		}
		t.Fatalf("code-sample links must not be reported as broken refs, got %d: %v",
			len(raws), raws)
	}
	for _, d := range g.Diagnostics {
		if d.Code == "BROKEN_REF" {
			t.Fatalf("no BROKEN_REF expected in the suite, got %s %s", d.Code, d.Where)
		}
	}

	// Genuine prose links still resolve to real edges (proven by the blast
	// radius test above); the code stripping does not hide them.
}

// The vercel-labs/find-skills skill (skills.sh #1 all-time, ~3.2M installs):
// a single self-contained skill that compiles completely clean.
func TestCatalogueFindSkillsClean(t *testing.T) {
	root := corpusRoot(t, "vercel-labs/find-skills")
	g := compileRoots(t, root)
	s := g.Summary
	if s.SkillCount != 1 || s.BrokenRefCount != 0 || s.ErrorCount != 0 || s.WarnCount != 0 {
		t.Fatalf("find-skills should be a single clean skill: %+v", s)
	}
	var name string
	for _, n := range g.Skills {
		name = n.NameField
		if n.Description == "" || len(n.FrontmatterKeys) != 2 {
			t.Fatal("find-skills should ship name + description frontmatter")
		}
	}
	if name != "find-skills" {
		t.Fatalf("find-skills name field = %q", name)
	}
}

// Several independent suites scanned together stay independent roots.
func TestCatalogueMultipleRoots(t *testing.T) {
	g := compileRoots(t,
		corpusRoot(t, "obra-superpowers/skills"),
		corpusRoot(t, "vercel-labs/find-skills"),
	)
	if g.Summary.RootCount != 2 || g.Summary.SkillCount != 15 {
		t.Fatalf("two roots / 15 skills, got %d / %d", g.Summary.RootCount, g.Summary.SkillCount)
	}
	kinds := map[string]int{}
	for _, r := range g.Roots {
		kinds[r.Kind]++
	}
	if kinds["agent"] != 2 {
		t.Fatalf("both --only-root dirs are agent-kind, got %+v", kinds)
	}
}

// Real-world corpus must compile deterministically, like any other state.
func TestCatalogueDeterministic(t *testing.T) {
	root := corpusRoot(t, "obra-superpowers/skills")
	a := compileRoots(t, root)
	b := compileRoots(t, root)

	shape := func(g *state.Graph) string {
		ids := make([]string, 0, len(g.Skills))
		for id := range g.Skills {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		parts := append([]string{}, ids...)
		for _, d := range g.Diagnostics {
			parts = append(parts, d.Code+"|"+d.Detail)
		}
		for _, br := range g.BrokenRefs {
			parts = append(parts, "broken|"+br.Raw+"|"+filepath.Base(br.From))
		}
		sort.Strings(parts)
		return strings.Join(parts, "|")
	}
	if shape(a) != shape(b) {
		t.Fatal("two compiles of the vendored corpus differ")
	}
}
