/* ═══════════════════════════════════════════════════════════════════
   health.js — system health polling, GPU temps, alerts, RAM health,
               GPU-drop banner, uptime/crash history
   ═══════════════════════════════════════════════════════════════════ */

let _gpuHist = [];
let _lastSysHealth = null;
let _lastRam = { ok: false, swap: '—', ce: '—', ue: '—', mce: '—', sev: 'warn' };

// ── GPU temp history (core + VRAM + power + util + mem) ───────────
async function pollGpuTemps() {
  try {
    const d = await api('/api/gpu-temps');
    if (Array.isArray(d) && d.length) {
      _gpuHist = d;
      const now = Date.now() / 1000;
      for (let idx = 0; idx < 8; idx++) {
        if (!document.getElementById('gchart-' + idx)) continue;
        const core = [], vram = [], pow = [], utl = [], mem = [];
        for (const s of d) {
          const c = s.gpu ? s.gpu[idx] : null, v = s.vram ? s.vram[idx] : null;
          const p = s.power ? s.power[idx] : null, u = s.util ? s.util[idx] : null, mm = s.mem ? s.mem[idx] : null;
          if (typeof c === 'number' && c >= 0) core.push([s.ts, c]);
          if (typeof v === 'number' && v > 0) vram.push([s.ts, v]);
          if (typeof p === 'number' && p >= 0) pow.push([s.ts, p]);
          if (typeof u === 'number' && u >= 0) utl.push([s.ts, u]);
          if (typeof mm === 'number' && mm >= 0) mem.push([s.ts, mm / 1024]);
        }
        if (!core.length) continue;
        setChart('gchart-' + idx, [{ color: '#f59e0b', pts: recentPts(core, now) }, { color: '#22d3ee', pts: recentPts(vram, now) }], { fmt: v => Math.round(v) + '°', padL: 26 });
        if (pow.length) setChart('gpow-' + idx, [{ color: '#f59e0b', pts: recentPts(pow, now) }], { fmt: v => Math.round(v) + 'W', padL: 30 });
        if (utl.length) setChart('gutl-' + idx, [{ color: '#22d3ee', pts: recentPts(utl, now) }], { fmt: v => Math.round(v) + '%', padL: 26 });
        if (mem.length) setChart('gmem-' + idx, [{ color: '#22c55e', pts: recentPts(mem, now) }], { fmt: v => Math.round(v) + 'G', padL: 26 });
      }
    }
  } catch (e) {}
}

// ── Per-model live charts (Decode | Prompt | Context) ────────────
async function pollModelStats() {
  const models = (S.cache && S.cache.models) || [];
  const running = models.filter(m => m.running);
  for (const m of running) {
    const el = document.getElementById('meff-' + m.id);
    if (el) { const e = modelEfficiency(m); el.textContent = e ? '⟐ ' + e : ''; }
  }
  await Promise.all(running.map(async m => {
    try {
      const r = await api('/api/model/' + encodeURIComponent(m.id) + '/stats');
      setModelCharts(m.id, (r && r.history) || []);
    } catch (e) {}
  }));
}

function setModelCharts(mid, hist) {
  const now = Date.now() / 1000;
  const pts = hist.filter(e => typeof e.ts === 'number');
  if (!pts.length) return;
  const dec = [], prm = [], ctxu = [];
  for (const e of pts) {
    if (typeof e.tps === 'number' && e.tps > 0) dec.push([e.ts, e.tps]);
    if (typeof e.prompt_tps === 'number' && e.prompt_tps > 0) prm.push([e.ts, e.prompt_tps]);
    if (typeof e.ctx_used === 'number' && e.ctx_used > 0) ctxu.push([e.ts, e.ctx_used]);
  }
  if (document.getElementById('mdec-' + mid)) setChart('mdec-' + mid, [{ color: '#f59e0b', pts: recentPts(dec, now) }], { fmt: tpsAxis });
  if (document.getElementById('mprm-' + mid)) setChart('mprm-' + mid, [{ color: '#22d3ee', pts: recentPts(prm, now) }], { fmt: tpsAxis });
  if (document.getElementById('mctx-' + mid) && ctxu.length) setChart('mctx-' + mid, [{ color: '#22c55e', pts: recentPts(ctxu, now) }], { fmt: v => fmtK(v), padL: 40 });
}

