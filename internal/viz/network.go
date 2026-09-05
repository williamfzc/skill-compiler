// Network rendering for human viewing: a force-directed reference graph.
//
// This is what "skillc viz --out graph.html" produces by default. One
// self-contained page with two views: an overview (one dot per skill, sized
// by file count, edges only between skills) and a per-skill detail (that
// skill's files plus whatever they reference). Click a skill to drill in;
// the back button returns. Vanilla-JS force simulation, no external scripts.
package viz

import (
	"encoding/json"
	"fmt"
	"html"
	"path/filepath"
	"sort"
	"strings"

	"skillscope/internal/state"
)

// HTML renders the interactive overview + drill-down network page.
func HTML(g *state.Graph) string {
	type nNode struct {
		L string `json:"l"` // display label
		G int    `json:"g"` // skill index, -1 = outside any skill
	}
	type nLink struct {
		S int  `json:"s"`
		T int  `json:"t"`
		Q bool `json:"q,omitempty"`
	}
	type nSkill struct {
		N  string `json:"n"`  // name
		Fc int    `json:"fc"` // file count
	}
	type nData struct {
		Skills []nSkill `json:"skills"`
		Sl     [][2]int `json:"sl"` // skill-level links, aggregated cross-skill refs
		Nodes  []nNode  `json:"nodes"`
		Links  []nLink  `json:"links"`
	}

	skills := sortedSkills(g)
	skillIdx := map[string]int{}
	for i, sk := range skills {
		skillIdx[sk] = i
	}

	nodeIdx := map[string]int{}
	var paths []string
	for p := range g.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	// Sl and Links must marshal as [] not null: the page script calls .map
	// on both unconditionally, and a machine may genuinely have zero
	// cross-skill references.
	data := nData{
		Skills: make([]nSkill, len(skills)),
		Sl:     [][2]int{},
		Links:  []nLink{},
	}
	for i, sk := range skills {
		data.Skills[i] = nSkill{N: skillName(sk)}
	}
	for _, p := range paths {
		fn := g.Files[p]
		nodeIdx[p] = len(data.Nodes)
		label := p
		if rel, err := filepath.Rel(fn.Owner, p); err == nil {
			label = rel
		}
		data.Nodes = append(data.Nodes, nNode{L: label, G: skillIdx[fn.Owner]})
		data.Skills[skillIdx[fn.Owner]].Fc++
	}
	var extra []string
	seen := map[string]bool{}
	for _, e := range g.FileEdges {
		for _, p := range []string{e.From, e.To} {
			if _, ok := nodeIdx[p]; ok || seen[p] {
				continue
			}
			seen[p] = true
			extra = append(extra, p)
		}
	}
	sort.Strings(extra)
	for _, p := range extra {
		nodeIdx[p] = len(data.Nodes)
		data.Nodes = append(data.Nodes, nNode{L: filepath.Base(p), G: -1})
	}
	slSeen := map[[2]int]bool{}
	for _, e := range g.FileEdges {
		data.Links = append(data.Links, nLink{S: nodeIdx[e.From], T: nodeIdx[e.To], Q: e.Quoted})
		fs, ts := e.FromSkill, e.ToSkill
		if fs == "" || ts == "" || fs == ts {
			continue
		}
		key := [2]int{skillIdx[fs], skillIdx[ts]}
		if key[0] > key[1] {
			key[0], key[1] = key[1], key[0]
		}
		if !slSeen[key] {
			slSeen[key] = true
			data.Sl = append(data.Sl, key)
		}
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	s := g.Summary
	var b strings.Builder
	b.WriteString("<!doctype html>\n<meta charset=\"utf-8\">\n<title>skillc graph</title>\n")
	b.WriteString("<style>body{font-family:-apple-system,sans-serif;margin:0;overflow:hidden}" +
		"canvas{display:block}#legend{position:fixed;top:8px;left:8px;background:#fffc;" +
		"padding:8px 10px;border-radius:8px;font-size:12px;line-height:1.7;max-height:90vh;" +
		"overflow:auto}#legend .dot{display:inline-block;width:9px;height:9px;border-radius:50%;" +
		"margin-right:5px}#legend .sk{cursor:pointer}#legend .sk:hover{text-decoration:underline}" +
		"#hint{position:fixed;bottom:8px;left:8px;color:#666;font-size:12px}" +
		"#skill-back{position:fixed;top:8px;right:8px;background:#fff;border:1px solid #ccc;" +
		"border-radius:8px;padding:6px 12px;font-size:13px;cursor:pointer;display:none}</style>\n")
	fmt.Fprintf(&b, "<div id=\"legend\"><b id=\"skill-overview\">skillc network</b>"+
		"<br>%d skills &middot; %d files &middot; %d edges &middot; %d broken<br>",
		s.SkillCount, s.FileCount, s.FileEdgeCount, s.BrokenRefCount)
	for i, sk := range data.Skills {
		fmt.Fprintf(&b, "<span class=\"sk\" data-i=\"%d\" onclick=\"enter(%d)\">"+
			"<span class=\"dot\" style=\"background:%s\"></span>%s <span style=\"color:#999\">(%d)</span></span><br>",
			i, i, groupColor(i), html.EscapeString(sk.N), sk.Fc)
	}
	b.WriteString("</div>\n<div id=\"skill-back\" onclick=\"enter(-1)\">&larr; all skills</div>\n")
	b.WriteString("<div id=\"hint\">click a skill to drill in &middot; drag to move &middot; drag background to pan &middot; wheel to zoom</div>\n")
	fmt.Fprintf(&b, "<canvas id=\"network-canvas\"></canvas>\n")
	b.WriteString("<script id=\"graph-data\" type=\"application/json\">")
	b.Write(encoded)
	b.WriteString("</script>\n")
	b.WriteString("<script>\n" + networkJS + "</script>\n")
	return b.String()
}

// groupColor gives each skill a stable, well-separated hue.
func groupColor(i int) string {
	return fmt.Sprintf("hsl(%d,62%%,52%%)", (i*137+31)%360)
}
