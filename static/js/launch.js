/* ═══════════════════════════════════════════════════════════════════
   launch.js — launch options: GPU/ctx/agents popups, VRAM feasibility
   ═══════════════════════════════════════════════════════════════════ */

const LS = {};  // mid -> {gpus,ctx,par,hl:{gpus:0,ctx:0,par:0}}
let _ddOpen = null;  // "mid:dim" of the currently open popup
const PER_GPU_GB = 24, GPU_OH_GB = 1, HL_MS = 4000;

function fmtK(c) { return c >= 1024 ? Math.round(c / 1024) + 'k' : '' + c; }
function fmtGB(mi) { return (mi / 1024).toFixed(1) + 'G'; }

function launchCaps(m) {
  const p = m.parallel || {};
  if (!(p.enabled && p.weights_gb && p.kv_gb_per_100k)) return null;
  const W = p.weights_gb, KV = p.kv_gb_per_100k;
  const gpusOpts = (p.gpu_options && p.gpu_options.length) ? p.gpu_options.slice().sort((a, b) => a - b) : [1, 2, 3, 4].filter(n => n <= (p.max_gpus || 4));
  const ctxOpts = (p.ctx_options || []).slice().sort((a, b) => a - b);
  const maxPar = p.max_parallel || 8;
  // Use detected capacity when available; the legacy profiles assume 24 GiB.
  const detected = ((S.cache && S.cache.gpus) || []).map(g => Number(g.mem_total) / 1024).filter(n => n > 0);
  const perGPU = detected.length ? Math.min(...detected) : PER_GPU_GB;
  const freeGB = n => (perGPU - GPU_OH_GB) * n - W;
  const fits = (n, ctx, par) => freeGB(n) >= 0 && KV * ctx / 1e5 * par <= freeGB(n);
  return { W, KV, gpusOpts, ctxOpts, maxPar, fits };
}

function feasibleList(caps) {
  const out = [];
  for (const g of caps.gpusOpts) for (const c of caps.ctxOpts) for (let p = 1; p <= caps.maxPar; p++)
    if (caps.fits(g, c, p)) out.push({ g, c, p });
  return out;
}

function launchPresets(m) {
  const caps = launchCaps(m);
  if (!caps || !caps.gpusOpts.length || !caps.ctxOpts.length) return [];
  const F = feasibleList(caps);
  if (!F.length) return [];
  const maxCtx = caps.ctxOpts[caps.ctxOpts.length - 1];
  const out = [], seen = new Set();
  const add = (g, c, p) => {
    if (!caps.fits(g, c, p)) return;
    const k = g + '|' + c + '|' + p;
    if (seen.has(k)) return;
    seen.add(k); out.push({ g, c, p });
  };
  for (const n of caps.gpusOpts) { if (caps.fits(n, maxCtx, 1)) { add(n, maxCtx, 1); break; } }
  { let best = null; for (const x of F) if (!best || x.p > best.p || (x.p === best.p && x.c > best.c)) best = x; if (best) add(best.g, best.c, best.p); }
  { let bal = null; for (const n of caps.gpusOpts) { if (caps.fits(n, maxCtx, 4)) { bal = { g: n, c: maxCtx, p: 4 }; break; } }
    if (!bal) for (const n of caps.gpusOpts) { if (caps.fits(n, maxCtx, 2)) { bal = { g: n, c: maxCtx, p: 2 }; break; } }
    if (bal) add(bal.g, bal.c, bal.p); }
  return out;
}

function resolveLaunch(caps, changed, s) {
  const F = feasibleList(caps);
  if (!F.length) return null;
  const k = { gpus: 'g', ctx: 'c', par: 'p' }[changed];
  const req = { g: s.gpus, c: s.ctx, p: s.par };
  const opts = changed === 'gpus' ? caps.gpusOpts : changed === 'ctx' ? caps.ctxOpts : Array.from({ length: Math.min(caps.maxPar, 8) }, (_, i) => i + 1);
  const order = opts.slice().sort((a, b) => { const da = Math.abs(a - req[k]), db = Math.abs(b - req[k]); return da === db ? a - b : da - db; });
  for (const v of order) {
    const pool = F.filter(x => x[k] === v);
    if (!pool.length) continue;
    let best;
    if (changed === 'gpus') best = pool.sort((a, b) => Math.abs(a.c - req.c) - Math.abs(b.c - req.c) || Math.abs(a.p - req.p) - Math.abs(b.p - req.p))[0];
    else if (changed === 'ctx') best = pool.sort((a, b) => Math.abs(a.g - req.g) - Math.abs(b.g - req.g) || Math.abs(a.p - req.p) - Math.abs(b.p - req.p))[0];
    else best = pool.sort((a, b) => Math.abs(a.g - req.g) - Math.abs(b.g - req.g) || Math.abs(a.c - req.c) - Math.abs(b.c - req.c))[0];
    return { g: best.g, c: best.c, p: best.p };
  }
  return { g: F[0].g, c: F[0].c, p: F[0].p };
}

