// skillc_test.go -- unit tests for the skillc compiler.
//
// Never touches the real skill dirs on this machine. Builds synthetic skill
// trees in temp dirs, points the compiler at them with --only-root, and asserts
// on the compiled state and diagnostics.
package skillscope_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"skillscope/internal/cli"
	"skillscope/internal/collect"
	"skillscope/internal/diagnostics"
	"skillscope/internal/paths"
	"skillscope/internal/state"
)

// ---------------------------------------------------------------- helpers

func mkskill(t *testing.T, root, rel, name string, hasName bool, desc *string,
	body, extraFM string) string {
	t.Helper()
	d := filepath.Join(root, rel)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	if hasName {
		b.WriteString("name: " + name + "\n")
	}
	if desc != nil {
		b.WriteString("description: " + *desc + "\n")
	}
	b.WriteString(extraFM)
	b.WriteString("---\n")
	b.WriteString(body)
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func simpleSkill(t *testing.T, root, rel, name string) {
	t.Helper()
	desc := "does a thing"
	mkskill(t, root, rel, name, true, &desc, "\n# body\n", "")
}

func mkfile(t *testing.T, root, rel, content string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func compileRoot(t *testing.T, root string) *state.Graph {
	t.Helper()
	return state.Compile(nil, []string{root})
}

// runCLI invokes the CLI in-process, capturing stdout and stderr; returns exit
// code and the combined output (errors are written to stderr).
func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = w, w
	code := cli.Main(args)
	w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(b)
}

func checkRootJSON(t *testing.T, root string) (int, map[string]any) {
	t.Helper()
	code, out := runCLI(t, "check", "--only-root", root, "--json")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("check --json produced invalid JSON: %v\n%s", err, out)
	}
	return code, parsed
}

func nodeByName(g *state.Graph, name string) *collect.Node {
	for _, n := range g.Skills {
		if n.Name == name {
			return n
		}
	}
	return nil
}

func diagsOf(cj map[string]any, code string) []any {
	var out []any
	for _, d := range cj["diagnostics"].([]any) {
		m := d.(map[string]any)
		if m["code"] == code {
			out = append(out, m)
		}
	}
	return out
}

