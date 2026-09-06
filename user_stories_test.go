// user_stories_test.go -- user-story-level integration tests.
//
// How this differs from skillc_test.go:
//
//	skillc_test.go      unit behavior: --only-root points the compiler at a
//	                    synthetic tree directly.
//	user_stories_test   end-to-end: $HOME points at a fake home dir so the
//	                    compiler runs the *real* root-discovery logic (agent
//	                    dirs + newest plugin-cache version + cross-root
//	                    symlinks), then checks stories S1..S4 one by one.
package skillscope_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/williamfzc/skill-compiler/internal/state"
)

// ---------------------------------------------------------------- fake home

func writeSkill(t *testing.T, home, rootRel, skillRel, name string,
	hasDesc bool, body, extraFM string) {
	t.Helper()
	d := filepath.Join(home, rootRel, skillRel)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	if hasDesc {
		b.WriteString("description: does a thing\n")
	}
	b.WriteString(extraFM)
	b.WriteString("---\n")
	b.WriteString(body)
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeHomeFile(t *testing.T, home, rel, content string) {
	t.Helper()
	p := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// baseHome builds a 'healthy' fake home and returns it.
func baseHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	writeSkill(t, home, ".agents/skills", "alpha", "alpha", true, "\n# body\n", "")
	writeSkill(t, home, ".trae/skills", "beta", "beta", true, "\n# body\n", "")
	return home
}

// withHome runs fn with HOME pointed at a fake home dir. Relocated agent
// homes (CODEX_HOME etc.) are blanked -- "" reads as unset -- so a dev
// machine's real env cannot leak into a fake-home test.
func withHome(t *testing.T, home string, fn func()) {
	t.Helper()
	t.Setenv("HOME", home)
	for _, env := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR",
		"VIBE_HOME", "HERMES_HOME", "AUTOHAND_HOME", "GROK_HOME"} {
		t.Setenv(env, "")
	}
	fn()
}

func buildHome(t *testing.T) *state.Graph {
	t.Helper()
	code, out := runCLI(t, "build")
	if code != 0 {
		t.Fatalf("build exit %d: %s", code, out)
	}
	var g state.Graph
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatal(err)
	}
	return &g
}

func checkHomeJSON(t *testing.T) (int, map[string]any) {
	t.Helper()
	code, out := runCLI(t, "check", "--json")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("check --json invalid JSON: %v\n%s", err, out)
	}
	return code, parsed
}

// ================================================================ S1
// Turn "what this machine will load" into deterministic fact: discover real
// load roots, take only the newest plugin version, dedup symlinks into one
// node with provenance for every mount, and compile the same twice into the
// same state.

func TestS1DiscoversRealRoots(t *testing.T) {
	home := baseHome(t)
	withHome(t, home, func() {
		g := buildHome(t)
		names := map[string]bool{}
		for _, n := range g.Skills {
			names[n.Name] = true
		}
		if len(names) != 2 || !names["alpha"] || !names["beta"] {
			t.Fatalf("S1 discovers skills under two agent roots: %v", names)
		}
		kinds := map[string]bool{}
		for _, r := range g.Roots {
			kinds[r.Kind] = true
		}
		if !kinds["agent"] {
			t.Fatal("S1 root kinds should include agent")
		}
	})
}

func TestS1PluginNewestVersionOnly(t *testing.T) {
	home := baseHome(t)
	writeSkill(t, home, ".trae/plugins/cache/local/myplugin/1.0.0/skills",
		"toolold", "toolold", true, "\n# body\n", "")
	writeSkill(t, home, ".trae/plugins/cache/local/myplugin/2.0.0/skills",
		"toolnew", "toolnew", true, "\n# body\n", "")
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	new := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(home, ".trae/plugins/cache/local/myplugin/1.0.0"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(home, ".trae/plugins/cache/local/myplugin/2.0.0"), new, new); err != nil {
		t.Fatal(err)
	}
	withHome(t, home, func() {
		g := buildHome(t)
		names := map[string]bool{}
		for _, n := range g.Skills {
			names[n.Name] = true
		}
		if !names["toolnew"] {
			t.Fatal("S1 newest plugin version should be loaded")
		}
		if names["toolold"] {
			t.Fatalf("S1 stale plugin version must not be loaded (zombie), got %v", names)
		}
		pluginRoots := 0
		for _, r := range g.Roots {
			if r.Kind == "plugin" {
				pluginRoots++
			}
		}
		if pluginRoots != 1 {
			t.Fatalf("S1 exactly one plugin root, got %d", pluginRoots)
		}
	})
}

func TestS1ZcodePluginCacheIsALoadRoot(t *testing.T) {
	home := baseHome(t)
	writeSkill(t, home, ".zcode/cli/plugins/cache/official/example/0.4.2/skills",
		"plugged", "plugged", true, "\n# body\n", "")
	withHome(t, home, func() {
		g := buildHome(t)
		found := false
		for _, n := range g.Skills {
			if n.Name == "plugged" {
				found = true
			}
		}
		if !found {
			t.Fatal("S1 zcode plugin cache should be discovered as a load root")
		}
	})
}

