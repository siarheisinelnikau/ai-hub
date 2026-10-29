/* ═══════════════════════════════════════════════════════════════════
   charts.js — live chart rendering system (requestAnimationFrame loop)
   ═══════════════════════════════════════════════════════════════════ */

const CHART_WINDOW = 120;  // seconds of history shown
const _specs = new Map();  // chart id -> {series:[{color,pts}], fmt, padL}
let _rafOn = false;

function setChart(id, series, opts) {
  opts = opts || {};
  _specs.set(id, { series, fmt: opts.fmt || null, padL: opts.padL != null ? opts.padL : 34, win: opts.win != null ? opts.win: CHART_WINDOW });
  ensureChartLoop();
}
function dropChart(id) { _specs.delete(id); }

function ensureChartLoop() {
  if (_rafOn) return;
  _rafOn = true;
  requestAnimationFrame(function tick() {
    if (!document.hidden) {
      const now = Date.now() / 1000;
      for (const [id, spec] of _specs) {
        const cv = document.getElementById(id);
        if (cv) drawLive(cv, spec, now);
      }
    }
    requestAnimationFrame(tick);
  });
}

function drawLive(cv, spec, now) {
  const dpr = window.devicePixelRatio || 1;
  const W = cv.clientWidth || 200, H = cv.clientHeight || 64;
  const pw = Math.round(W * dpr), ph = Math.round(H * dpr);
  if (cv.width !== pw || cv.height !== ph) { cv.width = pw; cv.height = ph; }
  const ctx = cv.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, W, H);
  const WIN = spec.win != null ? spec.win : CHART_WINDOW;
  const ts0 = now - WIN, ts1 = now;
  const all = [];
  for (const s of spec.series) for (const p of s.pts) { if (p[0] >= ts0 && p[0] <= ts1) all.push(p[1]); }
  if (!all.length) return;
  const mn = Math.min.apply(null, all), mx = Math.max.apply(null, all);
  let lo = mn - ((mx - mn) || mx || 1) * 0.12; lo = Math.max(0, lo);
  let hi = mx + ((mx - mn) || mx || 1) * 0.12;
  if (hi <= lo) hi = lo + 1;
  const padL = spec.padL, padR = 8, padT = 7, padB = 6;
  const cw = W - padL - padR, ch = H - padT - padB;
  const X = ts => padL + ((ts - ts0) / (ts1 - ts0)) * cw;
  const Y = v => padT + ch * (1 - (v - lo) / (hi - lo));
  const fmt = spec.fmt || (v => String(Math.round(v)));
  ctx.font = '9px "JetBrains Mono",monospace';
  for (let i = 0; i <= 4; i++) {
    const v = lo + (hi - lo) * i / 4, y = Y(v);
    ctx.strokeStyle = 'rgba(148,163,184,0.13)'; ctx.lineWidth = 1;
    ctx.beginPath(); ctx.moveTo(padL, y); ctx.lineTo(W - padR, y); ctx.stroke();
    ctx.fillStyle = 'rgba(148,163,184,0.6)'; ctx.textAlign = 'right';
    ctx.fillText(fmt(v), padL - 4, y + 3);
  }
  for (const s of spec.series) {
    const pts = [];
    for (const p of s.pts) if (p[0] >= ts0 && p[0] <= ts1) pts.push(p);
    if (pts.length < 1) continue;
    ctx.strokeStyle = s.color; ctx.lineWidth = 1.7; ctx.lineJoin = 'round'; ctx.lineCap = 'round';
    ctx.beginPath(); ctx.moveTo(X(pts[0][0]), Y(pts[0][1]));
    for (let i = 1; i < pts.length; i++) ctx.lineTo(X(pts[i][0]), Y(pts[i][1]));
    ctx.stroke();
    const lp = pts[pts.length - 1];
    ctx.fillStyle = s.color; ctx.beginPath(); ctx.arc(X(lp[0]), Y(lp[1]), 2.4, 0, 7); ctx.fill();
  }
}

function tpsAxis(v) { return v >= 1000 ? (v / 1000).toFixed(1) + 'k' : '' + Math.round(v); }
function recentPts(arr, now) { const cut = now - CHART_WINDOW - 15; return arr.filter(p => p[0] >= cut); }
