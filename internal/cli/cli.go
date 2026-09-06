// Package cli is the outside layer: argument parsing and human/JSON output.
//
// Every subcommand is a thin renderer over the compiled state graph (or a
// saved state file). It speaks to the terminal and the exit code; it holds no
// compilation logic.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/williamfzc/skill-compiler/internal/collect"
	"github.com/williamfzc/skill-compiler/internal/paths"
	"github.com/williamfzc/skill-compiler/internal/state"
	"github.com/williamfzc/skill-compiler/internal/viz"
)

// Run parses argv and dispatches; it returns the process exit code.
func Main(argv []string) int {
	if len(argv) == 0 || argv[0] == "-h" || argv[0] == "--help" || argv[0] == "help" {
		printTopHelp(os.Stdout)
		return 0
	}
	// Flag-first invocation: `skillc --skill` is skillc presenting its own
	// skill page (the document an agent reads to learn how to drive it). It
	// takes no value -- looking a skill up is `query --skill NAME`.
	if len(argv) > 0 && strings.HasPrefix(argv[0], "-") {
		switch argv[0] {
		case "--skill":
			if len(argv) > 1 {
				return usageErr(fmt.Errorf("--skill takes no value; " +
					"use 'skillc query --skill NAME' to look a skill up"))
			}
			printSkillPage(os.Stdout)
			return 0
		default:
			return usageErr(fmt.Errorf("flags need a command; see 'skillc --help'"))
		}
	}
	cmd := argv[0]
	rest := argv[1:]
	switch cmd {
	case "build":
		return cmdBuild(rest)
	case "check":
		return cmdCheck(rest)
	case "query":
		return cmdQuery(rest)
	case "roots":
		return cmdRoots(rest)
	case "diff":
		return cmdDiff(rest)
	case "viz":
		return cmdViz(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printTopHelp(os.Stderr)
		return 2
	}
}

type commonFlags struct {
	extraRoot []string
	onlyRoot  []string
}

// parseCommon pulls --extra-root / --only-root out of args. Non-common flags
// are left for the command to read itself; unknown value flags are skipped
// past so their value token is not mistaken for another flag.
func parseCommon(rest []string) (*commonFlags, error) {
	f := &commonFlags{}
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name, val, hasVal := splitFlag(arg)
		take := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(rest) {
				return "", fmt.Errorf("flag %s requires a value", name)
			}
			i++
			return rest[i], nil
		}
		switch name {
		case "--extra-root":
			v, err := take()
			if err != nil {
				return nil, err
			}
			f.extraRoot = append(f.extraRoot, v)
		case "--only-root":
			v, err := take()
			if err != nil {
				return nil, err
			}
			f.onlyRoot = append(f.onlyRoot, v)
		default:
			if !hasVal && i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "-") && takesValue(name) {
				i++ // skip the value of a known value flag
			}
		}
	}
	return f, nil
}

// takesValue reports whether a non-common flag consumes the next argv token.
func takesValue(name string) bool {
	switch name {
	case "--out", "--state", "--skill", "--before", "--after":
		return true
	}
	return false
}