func TestS1EnvRelocatedHomeLoads(t *testing.T) {
	home := baseHome(t)
	writeSkill(t, home, "custom-codex/skills", "moved", "moved", true, "\n# body\n", "")
	withHome(t, home, func() {
		t.Setenv("CODEX_HOME", filepath.Join(home, "custom-codex"))
		g := buildHome(t)
		found := false
		for _, n := range g.Skills {
			if n.Name == "moved" {
				found = true
			}
		}
		if !found {
			t.Fatal("S1 skills under an env-relocated CODEX_HOME should load")
		}
	})
}

func TestS1SymlinkDedupWithProvenance(t *testing.T) {
	home := baseHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude/skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".agents/skills/alpha"),
		filepath.Join(home, ".claude/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	withHome(t, home, func() {
		g := buildHome(t)
		count := 0
		prov := 0
		for _, n := range g.Skills {
			if n.Name == "alpha" {
				count++
				prov = len(n.Provenance)
			}
		}
		if count != 1 {
			t.Fatalf("S1 symlink should dedup into one node, got %d", count)
		}
		if prov != 2 {
			t.Fatalf("S1 provenance should record two mounts, got %d", prov)
		}
		if g.Summary.MultiMountedGroups != 1 {
			t.Fatal("S1 want one multi_mounted group")
		}
	})
}

func TestS1Deterministic(t *testing.T) {
	home := baseHome(t)
	withHome(t, home, func() {
		a := buildHome(t)
		b := buildHome(t)
		shape := func(g *state.Graph) []string {
			var s []string
			for id := range g.Skills {
				s = append(s, "node:"+id)
			}
			for _, e := range g.Edges {
				s = append(s, "edge:"+e.Kind+":"+e.From+":"+e.To)
			}
			for _, d := range g.Diagnostics {
				s = append(s, "diag:"+d.Code+":"+d.Where)
			}
			sort.Strings(s)
			return s
		}
		sa, sb := shape(a), shape(b)
		if strings.Join(sa, "|") != strings.Join(sb, "|") {
			t.Fatal("S1 two compiles must produce the same state (deterministic)")
		}
	})
}

// ================================================================ S2
// Know immediately whether an edit broke something.

func TestS2CatchesLoadTimeFaults(t *testing.T) {
	home := baseHome(t)
	writeSkill(t, home, ".agents/skills", "nodesc", "nodesc", false, "\n# body\n", "")
	d := filepath.Join(home, ".agents/skills/raw")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("# no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, home, ".agents/skills", "brk", "brk", true,
		"\nsee [x](references/gone.md)\n", "")
	if err := os.Symlink(filepath.Join(home, ".agents/skills/ghost"),
		filepath.Join(home, ".agents/skills/dead")); err != nil {
		t.Fatal(err)
	}
	withHome(t, home, func() {
		code, cj := checkHomeJSON(t)
		if code != 1 {
			t.Fatal("S2 exit code 1 when errors present")
		}
		for _, want := range []string{"NO_DESCRIPTION", "NO_FRONTMATTER", "BROKEN_REF", "DANGLING_SYMLINK"} {
			if len(diagsOf(cj, want)) != 1 {
				t.Fatalf("S2 should catch one %s", want)
			}
		}
	})
}

func TestS2BacktickMentionIsNotARef(t *testing.T) {
	home := baseHome(t)
	writeSkill(t, home, ".agents/skills", "talker", "talker", true,
		"\nmention `references/nope.md` in prose\n", "")
	withHome(t, home, func() {
		g := buildHome(t)
		for _, e := range g.Edges {
			if e.Kind == "ref" && strings.Contains(e.From, "talker") {
				t.Fatal("S2 backtick mention must not become a ref edge")
			}
		}
		if g.Summary.BrokenRefCount != 0 {
			t.Fatal("S2 backtick mention must not be a broken ref")
		}
	})
}

func TestS2SkillRootRelativeRefResolves(t *testing.T) {
	home := baseHome(t)
	writeSkill(t, home, ".agents/skills", "guided", "guided", true, "\n# body\n", "")
	writeHomeFile(t, home, ".agents/skills/guided/references/index.md",
		"see [g](references/deep/guide.md)\n")
	writeHomeFile(t, home, ".agents/skills/guided/references/deep/guide.md", "# guide\n")
	withHome(t, home, func() {
		_, cj := checkHomeJSON(t)
		if len(diagsOf(cj, "BROKEN_REF")) != 0 {
			t.Fatal("S2 skill-root-relative reference should resolve")
		}
	})
}

func TestS2CleanHomePasses(t *testing.T) {
	home := baseHome(t)
	withHome(t, home, func() {
		code, cj := checkHomeJSON(t)
		if code != 0 {
			t.Fatal("S2 healthy home exits 0")
		}
		summary := cj["summary"].(map[string]any)
		if summary["error_count"].(float64) != 0 {
			t.Fatal("S2 healthy home has no errors")
		}
	})
}

