// Package diagnostics turns facts into correctness findings.
//
// The four kinds of load-time correctness (loadable, name-unique, refs-intact,
// symlink-alive) become error/warn diagnostics here, and only here. Every
// finding carries a stable code, a human-readable location, and a detail line.
package diagnostics

import (
	"sort"
	"strconv"
	"strings"

	"skillscope/internal/analysis"
	"skillscope/internal/collect"
	"skillscope/internal/edges"
	"skillscope/internal/paths"
)

// Diagnostic is one correctness finding, pre-sorted error-first.
type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Skill    string `json:"skill"`
	Where    string `json:"where"`
	Detail   string `json:"detail"`
}

var sevOrder = map[string]int{"error": 0, "warn": 1, "info": 2}

// Diagnose converts broken refs, node facts, and collisions into ranked
// diagnostics.
func Diagnose(nodes map[string]*collect.Node, broken []edges.BrokenRef,
	collisions []analysis.Collision) []Diagnostic {
	d := []Diagnostic{}

	where := func(nid string) string {
		n := nodes[nid]
		root, rel := "", nid
		if n != nil && len(n.Provenance) > 0 {
			root = n.Provenance[0].Root
			rel = n.Provenance[0].Rel
		}
		if root != "" {
			return paths.Shorten(root) + "/" + rel
		}
		return paths.Shorten(nid)
	}

	for _, b := range broken {
		d = append(d, Diagnostic{
			Severity: "error", Code: "BROKEN_REF", Skill: b.From,
			Where:  where(b.From),
			Detail: b.FromFile + " has an unresolvable reference: " + b.Raw,
		})
	}

	nodeIDs := make([]string, 0, len(nodes))
	for nid := range nodes {
		nodeIDs = append(nodeIDs, nid)
	}
	sort.Strings(nodeIDs)
	for _, nid := range nodeIDs {
		d = append(d, nodeDiagnostics(nid, nodes[nid], where)...)
	}

	for _, c := range collisions {
		d = append(d, Diagnostic{
			Severity: "error", Code: "NAME_COLLISION", Skill: c.Skills[0],
			Where: paths.Shorten(c.Root),
			Detail: itoa(len(c.Skills)) + " skills in one root are all named '" +
				c.Name + "': " + strings.Join(c.Rels, ", "),
		})
	}

	sort.SliceStable(d, func(i, j int) bool {
		if sevOrder[d[i].Severity] != sevOrder[d[j].Severity] {
			return sevOrder[d[i].Severity] < sevOrder[d[j].Severity]
		}
		if d[i].Code != d[j].Code {
			return d[i].Code < d[j].Code
		}
		return d[i].Where < d[j].Where
	})
	return d
}

func nodeDiagnostics(nid string, n *collect.Node, where func(string) string) []Diagnostic {
	var out []Diagnostic
	add := func(sev, code, detail string) {
		out = append(out, Diagnostic{
			Severity: sev, Code: code, Skill: nid, Where: where(nid), Detail: detail,
		})
	}
	if n.BrokenSymlink {
		target := ""
		if n.SymlinkTarget != nil {
			target = *n.SymlinkTarget
		}
		add("error", "DANGLING_SYMLINK", "symlink points at a missing target: "+target)
		return out
	}
	if !n.HasFrontmatter {
		add("error", "NO_FRONTMATTER", "SKILL.md has no frontmatter; it cannot be loaded")
	} else {
		if n.NameField == "" {
			add("warn", "NO_NAME", "frontmatter is missing the name field")
		}
		if n.Description == "" {
			add("error", "NO_DESCRIPTION",
				"frontmatter is missing description; it cannot be triggered")
		}
	}
	if n.NameField != "" && n.NameField != n.Name {
		add("warn", "NAME_MISMATCH",
			"name field '"+n.NameField+"' does not match directory name '"+n.Name+"'")
	}
	if n.IsSymlink && n.SymlinkOK != nil && !*n.SymlinkOK {
		target := ""
		if n.SymlinkTarget != nil {
			target = *n.SymlinkTarget
		}
		add("error", "DANGLING_SYMLINK", "symlink points at a missing target: "+target)
	}
	if n.DescChars > 1536 {
		add("warn", "LONG_DESCRIPTION",
			"description is "+strconv.Itoa(n.DescChars)+" chars; may be truncated by budget")
	}
	return out
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0)) + intToString(n)
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
