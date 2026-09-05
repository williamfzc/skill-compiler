// Network rendering for human viewing: a force-directed reference graph.
//
// This is what "skillc viz --out graph.html" produces by default. One
// self-contained page -- files as dots colored by owning skill, references as
// edges (quoted = dashed), the layout run by a small vanilla-JS force
// simulation -- so it opens offline and supports drag / pan / zoom.
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

// HTML renders an interactive force-directed network: files as dots
// colored by owning skill, references as edges (quoted = dashed). It is one
// self-contained page -- the layout runs on a small vanilla-JS simulation,
// no external scripts -- so it opens offline and supports drag / pan / zoom.
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
	type nData struct {
		Skills []string `json:"skills"`
		Nodes  []nNode  `json:"nodes"`
		Links  []nLink  `json:"links"`
	}
	data := nData{Skills: sortedSkills(g)}
	skillIdx := map[string]int{}
	for i, sk := range data.Skills {
		skillIdx[sk] = i
	}
	nodeIdx := map[string]int{}
	var paths []string
	for p := range g.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fn := g.Files[p]
		nodeIdx[p] = len(data.Nodes)
		label := p
		if rel, err := filepath.Rel(fn.Owner, p); err == nil {
			label = rel
		}
		data.Nodes = append(data.Nodes, nNode{L: label, G: skillIdx[fn.Owner]})
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
	for _, e := range g.FileEdges {
		data.Links = append(data.Links, nLink{S: nodeIdx[e.From], T: nodeIdx[e.To], Q: e.Quoted})
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
		"padding:8px 10px;border-radius:8px;font-size:12px;line-height:1.7;max-height:90vh;overflow:auto}" +
		"#legend .dot{display:inline-block;width:9px;height:9px;border-radius:50%;margin-right:5px}" +
		"#hint{position:fixed;bottom:8px;left:8px;color:#666;font-size:12px}</style>\n")
	fmt.Fprintf(&b, "<div id=\"legend\"><b>skillc network</b><br>%d skills &middot; %d files &middot; %d edges &middot; %d broken<br>",
		s.SkillCount, s.FileCount, s.FileEdgeCount, s.BrokenRefCount)
	for i, sk := range data.Skills {
		fmt.Fprintf(&b, "<span class=\"dot\" style=\"background:%s\"></span>%s<br>",
			groupColor(i), html.EscapeString(sk))
	}
	b.WriteString("</div>\n<div id=\"hint\">drag node to move &middot; drag background to pan &middot; wheel to zoom</div>\n")
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