// ================================================================ S3
// Tell new debt from old debt.

func TestS3DiffFlagsNewRegressionOnly(t *testing.T) {
	home := baseHome(t)
	writeSkill(t, home, ".agents/skills", "a", "a", true, "\n[b](../b/SKILL.md)\n", "")
	writeSkill(t, home, ".agents/skills", "b", "b", true, "\n# body\n", "")
	writeSkill(t, home, ".agents/skills", "legacy", "legacy", true,
		"\n[old](references/already-gone.md)\n", "")
	before := filepath.Join(home, "before.json")
	after := filepath.Join(home, "after.json")
	withHome(t, home, func() {
		code, _ := runCLI(t, "build", "--out", before)
		if code != 0 {
			t.Fatal("build before failed")
		}
		if err := os.RemoveAll(filepath.Join(home, ".agents/skills/b")); err != nil {
			t.Fatal(err)
		}
		code, _ = runCLI(t, "build", "--out", after)
		if code != 0 {
			t.Fatal("build after failed")
		}
		code, out := runCLI(t, "diff", "--before", before, "--after", after)
		if code != 1 {
			t.Fatal("S3 regression present -> exit code 1")
		}
		if !strings.Contains(out, "NEW_BROKEN_REF") && !strings.Contains(out, "SKILL_DISAPPEARED") {
			t.Fatal("S3 should report the new regression")
		}
		// the pre-existing broken ref must not be counted as this change's regression
		regressionSection := strings.Split(out, "notes:")[0]
		if strings.Contains(regressionSection, "already-gone.md") {
			t.Fatal("S3 pre-existing broken ref goes to notes, not regressions")
		}
	})
}

func TestS3DiffNoRegressionOnIdentical(t *testing.T) {
	home := baseHome(t)
	p := filepath.Join(home, "s.json")
	withHome(t, home, func() {
		if code, _ := runCLI(t, "build", "--out", p); code != 0 {
			t.Fatal("build failed")
		}
		code, out := runCLI(t, "diff", "--before", p, "--after", p)
		if code != 0 {
			t.Fatal("S3 identical before/after -> exit code 0")
		}
		if !strings.Contains(out, "no regressions found.") {
			t.Fatal("S3 should explicitly report no regressions")
		}
	})
}

// ================================================================ S4
// Clean consumer entry point; no hardcoded skill knowledge in the compiler.

func TestS4StateExposesConsumerContract(t *testing.T) {
	home := baseHome(t)
	withHome(t, home, func() {
		g := buildHome(t)
		raw, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		var top map[string]any
		if err := json.Unmarshal(raw, &top); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"schema", "roots", "skills", "edges", "files",
			"file_edges", "broken_refs",
			"external_refs", "identities", "name_collisions", "diagnostics", "summary"} {
			if _, ok := top[key]; !ok {
				t.Fatalf("S4 state missing top-level field %s", key)
			}
		}
		ids := top["identities"].(map[string]any)
		if len(ids) != 2 || ids["multi_mounted"] == nil || ids["same_content"] == nil {
			t.Fatal("S4 identities should have multi_mounted / same_content")
		}
		for _, e := range g.Edges {
			if e.Kind != "ref" && e.Kind != "contains" {
				t.Fatalf("S4 edges are only ref / contains, got %s", e.Kind)
			}
		}
		alpha := nodeByName(g, "alpha")
		if alpha == nil {
			t.Fatal("alpha node missing")
		}
		nb, err := json.Marshal(alpha)
		if err != nil {
			t.Fatal(err)
		}
		var node map[string]any
		if err := json.Unmarshal(nb, &node); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"id", "provenance", "content_hash", "refs_in_count",
			"refs_out", "contains", "contained_by"} {
			if _, ok := node[key]; !ok {
				t.Fatalf("S4 node missing field %s", key)
			}
		}
	})
}

func TestS4CompilerHasNoHardcodedSkillKnowledge(t *testing.T) {
	// Judgment belongs to the agent: the compiler source must contain no
	// concrete skill names / domain words / classification tables. Comments
	// are stripped first (mentions in prose are allowed as examples).
	forbidden := []string{"lark-doc", "bytedcli", "metis-case-writer", "lark-suite", "traex"}
	reLineComment := regexp.MustCompile(`//.*`)
	reBlockComment := regexp.MustCompile(`(?s)/\*.*?\*/`)

	var sources []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || strings.HasPrefix(d.Name(), ".")) && path != "." {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			sources = append(sources, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("no Go sources found to scan")
	}
	for _, path := range sources {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		code := reBlockComment.ReplaceAllString(string(src), "")
		code = reLineComment.ReplaceAllString(code, "")
		for _, w := range forbidden {
			if strings.Contains(code, w) {
				t.Fatalf("S4 compiler code contains hardcoded skill name %q in %s", w, path)
			}
		}
		if strings.Contains(code, "CATEGORY") || strings.Contains(code, "categories") {
			t.Fatalf("S4 compiler has a classification table in %s", path)
		}
	}
}