func diagsOfGraph(g *state.Graph, code string) []diagnostics.Diagnostic {
	var out []diagnostics.Diagnostic
	for _, d := range g.Diagnostics {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

// ---------------------------------------------------------------- tests

func TestRecursiveDiscovery(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "suite", "suite")
	simpleSkill(t, root, "suite/roles/worker", "worker")
	simpleSkill(t, root, "suite/roles/scorer", "scorer")
	g := compileRoot(t, root)

	names := map[string]bool{}
	for _, n := range g.Skills {
		names[n.Name] = true
	}
	if len(names) != 3 || !names["suite"] || !names["worker"] || !names["scorer"] {
		t.Fatalf("recursive discovery: got %v", names)
	}
	contains := 0
	for _, e := range g.Edges {
		if e.Kind == "contains" {
			contains++
		}
	}
	if contains != 2 {
		t.Fatalf("contains edges: %d", contains)
	}
	worker := nodeByName(g, "worker")
	if worker == nil || len(worker.ContainedBy) == 0 {
		t.Fatal("worker should be contained_by suite")
	}
}

func TestMissingDescription(t *testing.T) {
	root := t.TempDir()
	ok := "fine"
	mkskill(t, root, "ok", "ok", true, &ok, "\n# body\n", "")
	mkskill(t, root, "nodesc", "nodesc", true, nil, "\n# body\n", "")
	code, cj := checkRootJSON(t, root)
	if code != 1 {
		t.Fatal("missing description -> exit 1")
	}
	if len(diagsOf(cj, "NO_DESCRIPTION")) != 1 {
		t.Fatal("want one NO_DESCRIPTION")
	}
}

func TestNoFrontmatter(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "raw")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("# no frontmatter here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, cj := checkRootJSON(t, root)
	if code != 1 {
		t.Fatal("no frontmatter -> exit 1")
	}
	if len(diagsOf(cj, "NO_FRONTMATTER")) != 1 {
		t.Fatal("want one NO_FRONTMATTER")
	}
}

func TestBrokenRef(t *testing.T) {
	root := t.TempDir()
	mkskill(t, root, "a", "a", true, strp("does a"),
		"\nsee [ref](references/missing.md)\n", "")
	code, cj := checkRootJSON(t, root)
	if code != 1 {
		t.Fatal("broken ref -> exit 1")
	}
	if br := diagsOf(cj, "BROKEN_REF"); len(br) != 1 {
		t.Fatalf("want one BROKEN_REF, got %d", len(br))
	}
}

func TestRefResolvesAndEdges(t *testing.T) {
	root := t.TempDir()
	mkskill(t, root, "a", "a", true, strp("does a"), "\nuse [b](../b/SKILL.md)\n", "")
	simpleSkill(t, root, "b", "b")
	g := compileRoot(t, root)
	refs := 0
	for _, e := range g.Edges {
		if e.Kind == "ref" {
			refs++
		}
	}
	if refs != 1 {
		t.Fatalf("want one ref edge, got %d", refs)
	}
	if g.Summary.BrokenRefCount != 0 {
		t.Fatal("want no broken refs")
	}
	b := nodeByName(g, "b")
	if b == nil || b.RefsInCount != 1 {
		t.Fatal("b should be referenced once")
	}
}

func TestSkillRootRelativeRef(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "a", "a")
	mkfile(t, root, "a/references/index.md", "see [g](references/deep/guide.md)\n")
	mkfile(t, root, "a/references/deep/guide.md", "# guide\n")
	g := compileRoot(t, root)
	if len(diagsOfGraph(g, "BROKEN_REF")) != 0 {
		t.Fatal("skill-root-relative ref should resolve")
	}
}

// A path-shaped inline-code span is a reference when it resolves (rustdoc
// treats backticks as links too); an unresolvable one -- an example filename
// like TODO.md -- stays silent rather than becoming a broken ref.
func TestBacktickTwoTier(t *testing.T) {
	root := t.TempDir()
	mkskill(t, root, "a", "a", true, strp("does a"),
		"\nsee `../b/SKILL.md` and mention `TODO.md` in backticks\n", "")
	simpleSkill(t, root, "b", "b")
	g := compileRoot(t, root)
	refs := 0
	for _, e := range g.Edges {
		if e.Kind != "ref" {
			continue
		}
		refs++
		if !strings.HasSuffix(e.From, "/a") || !strings.HasSuffix(e.To, "/b") {
			t.Fatalf("quoted ref should run a -> b, got %+v", e)
		}
	}
	if refs != 1 {
		t.Fatalf("resolvable backtick path should be exactly one ref edge, got %d", refs)
	}
	if g.Summary.BrokenRefCount != 0 {
		t.Fatal("unresolvable backtick mention stays silent, not a broken ref")
	}
}

// The file-level graph: a doc inside a skill referencing another doc of the
// same skill is a file edge with in/out counts (previously dropped silently).
// It is a self reference, so no node-level edge appears.
func TestFileGraphIntraSkillDoc(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "a", "a")
	mkfile(t, root, "a/references/index.md", "see [g](references/deep/guide.md)\n")
	mkfile(t, root, "a/references/deep/guide.md", "# guide\n")
	g := compileRoot(t, root)
	idx := paths.RealPath(filepath.Join(root, "a/references/index.md"))
	guide := paths.RealPath(filepath.Join(root, "a/references/deep/guide.md"))
	if len(g.Files) != 3 {
		t.Fatalf("three md files should be graph nodes, got %d", len(g.Files))
	}
	if g.Files[guide].InCount != 1 || g.Files[idx].OutCount != 1 {
		t.Fatalf("file counts wrong: index out=%d guide in=%d",
			g.Files[idx].OutCount, g.Files[guide].InCount)
	}
	if len(g.FileEdges) != 1 {
		t.Fatalf("want one file edge, got %+v", g.FileEdges)
	}
	fe := g.FileEdges[0]
	if fe.From != idx || fe.To != guide || fe.Quoted {
		t.Fatalf("wrong file edge: %+v", fe)
	}
	if fe.FromSkill != fe.ToSkill || !strings.HasSuffix(fe.FromSkill, "/a") {
		t.Fatalf("file edge should stay inside skill a: %+v", fe)
	}
	for _, e := range g.Edges {
		if e.Kind == "ref" {
			t.Fatalf("intra-skill doc link is a self reference, no node edge: %+v", e)
		}
	}
	if g.Summary.BrokenRefCount != 0 {
		t.Fatal("skill-root-relative doc link should resolve")
	}
}

