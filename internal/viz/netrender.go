package viz

// networkJS is the embedded page script for HTML: the two-view force
// simulation (skill overview + per-skill detail) with drag / pan / zoom.
const networkJS = `
const data = JSON.parse(document.getElementById('graph-data').textContent);
const canvas = document.getElementById('network-canvas');
const ctx = canvas.getContext('2d');
const backBtn = document.getElementById('skill-back');
let W, H, dpr;
function resize() {
  dpr = window.devicePixelRatio || 1;
  W = innerWidth; H = innerHeight;
  canvas.width = W * dpr; canvas.height = H * dpr;
  canvas.style.width = W + 'px'; canvas.style.height = H + 'px';
}
resize(); addEventListener('resize', resize);

function hue(g) { return 'hsl(' + ((g * 137 + 31) % 360) + ',62%,52%)'; }

// Persistent particles: skill dots and file dots keep positions across views.
const SK = data.skills.map((s, i) => ({
  x: innerWidth / 2 + Math.cos(i * 2.4) * 180,
  y: innerHeight / 2 + Math.sin(i * 2.4) * 140,
  vx: 0, vy: 0, l: s.n, g: i, fc: s.fc, kind: 'skill', i,
}));
const F = data.nodes.map((n, i) => ({
  x: innerWidth / 2 + Math.cos(i * 2.4) * (120 + (i % 7) * 20),
  y: innerHeight / 2 + Math.sin(i * 2.4) * (120 + (i % 5) * 24),
  vx: 0, vy: 0, l: n.l, g: n.g, kind: 'file', i,
}));

// Active view: P = particles, L = links between them (local indices).
let mode = 'overview';
let P = [], L = [];
function setView(m) {
  mode = m; alpha = 1;
  if (m === 'overview') {
    P = SK;
    L = data.sl.map(l => ({ a: l[0], b: l[1], q: false }));
    backBtn.style.display = 'none';
  } else {
    const keep = new Set();
    for (const l of data.links) {
      const gs = data.nodes[l.s].g, gt = data.nodes[l.t].g;
      if (gs === m || gt === m) { keep.add(l.s); keep.add(l.t); }
    }
    const idx = new Map();
    let np = 0;
    for (const i of keep) { idx.set(i, np); np++; }
    P = [...keep].map(i => F[i]);
    L = [];
    for (const l of data.links) {
      if (keep.has(l.s) && keep.has(l.t)) L.push({ a: idx.get(l.s), b: idx.get(l.t), q: l.q });
    }
    backBtn.style.display = 'block';
  }
  scale = 1; ox = 0; oy = 0; hover = -1;
}

let alpha = 1, scale = 1, ox = 0, oy = 0;
let hover = -1, dragNode = null, panning = false, px = 0, py = 0, downX = 0, downY = 0;

function tick() {
  alpha *= 0.995; if (alpha < 0.03) alpha = 0.03;
  for (let i = 0; i < P.length; i++) {
    const a = P[i];
    for (let j = i + 1; j < P.length; j++) {
      const b = P[j];
      let dx = a.x - b.x, dy = a.y - b.y;
      let d2 = dx * dx + dy * dy;
      if (d2 < 1) { dx = Math.random() - 0.5; dy = Math.random() - 0.5; d2 = 1; }
      if (d2 > 400000) continue;
      const f = (mode === 'overview' ? 6000 : 1800) * alpha / d2;
      const d = Math.sqrt(d2);
      a.vx += dx / d * f; a.vy += dy / d * f;
      b.vx -= dx / d * f; b.vy -= dy / d * f;
    }
    a.vx -= (a.x - W / 2) * 0.001 * alpha;
    a.vy -= (a.y - H / 2) * 0.001 * alpha;
  }
  for (const l of L) {
    const a = P[l.a], b = P[l.b];
    const dx = b.x - a.x, dy = b.y - a.y;
    const d = Math.sqrt(dx * dx + dy * dy) || 1;
    const f = (d - (mode === 'overview' ? 160 : 70)) * 0.02 * alpha;
    a.vx += dx / d * f; a.vy += dy / d * f;
    b.vx -= dx / d * f; b.vy -= dy / d * f;
  }
  for (const n of P) {
    if (n === dragNode) { n.vx = n.vy = 0; continue; }
    n.vx *= 0.85; n.vy *= 0.85;
    n.x += n.vx; n.y += n.vy;
  }
}

function radius(n, deg) {
  return n.kind === 'skill' ? 7 + Math.sqrt(n.fc) * 3 : 3 + Math.sqrt(deg) * 1.6;
}

function draw() {
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, W, H);
  ctx.fillStyle = '#fff'; ctx.fillRect(0, 0, W, H);
  ctx.save();
  ctx.translate(ox, oy); ctx.scale(scale, scale);
  const deg = P.map(() => 0);
  for (const l of L) { deg[l.a]++; deg[l.b]++; }
  for (const l of L) {
    const a = P[l.a], b = P[l.b];
    const hot = hover >= 0 && (l.a === hover || l.b === hover);
    ctx.strokeStyle = hot ? '#d33' : (hover >= 0 ? 'rgba(170,170,170,0.25)' : 'rgba(140,140,140,0.6)');
    ctx.lineWidth = hot ? 1.8 : 1;
    if (l.q) ctx.setLineDash([3, 3]); else ctx.setLineDash([]);
    ctx.beginPath(); ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y); ctx.stroke();
  }
  ctx.setLineDash([]);
  for (let i = 0; i < P.length; i++) {
    const n = P[i], r = radius(n, deg[i]);
    const dim = hover >= 0 && hover !== i &&
      !L.some(l => (l.a === hover && l.b === i) || (l.b === hover && l.a === i));
    ctx.globalAlpha = hover >= 0 && dim ? 0.25 : 1;
    ctx.fillStyle = (n.kind === 'file' && n.g < 0) ? '#999' : hue(n.g < 0 ? -n.g + 17 : n.g);
    ctx.beginPath(); ctx.arc(n.x, n.y, r, 0, 6.29); ctx.fill();
    if (hover === i) {
      ctx.strokeStyle = '#d33'; ctx.lineWidth = 2;
      ctx.beginPath(); ctx.arc(n.x, n.y, r + 3, 0, 6.29); ctx.stroke();
    }
    ctx.fillStyle = '#333'; ctx.font = n.kind === 'skill' ? '12px sans-serif' : '10px sans-serif';
    ctx.textAlign = 'center';
    ctx.fillText(n.l + (n.kind === 'skill' ? ' (' + n.fc + ')' : ''), n.x, n.y - r - 4);
  }
  ctx.globalAlpha = 1;
  ctx.restore();
}
function frame() { tick(); draw(); requestAnimationFrame(frame); }
setView('overview');
frame();

function at(x, y) {
  const wx = (x - ox) / scale, wy = (y - oy) / scale;
  let best = -1, bd = 400;
  for (let i = 0; i < P.length; i++) {
    const dx = P[i].x - wx, dy = P[i].y - wy;
    const d = dx * dx + dy * dy;
    if (d < bd) { bd = d; best = i; }
  }
  return best;
}
function enter(m) { setView(m < 0 ? 'overview' : m); }

canvas.onmousedown = e => {
  downX = e.offsetX; downY = e.offsetY;
  const i = at(e.offsetX, e.offsetY);
  if (i >= 0) dragNode = P[i];
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
addEventListener('mouseup', e => {
  const moved = Math.abs(e.offsetX - downX) + Math.abs(e.offsetY - downY);
  if (moved < 5 && mode === 'overview') {
    const i = at(e.offsetX, e.offsetY);
    if (i >= 0) enter(P[i].i);
  }
  dragNode = null; panning = false;
});
canvas.onwheel = e => {
  e.preventDefault();
  const k = e.deltaY < 0 ? 1.12 : 1 / 1.12;
  ox = e.offsetX - (e.offsetX - ox) * k;
  oy = e.offsetY - (e.offsetY - oy) * k;
  scale *= k;
};
`