// ── Persist per-model launch selections ──────────────────────────
const LS_KEY = 'aih_launch_v1';
function lsLoad(mid) {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (!raw) return null;
    const e = (JSON.parse(raw) || {})[mid];
    return (e && typeof e.gpus === 'number' && typeof e.ctx === 'number') ? { gpus: e.gpus, ctx: e.ctx, par: (typeof e.par === 'number' ? e.par : 1), touched: !!e.touched, open: !!e.open } : null;
  } catch (err) { return null; }
}
function lsSave(mid) {
  const s = LS[mid]; if (!s) return;
  try {
    let all = {}; const raw = localStorage.getItem(LS_KEY);
    if (raw) { try { all = JSON.parse(raw) || {}; } catch (e2) { all = {}; } }
    all[mid] = { gpus: s.gpus, ctx: s.ctx, par: s.par, touched: !!s.touched, open: !!s.open };
    localStorage.setItem(LS_KEY, JSON.stringify(all));
  } catch (err) {}
}
function lsSel(m) {
  const p = m.parallel || {};
  if (!LS[m.id]) {
    const dg = p.default_gpus || 1, dc = p.default_ctx || ((p.ctx_options || [65536])[0]);
    let gg = dg, cc = dc, pp = 1, touched = false;
    if (m.running) { gg = m.chosen_gpus || dg; cc = m.chosen_ctx || dc; pp = m.chosen_parallel || 1; }
    else { const st = lsLoad(m.id); if (st) { gg = st.gpus; cc = st.ctx; pp = st.par; touched = st.touched; } }
    const s = { gpus: gg, ctx: cc, par: pp, touched: touched, hl: { gpus: 0, ctx: 0, par: 0 } };
    const caps = launchCaps(m); if (caps) { const r = resolveLaunch(caps, 'gpus', s); if (r) { s.gpus = r.g; s.ctx = r.c; s.par = r.p; } }
    LS[m.id] = s;
  }
  return LS[m.id];
}

function dimOptions(m, dim) {
  const caps = launchCaps(m); if (!caps) return [];
  if (dim === 'gpus') return caps.gpusOpts.map(n => ({ v: n, lab: n + ' GPU' }));
  if (dim === 'ctx') return caps.ctxOpts.slice().sort((a, b) => b - a).map(c => ({ v: c, lab: fmtK(c) }));
  const out = []; for (let k = caps.maxPar; k >= 1; k--) out.push({ v: k, lab: k + (k > 1 ? ' agents' : ' agent') });
  return out;
}

// ── VRAM projection per option ───────────────────────────────────
const _projCache = {};
function projClass(gb) { return gb <= 23 ? 'ok' : 'warn'; }
function projFor(m, dim) {
  const caps = launchCaps(m); if (!caps) return null;
  const s = lsSel(m);
  const key = m.id + '|' + dim + '|' + s.gpus + '|' + s.ctx + '|' + s.par;
  if (_projCache[key]) return _projCache[key];
  const out = {};
  for (const o of dimOptions(m, dim)) {
    const cand = { gpus: s.gpus, ctx: s.ctx, par: s.par }; cand[dim] = o.v;
    const r = resolveLaunch(caps, dim, cand);
    if (r) out[o.v] = (caps.W + caps.KV * r.c / 1e5 * r.p) / r.g;
  }
  _projCache[key] = out;
  return out;
}
function projHtml(m, dim, v) {
  if (m.backend === 'Maya') return '';
  const p = projFor(m, dim);
  if (!p || typeof p[v] !== 'number') return '';
  const gb = p[v];
  return `<span class="dd-proj ${projClass(gb)}" title="VRAM per GPU if this option is picked (weights + KV)">${gb.toFixed(1)}G</span>`;
}