// networkJS is the force-simulation renderer embedded into HTML.
const networkJS = `
const data = JSON.parse(document.getElementById('graph-data').textContent);
const canvas = document.getElementById('network-canvas');
const ctx = canvas.getContext('2d');
let W, H, dpr;
function resize() {
  dpr = window.devicePixelRatio || 1;
  W = innerWidth; H = innerHeight;
  canvas.width = W * dpr; canvas.height = H * dpr;
  canvas.style.width = W + 'px'; canvas.style.height = H + 'px';
}
resize(); addEventListener('resize', resize);

const N = data.nodes.map((n, i) => ({
  x: W / 2 + Math.cos(i * 2.4) * (120 + i * 3),
  y: H / 2 + Math.sin(i * 2.4) * (120 + i * 3),
  vx: 0, vy: 0, g: n.g, l: n.l,
}));
const deg = N.map(() => 0);
for (const l of data.links) { deg[l.s]++; deg[l.t]++; }

let alpha = 1;
let scale = 1, ox = 0, oy = 0;
function tick() {
  alpha *= 0.995; if (alpha < 0.03) alpha = 0.03;
  for (let i = 0; i < N.length; i++) {
    const a = N[i];
    for (let j = i + 1; j < N.length; j++) {
      const b = N[j];
      let dx = a.x - b.x, dy = a.y - b.y;
      let d2 = dx * dx + dy * dy;
      if (d2 < 1) { dx = Math.random() - 0.5; dy = Math.random() - 0.5; d2 = 1; }
      if (d2 > 250000) continue;
      const f = 1800 * alpha / d2;
      const d = Math.sqrt(d2);
      a.vx += dx / d * f; a.vy += dy / d * f;
      b.vx -= dx / d * f; b.vy -= dy / d * f;
    }
    a.vx -= (a.x - W / 2) * 0.0008 * alpha;
    a.vy -= (a.y - H / 2) * 0.0008 * alpha;
  }
  for (const l of data.links) {
    const a = N[l.s], b = N[l.t];
    let dx = b.x - a.x, dy = b.y - a.y;
    const d = Math.sqrt(dx * dx + dy * dy) || 1;
    const f = (d - 70) * 0.02 * alpha;
    a.vx += dx / d * f; a.vy += dy / d * f;
    b.vx -= dx / d * f; b.vy -= dy / d * f;
  }
  for (let i = 0; i < N.length; i++) {
    const n = N[i];
    if (n === dragNode) { n.vx = n.vy = 0; continue; }
    n.vx *= 0.85; n.vy *= 0.85;
    n.x += n.vx; n.y += n.vy;
  }
}

let hover = -1;
let dragNode = null, panning = false, px = 0, py = 0;
function draw() {
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, W, H);
  ctx.fillStyle = "#fff"; ctx.fillRect(0, 0, W, H);
  ctx.save();
  ctx.translate(ox, oy); ctx.scale(scale, scale);
  for (const l of data.links) {
    const a = N[l.s], b = N[l.t];
    const hot = hover >= 0 && (l.s === hover || l.t === hover);
    ctx.strokeStyle = hot ? '#d33' : (hover >= 0 ? 'rgba(170,170,170,0.25)' : 'rgba(140,140,140,0.6)');
    ctx.lineWidth = hot ? 1.6 : 1;
    if (l.q) ctx.setLineDash([3, 3]); else ctx.setLineDash([]);
    ctx.beginPath(); ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y); ctx.stroke();
  }
  ctx.setLineDash([]);
  for (let i = 0; i < N.length; i++) {
    const n = N[i];
    const r = 3 + Math.sqrt(deg[i]) * 1.6;
    const dim = hover >= 0 && hover !== i &&
      !data.links.some(l => (l.s === hover && l.t === i) || (l.t === hover && l.s === i));
    ctx.globalAlpha = hover >= 0 && dim ? 0.25 : 1;
    ctx.fillStyle = n.g < 0 ? '#999' : 'hsl(' + ((n.g * 137 + 31) % 360) + ',62%,52%)';
    ctx.beginPath(); ctx.arc(n.x, n.y, r, 0, 6.29); ctx.fill();
    if (hover === i) {
      ctx.strokeStyle = '#d33'; ctx.lineWidth = 2;
      ctx.beginPath(); ctx.arc(n.x, n.y, r + 3, 0, 6.29); ctx.stroke();
    }
    if (scale > 0.7 || deg[i] > 2) {
      ctx.fillStyle = '#333'; ctx.font = '10px sans-serif'; ctx.textAlign = 'center';
      ctx.fillText(n.l, n.x, n.y - r - 3);
    }
  }
  ctx.globalAlpha = 1;
  ctx.restore();
}
function frame() { tick(); draw(); requestAnimationFrame(frame); }
frame();


function at(x, y) {
  const wx = (x - ox) / scale, wy = (y - oy) / scale;
  let best = -1, bd = 100;
  for (let i = 0; i < N.length; i++) {
    const dx = N[i].x - wx, dy = N[i].y - wy;
    const d = dx * dx + dy * dy;
    if (d < bd) { bd = d; best = i; }
  }
  return best;
}
canvas.onmousedown = e => {
  const i = at(e.offsetX, e.offsetY);
  if (i >= 0) { dragNode = N[i]; dragNode.fixed = true; }
  else { panning = true; px = e.offsetX; py = e.offsetY; }
};
canvas.onmousemove = e => {
  if (dragNode) {
    dragNode.x = (e.offsetX - ox) / scale;
    dragNode.y = (e.offsetY - oy) / scale;
    alpha = Math.max(alpha, 0.3);
  } else if (panning) {
    ox += e.offsetX - px; oy += e.offsetY - py; px = e.offsetX; py = e.offsetY;
  } else {
    hover = at(e.offsetX, e.offsetY);
  }
};
addEventListener('mouseup', () => { dragNode = null; panning = false; });
canvas.onwheel = e => {
  e.preventDefault();
  const k = e.deltaY < 0 ? 1.12 : 1 / 1.12;
  ox = e.offsetX - (e.offsetX - ox) * k;
  oy = e.offsetY - (e.offsetY - oy) * k;
  scale *= k;
};
`