// A quoted path that resolves inside the skill becomes a quoted file edge.
func TestQuotedFileEdge(t *testing.T) {
	root := t.TempDir()
	mkskill(t, root, "a", "a", true, strp("does a"),
		"\nfirst read `notes/plan.md`\n", "")
	mkfile(t, root, "a/notes/plan.md", "# plan\n")
	g := compileRoot(t, root)
	if len(g.FileEdges) != 1 || !g.FileEdges[0].Quoted {
		t.Fatalf("want one quoted file edge, got %+v", g.FileEdges)
	}
	if !strings.HasSuffix(g.FileEdges[0].To, "/notes/plan.md") {
		t.Fatalf("quoted edge should target plan.md: %+v", g.FileEdges[0])
	}
}

// Severity split: a broken link written in SKILL.md is an error; the same
// fault in a reference doc is a warn.
func TestBrokenRefSeverityBySource(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "a", "a")
	mkskill(t, root, "b", "b", true, strp("does b"),
		"\nsee [x](references/gone.md)\n", "")
	mkfile(t, root, "b/references/old.md", "see [y](missing.md)\n")
	g := compileRoot(t, root)
	errs, warns := 0, 0
	for _, d := range g.Diagnostics {
		if d.Code != "BROKEN_REF" {
			continue
		}
		switch d.Severity {
		case "error":
			errs++
		case "warn":
			warns++
		}
	}
	if errs != 1 || warns != 1 {
		t.Fatalf("want 1 error (SKILL.md) + 1 warn (reference doc), got %d/%d", errs, warns)
	}
}

