// Package viz renders the compiled state graph for human viewing.
//
// Three formats, one deterministic layout each: Mermaid flowchart text,
// Graphviz DOT, and a self-contained HTML page that wraps the Mermaid text
// (open it in a browser; rendering uses the Mermaid CDN, and offline the raw
// text still shows). Files are grouped under their owning skill; quoted
// references (inline-code paths) render dashed -- a weaker, resolved-only
// kind of link.
package viz

import (
	"fmt"
	"html"
	"path/filepath"
	"sort"
	"strings"

	"skillscope/internal/state"
)

// Mermaid renders the graph as a Mermaid flowchart.
func Mermaid(g *state.Graph) string {
	ids, names := fileIDs(g)
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	defined := map[string]bool{}
	for _, sk := range sortedSkills(g) {
		fmt.Fprintf(&b, "  subgraph %s[\"%s\"]\n", mermaidID(sk), skillName(sk))
		for _, id := range idsByOwner(g, ids, sk) {
			defined[id] = true
			fmt.Fprintf(&b, "    %s[\"%s\"]\n", id, names[id])
		}
		b.WriteString("  end\n")
	}
	// Endpoints outside any skill are not in a subgraph; define them bare.
	for _, id := range extraIDs(ids, defined) {
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", id, names[id])
	}
	for _, e := range g.FileEdges {
		style := "-->"
		if e.Quoted {
			style = "-.->"
		}
		fmt.Fprintf(&b, "  %s %s %s\n", ids[e.From], style, ids[e.To])
	}
	return b.String()
}

// DOT renders the graph as Graphviz DOT.
func DOT(g *state.Graph) string {
	ids, names := fileIDs(g)
	var b strings.Builder
	b.WriteString("digraph skills {\n  rankdir=LR;\n  node [shape=box, style=rounded];\n")
	defined := map[string]bool{}
	for _, sk := range sortedSkills(g) {
		fmt.Fprintf(&b, "  subgraph cluster_%s {\n    label=%s;\n", dotID(sk), dotQuote(skillName(sk)))
		for _, id := range idsByOwner(g, ids, sk) {
			defined[id] = true
			fmt.Fprintf(&b, "    %s [label=%s];\n", id, dotQuote(names[id]))
		}
		b.WriteString("  }\n")
	}
	for _, id := range extraIDs(ids, defined) {
		fmt.Fprintf(&b, "  %s [label=%s];\n", id, dotQuote(names[id]))
	}
	for _, e := range g.FileEdges {
		style := ""
		if e.Quoted {
			style = ", style=dashed"
		}
		fmt.Fprintf(&b, "  %s -> %s [label=%s%s];\n",
			ids[e.From], ids[e.To], dotQuote(e.Raw), style)
	}
	b.WriteString("}\n")
	return b.String()
}

// extraIDs lists ids not yet defined by a subgraph, in id order.
func extraIDs(ids map[string]string, defined map[string]bool) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		if !defined[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// HTML renders a self-contained page around the Mermaid text. Rendering
// needs the Mermaid CDN; offline, the raw text remains visible in the page.
func HTML(g *state.Graph) string {
	s := g.Summary
	mm := Mermaid(g)
	var b strings.Builder
	b.WriteString("<!doctype html>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<title>skillc graph</title>\n")
	b.WriteString("<style>body{font-family:-apple-system,sans-serif;margin:2rem;background:#fafafa}" +
		".mermaid{background:#fff;border:1px solid #ddd;border-radius:8px;padding:1rem}</style>\n")
	fmt.Fprintf(&b, "<h1>skillc graph</h1>\n<p>%d skills, %d files, %d file edges, %d broken refs</p>\n",
		s.SkillCount, s.FileCount, s.FileEdgeCount, s.BrokenRefCount)
	b.WriteString("<div class=\"mermaid\">\n")
	b.WriteString(html.EscapeString(mm))
	b.WriteString("\n</div>\n")
	b.WriteString("<script src=\"https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js\"></script>\n")
	b.WriteString("<script>mermaid.initialize({startOnLoad:true,maxTextSize:9000000});</script>\n")
	return b.String()
}

// fileIDs assigns a deterministic id (f0, f1, ...) to every file node, sorted
// by realpath, plus its display name (path relative to the owning skill).
// Edge endpoints that resolve outside any skill are not file nodes, but they
// get ids too -- labeled by basename -- so their edges stay visible instead
// of rendering with an empty endpoint.
func fileIDs(g *state.Graph) (map[string]string, map[string]string) {
	ids := map[string]string{}
	names := map[string]string{}
	var paths []string
	for p := range g.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for i, p := range paths {
		id := fmt.Sprintf("f%d", i)
		ids[p] = id
		names[id] = fileDisplayName(g, p)
	}
	var extra []string
	seen := map[string]bool{}
	for _, e := range g.FileEdges {
		for _, p := range []string{e.From, e.To} {
			if _, ok := ids[p]; ok || seen[p] {
				continue
			}
			seen[p] = true
			extra = append(extra, p)
		}
	}
	sort.Strings(extra)
	for i, p := range extra {
		id := fmt.Sprintf("f%d", len(paths)+i)
		ids[p] = id
		names[id] = filepath.Base(p)
	}
	return ids, names
}

func fileDisplayName(g *state.Graph, p string) string {
	fn := g.Files[p]
	if fn == nil {
		return p
	}
	if rel, err := filepath.Rel(fn.Owner, p); err == nil {
		return rel
	}
	return p
}

// sortedSkills lists owning-skill realpaths deterministically.
func sortedSkills(g *state.Graph) []string {
	seen := map[string]bool{}
	var out []string
	for _, fn := range g.Files {
		if !seen[fn.Owner] {
			seen[fn.Owner] = true
			out = append(out, fn.Owner)
		}
	}
	sort.Strings(out)
	return out
}

func idsByOwner(g *state.Graph, ids map[string]string, owner string) []string {
	var out []string
	for p, fn := range g.Files {
		if fn.Owner == owner {
			out = append(out, ids[p])
		}
	}
	sort.Strings(out)
	return out
}

func skillName(realpath string) string {
	i := strings.LastIndex(realpath, "/")
	if i < 0 {
		return realpath
	}
	return realpath[i+1:]
}

func mermaidID(realpath string) string { return "s" + dotID(realpath) }

// dotID renders a safe alphanumeric id from a path.
func dotID(realpath string) string {
	var b strings.Builder
	for _, r := range realpath {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func dotQuote(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s) + "\""
}