func splitFlag(arg string) (name, val string, hasVal bool) {
	if i := strings.Index(arg, "="); i >= 0 && strings.HasPrefix(arg, "-") {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}

// checkFlags reports an error for any flag the command does not accept.
// Common flags (--extra-root / --only-root) are allowed for commands that
// compile; allowCommon toggles that.
func checkFlags(rest []string, allowCommon bool, allowed ...string) error {
	known := map[string]bool{}
	if allowCommon {
		known["--extra-root"] = true
		known["--only-root"] = true
	}
	for _, a := range allowed {
		known[a] = true
	}
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name, _, hasVal := splitFlag(arg)
		if !known[name] {
			return fmt.Errorf("unknown flag: %s", name)
		}
		if !hasVal && takesValue(name) {
			if i+1 >= len(rest) {
				return fmt.Errorf("flag %s requires a value", name)
			}
			i++
		}
	}
	return nil
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func loadState(path string) (*state.Graph, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	g := &state.Graph{}
	if err := json.Unmarshal(b, g); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return g, nil
}

// ---------------------------------------------------------------- build

func cmdBuild(rest []string) int {
	if err := checkFlags(rest, true, "--out"); err != nil {
		return usageErr(err)
	}
	f, err := parseCommon(rest)
	if err != nil {
		return usageErr(err)
	}
	g := state.Compile(f.extraRoot, f.onlyRoot)
	out := flagValue(rest, "--out")
	if out == "" {
		writeJSON(os.Stdout, g)
		return 0
	}
	fp, err := os.Create(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	enc := json.NewEncoder(fp)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(g); err != nil {
		fp.Close()
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fp.Close()
	s := g.Summary
	fmt.Printf("compiled -> %s (%.2fs)\n", out, g.ScanSeconds)
	fmt.Printf("  roots %d  skills %d  edges %d %v  file edges %d\n",
		s.RootCount, s.SkillCount, s.EdgeCount, s.EdgesByKind, s.FileEdgeCount)
	fmt.Printf("  files %d  broken %d  collisions %d  error %d  warn %d\n",
		s.FileCount, s.BrokenRefCount, s.NameCollisionCount, s.ErrorCount, s.WarnCount)
	return 0
}

// flagValue extracts --name value / --name=value from args; "" if absent.
func flagValue(args []string, name string) string {
	for i, a := range args {
		n, v, hasVal := splitFlag(a)
		if n == name {
			if hasVal {
				return v
			}
			if i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return ""
}

func hasBoolFlag(args []string, name string) bool {
	for _, a := range args {
		n, _, _ := splitFlag(a)
		if n == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- roots

func cmdRoots(rest []string) int {
	if err := checkFlags(rest, true); err != nil {
		return usageErr(err)
	}
	f, err := parseCommon(rest)
	if err != nil {
		return usageErr(err)
	}
	roots := state.DiscoverRoots(f.extraRoot, f.onlyRoot)
	if len(roots) == 0 {
		fmt.Println("no load roots found")
		return 0
	}
	fmt.Printf("found %d load roots:\n", len(roots))
	for _, r := range roots {
		n := countSkillFiles(r.Path)
		fmt.Printf("  [%-6s] %s  (%d skills)\n", r.Kind, paths.Shorten(r.Path), n)
	}
	return 0
}

func countSkillFiles(root string) int {
	n := 0
	paths.WalkFollow(root, func(_ string, names []string) bool {
		for _, name := range names {
			if name == "SKILL.md" {
				n++
			}
		}
		return false
	})
	return n
}

// ---------------------------------------------------------------- check

func cmdCheck(rest []string) int {
	if err := checkFlags(rest, true, "--state", "--json"); err != nil {
		return usageErr(err)
	}
	f, err := parseCommon(rest)
	if err != nil {
		return usageErr(err)
	}
	asJSON := hasBoolFlag(rest, "--json")
	statePath := flagValue(rest, "--state")

	var g *state.Graph
	if statePath != "" {
		g, err = loadState(statePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	} else {
		g = state.Compile(f.extraRoot, f.onlyRoot)
	}
	s, diags := g.Summary, g.Diagnostics

	if asJSON {
		writeJSON(os.Stdout, map[string]any{"summary": s, "diagnostics": diags})
		if s.ErrorCount > 0 {
			return 1
		}
		return 0
	}

	fmt.Printf("roots %d  |  skills %d  |  ref edges %d  |  nesting %d\n",
		s.RootCount, s.SkillCount, s.EdgesByKind["ref"], s.EdgesByKind["contains"])
	fmt.Printf("resident description total %d chars (~%d tokens/turn)\n",
		s.ResidentDescChars, s.ResidentDescChars/3)
	fmt.Printf("broken %d  |  collisions %d  |  dangling %d  |  "+
		"multi-mounted %d  |  content copies %d\n",
		s.BrokenRefCount, s.NameCollisionCount, s.BrokenSymlinkCount,
		s.MultiMountedGroups, s.DuplicateContentGroups)
	fmt.Println()

	if len(diags) == 0 {
		fmt.Println("correctness check passed, no issues.")
		return 0
	}

	cur := ""
	nErr := 0
	for _, d := range diags {
		if d.Severity != cur {
			cur = d.Severity
			fmt.Printf("--- %s ---\n", strings.ToUpper(cur))
		}
		if d.Severity == "error" {
			nErr++
		}
		fmt.Printf("  [%s] %s\n", d.Code, d.Where)
		fmt.Printf("      %s\n", d.Detail)
	}
	fmt.Println()
	fmt.Printf("%d diagnostics total, %d of them errors\n", len(diags), nErr)
	if nErr > 0 {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------- query

func cmdQuery(rest []string) int {
	if err := checkFlags(rest, true, "--state", "--skill"); err != nil {
		return usageErr(err)
	}
	f, err := parseCommon(rest)
	if err != nil {
		return usageErr(err)
	}
	skill := flagValue(rest, "--skill")
	if skill == "" {
		return usageErr(fmt.Errorf("query requires --skill NAME"))
	}
	statePath := flagValue(rest, "--state")

	var g *state.Graph
	if statePath != "" {
		g, err = loadState(statePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	} else {
		g = state.Compile(f.extraRoot, f.onlyRoot)
	}

	var hits []*collect.Node
	for _, n := range g.Skills {
		if n.Name == skill || n.NameField == skill {
			hits = append(hits, n)
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].ID < hits[j].ID })
	if len(hits) == 0 {
		fmt.Printf("not found: %s\n", skill)
		return 1
	}

	fmt.Printf("=== %s -- %d match(es) ===\n\n", skill, len(hits))
	for _, n := range hits {
		var mounts []string
		for _, p := range n.Provenance {
			mounts = append(mounts,
				fmt.Sprintf("[%s] %s/%s", p.RootKind, paths.Shorten(p.Root), p.Rel))
		}
		fmt.Printf("realpath: %s\n", paths.Shorten(n.ID))
		fmt.Printf("  loaded from %d place(s): %s\n", len(n.Provenance), strings.Join(mounts, "; "))
		fmt.Printf("  entry %dB  description %d chars  frontmatter=%v\n",
			n.SkillMDBytes, n.DescChars, n.FrontmatterKeys)
		if n.IsSymlink {
			ok := "alive"
			if n.SymlinkOK != nil && !*n.SymlinkOK {
				ok = "broken"
			}
			style := ""
			target := ""
			if n.SymlinkStyle != nil {
				style = *n.SymlinkStyle
			}
			if n.SymlinkTarget != nil {
				target = *n.SymlinkTarget
			}
			fmt.Printf("  symlink(%s, %s) -> %s\n", style, ok, target)
		}
		if len(n.ContainedBy) > 0 {
			fmt.Println("  nested under: " + strings.Join(nodeNames(g, n.ContainedBy), ", "))
		}
		if len(n.Contains) > 0 {
			shown := n.Contains
			more := ""
			if len(shown) > 8 {
				shown = shown[:8]
				more = ""
			}
			fmt.Printf("  contains %d nested skill(s): %s%s\n",
				len(n.Contains), strings.Join(nodeNames(g, shown), ", "), more)
		}
		if len(n.RefsOut) > 0 {
			shown := n.RefsOut
			if len(shown) > 10 {
				shown = shown[:10]
			}
			fmt.Println("  references -> " + strings.Join(nodeNames(g, shown), ", "))
		}
		fmt.Printf("  referenced %d time(s)\n\n", n.RefsInCount)
	}
	return 0
}

func nodeNames(g *state.Graph, ids []string) []string {
	var out []string
	for _, id := range ids {
		if n, ok := g.Skills[id]; ok {
			out = append(out, n.Name)
		}
	}
	return out
}

// ---------------------------------------------------------------- diff

func cmdDiff(rest []string) int {
	before := flagValue(rest, "--before")
	after := flagValue(rest, "--after")
	if before == "" || after == "" {
		return usageErr(fmt.Errorf("diff requires --before FILE --after FILE"))
	}
	b, err := loadState(before)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	a, err := loadState(after)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	// Both files are our own `build --out` output. A diff is only meaningful
	// between two snapshots from the same compiler version, so refuse a schema
	// mismatch loudly instead of set-subtracting two different shapes into a
	// wrong regression count.
	if b.Schema != a.Schema {
		fmt.Printf("!! schema mismatch: before=%d, after=%d\n", b.Schema, a.Schema)
		fmt.Println("   These snapshots came from different skillc versions and")
		fmt.Println("   cannot be compared.")
		fmt.Println("   Re-run both with the current build:")
		fmt.Println("     skillc build --out before.json  # then make changes")
		fmt.Println("     skillc build --out after.json")
		return 2
	}

	reg, imp, notes := diffStates(b, a)
	sb, sa := b.Summary, a.Summary
	fmt.Println("=== compiled state, before vs after ===")
	fmt.Printf("skill count: %d -> %d\n", sb.SkillCount, sa.SkillCount)
	fmt.Printf("resident description: %d -> %d chars (%+d)\n",
		sb.ResidentDescChars, sa.ResidentDescChars,
		sa.ResidentDescChars-sb.ResidentDescChars)
	fmt.Printf("broken: %d -> %d  |  error: %d -> %d\n",
		sb.BrokenRefCount, sa.BrokenRefCount, sb.ErrorCount, sa.ErrorCount)
	fmt.Println()
	if len(reg) > 0 {
		fmt.Printf("!! %d regression(s)\n", len(reg))
		for _, x := range reg {
			fmt.Printf("  %s\n", x)
		}
		fmt.Println()
	} else {
		fmt.Println("no regressions found.")
		fmt.Println()
	}
	if len(imp) > 0 {
		fmt.Printf("%d improvement(s)\n", len(imp))
		for _, x := range imp {
			fmt.Printf("  %s\n", x)
		}
		fmt.Println()
	}
	if len(notes) > 0 {
		fmt.Println("notes:")
		for _, x := range notes {
			fmt.Printf("  - %s\n", x)
		}
		fmt.Println()
	}
	if len(reg) > 0 {
		return 1
	}
	return 0
}

// diffStates does set subtraction only: a regression is what `after` has and
// `before` lacks.
// cmdViz renders the compiled graph for human viewing. Mermaid and DOT go to
// stdout; the default format, HTML, is a self-contained page and needs --out.
func cmdViz(rest []string) int {
	if err := checkFlags(rest, true, "--format", "--out"); err != nil {
		return usageErr(err)
	}
	f, err := parseCommon(rest)
	if err != nil {
		return usageErr(err)
	}
	format := flagValue(rest, "--format")
	if format == "" {
		format = "html"
	}
	g := state.Compile(f.extraRoot, f.onlyRoot)
	switch format {
	case "mermaid":
		fmt.Print(viz.Mermaid(g))
	case "dot":
		fmt.Print(viz.DOT(g))
	case "html":
		out := flagValue(rest, "--out")
		if out == "" {
			return usageErr(fmt.Errorf("viz --format html requires --out FILE"))
		}
		if err := os.WriteFile(out, []byte(viz.HTML(g)), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Printf("wrote %s (%d file edges) -- open it in a browser\n",
			out, g.Summary.FileEdgeCount)
	default:
		return usageErr(fmt.Errorf("unknown viz format %q (html, mermaid, dot)", format))
	}
	return 0
}

type brokenKey struct{ src, raw string }

func diffStates(b, a *state.Graph) (reg, imp, notes []string) {
	keyOf := func(from, realFrom, raw string) brokenKey {
		src := realFrom
		if src == "" {
			src = from
		}
		return brokenKey{src, raw}
	}
	kb := map[brokenKey]bool{}
	for _, x := range b.BrokenRefs {
		kb[keyOf(x.From, x.RealFrom, x.Raw)] = true
	}
	ka := map[brokenKey]bool{}
	for _, x := range a.BrokenRefs {
		ka[keyOf(x.From, x.RealFrom, x.Raw)] = true
	}
	for k := range ka {
		if !kb[k] {
			reg = append(reg, fmt.Sprintf("[NEW_BROKEN_REF] %s -> %s", paths.Shorten(k.src), k.raw))
		}
	}
	for k := range kb {
		if !ka[k] {
			imp = append(imp, fmt.Sprintf("[FIXED_BROKEN_REF] %s -> %s", paths.Shorten(k.src), k.raw))
		}
	}
	both := 0
	for k := range kb {
		if ka[k] {
			both++
		}
	}
	if both > 0 {
		notes = append(notes, fmt.Sprintf("%d broken refs present in both (pre-existing)", both))
	}

	// Index effective name -> one node. Several nodes can share a name
	// (collisions, dangling symlinks beside real dirs); the pick must be
	// deterministic or map-iteration randomness invents symlink flips between
	// two identical snapshots. Take the lexicographically-first id, the same
	// node a JSON round-trip (sorted keys) would surface first.
	index := func(g *state.Graph) map[string]*collect.Node {
		ids := make([]string, 0, len(g.Skills))
		for id := range g.Skills {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		out := map[string]*collect.Node{}
		for _, id := range ids {
			n := g.Skills[id]
			name := n.NameField
			if name == "" {
				name = n.Name
			}
			if _, ok := out[name]; !ok {
				out[name] = n
			}
		}
		return out
	}
	ib, ia := index(b), index(a)
	namesA := make([]string, 0, len(ia))
	for name := range ia {
		namesA = append(namesA, name)
	}
	sort.Strings(namesA)
	for _, name := range namesA {
		na := ia[name]
		nb, ok := ib[name]
		if !ok {
			notes = append(notes, "new skill: "+name)
			continue
		}
		if na.BrokenSymlink && !nb.BrokenSymlink {
			target := ""
			if na.SymlinkTarget != nil {
				target = *na.SymlinkTarget
			}
			reg = append(reg, fmt.Sprintf("[BECAME_DANGLING] %s: symlink broke -> %s", name, target))
		} else if !na.BrokenSymlink && nb.BrokenSymlink {
			imp = append(imp, "[SYMLINK_FIXED] "+name)
		}
	}
	namesB := make([]string, 0, len(ib))
	for name := range ib {
		if _, ok := ia[name]; !ok {
			namesB = append(namesB, name)
		}
	}
	sort.Strings(namesB)
	for _, name := range namesB {
		reg = append(reg, "[SKILL_DISAPPEARED] "+name)
	}

	type collKey struct{ root, name string }
	cb := map[collKey]bool{}
	for _, c := range b.NameCollisions {
		cb[collKey{c.Root, c.Name}] = true
	}
	ca := map[collKey]bool{}
	for _, c := range a.NameCollisions {
		ca[collKey{c.Root, c.Name}] = true
	}
	for k := range ca {
		if !cb[k] {
			reg = append(reg, fmt.Sprintf("[NEW_NAME_COLLISION] %s @ %s", k.name, paths.Shorten(k.root)))
		}
	}
	for k := range cb {
		if !ca[k] {
			imp = append(imp, fmt.Sprintf("[COLLISION_RESOLVED] %s @ %s", k.name, paths.Shorten(k.root)))
		}
	}

	sort.Strings(reg)
	sort.Strings(imp)
	sort.Strings(notes)
	return reg, imp, notes
}

func usageErr(err error) int {
	fmt.Fprintf(os.Stderr, "error: %s\n", err)
	return 2
}

// ---------------------------------------------------------------- skillc's own skill page

// printSkillPage prints skillc presenting itself the way a SKILL.md presents
// any other skill: frontmatter-style header plus the usage an invoking agent
// keeps resident. This is what `skillc --skill` means -- skillc is itself a
// skill, and this is its page. --help is the product reference; this page is
// the onboarding guide an agent reads first.
func printSkillPage(w io.Writer) {
	fmt.Fprint(w, skillPage)
}

const skillPage = `---
name: skillc
description: Read-only skill compiler and health checker. Run this when you need
  the facts about this machine's agent skills -- what loads, what is broken,
  whether an edit regressed anything -- or before folding/deduplicating skills.
  It compiles skills and their references into one deterministic state.json,
  reports load-time correctness, and diffs two snapshots. It never rewrites
  skills.
---

# skillc

skillc observes and reports. It never tidies, rewrites, or guesses groupings --
those are your decisions, built on its state.

## When to use it

- After editing a skill: can it still be loaded and triggered?
- Before/after a cleanup or fold: did I break it, or was it already broken?
- Any question about the skills on this machine: what loads, where a skill is,
  what depends on it.

## How to drive it -- recipes by situation

"I just edited a skill -- can it still be loaded and triggered?"
  skillc check                 # compile + diagnostics; exit 1 on any error (drop into CI)
  skillc check --json          # machine-readable summary + diagnostics
  Error codes: NO_FRONTMATTER, NO_DESCRIPTION, BROKEN_REF, DANGLING_SYMLINK,
  NAME_COLLISION. Warns: NO_NAME, NAME_MISMATCH, LONG_DESCRIPTION.

"Did my round of edits make the tree worse, or was it already broken?"
  skillc build --out before.json    # snapshot before
  # ... edit skills ...
  skillc build --out after.json     # snapshot after
  skillc diff --before before.json --after after.json
  Only what after has and before lacks is a regression (exit 1); pre-existing
  debt goes to "notes". Schema mismatch refuses with exit 2, never miscomputes.

"What does this machine actually load, and how healthy is it right now?"
  skillc roots                 # the load roots discovery would scan, with counts
  skillc build --out state.json    # snapshot once, then answer without rescanning
  jq -r '.summary | "skills=\(.skill_count) broken=\(.broken_ref_count) "
       + "collisions=\(.name_collision_count) dangling=\(.broken_symlink_count)"' state.json

"Where is a skill, and what is its blast radius?"
  skillc query --skill NAME    # mounts, references, nesting, referenced-by count
  jq -r '.skills[]|select(.refs_in_count>0)|"\(.refs_in_count)\t\(.name)"' state.json | sort -rn
  jq -r '.skills[]|select((.name+" "+.description)|ascii_downcase|test("KW"))
       | .name' state.json     # find a skill by keyword

"I am folding or deduplicating a set of skills -- is it safe?"
  Fold = grouping flat skills behind one parent entry. Check after the move:
  the parent's contents grow, no NEW_BROKEN_REF appears in diff, and any member
  referenced from outside the set still resolves (it shows up in refs_in_count
  and the caller's edge). A copy present on several roots is identities.multi_mounted
  / same_content, not a missing skill.

## Scope flags (repeatable on build/check/query/roots)

  --only-root DIR   compile only these roots, skip discovery
  --extra-root DIR  append one load root to discovery

## What the state contains

state.json: roots, skills (keyed by realpath; name, description, provenance for
every mount, refs_in_count, refs_out, contains), edges (ref / contains),
broken_refs, external_refs, identities (multi_mounted / same_content),
name_collisions, diagnostics, summary. Full field contract: docs/state-contract.md.
`

// ---------------------------------------------------------------- help

const topDesc = `skillc -- a read-only skill compiler.

Compiles every skill this machine's agents load, plus their references, into one
deterministic state graph (state.json), and checks correctness. It observes and
reports; it never rewrites your skills.

The command set is small on purpose. build / check / diff do what you cannot
trivially reproduce by hand: scan the load roots, rank diagnostics, and subtract
two snapshots correctly. Descriptive questions -- find a skill, show its blast
radius, print a health line -- are one jq over state.json (see below), so they
are recipes, not frozen flags.
`

func printTopHelp(w io.Writer) {
	fmt.Fprint(w, topDesc)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "usage: skillc --skill | COMMAND [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "start here:")
	fmt.Fprintln(w, "  --skill    skillc's own skill page: what it is, when to use it,")
	fmt.Fprintln(w, "            and copy-paste recipes for every situation. This is")
	fmt.Fprintln(w, "            how an agent learns skillc; --help (this page) is the")
	fmt.Fprintln(w, "            compact product reference.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  build    compile the state graph JSON")
	fmt.Fprintln(w, "  check    compile + correctness diagnostics (exit 1 on error)")
	fmt.Fprintln(w, "  query    all relationships of one skill (--skill NAME)")
	fmt.Fprintln(w, "  roots    list the discovered load roots only")
	fmt.Fprintln(w, "  diff     compare two states, flag regressions (--before, --after)")
	fmt.Fprintln(w, "  viz      render the graph for viewing (html default; mermaid, dot)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "common flags:")
	fmt.Fprintln(w, "  --extra-root DIR  append one load root (repeatable)")
	fmt.Fprintln(w, "  --only-root DIR   compile only these roots (repeatable), skip discovery")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "state graph and recipes:")
	fmt.Fprintln(w, "  the state.json field contract plus jq recipes (find a skill,")
	fmt.Fprintln(w, "  blast radius, health) live in docs/state-contract.md.")
}