function ddPopup(m, dim) {
  const open = _ddOpen === m.id + ':' + dim;
  const s = lsSel(m);
  const hl = s.hl[dim] && (Date.now() - s.hl[dim] < HL_MS);
  const cur = { gpus: s.gpus, ctx: s.ctx, par: s.par };
  const opts = dimOptions(m, dim).map(o =>
    `<span class="dd-opt${o.v === cur[dim] ? ' sel' : ''}" role="option" aria-selected="${o.v === cur[dim] ? 'true' : 'false'}" onclick="ddPick(event,'${m.id}','${dim}',${o.v})">${o.lab}<span class="dd-check">✓</span>${projHtml(m, dim, o.v)}</span>`).join('');
  const valTxt = dim === 'gpus' ? s.gpus + ' GPU' : dim === 'ctx' ? fmtK(s.ctx) : s.par + (s.par > 1 ? ' agents' : ' agent');
  return `<span class="dd${open ? ' open' : ''}${hl ? ' hl' : ''}">
    <button type="button" class="dd-btn" aria-haspopup="listbox" aria-expanded="${open ? 'true' : 'false'}" onclick="ddToggle(event,'${m.id}','${dim}')">${valTxt}<span class="dd-chev">▾</span></button>
    <span class="dd-pop" role="listbox" tabindex="-1">${opts}</span>
  </span>`;
}

// D-02: progressive disclosure. Stopped cards show a ONE-LINE launch summary
// (basic action = ▶ Start in the card footer, always one click). The advanced
// controls (GPU / ctx / agents popups + presets) are collapsed behind that
// line and expand on click — fewer things on screen, fewer mis-clicks.
function launchControls(m) {
  const caps = launchCaps(m);
  if (!caps || !caps.gpusOpts.length || !caps.ctxOpts.length) return '';
  const s = lsSel(m);
  const perGPU = (caps.W + caps.KV * s.ctx / 1e5 * s.par) / s.gpus;
  const memory = m.backend === 'Maya' ? 'adaptive GPU + RAM cache' : `≈${perGPU.toFixed(1)} GB/GPU`;
  const summary = `${s.gpus} GPU · ${fmtK(s.ctx)} · ${s.par} agent${s.par > 1 ? 's' : ''} · ${memory}`;
  if (!s.open) {
    return `<div class="launch launch-collapsed" onclick="launchToggle('${m.id}')" title="Expand launch options (GPU / ctx / agents) — Start below uses this selection">
      <span class="launch-title">⚙ Launch</span>
      <span class="launch-sum">${summary}</span>
      <span class="launch-chev">▸</span>
    </div>`;
  }
  const preview = `<div class="launch-preview" title="Estimated VRAM per GPU for this selection (weights + KV)">→ ${summary}</div>`;
  const presets = launchPresets(m).map(p =>
    `<button type="button" class="preset-btn" title="${fmtK(p.c)} ctx" onclick="presetPick('${m.id}',${p.g},${p.c},${p.p})">${p.g}×${p.p}</button>`).join('');
  const presetsHtml = presets ? `<div class="launch-presets">${presets}</div>` : '';
  return `<div class="launch launch-open">
    <span class="launch-title">⚙ Launch</span>
    ${ddPopup(m, 'gpus')}${ddPopup(m, 'ctx')}${ddPopup(m, 'par')}
    ${presetsHtml}
    ${preview}
    <button type="button" class="launch-collapse" onclick="launchToggle('${m.id}')" title="Collapse">▾</button>
  </div>`;
}

function launchToggle(mid) {
  const s = LS[mid];
  if (!s) return;
  s.open = !s.open;
  lsSave(mid);
  const m = S.cache && S.cache.models.find(x => x.id === mid);
  if (m) replaceCard(m);
}

