/* ═══════════════════════════════════════════════════════════════════
   stats.js — live stats modal (KPI cards + 3 stacked charts)
   ═══════════════════════════════════════════════════════════════════ */

let _statsTimer = null;

async function showStats(mid) {
  if (_statsTimer) { clearInterval(_statsTimer); _statsTimer = null; }
  document.querySelectorAll('.stats-modal-overlay').forEach(o => o.remove());
  const mdl = (S.cache?.models || []).find(x => x.id === mid);
  const name = mdl ? (mdl.short || mdl.name) : mid;
  const wrap = document.createElement('div');
  wrap.className = 'stats-modal';
  wrap.innerHTML = `<div class="modal" style="padding:25px 30px 22px">
    <div class="stats-header"><span style="font-size:19px;font-weight:700">📈 Live Stats — ${name}</span><span class="live-dot" style="margin-left:auto" title="Live — polls every 2s"></span><span style="font-size:15px;color:var(--fg3)" id="stats-time"></span></div>
    <div class="stats-body">
      <div class="stats-kpi">
        <div class="stats-kpi-item"><div class="stats-kpi-label">TPS (decode)</div><div class="stats-kpi-val" id="kpi-tps">—<span class="stats-kpi-unit">tok/s</span></div></div>
        <div class="stats-kpi-item"><div class="stats-kpi-label">Prompt TPS</div><div class="stats-kpi-val" id="kpi-prompt">—<span class="stats-kpi-unit">tok/s</span></div></div>
        <div class="stats-kpi-item"><div class="stats-kpi-label">Context</div><div class="stats-kpi-val" id="kpi-ctx">—<span class="stats-kpi-unit">tokens</span></div></div>
        <div class="stats-kpi-item"><div class="stats-kpi-label">Total Tokens</div><div class="stats-kpi-val" id="kpi-tokens">—</div></div>
        <div class="stats-kpi-item"><div class="stats-kpi-label">Queue</div><div class="stats-kpi-val" id="kpi-queue">—</div></div>
      </div>
      <div class="stats-charts">
        <div class="stats-chart-box"><div class="stats-chart-title" style="color:#f59e0b"><span class="dot" style="background:#f59e0b"></span>Context (tokens)</div><canvas id="chartTokens"></canvas></div>
        <div class="stats-chart-box"><div class="stats-chart-title" style="color:#22c55e"><span class="dot" style="background:#22c55e"></span>Decode TPS</div><canvas id="chartDecode"></canvas></div>
        <div class="stats-chart-box"><div class="stats-chart-title" style="color:#3b82f6"><span class="dot" style="background:#3b82f6"></span>Prompt TPS</div><canvas id="chartPrompt"></canvas></div>
      </div>
    </div>
    <div style="text-align:right;margin-top:13px"><button class="btn btn-ghost" onclick="closeStats()">Close</button></div>
  </div>`;
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay stats-modal-overlay';
  overlay.appendChild(wrap);
  $('modal-root').appendChild(overlay);
  overlay.addEventListener('click', e => { if (e.target === overlay) closeStats(); });

  const statsEsc = e => { if (e.key === 'Escape') closeStats(); };
  const statsVis = () => {
    if (document.hidden) { if (_statsTimer) { clearInterval(_statsTimer); _statsTimer = null; } }
    else if (!_statsTimer) { pollStats(); _statsTimer = setInterval(pollStats, 2000); }
  };
  function closeStats() {
    if (_statsTimer) { clearInterval(_statsTimer); _statsTimer = null; }
    document.removeEventListener('keydown', statsEsc);
    document.removeEventListener('visibilitychange', statsVis);
    overlay.remove();
  }
  document.addEventListener('keydown', statsEsc);
  document.addEventListener('visibilitychange', statsVis);
  window.closeStats = closeStats;

  // Monotone cubic (Fritsch–Butland) for smooth chart lines
  function monotoneLine(ctx, pts) {
    const n = pts.length;
    if (n < 2) return;
    ctx.beginPath();
    ctx.moveTo(pts[0].x, pts[0].y);
    if (n === 2) { ctx.lineTo(pts[1].x, pts[1].y); return; }
    const h = [], d = [];
    for (let i = 0; i < n - 1; i++) { h[i] = pts[i + 1].x - pts[i].x; d[i] = h[i] > 1e-6 ? (pts[i + 1].y - pts[i].y) / h[i] : 0; }
    const m = new Array(n);
    m[0] = d[0]; m[n - 1] = d[n - 2];
    for (let i = 1; i < n - 1; i++) {
      if (d[i - 1] * d[i] <= 0) { m[i] = 0; continue; }
      const w1 = 3 * h[i - 1], w2 = 3 * h[i];
      m[i] = (w1 + w2) / (w1 / d[i - 1] + w2 / d[i]);
    }
    for (let i = 0; i < n - 1; i++) {
      if (d[i] === 0) { m[i] = 0; m[i + 1] = 0; continue; }
      const a = m[i] / d[i], b = m[i + 1] / d[i];
      const s = a * a + b * b;
      if (s > 9) { const t = 3 / Math.sqrt(s); m[i] = t * a * d[i]; m[i + 1] = t * b * d[i]; }
    }
    for (let i = 0; i < n - 1; i++) {
      const x1 = pts[i].x, y1 = pts[i].y, x2 = pts[i + 1].x, y2 = pts[i + 1].y;
      const hh = (h[i] || 1) / 3;
      ctx.bezierCurveTo(x1 + hh, y1 + m[i] * hh, x2 - hh, y2 - m[i + 1] * hh, x2, y2);
    }
  }

  function drawMini(c, history, ts0, timeSpan, now) {
    const canvas = document.getElementById(c.id);
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    const W = rect.width, H = rect.height;
    canvas.width = W * dpr; canvas.height = H * dpr;
    const ctx = canvas.getContext('2d');
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, W, H);
    const pad = { l: 40, r: 10, t: 6, b: c.xlabels ? 16 : 8 };
    const cw = W - pad.l - pad.r, ch = H - pad.t - pad.b;
    if (cw < 20 || ch < 10) return;
    ctx.strokeStyle = 'rgba(148,163,184,0.1)'; ctx.lineWidth = 1;
    for (let i = 0; i <= 4; i++) { const y = pad.t + ch * i / 4; ctx.beginPath(); ctx.moveTo(pad.l, y); ctx.lineTo(W - pad.r, y); ctx.stroke(); }
    ctx.fillStyle = c.color.replace('rgb', 'rgba').replace(')', ',0.75)');
    ctx.font = '11px JetBrains Mono, monospace'; ctx.textAlign = 'right';
    for (let i = 0; i <= 4; i++) { const v = c.max * (4 - i) / 4; ctx.fillText(c.fmt(v), pad.l - 5, pad.t + ch * i / 4 + 3); }
    if (c.xlabels) {
      ctx.fillStyle = 'rgba(148,163,184,0.6)'; ctx.textAlign = 'center';
      const xSteps = Math.min(6, Math.max(2, Math.floor(timeSpan / 10)));
      for (let i = 0; i < xSteps; i++) {
        const t = ts0 + timeSpan * i / Math.max(xSteps - 1, 1);
        const x = pad.l + ((t - ts0) / timeSpan) * cw;
        const dt = new Date(t * 1000);
        ctx.fillText(dt.getHours() + ':' + String(dt.getMinutes()).padStart(2, '0') + ':' + String(dt.getSeconds()).padStart(2, '0'), x, H - 4);
      }
    }
    const nowX = pad.l + ((now - ts0) / timeSpan) * cw;
    if (nowX > pad.l && nowX < W - pad.r) { ctx.strokeStyle = 'rgba(148,163,184,0.25)'; ctx.lineWidth = 1; ctx.setLineDash([3, 3]); ctx.beginPath(); ctx.moveTo(nowX, pad.t); ctx.lineTo(nowX, pad.t + ch); ctx.stroke(); ctx.setLineDash([]); }
    let pts = history.map(p => { const x = pad.l + ((p.ts - ts0) / timeSpan) * cw; let v = p[c.key] || 0; if (c.fallback && !v) v = p[c.fallback] || 0; return { x, y: pad.t + ch * (1 - Math.min(v / c.max, 1)), v }; });
    if (c.fallback) pts = pts.filter(p => p.v > 0);
    if (pts.length < 2) return;
    ctx.strokeStyle = c.color; ctx.lineWidth = 2; ctx.lineJoin = 'round'; ctx.lineCap = 'round';
    if (c.dashed) ctx.setLineDash([6, 3]);
    monotoneLine(ctx, pts); ctx.stroke(); ctx.setLineDash([]);
    if (!c.dashed) { ctx.lineTo(pts[pts.length - 1].x, pad.t + ch); ctx.lineTo(pts[0].x, pad.t + ch); ctx.closePath(); ctx.fillStyle = c.color.replace(')', ',0.08)').replace('rgb', 'rgba'); ctx.fill(); }
  }

  function drawCharts(history) {
    if (!history || !history.length) return;
    const now = Date.now() / 1000;
    const ts0 = history[0].ts;
    const timeEnd = Math.max(history[history.length - 1].ts, now);
    const timeSpan = Math.max(timeEnd - ts0, 1);
    let maxTps = 1, maxPrompt = 1, maxCtx = 1;
    for (const p of history) { if (p.tps > maxTps) maxTps = p.tps; if ((p.prompt_tps || 0) > maxPrompt) maxPrompt = p.prompt_tps; const ctxv = p.ctx_used || 0; if (ctxv > maxCtx) maxCtx = ctxv; }
    maxTps = Math.ceil(maxTps * 1.15) || 1;
    maxPrompt = Math.ceil(maxPrompt * 1.15) || 1;
    if (maxCtx <= 1024) maxCtx = 1024; else if (maxCtx <= 4096) maxCtx = 4096; else if (maxCtx <= 16384) maxCtx = 16384; else if (maxCtx <= 65536) maxCtx = 65536; else maxCtx = Math.ceil(maxCtx / 10240) * 10240 || 131072;
    const charts = [
      { id: 'chartTokens', key: 'ctx_used', color: 'rgb(245,158,11)', max: maxCtx, dashed: true, xlabels: false, fmt: v => v >= 1024 ? Math.round(v / 1024) + 'k' : Math.round(v) + '' },
      { id: 'chartDecode', key: 'tps', color: 'rgb(34,197,94)', max: maxTps, dashed: false, xlabels: false, fmt: v => Math.round(v) + '' },
      { id: 'chartPrompt', key: 'prompt_tps', color: 'rgb(59,130,246)', max: maxPrompt, dashed: false, xlabels: true, fmt: v => Math.round(v) + '' },
    ];
    for (const c of charts) drawMini(c, history, ts0, timeSpan, now);
  }

  async function pollStats() {
    try {
      const r = await fetch(`/api/model/${mid}/stats`);
      if (r.status === 401) { closeStats(); return; }
      const d = await r.json();
      const live = d.live;
      if (live) {
        const el = id => document.getElementById(id);
        if (el('kpi-tps')) el('kpi-tps').innerHTML = `${live.tps || 0}<span class="stats-kpi-unit">tok/s</span>`;
        if (el('kpi-prompt') && live.prompt_tps) el('kpi-prompt').innerHTML = `${live.prompt_tps}<span class="stats-kpi-unit">tok/s</span>`;
        const ctxv = live.running ? (live.ctx_used || 0) : 0;
        if (el('kpi-ctx')) el('kpi-ctx').innerHTML = ctxv >= 1024 ? `${(ctxv / 1024).toFixed(1)}<span class="stats-kpi-unit">k</span>` : `${ctxv}<span class="stats-kpi-unit">tok</span>`;
        if (el('kpi-tokens')) el('kpi-tokens').textContent = (live.tokens || 0).toLocaleString();
        const q = live.running !== undefined ? (live.running + (live.waiting || 0)) : '—';
        if (el('kpi-queue')) el('kpi-queue').textContent = q;
        if (el('stats-time')) el('stats-time').textContent = 'scraped ' + new Date(live.ts * 1000).toLocaleTimeString();
      }
      drawCharts(d.history);
    } catch (e) {}
  }
  await pollStats();
  _statsTimer = setInterval(pollStats, 2000);
}