// A warn-only tree (broken link in a reference doc) does not fail the gate.
func TestDocBrokenRefDoesNotFailCheck(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "a", "a")
	mkfile(t, root, "a/references/old.md", "see [y](missing.md)\n")
	code, out := runCLI(t, "check", "--only-root", root, "--json")
	if code != 0 {
		t.Fatalf("warn-only tree should exit 0, got %d: %s", code, out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	summary := parsed["summary"].(map[string]any)
	if summary["error_count"].(float64) != 0 || summary["warn_count"].(float64) != 1 {
		t.Fatalf("want 0 errors + 1 warn, got %v", summary)
	}
}

// A markdown link presented only inside an inline-code span is an example,
// not a real reference: prose like "image syntax like `![x](./a.png)` is not
// supported" must not produce a broken ref, while the same link written in
// real prose and pointing nowhere is still reported.
func TestInlineCodeLinkNotARef(t *testing.T) {
	root := t.TempDir()
	mkskill(t, root, "ex", "ex", true, strp("examples"),
		"# docs\n\nLocal paths like `![img](./a.png)` are not supported.\n", "")
	g := compileRoot(t, root)
	for _, b := range g.BrokenRefs {
		if b.Raw == "./a.png" {
			t.Fatalf("inline-code example link must not be a broken ref: %+v", b)
		}
	}

	// Same link in genuine prose and unresolvable is still caught.
	mkskill(t, root, "real", "real", true, strp("real"),
		"# docs\n\nSee the [diagram](./a.png) before shipping.\n", "")
	g2 := compileRoot(t, root)
	found := false
	for _, b := range g2.BrokenRefs {
		if b.Raw == "./a.png" {
			found = true
		}
	}
	if !found {
		t.Fatal("a genuine prose link to a missing file must still be a broken ref")
	}
}

// A markdown link that appears only inside a fenced code block is sample
// content, not a reference; real prose links in the same file still count.
func TestFencedCodeLinkNotARef(t *testing.T) {
	root := t.TempDir()
	body := "# guide\n\n" +
		"Follow [deploy](../deploy/SKILL.md) to ship.\n\n" +
		"```markdown\n" +
		"See [DOCS.md](DOCS.md) and [missing](../nowhere/SKILL.md) in this sample.\n" +
		"```\n"
	mkskill(t, root, "guide", "guide", true, strp("guides"), body, "")
	mkskill(t, root, "deploy", "deploy", true, strp("deploys"), "\n# deploy\n", "")
	g := compileRoot(t, root)

	got := map[string]bool{}
	for _, b := range g.BrokenRefs {
		got[b.Raw] = true
	}
	if got["DOCS.md"] || got["../nowhere/SKILL.md"] {
		t.Fatalf("fenced sample links must not be broken refs, got %v", got)
	}
	edge := false
	for _, e := range g.Edges {
		if e.Kind == "ref" && filepath.Base(e.To) == "deploy" {
			edge = true
		}
	}
	if !edge {
		t.Fatal("genuine prose link ../deploy/SKILL.md must still produce a ref edge")
	}
	if g.Summary.BrokenRefCount != 0 {
		t.Fatalf("fenced samples aside the guide should be clean, got %v", got)
	}
}

// Code-vs-prose separation follows markdown structure, not any particular
// repo's wording. These pin the structural edges a maintainer could get
// wrong: a deeper-opened fence encloses shallower marks, an unclosed fence
// swallows the rest of the file, and prose after a closed fence counts.
func TestCodeProseStructuralEdges(t *testing.T) {
	root := t.TempDir()

	// 4-backtick fence enclosing ``` lines: the inner ``` is sample text,
	// not a fence close.
	fourBacktick := "# d\n\n[real](../real/SKILL.md)\n\n" +
		"````markdown\n" +
		"```python\n[x](../fenced/SKILL.md)\n```\n" +
		"````\n"
	mkskill(t, root, "four", "four", true, strp("four"), fourBacktick, "")
	mkskill(t, root, "real", "real", true, strp("real target"), "\n# real\n", "")
	g := state.Compile(nil, []string{filepath.Join(root, "four")})
	if g.Summary.BrokenRefCount != 0 {
		t.Fatalf("links inside a 4-backtick fence must not count, got %d broken", g.Summary.BrokenRefCount)
	}

	// Unclosed fence: everything after the opener is code, so a trailing
	// link is excluded.
	unclosed := "# d\n\n```python\n[x](../swallowed/SKILL.md)\n"
	root2 := t.TempDir()
	mkskill(t, root2, "u", "u", true, strp("u"), unclosed, "")
	g2 := state.Compile(nil, []string{root2})
	if g2.Summary.BrokenRefCount != 0 {
		t.Fatal("an unclosed fence swallows the rest of the file; no refs after it")
	}

	// Indented fence close marks the end of code; a link below it is prose.
	indented := "# d\n\n```markdown\n[a](../inside/SKILL.md)\n   ```\n[b](../outside/SKILL.md)\n"
	root3 := t.TempDir()
	mkskill(t, root3, "i", "i", true, strp("i"), indented, "")
	g3 := state.Compile(nil, []string{root3})
	var sawB bool
	for _, b := range g3.BrokenRefs {
		if b.Raw == "../outside/SKILL.md" {
			sawB = true
		}
	}
	var sawA bool
	for _, b := range g3.BrokenRefs {
		if b.Raw == "../inside/SKILL.md" {
			sawA = true
		}
	}
	if sawA || !sawB {
		t.Fatalf("link inside fence excluded, link after indented close included: inside=%v outside=%v", sawA, sawB)
	}
}

func TestValidSymlinkIdentity(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "real", "real")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	g := compileRoot(t, root)
	reals := 0
	for _, n := range g.Skills {
		if filepath.Base(n.ID) == "real" {
			reals++
		}
	}
	if reals != 1 {
		t.Fatalf("symlink should dedup to one node, got %d", reals)
	}
	if g.Summary.MultiMountedGroups != 1 {
		t.Fatal("want one multi-mounted group")
	}
}

func TestDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "keep", "keep")
	if err := os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "broken")); err != nil {
		t.Fatal(err)
	}
	code, cj := checkRootJSON(t, root)
	if code != 1 {
		t.Fatal("dangling symlink -> exit 1")
	}
	if len(diagsOf(cj, "DANGLING_SYMLINK")) != 1 {
		t.Fatal("want one DANGLING_SYMLINK")
	}
	g := compileRoot(t, root)
	if g.Summary.BrokenSymlinkCount != 1 {
		t.Fatalf("broken_symlink_count == %d, want 1", g.Summary.BrokenSymlinkCount)
	}
}

func TestNameCollision(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "x", "dup")
	simpleSkill(t, root, "sub/y", "dup")
	code, cj := checkRootJSON(t, root)
	if code != 1 {
		t.Fatal("name collision -> exit 1")
	}
	if nc := diagsOf(cj, "NAME_COLLISION"); len(nc) != 1 {
		t.Fatalf("want one NAME_COLLISION, got %d", len(nc))
	}
}

func TestNameMismatchIsWarn(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "realdir", "different")
	code, cj := checkRootJSON(t, root)
	if code != 0 {
		t.Fatal("name mismatch is warn, exit 0")
	}
	if len(diagsOf(cj, "NAME_MISMATCH")) != 1 {
		t.Fatal("want one NAME_MISMATCH")
	}
}

func TestDuplicateContent(t *testing.T) {
	root := t.TempDir()
	mkskill(t, root, "one", "twin", true, strp("same"), "\nidentical\n", "")
	mkskill(t, root, "two", "twin", true, strp("same"), "\nidentical\n", "")
	g := compileRoot(t, root)
	if g.Summary.DuplicateContentGroups != 1 {
		t.Fatal("want one duplicate content group")
	}
}

func TestDisableModelInvocation(t *testing.T) {
	root := t.TempDir()
	mkskill(t, root, "manual", "manual", true, strp("does a"),
		"\n# body\n", "disable-model-invocation: true\n")
	g := compileRoot(t, root)
	n := nodeByName(g, "manual")
	if n == nil || !n.DisableModelInvocation {
		t.Fatal("disable-model-invocation should be parsed as true")
	}
}

func TestCleanTreePasses(t *testing.T) {
	root := t.TempDir()
	mkskill(t, root, "a", "a", true, strp("does a"), "\n[b](../b/SKILL.md)\n", "")
	simpleSkill(t, root, "b", "b")
	code, cj := checkRootJSON(t, root)
	if code != 0 {
		t.Fatal("clean tree exits 0")
	}
	summary := cj["summary"].(map[string]any)
	if summary["error_count"].(float64) != 0 {
		t.Fatal("clean tree has no errors")
	}
}

func writeState(t *testing.T, g *state.Graph, path string) {
	t.Helper()
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiffDetectsRegression(t *testing.T) {
	root := t.TempDir()
	b := mkskill(t, root, "a", "a", true, strp("does a"), "\n[b](../b/SKILL.md)\n", "")
	simpleSkill(t, root, "b", "b")
	before := filepath.Join(root, "before.json")
	after := filepath.Join(root, "after.json")
	writeState(t, compileRoot(t, root), before)
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	writeState(t, compileRoot(t, root), after)
	code, out := runCLI(t, "diff", "--before", before, "--after", after)
	if code != 1 {
		t.Fatal("diff regression -> exit 1")
	}
	if !strings.Contains(out, "NEW_BROKEN_REF") && !strings.Contains(out, "SKILL_DISAPPEARED") {
		t.Fatal("diff should report the regression")
	}
}

func TestDiffNoRegression(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "a", "a")
	p := filepath.Join(root, "s.json")
	writeState(t, compileRoot(t, root), p)
	code, _ := runCLI(t, "diff", "--before", p, "--after", p)
	if code != 0 {
		t.Fatal("identical diff -> exit 0")
	}
}