// ── Dropdown interactions ────────────────────────────────────────
function ddToggle(ev, mid, dim) {
  ev.stopPropagation();
  const wasOpen = _ddOpen === mid + ':' + dim;
  _ddOpen = wasOpen ? null : mid + ':' + dim;
  const m = S.cache.models.find(x => x.id === mid);
  if (!m) return;
  replaceCard(m);
  if (_ddOpen) { ddApplyEdge(mid, dim); const pop = document.querySelector(`#card-${mid} .dd.open .dd-pop`); if (pop) pop.focus(); }
  else focusDdBtn(mid, dim);
}
function ddApplyEdge(mid, dim) {
  const btn = document.querySelector(`#card-${mid} .dd.open .dd-btn`);
  if (!btn) return;
  const dd = btn.closest('.dd');
  const r = btn.getBoundingClientRect();
  if (r.right + 140 > window.innerWidth) dd.classList.add('right'); else dd.classList.remove('right');
}
function focusDdBtn(mid, dim) {
  const idx = { gpus: 0, ctx: 1, par: 2 }[dim] || 0;
  const btns = document.querySelectorAll(`#card-${mid} .dd-btn`);
  if (btns[idx]) btns[idx].focus();
}
function ddClose(mid, dim) {
  _ddOpen = null;
  const m = S.cache && S.cache.models.find(x => x.id === mid);
  if (!m) return;
  replaceCard(m);
  focusDdBtn(mid, dim);
}
function ddPick(ev, mid, dim, val) {
  ev.stopPropagation();
  _ddOpen = null;
  const m = S.cache.models.find(x => x.id === mid); if (!m) return;
  const caps = launchCaps(m); if (!caps) return;
  const s = lsSel(m);
  s.touched = true;
  const old = { gpus: s.gpus, ctx: s.ctx, par: s.par };
  s[dim] = val;
  const r = resolveLaunch(caps, dim, s);
  if (r) { s.gpus = r.g; s.ctx = r.c; s.par = r.p; }
  const now = Date.now(), changed = [];
  if (s.gpus !== old.gpus) { s.hl.gpus = now; changed.push('GPU'); }
  if (s.ctx !== old.ctx) { s.hl.ctx = now; changed.push('ctx'); }
  if (s.par !== old.par) { s.hl.par = now; changed.push('agents'); }
  lsSave(mid);
  replaceCard(m);
  focusDdBtn(mid, dim);
  if (changed.length) toast(`${m.short || mid}: adjusted ${changed.join(' + ')} → ${s.gpus} GPU · ${fmtK(s.ctx)} · ${s.par} agent${s.par > 1 ? 's' : ''}`);
}
function presetPick(mid, g, c, p) {
  _ddOpen = null;
  const m = S.cache.models.find(x => x.id === mid); if (!m) return;
  const caps = launchCaps(m); if (!caps) return;
  const s = lsSel(m);
  s.gpus = g; s.ctx = c; s.par = p; s.touched = true;
  const r = resolveLaunch(caps, 'gpus', s);
  if (r) { s.gpus = r.g; s.ctx = r.c; s.par = r.p; }
  lsSave(mid);
  replaceCard(m);
  toast(`Preset: ${s.gpus} GPU · ${fmtK(s.ctx)} · ${s.par} agent${s.par > 1 ? 's' : ''}`);
}

// Close popup on outside click / Esc
document.addEventListener('click', e => {
  if (!_ddOpen) return;
  if (e.target.closest && e.target.closest('.dd')) return;
  const i = _ddOpen.indexOf(':');
  ddClose(_ddOpen.slice(0, i), _ddOpen.slice(i + 1));
});
document.addEventListener('keydown', e => {
  if (e.key !== 'Escape' || !_ddOpen) return;
  const i = _ddOpen.indexOf(':');
  ddClose(_ddOpen.slice(0, i), _ddOpen.slice(i + 1));
});

// ── Efficiency (tok/s·GPU, tok/s·W) ─────────────────────────────
function modelEfficiency(m) {
  const nG = (m.gpus_used && m.gpus_used.length) || m.chosen_gpus || 1;
  const dtp = (typeof m.tps_live === 'number' && m.tps_live > 0) ? m.tps_live : (m.tps || 0);
  const pwr = (m.gpus_used || []).reduce((s, i) => { const g = ((S.cache && S.cache.gpus) || []).find(gg => gg.index === i); return s + (g && g.power ? g.power : 0); }, 0);
  if (!(dtp > 0)) return '';
  let e = (dtp / nG).toFixed(0) + ' t/s·GPU';
  if (pwr > 0) e += ' · ' + (dtp / pwr).toFixed(2) + ' t/s·W';
  return e;
}