// ── System health (RAM/swap/EDAC/MCE/AER) ───────────────────────
async function pollSysHealth() {
  try {
    const h = await api('/api/system-health');
    _lastSysHealth = h;
    renderGpuDropBanner();
    const dot = document.getElementById('mem-dot');
    if (!h || h.available === false) {
      _lastRam = { ok: false, swap: '—', ce: '—', ue: '—', mce: '—', sev: 'warn' };
      if (dot) { dot.classList.remove('crit'); dot.classList.add('warn'); }
      renderSystemHistory(h); return;
    }
    const edac = h.edac || {}, aer = h.aer || {};
    const swap = h.swap_gb != null ? h.swap_gb : 0;
    const ce = edac.ce_total || 0, ue = edac.ue_total || 0;
    const mce = h.mce_total || 0, ac = aer.correctable || 0, af = aer.fatal || 0;
    let sev = 'ok';
    if (swap > 0 || ce > 0) sev = 'warn';
    if (ue > 0 || af > 0) sev = 'crit';
    _lastRam = { ok: true, swap: swap.toFixed(1) + 'G', ce: ce.toLocaleString(), ue: ue.toLocaleString(), mce: `${mce} / ${ac}·${af}`, sev };
    if (dot) { dot.classList.remove('warn', 'crit'); if (sev !== 'ok') dot.classList.add(sev); }
    const hist = h.history || [];
    if (hist.length) {
      const cePts = hist.filter(e => typeof e.edac_ce === 'number').map(e => [e.ts, e.edac_ce]);
      if (cePts.length) setChart('ram-ce-trend', [{ color: '#f59e0b', pts: cePts }], { fmt: v => String(Math.round(v)), win: 600 });
    }
    renderSystemHistory(h);
  } catch (e) {}
}

// ── GPU-drop banner ──────────────────────────────────────────────
function gpuDropDismissed() { try { return parseFloat(localStorage.getItem('aihub_gpu_drop_dismissed') || '0') || 0; } catch (e) { return 0; } }
function dismissGpuDrop() {
  const ev = _lastSysHealth && _lastSysHealth.gpu_event;
  if (ev && ev.ts) { try { localStorage.setItem('aihub_gpu_drop_dismissed', String(ev.ts)); } catch (e) {} }
  const b = $('gpu-drop-banner'); if (b) b.style.display = 'none';
}
function renderGpuDropBanner() {
  const b = $('gpu-drop-banner'); if (!b) return;
  const ev = _lastSysHealth && _lastSysHealth.gpu_event;
  if (!ev || !ev.ts) { b.style.display = 'none'; return; }
  if (ev.ts <= gpuDropDismissed()) { b.style.display = 'none'; return; }
  const msg = $('gdb-msg'); if (msg) msg.textContent = ev.message || ('GPU count changed ' + ev.from + ' → ' + ev.to);
  b.style.display = 'flex';
}

// ── Uptime / crash history ──────────────────────────────────────
function humanUptime(s) {
  if (!(s > 0)) return '—';
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60);
  if (d > 0) return d + 'd ' + h + 'h';
  if (h > 0) return h + 'h ' + m + 'm';
  return m + 'm';
}
function renderSystemHistory(h) {
  const val = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v; };
  if (!h) return;  // null-safe: pollSysHealth passes null on fetch error
  if (h.uptime_s > 0) {
    val('hist-uptime', humanUptime(h.uptime_s));
  }
  const reb = h.reboots || [];
  if (reb.length) {
    const newest = reb[0];
    const dt = new Date((newest.ts || 0) * 1000);
    const sameDay = new Date().toDateString() === dt.toDateString();
    val('hist-reboots', reb.length + (newest.iso ? '' : ''));
    const pad = n => n.toString().padStart(2, '0'); const hm = pad(dt.getHours()) + ':' + pad(dt.getMinutes()); val('hist-reb-sub', sameDay ? `last boot ${hm}` : `last boot ${pad(dt.getMonth()+1)}/${pad(dt.getDate())} ${hm}`);
  }
  const lw = h.last_words || [];
  const mini = $('lw-mini');
  if (mini) {
    if (!lw.length) mini.style.display = 'none';
    else { mini.style.display = ''; mini.textContent = '📄 ' + lw.length; mini.title = lw.length + ' last-words snapshot(s) — click to view'; }
  }
}

// ── RAM health dialog ────────────────────────────────────────────
function openRamHealth() {
  const r = _lastRam;
  const swapCls = (r.swap !== '—' && r.swap !== '0.0G') ? 'warn' : '';
  const ceCls = (r.ce !== '—' && r.ce !== '0') ? 'warn' : '';
  const ueCls = (r.ue !== '—' && r.ue !== '0') ? 'crit' : '';
  const mceCls = (r.sev === 'crit') ? 'crit' : '';
  const body = `<div class="ram-h"><div class="rh-grid">
    <span class="rh ${swapCls}" title="Swap in use (want 0)"><span class="k">SWAP</span><b>${r.swap}</b></span>
    <span class="rh ${ceCls}" title="EDAC correctable errors — growth = early RAM degradation"><span class="k">EDAC CE</span><b>${r.ce}</b></span>
    <span class="rh ${ueCls}" title="EDAC uncorrectable — fatal RAM errors (want 0)"><span class="k">EDAC UE</span><b>${r.ue}</b></span>
    <span class="rh ${mceCls}" title="MCE / AER correctable·fatal (want 0 / 0·0)"><span class="k">MCE/AER</span><b>${r.mce}</b></span>
  </div><div class="sh-trend" style="margin:14px 0 0"><div class="sh-trend-head"><span>EDAC correctable errors — last 10 min (any growth = early RAM warning)</span><span class="lt"><i style="background:#f59e0b"></i>CE</span></div><canvas id="ram-ce-trend" height="72"></canvas></div></div>`;
  showModal({ title: 'RAM Health — memory · swap · EDAC · MCE/AER', body, buttons: [{ label: 'Close', type: 'ghost', value: true }] })
    .then(() => { dropChart('ram-ce-trend'); });
}