func TestDiffSchemaMismatchRefuses(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "a", "a")
	g := compileRoot(t, root)
	old := *g
	old.Schema = g.Schema - 1 // pretend an older snapshot
	pb := filepath.Join(root, "before.json")
	pa := filepath.Join(root, "after.json")
	writeState(t, &old, pb)
	writeState(t, g, pa)
	code, out := runCLI(t, "diff", "--before", pb, "--after", pa)
	if code != 2 {
		t.Fatalf("schema mismatch -> exit 2, got %d", code)
	}
	if !strings.Contains(out, "schema mismatch") {
		t.Fatal("should explain the schema mismatch")
	}
}

func TestExternalRefNotBroken(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "outside/config.yaml", "k: v\n")
	mkskill(t, root, "skills/a", "a", true, strp("does a"),
		"\nsee [cfg](../../outside/config.yaml)\n", "")
	skillsRoot := filepath.Join(root, "skills")
	g := compileRoot(t, skillsRoot)
	if g.Summary.BrokenRefCount != 0 {
		t.Fatal("external existing ref should not be broken")
	}
	if g.Summary.ExternalRefCount != 1 {
		t.Fatalf("want one external ref, got %d", g.Summary.ExternalRefCount)
	}
}

// `skillc --skill` presents skillc's own SKILL.md-style page: the document an
// agent reads to learn what skillc is and how to drive it. It takes no value;
// looking a skill up is `query --skill NAME`.
func TestSkillFlagIsSkillcOwnPage(t *testing.T) {
	code, out := runCLI(t, "--skill")
	if code != 0 {
		t.Fatal("--skill exits 0")
	}
	for _, want := range []string{
		"name: skillc",         // it reads like a SKILL.md frontmatter
		"description:",         // with a trigger description
		"recipes by situation", // and the usage brief
		"skillc check",         // including concrete commands
		"skillc diff --before",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("--skill page should contain %q", want)
		}
	}
}

// Passing a value to the flag is a misuse with an explicit pointer to query.
func TestSkillFlagRejectsValue(t *testing.T) {
	code, out := runCLI(t, "--skill", "a")
	if code != 2 {
		t.Fatalf("--skill with a value -> exit 2, got %d", code)
	}
	if !strings.Contains(out, "query --skill NAME") {
		t.Fatal("error should route to query for looking a skill up")
	}
}

// query stays the pure relationship lookup (never the self-guide page).
func TestQueryLooksSkillsUp(t *testing.T) {
	root := t.TempDir()
	simpleSkill(t, root, "a", "a")
	code, out := runCLI(t, "query", "--skill", "a", "--only-root", root)
	if code != 0 {
		t.Fatal("found skill -> exit 0")
	}
	if !strings.Contains(out, "=== a --") {
		t.Fatal("query prints the skill's relationships")
	}
	if strings.Contains(out, "name: skillc") {
		t.Fatal("query must not print skillc's self-guide page")
	}
}

func TestFlagWithoutCommandIsUsageError(t *testing.T) {
	code, _ := runCLI(t, "--only-root", t.TempDir())
	if code != 2 {
		t.Fatalf("flags without a command -> exit 2, got %d", code)
	}
}

func TestHelpRoutesToSkillFlag(t *testing.T) {
	code, out := runCLI(t, "--help")
	if code != 0 {
		t.Fatal("--help exits 0")
	}
	if !strings.Contains(out, "--skill") || !strings.Contains(out, "skill page") {
		t.Fatal("help should route to the --skill self-guide")
	}
}

func strp(s string) *string { return &s }
