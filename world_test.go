// world_test.go -- end-to-end scenarios over a small simulated multi-agent
// world in testdata/world: guidance edges, error discoverability, the fold
// story, and regression gating between two snapshots.
//
// The world is guidance-rich markdown (release orchestration, a deploy guard,
// release notes, a nested planning suite, and a deliberately broken draft),
// plus two extra agent roots holding copied material. The compiler observes
// all of it; these tests pin what a consumer must be able to prove and do.
package skillscope_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/williamfzc/skill-compiler/internal/state"
)

func worldRoot(t *testing.T, rel ...string) string {
	t.Helper()
	parts := append([]string{"testdata", "world"}, rel...)
	abs, err := filepath.Abs(filepath.Join(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// dirIndex maps each skill's directory basename to its node id.
func dirIndex(g *state.Graph) map[string]string {
	out := map[string]string{}
	for id := range g.Skills {
		out[filepath.Base(id)] = id
	}
	return out
}

// --- baseline: the world's observed facts --------------------------------

// The canonical root holds the interlinked orchestration skills plus a flat
// set of eight small ticket-lifecycle skills -- the "many skills at the top
// level" clutter that folding exists to reduce. A draft's two references
// resolve nowhere; guidance links become ref edges and the nested suite a
// contains edge.
func TestWorldBaselineFacts(t *testing.T) {
	g := state.Compile(nil, []string{worldRoot(t, "skills")})
	s := g.Summary

	if s.SkillCount != 14 {
		t.Fatalf("14 skills, got %d", s.SkillCount)
	}
	if s.EdgesByKind["ref"] != 7 || s.EdgesByKind["contains"] != 1 {
		t.Fatalf("7 ref edges / 1 contains edge, got %v", s.EdgesByKind)
	}

	// Eight of the fourteen are the flat ticket skills, sitting at the root
	// level right beside the orchestration skills; none is nested.
	var flatTicket []string
	for _, n := range g.Skills {
		if !strings.HasPrefix(n.Name, "ticket-") {
			continue
		}
		flatTicket = append(flatTicket, n.Name)
		if len(n.ContainedBy) > 0 {
			t.Fatalf("flat ticket skill %s must not be nested under anything", n.Name)
		}
		if n.Name == "ticket-escalate" && n.RefsInCount != 1 {
			// The one ticket skill reached from outside the flat set: the
			// incident draft points at it (a load-bearing member).
			t.Fatalf("ticket-escalate is referenced once (by risky), got %d", n.RefsInCount)
		}
	}
	sort.Strings(flatTicket)
	if len(flatTicket) != 8 {
		t.Fatalf("eight flat ticket skills, got %d: %v", len(flatTicket), flatTicket)
	}

	if s.BrokenRefCount != 2 || s.ErrorCount != 2 || s.WarnCount != 1 {
		t.Fatalf("2 broken refs (2 errors) + 1 name-mismatch warn, got "+
			"broken=%d errors=%d warns=%d", s.BrokenRefCount, s.ErrorCount, s.WarnCount)
	}

	// Ref edges, keyed by directory names: release -> deploy-check & notes;
	// workflows -> release, its nested sub, and notes (via plan.md);
	// sub -> notes (via ../../notes).
	pairs := map[string][]string{}
	for _, e := range g.Edges {
		if e.Kind == "ref" {
			pairs[filepath.Base(e.From)] = append(pairs[filepath.Base(e.From)], filepath.Base(e.To))
		}
	}
	for k := range pairs {
		sort.Strings(pairs[k])
	}
	want := map[string][]string{
		"release":   {"deploy-check", "notes"},
		"workflows": {"notes", "release", "sub"},
		"sub":       {"notes"},
	}
	for from, tos := range want {
		if strings.Join(pairs[from], ",") != strings.Join(tos, ",") {
			t.Fatalf("ref edges from %s: want %v, got %v", from, tos, pairs[from])
		}
	}

	// Nesting: workflows contains its nested planning skill.
	var contains [2]string
	for _, e := range g.Edges {
		if e.Kind == "contains" {
			contains = [2]string{filepath.Base(e.From), filepath.Base(e.To)}
		}
	}
	if contains != [2]string{"workflows", "sub"} {
		t.Fatalf("contains edge workflows->sub, got %v", contains)
	}

	// Both broken refs belong to the draft; the healthy core is intact.
	idx := dirIndex(g)
	risky := g.Skills[idx["risky"]]
	broke := map[string]bool{}
	for _, b := range g.BrokenRefs {
		if b.From == risky.ID {
			broke[b.Raw] = true
		}
	}
	if !broke["postmortem.md"] || !broke["safety/checklist.md"] || len(broke) != 2 {
		t.Fatalf("risky draft has exactly two unresolvable refs, got %v", broke)
	}

	// Blast radius: notes is the hub (release, workflows, and the nested
	// plan all point at it); release is referenced once.
	if g.Skills[idx["notes"]].RefsInCount != 3 {
		t.Fatal("notes should be referenced by 3 skills (release, workflows, sub)")
	}
	if g.Skills[idx["release"]].RefsInCount != 1 {
		t.Fatal("release should be referenced once (by workflows)")
	}

	// The nested skill deliberately declares name: plan while living in
	// directory sub/ -- a NAME_MISMATCH warn the consumer must see.
	var mismatches []string
	for _, d := range g.Diagnostics {
		if d.Code == "NAME_MISMATCH" {
			mismatches = append(mismatches, d.Where)
		}
	}
	if len(mismatches) != 1 || !strings.HasSuffix(mismatches[0], "/sub") {
		t.Fatalf("one NAME_MISMATCH on the nested sub skill, got %v", mismatches)
	}
}

// --- errors must be discoverable through both surfaces -------------------

func TestWorldErrorsAreDiscoverable(t *testing.T) {
	root := worldRoot(t, "skills")

	// State/JSON surface: a gate or consumer reads codes without parsing prose.
	g := state.Compile(nil, []string{root})
	codes := map[string]int{}
	for _, d := range g.Diagnostics {
		codes[d.Code]++
	}
	if codes["BROKEN_REF"] != 2 {
		t.Fatalf("state surface: 2 BROKEN_REF, got %d", codes["BROKEN_REF"])
	}

	// Human surface: check exits 1 and names the faulty skill and missing file.
	code, out := runCLI(t, "check", "--only-root", root, "--json")
	if code != 1 {
		t.Fatalf("check must exit 1 when errors exist, got %d", code)
	}
	if !strings.Contains(out, "risky") || !strings.Contains(out, "postmortem.md") {
		t.Fatal("check output should name the draft skill and its missing reference")
	}
}

// --- the fold story ------------------------------------------------------

// Independent copies across agent roots are observed as same_content groups:
// the exact fact a folding/dedup consumer acts on. Folding then means the
// consumer points agents at the canonical root -- the graph proves the copies
// carry no unique skill.
func TestWorldFoldObservedAndFoldable(t *testing.T) {
	roots := []string{
		worldRoot(t, "skills"),
		worldRoot(t, "agents", "agent-1", "skills"),
		worldRoot(t, "agents", "agent-2", "skills"),
	}
	copied := state.Compile(nil, roots)
	if copied.Summary.RootCount != 3 || copied.Summary.SkillCount != 17 {
		t.Fatalf("copied world: 3 roots / 17 physical skills, got %d/%d",
			copied.Summary.RootCount, copied.Summary.SkillCount)
	}
	if copied.Summary.DuplicateContentGroups != 3 {
		t.Fatalf("3 same_content groups (release, workflows, plan), got %d",
			copied.Summary.DuplicateContentGroups)
	}
	groups := map[string]bool{}
	for k := range copied.Identities.SameContent {
		groups[strings.SplitN(k, "@", 2)[0]] = true
	}
	for _, want := range []string{"release", "workflows", "plan"} {
		if !groups[want] {
			t.Fatalf("same_content should group the copied %s, groups=%v", want, groups)
		}
	}

	// Every group spans the canonical root and an agent root: the copy adds
	// no skill that the canonical root lacks.
	canonicalRoot := worldRoot(t, "skills")
	for _, ids := range copied.Identities.SameContent {
		var inCanonical, inCopy bool
		for _, id := range ids {
			if strings.HasPrefix(id, canonicalRoot+string(os.PathSeparator)) || id == canonicalRoot {
				inCanonical = true
			} else {
				inCopy = true
			}
		}
		if !inCanonical || !inCopy {
			t.Fatalf("fold group should span canonical and a copy root: %v", ids)
		}
	}

	// After the fold (agents point at the canonical root): no copy groups,
	// and the guidance graph among canonical skills is unchanged.
	folded := state.Compile(nil, []string{canonicalRoot})
	if folded.Summary.DuplicateContentGroups != 0 || folded.Summary.SkillCount != 14 {
		t.Fatalf("folded world: 0 copy groups / 14 skills, got %d/%d",
			folded.Summary.DuplicateContentGroups, folded.Summary.SkillCount)
	}
	if folded.Summary.EdgesByKind["ref"] != 7 || folded.Summary.EdgesByKind["contains"] != 1 {
		t.Fatalf("folded world keeps all 7 ref + 1 contains edges, got %v", folded.Summary.EdgesByKind)
	}
}

// --- folding reduces flat-set clutter ------------------------------------

// The main fold problem: many small skills sit flat at the root level and
// every one pays resident-context cost to advertise itself. Grouping the
// cohesive flat set (the eight ticket-lifecycle skills) under one parent
// collapses eight top-level entries into one. The graph proves both the
// reduction and the safety condition -- a member that is referenced from
// outside the set must remain reachable, and moving files must not break any
// reference (an unsafe fold is caught by check, not guessed).
func TestWorldFoldFlattensTheTicketSet(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "skills")
	copyTree(t, worldRoot(t, "skills"), dir)

	// Before: eight ticket skills advertised at the root level, alongside
	// the orchestration skills and the workflows/sub nesting.
	topLevel := func(g *state.Graph) int {
		n := 0
		for _, node := range g.Skills {
			if len(node.ContainedBy) == 0 {
				n++
			}
		}
		return n
	}
	before := state.Compile(nil, []string{dir})
	if before.Summary.SkillCount != 14 {
		t.Fatalf("14 skills before folding, got %d", before.Summary.SkillCount)
	}
	if top := topLevel(before); top != 13 {
		t.Fatalf("13 top-level entries before folding (only sub is nested), got %d", top)
	}

	// Safety rule, read straight from the graph: ticket-escalate is the one
	// flat-set member with an inbound reference from outside the set --
	// risky points at it. A fold may group the set but must not make that
	// external reference unresolvable.
	escalateID := ""
	var externalCaller string
	for id, n := range before.Skills {
		if n.Name == "ticket-escalate" {
			escalateID = id
		}
	}
	for _, e := range before.Edges {
		if e.Kind == "ref" && e.To == escalateID {
			externalCaller = e.From
		}
	}
	if externalCaller == "" {
		t.Fatal("ticket-escalate has an inbound edge from risky in the baseline world")
	}

	// Perform the fold: add a parent SKILL.md above the eight ticket skill
	// dirs (their container dir already exists), turning the flat set into a
	// nested suite, and rewrite the one external reference to follow the
	// files -- risky links ../tickets/ticket-escalate/SKILL.md, which is
	// unchanged by adding a parent above the same path.
	writeFile(t, filepath.Join(dir, "tickets", "SKILL.md"),
		"---\nname: tickets\n"+
			"description: Ticket lifecycle workflow: triage, route, escalate, and close support tickets.\n"+
			"---\n\n# Tickets\n\nOne entry point for the ticket workflow; the "+
			"individual steps live as nested skills and load on demand.\n")

	after := state.Compile(nil, []string{dir})

	// The fold introduces no new faults: same broken-ref count (the draft's
	// two pre-existing gaps) and the escalate reference resolves.
	if after.Summary.BrokenRefCount != before.Summary.BrokenRefCount {
		t.Fatalf("folding must not break references; broken %d -> %d",
			before.Summary.BrokenRefCount, after.Summary.BrokenRefCount)
	}
	if after.Summary.SkillCount != 15 {
		t.Fatalf("folding adds the parent: 15 skills, got %d", after.Summary.SkillCount)
	}
	// Reduction: eight flat advertisements collapse under one parent.
	if top := topLevel(after); top != 6 {
		t.Fatalf("6 top-level entries after folding (was 13), got %d", top)
	}
	// The parent contains all eight ticket skills.
	idx := dirIndex(after)
	parent := after.Skills[idx["tickets"]]
	if parent == nil || len(parent.Contains) != 8 {
		t.Fatalf("tickets parent contains 8 nested skills, got %d", len(parent.Contains))
	}
	// The external caller still reaches escalate, now through the folded
	// path: the ref edge survives and lands on the nested skill.
	var edgeSurvived bool
	for _, e := range after.Edges {
		if e.Kind == "ref" && filepath.Base(e.From) == "risky" &&
			filepath.Base(e.To) == "ticket-escalate" {
			edgeSurvived = true
		}
	}
	if !edgeSurvived {
		t.Fatal("risky -> ticket-escalate reference must still resolve after the fold")
	}

	// An unsafe fold is caught too: regroup the files into a renamed parent
	// directory without updating the caller. risky's link now points at a
	// path that no longer exists, so check gains exactly one broken ref.
	unsafeDir := filepath.Join(work, "unsafe")
	copyTree(t, dir, unsafeDir)
	if err := os.Rename(filepath.Join(unsafeDir, "tickets"),
		filepath.Join(unsafeDir, "tickets-moved")); err != nil {
		t.Fatal(err)
	}
	unsafeGraph := state.Compile(nil, []string{unsafeDir})
	if unsafeGraph.Summary.BrokenRefCount != after.Summary.BrokenRefCount+1 {
		t.Fatalf("stale caller path adds one broken ref: %d -> want %d",
			unsafeGraph.Summary.BrokenRefCount, after.Summary.BrokenRefCount+1)
	}
	var caught bool
	for _, b := range unsafeGraph.BrokenRefs {
		if strings.Contains(b.Raw, "tickets/ticket-escalate") {
			caught = true
		}
	}
	if !caught {
		t.Fatal("check must surface the stale reference after an unsafe fold")
	}
}

// --- the regression story: new debt separated from old debt --------------

// A well-meaning edit adds a reference to a file that does not exist. diff
// must flag it as a NEW regression while the draft's pre-existing broken refs
// stay in "notes"; repairing the reference returns the tree to baseline. The
// before/after snapshots are the same directory at different times, exactly
// as diff is used in practice.
func TestWorldRegressionThenRepair(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "skills")
	copyTree(t, worldRoot(t, "skills"), dir)

	writeStateFile(t, filepath.Join(work, "before.json"), state.Compile(nil, []string{dir}))

	// The change: release now points at a rollback playbook that was never
	// carried over (the classic refactor/fold mishap).
	appendToFile(t, filepath.Join(dir, "release", "SKILL.md"),
		"\nAlso read the [rollback playbook](rollback-playbook.md) before cutting.\n")
	after := state.Compile(nil, []string{dir})
	if after.Summary.BrokenRefCount != 3 {
		t.Fatalf("after the edit there are 3 broken refs (2 old + 1 new), got %d",
			after.Summary.BrokenRefCount)
	}
	writeStateFile(t, filepath.Join(work, "after.json"), after)

	code, out := runCLI(t, "diff",
		"--before", filepath.Join(work, "before.json"),
		"--after", filepath.Join(work, "after.json"))
	if code != 1 {
		t.Fatalf("diff exits 1 on regression, got %d", code)
	}
	if !strings.Contains(out, "NEW_BROKEN_REF") || !strings.Contains(out, "rollback-playbook.md") {
		t.Fatalf("diff should flag the new reference, got:\n%s", out)
	}
	if strings.Contains(out, "SKILL_DISAPPEARED") {
		t.Fatal("no skill was removed; diff must not report a disappearance")
	}
	// The draft's two broken refs predate the change and must not be blamed
	// on it: counted as present in both, never as regressions.
	if !strings.Contains(out, "present in both") {
		t.Fatalf("pre-existing broken refs should be noted as present-in-both, got:\n%s", out)
	}
	if strings.Contains(out, "postmortem.md") {
		t.Fatal("the draft's pre-existing ref must not appear in diff output as a regression")
	}

	// Repair: the missing playbook lands in release/ and the reference resolves.
	writeFile(t, filepath.Join(dir, "release", "rollback-playbook.md"),
		"# Rollback playbook\n\nRe-run the deploy guard with the previous artifact.\n")
	writeStateFile(t, filepath.Join(work, "repaired.json"), state.Compile(nil, []string{dir}))
	code, _ = runCLI(t, "diff",
		"--before", filepath.Join(work, "before.json"),
		"--after", filepath.Join(work, "repaired.json"))
	if code != 0 {
		t.Fatalf("after repair the tree matches baseline; diff exits 0, got %d", code)
	}
}

// --- helpers -------------------------------------------------------------

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyTree(t, s, d)
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(d, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writeStateFile(t *testing.T, path string, g *state.Graph) {
	t.Helper()
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendToFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func rewriteFile(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("rewriteFile: %q not found in %s", old, path)
	}
	updated := strings.ReplaceAll(string(data), old, new)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