// ── Last-words dialog ───────────────────────────────────────────
function lwChip(x) {
  const dt = new Date((x.mtime || 0) * 1000);
  return `<span class="lw-chip" onclick="openLastWords('${x.name}')" title="${dt.toLocaleString()} · ${(x.size / 1024).toFixed(1)}KB">${x.name.replace('snapshot-', '').replace('.txt', '')} · ${dt.toLocaleTimeString()}</span>`;
}
function openLastWordsDialog() {
  const lw = (_lastSysHealth && _lastSysHealth.last_words) || [];
  if (!lw.length) { toast('No last-words snapshots recorded yet', 'info'); return; }
  showModal({ title: 'Last-words · ' + lw.length + ' snapshot(s) — pre-crash',
    body: `<div class="lw-all">${lw.map(lwChip).join('')}</div>`,
    buttons: [{ label: 'Close', type: 'ghost', value: true }] });
}
async function openLastWords(name) {
  try {
    const r = await fetch('/api/last-words/' + encodeURIComponent(name));
    if (!r.ok) throw new Error(r.status);
    const txt = await r.text();
    showModal({ title: 'Last-words · ' + name, body: `<pre class="lw-pre">${txt.replace(/</g, '&lt;')}</pre>`, buttons: [{ label: 'Close', type: 'ghost', value: true }] });
  } catch (e) { toast('Could not load snapshot: ' + e.message, 'err'); }
}

// ── Alert engine ─────────────────────────────────────────────────
const GPU_POWER_LIMIT_W = 290;
function computeAlerts() {
  const A = [];
  const gpus = (S.cache && S.cache.gpus) || [];
  for (const g of gpus) {
    const pct = g.mem_total ? Math.round(100 * g.mem_used / g.mem_total) : 0;
    if (pct > 90) A.push({ sev: 'warn', txt: `GPU${g.index} VRAM ${pct}% (${(g.mem_used / 1024).toFixed(1)}/${(g.mem_total / 1024).toFixed(0)}G)` });
    if ((g.power || 0) > GPU_POWER_LIMIT_W) A.push({ sev: 'warn', txt: `GPU${g.index} ${g.power}W > ${GPU_POWER_LIMIT_W}W limit` });
  }
  const last = _gpuHist && _gpuHist.length ? _gpuHist[_gpuHist.length - 1] : null;
  if (last) {
    const vram = last.vram || [], core = last.gpu || [];
    for (let i = 0; i < vram.length; i++) if (vram[i] > 95) A.push({ sev: 'crit', txt: `GPU${i} VRAM temp ${vram[i]}°C` });
    for (let i = 0; i < core.length; i++) if (core[i] > 90) A.push({ sev: 'warn', txt: `GPU${i} core ${core[i]}°C` });
  }
  const sh = _lastSysHealth;
  if (sh && sh.available !== false) {
    if ((sh.swap_gb || 0) > 0) A.push({ sev: 'warn', txt: `swap ${sh.swap_gb}G in use` });
    const hist = sh.history || [];
    if (hist.length >= 10) {
      const first = hist[0].edac_ce || 0, lastc = hist[hist.length - 1].edac_ce || 0;
      if (lastc > first) A.push({ sev: 'crit', txt: `EDAC CE growing +${lastc - first} (${first}→${lastc})` });
      else if (lastc > 0) A.push({ sev: 'warn', txt: `EDAC CE ${lastc} (static)` });
    }
  }
  return A;
}
function renderAlertBanner() {
  const A = computeAlerts();
  let b = $('alert-banner');
  if (!A.length) { if (b) b.remove(); return; }
  if (!b) { b = document.createElement('div'); b.id = 'alert-banner'; var wrap = $('alert-banner-wrap'); if (wrap) wrap.appendChild(b); else document.body.appendChild(b); }
  const hasCrit = A.some(a => a.sev === 'crit');
  b.className = 'alert-banner ' + (hasCrit ? 'crit' : 'warn');
  b.innerHTML = '<span class="alert-title">' + (hasCrit ? '⛔ CRITICAL' : '⚠ WARNING') + '</span>' +
    A.map(a => `<span class="alert-item">${a.txt}</span>`).join('');
}
