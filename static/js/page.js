/* ═══════════════════════════════════════════════════════════════════
   page.js — buildPage, updatePage, setSys, setGpus, model actions
   ═══════════════════════════════════════════════════════════════════ */

const CAT_ORDER = ["LLM", "Cloud", "Image", "Voice", "Music", "3D"];
const CAT_META = {
  LLM: { icon: "🧠", title: "Language Models", desc: "Qwen · Gemma · GPT-OSS" },
  Cloud: { icon: "☁️", title: "Cloud (OpenAI OAuth)", desc: "ChatGPT · GPT-6 Astra · Codex" },
  Image: { icon: "🎨", title: "Image & Video", desc: "FLUX · SDXL · MiniMax-H3 · LTX · Wan 2.1" },
  Voice: { icon: "🎙", title: "Voice", desc: "Kokoro TTS · faster-whisper STT" },
  Music: { icon: "🎵", title: "Music", desc: "ACE-Step 1.5" },
  "3D": { icon: "🧊", title: "3D", desc: "Hunyuan3D-2.1" }
};

// ── Build full page (first render) ───────────────────────────────
function buildPage(d, enter) {
  const cpu = d.cpu || {}, dk = d.disk || {};
  let o = '';
  const th = document.documentElement.getAttribute('data-theme');
  if (window.AIHUB_CONFIG?.demoMode) o += '<div class="demo-banner">Documentation preview · Synthetic data · Actions disabled</div>';
  o += `<div id="alert-banner-wrap"></div><header><div class="logo">ai-hub <span>Local AI control panel</span></div><input id="model-filter" class="model-filter" type="search" placeholder="Filter models…" oninput="applyFilter(this.value)"><span class="spacer"></span><button class="btn-logout" onclick="openPalette()" title="Command palette — quickly run any action (Ctrl/Cmd+K)"><span class="hg">⌘</span>K&nbsp;Commands</button><button class="btn-logout" onclick="openCompare()" title="Compare all models + get a recommendation for your use case"><span class="hg">⚖</span>Compare</button><span class="hsep"></span><select id="exp-hours" class="exp-select" title="GPU telemetry CSV export range"><option value="1">1h</option><option value="24" selected>24h</option><option value="168">7d</option><option value="720">30d</option></select><button class="icon-btn wide" onclick="exportCSV()" title="Download GPU telemetry (core/VRAM temp, power, util, mem) as CSV"><span class="hg">⬇</span>CSV</button><span class="hsep"></span><button class="btn-logout" onclick="openModels()" title="Model manager — download models (resumable after reboot) + check hub connection"><span class="hg">📦</span>Models</button>${window.AIHUB_CONFIG?.caseURL ? `<a class="btn-logout" href="${window.AIHUB_CONFIG.caseURL.replace(/&/g, '&amp;').replace(/"/g, '&quot;')}" target="_blank" rel="noopener">Hardware</a>` : ''}<span class="hsep"></span><button id="themeBtn" class="icon-btn" onclick="toggleTheme()" title="Theme"><span class="hg">${th === 'light' ? '☀️' : '🌙'}</span></button><button class="icon-btn" onclick="openSettings()" title="Settings — notifications / Telegram bot"><span class="hg">⚙</span></button><button class="btn-logout" onclick="logout()">Logout</button></header>`;
  o += `<div class="gpu-drop-banner" id="gpu-drop-banner" style="display:none"><span class="gdb-icon">⚠️</span><span class="gdb-msg" id="gdb-msg"></span><button class="gdb-dismiss" onclick="dismissGpuDrop()">✕ dismiss</button></div>`;
  // D-01: modular page — overview / models /
  // connections+ops — instead of one flat block stream.
  o += `<section id="sec-overview" class="sec"><div class="sec-head"><span class="sec-title">Overview</span><span class="sec-sub">system & GPU telemetry · live</span></div>`;

  const tp = d.gpus.reduce((a, g) => a + g.power, 0);
  const tv = d.gpus.reduce((a, g) => a + g.mem_used, 0);
  const tm = d.gpus.reduce((a, g) => a + g.mem_total, 0);
  o += `<div class="sys">
    <div class="sys-card" title="CPU — both sockets (Socket0 / Socket1)"><div class="sys-label">CPU</div><div class="sys-val"><span id="sys-cpu0">${cpu.socket0 || 0}</span><span class="dim">/</span><span id="sys-cpu1">${cpu.socket1 || 0}</span><span class="dim">%</span></div><div class="sys-sub" id="sys-cpus">load ${cpu.load1 || 0} · ${cpu.cpu_count || 0} thr</div></div>
    <div class="sys-card clickable" onclick="openRamHealth()" title="Memory (RAM) — click for health: swap · EDAC CE/UE · MCE/AER + CE trend"><div class="sys-label">Memory</div><div class="sys-val" id="sys-mem">${(cpu.mem_used / 1024 || 0).toFixed(1)}<span class="dim"> / ${(cpu.mem_total / 1024 || 0).toFixed(0)} GB</span></div><div class="sys-sub"><span id="sys-mems">${Math.round(100 * (cpu.mem_used || 0) / (cpu.mem_total || 1))}% used</span><span class="mem-dot" id="mem-dot" title="RAM health — click to view"></span></div></div>
    <div class="sys-card" title="GPU — total power + total VRAM across all GPUs"><div class="sys-label">GPU</div><div class="sys-val"><span id="sys-gpu">${tp}W · ${(tv / 1024 || 0).toFixed(0)}/${(tm / 1024 || 0).toFixed(0)}G</span></div><div class="sys-sub" id="sys-gpus">${d.gpus.length} GPU${d.gpus.length === 1 ? '' : 's'} detected</div></div>
    <div class="sys-card clickable" onclick="openDiskModal()" title="Free disk space — click for the full disk breakdown"><div class="sys-label">Disk Free</div><div class="sys-val" id="sys-disk">${dk.disk_free || '—'}</div><div class="sys-sub" id="sys-disks">of ${dk.disk_total || '—'}</div></div>
    <div class="sys-card" title="System load average"><div class="sys-label">Load</div><div class="sys-val" id="sys-load">${(cpu.load1 || 0).toFixed(1)}</div><div class="sys-sub" id="sys-loads">5m ${(cpu.load5 || 0).toFixed(1)} · 15m ${(cpu.load15 || 0).toFixed(1)}</div></div>
    <div class="sys-card" title="Uptime since last boot"><div class="sys-label">Uptime</div><div class="sys-val" id="hist-uptime">—</div><div class="sys-sub" id="hist-up-sub">since last boot</div></div>
    <div class="sys-card clickable" onclick="openLastWordsDialog()" title="Reboots — click for last-words (pre-crash snapshots)"><div class="sys-label">Reboots</div><div class="sys-val"><span id="hist-reboots">—</span><span class="lw-mini" id="lw-mini" title="last-words snapshots">📄</span></div><div class="sys-sub" id="hist-reb-sub">recent boot events</div></div>
  </div>`;
  o += `<div class="gpus">`;
  for (const g of d.gpus) {
    const pct = Math.round(100 * g.mem_used / g.mem_total);
    const cls = pct > 88 ? 'crit' : pct > 75 ? 'warn' : '';
    o += `<div class="gpu"><div class="gpu-head"><span class="gpu-name">GPU${g.index}</span><span class="gpu-meta" id="gpu-${g.index}-meta">${gpuMeta(g)}</span></div><span class="gpu-tenants" id="gpu-${g.index}-tenants"></span><div class="gpu-bar"><div class="gpu-fill ${cls}" id="gpu-${g.index}-fill" style="width:${pct}%"></div></div><div class="gpu-foot"><span id="gpu-${g.index}-f1">${(g.mem_used / 1024).toFixed(1)} / ${(g.mem_total / 1024).toFixed(0)} GB</span><span id="gpu-${g.index}-f2">${pct}% · ${g.util}%</span></div><div class="gpu-charts"><div class="gc"><div class="gc-head"><span class="lt"><i style="background:#f59e0b"></i>GPU</span><span class="lt"><i style="background:#22d3ee"></i>VRAM</span></div><canvas id="gchart-${g.index}" height="56"></canvas></div><div class="gc"><div class="gc-head"><span class="lt"><i style="background:#f59e0b"></i>Power</span></div><canvas id="gpow-${g.index}" height="56"></canvas></div><div class="gc"><div class="gc-head"><span class="lt"><i style="background:#22d3ee"></i>Util</span></div><canvas id="gutl-${g.index}" height="56"></canvas></div><div class="gc"><div class="gc-head"><span class="lt"><i style="background:#22c55e"></i>Memory</span></div><canvas id="gmem-${g.index}" height="56"></canvas></div></div></div>`;
  }
  o += `</div></section>`;
  o += `<section id="sec-models" class="sec"><div class="sec-head"><span class="sec-title">Models</span><span class="sec-sub">language · media · start/stop from the card</span></div>`;
  const byCat = {};
  for (const m of d.models || []) (byCat[m.category] = byCat[m.category] || []).push(m);
  if (!(d.models || []).length) {
    o += `<div class="empty-models"><strong>Your model workspace is ready.</strong><p>Add an installed service to your registry, then restart ai-hub. The installation guide includes a llama.cpp example.</p><span>Model downloads and inference services are configured separately.</span></div>`;
  }
  let cardIdx = 0;
  for (const cat of CAT_ORDER) {
    const list = byCat[cat]; if (!list || !list.length) continue;
    const meta = CAT_META[cat] || { icon: "•", title: cat, desc: "" };
    const stopAllBtn = cat === 'LLM' && list.some(m => m.running) ? `<button class="btn btn-danger btn-stopall" style="margin-left:8px;padding:5px 10px;font-size:11.5px" onclick="stopAll()">■ Stop all</button>` : '';
    o += `<div class="cat"><div class="cat-head"${cat === 'LLM' ? ' id="llm-cat-head"' : ''}><div class="cat-icon">${meta.icon}</div><div class="cat-title">${meta.title}</div><span class="cat-count">${list.length} ${list.length === 1 ? 'model' : 'models'}</span>${stopAllBtn}<span class="cat-desc">${meta.desc}</span></div><div class="cat-grid">`;
    for (const m of list) { o += modelCard(m, cat, enter, cardIdx++); }
    o += `</div></div>`;
  }
  o += `</section>`;
  o += `<section id="sec-ops" class="sec"><div class="sec-head"><span class="sec-title">Connections & operations</span><span class="sec-sub">stable endpoints · data freshness · ops</span></div>${opsSection(d)}</section>`;
  o += `<div class="divider"></div><div style="display:flex;gap:10px;justify-content:space-between;align-items:center;padding:14px 2px;color:var(--fg3);font-size:12px"><span>ai-hub · Your models. Your hardware. · <span id="foot-ts">${new Date().toLocaleString()}</span></span><span style="display:flex;gap:12px"><a href="#" onclick="location.reload();return false" style="color:var(--fg3)">Refresh</a><a href="${hubURL()}" style="color:var(--fg3)">ai-hub</a></span></div>`;
  $('app').innerHTML = o;
  S.cardKey = {};
  for (const m of d.models || []) S.cardKey[m.id] = modelKey(m);
}

// ── D-01: connections & operations section ─────────────────────────
// Stable endpoints and the
// C-02 freshness contract rendered as data, not as an implicit assumption.
function opsSection(d) {
  const ts = (typeof d.ts === 'number') ? new Date(d.ts * 1000) : null;
  const stale = !!d.stale;
  const fresh = stale
    ? `<span class="ops-fresh stale" id="ops-fresh">⚠ snapshot is stale — data as of ${ts ? ts.toLocaleTimeString() : '—'} (${Math.round(d.age_s || 0)}s old), poller may be wedged</span>`
    : `<span class="ops-fresh" id="ops-fresh">● live · last poll ${ts ? ts.toLocaleTimeString() : '—'}</span>`;
  const llms = (d.models || []).filter(m => m.category === 'LLM');
  const modelRows = llms.map(m => {
    const dot = m.running ? '🟢' : '⚪';
    return `<div class="ops-row"><span>${dot} ${m.short || m.id}</span><a href="${serviceURL(m.port, '/v1')}" target="_blank" title="OpenAI-compatible base URL">:${m.port}/v1</a></div>`;
  }).join('');
  return `<div class="sec-ops">
    <div><h4>Stable endpoints</h4>
      <div class="ops-row"><span>ai-hub panel</span><a href="${hubURL()}" target="_blank">${new URL(hubURL()).host}</a></div>
      <div class="ops-row"><span>universal proxy</span><a href="${hubURL('/v1/models')}" target="_blank" title="Follows the last-started running LLM — one base URL for all clients">${new URL(hubURL()).host}/v1</a></div>
      <div class="ops-row"><span>Open WebUI</span><a href="${webuiURL()}" target="_blank">${new URL(webuiURL()).host}</a></div>
      <div class="ops-row"><span>MCP (7 tools)</span><a href="${mcpURL()}" target="_blank" title="MCP endpoint">${new URL(mcpURL()).host}/mcp</a></div>
    </div>
    <div><h4>LLM base URLs</h4>${modelRows || '<div class="ops-row"><span>—</span></div>'}</div>
    <div><h4>Ops</h4>${fresh}
      <div class="ops-row"><span>Telemetry</span><a href="#" onclick="exportCSV();return false">⬇ CSV export</a></div>
      <div class="ops-row"><span>API authentication</span><span>Bearer token / session</span></div>
    </div>
  </div>`;
}

// ── In-place update (no full rebuild) ────────────────────────────
function updatePage(d) {
  setSys(d); setGpus(d); setCards(d); syncStopAll(d);
  const fts = $('foot-ts'); if (fts) fts.textContent = new Date().toLocaleString();
  updateOpsFresh(d);
}

function updateOpsFresh(d) {
  const el = $('ops-fresh'); if (!el) return;
  const ts = (typeof d.ts === 'number') ? new Date(d.ts * 1000) : null;
  const stale = !!d.stale;
  el.className = 'ops-fresh' + (stale ? ' stale' : '');
  el.textContent = stale
    ? `⚠ snapshot is stale — data as of ${ts ? ts.toLocaleTimeString() : '—'} (${Math.round(d.age_s || 0)}s old), poller may be wedged`
    : `● live · last poll ${ts ? ts.toLocaleTimeString() : '—'}`;
}

function setSys(d) {
  const cpu = d.cpu || {}, dk = d.disk || {};
  const tp = d.gpus.reduce((a, g) => a + g.power, 0);
  const tv = d.gpus.reduce((a, g) => a + g.mem_used, 0);
  const tm = d.gpus.reduce((a, g) => a + g.mem_total, 0);
  const el = id => document.getElementById(id);
  const v = (id, html) => { const e = el(id); if (e) e.innerHTML = html; };
  const t = (id, txt) => { const e = el(id); if (e) e.textContent = txt; };
  t('sys-cpu0', `${cpu.socket0 || 0}`); t('sys-cpu1', `${cpu.socket1 || 0}`);
  t('sys-cpus', `load ${cpu.load1 || 0} · ${cpu.cpu_count || 0} thr`);
  v('sys-mem', `${(cpu.mem_used / 1024 || 0).toFixed(1)}<span style="font-size:13px;color:var(--fg3)"> / ${(cpu.mem_total / 1024 || 0).toFixed(0)} GB</span>`);
  t('sys-mems', `${Math.round(100 * (cpu.mem_used || 0) / (cpu.mem_total || 1))}% used`);
  v('sys-gpu', `${tp}W · ${(tv / 1024 || 0).toFixed(0)}/${(tm / 1024 || 0).toFixed(0)}G`);
  t('sys-gpus', `${d.gpus.length} GPU${d.gpus.length === 1 ? '' : 's'} detected`);
  v('sys-disk', `${dk.disk_free || '—'}`); t('sys-disks', `of ${dk.disk_total || '—'}`);
  t('sys-load', `${(cpu.load1 || 0).toFixed(1)}`);
  t('sys-loads', `5m ${(cpu.load5 || 0).toFixed(1)} · 15m ${(cpu.load15 || 0).toFixed(1)}`);
}

// Disk modal: openDiskModal / loadDiskModalData / renderDiskReport are in disk-modal.js

function gpuMeta(g) {
  const hasV = (typeof g.vram_temp === 'number' && g.vram_temp > 0);
  const vramCls = hasV ? 'tm-vram' : 'tm-vram na';
  const vramTxt = hasV ? `<b>${g.vram_temp}</b>°C` : '<b>—</b>';
  return `<span class="tm-core">GPU <b>${g.temp}</b>°C</span><span class="${vramCls}" title="GDDR6X memory temperature">VRAM ${vramTxt}</span><span><b>${g.power}</b>W</span>`;
}

function setGpus(d) {
  for (const g of d.gpus || []) {
    const fill = document.getElementById('gpu-' + g.index + '-fill');
    if (!fill) continue;
    const pct = Math.round(100 * g.mem_used / g.mem_total);
    fill.style.width = pct + '%';
    fill.className = 'gpu-fill ' + (pct > 88 ? 'crit' : pct > 75 ? 'warn' : '');
    const meta = document.getElementById('gpu-' + g.index + '-meta'); if (meta) meta.innerHTML = gpuMeta(g);
    const f1 = document.getElementById('gpu-' + g.index + '-f1'); if (f1) f1.textContent = `${(g.mem_used / 1024).toFixed(1)} / ${(g.mem_total / 1024).toFixed(0)} GB`;
    const f2 = document.getElementById('gpu-' + g.index + '-f2'); if (f2) f2.textContent = `${pct}% · ${g.util}%`;
    const ten = document.getElementById('gpu-' + g.index + '-tenants');
    if (ten) {
      const list = (d.models || []).filter(m => m.running && m.healthy && (m.gpus_used || []).includes(g.index));
      ten.innerHTML = list.map(m => `<span class="tenant-chip" title="${m.id}">${m.short || m.id}</span>`).join('');
    }
  }
}

function setCards(d) {
  const seen = new Set();
  for (const m of d.models || []) {
    seen.add(m.id);
    if (_pending[m.id] && m.running && m.healthy) _pending[m.id] = null;
    const key = modelKey(m);
    if (S.cardKey[m.id] === key) { patchLiveStats(m); continue; }
    S.cardKey[m.id] = key;
    replaceCard(m);
  }
  // Remove cards that are no longer in the response (e.g. a model whose
  // files are still downloading is hidden from /api/status until complete).
  for (const el of document.querySelectorAll('.model')) {
    const mid = (el.id || '').replace(/^card-/, '');
    if (mid && !seen.has(mid)) { delete S.cardKey[mid]; el.remove(); }
  }
}

// ── Model filter ─────────────────────────────────────────────────
function applyFilter(q) {
  q = (q || '').trim().toLowerCase();
  S.filter = q;
  for (const el of document.querySelectorAll('.model')) {
    const mid = (el.id || '').replace(/^card-/, '');
    const nameEl = el.querySelector('.model-name');
    const fullEl = el.querySelector('.model-full');
    const hay = (mid + ' ' + (nameEl ? nameEl.textContent : '') + ' ' + (fullEl ? fullEl.textContent : '')).toLowerCase();
    el.style.display = (!q || hay.indexOf(q) !== -1) ? '' : 'none';
  }
  for (const cat of document.querySelectorAll('.cat')) {
    let vis = false;
    for (const el of cat.querySelectorAll('.model')) { if (el.style.display !== 'none') { vis = true; break; } }
    cat.style.display = vis ? '' : 'none';
  }
}

// ── Stop-all sync ────────────────────────────────────────────────
function syncStopAll(d) {
  const head = $('llm-cat-head');
  if (!head) return;
  const any = (d.models || []).some(m => m.category === 'LLM' && m.running);
  const btn = head.querySelector('.btn-stopall');
  if (any && !btn) {
    const b = document.createElement('button');
    b.className = 'btn btn-danger btn-stopall';
    b.style.cssText = 'margin-left:8px;padding:5px 10px;font-size:11.5px';
    b.textContent = '■ Stop all';
    b.onclick = () => stopAll();
    head.insertBefore(b, head.querySelector('.cat-desc') || null);
  } else if (!any && btn) btn.remove();
}

// ── Render (diff-based) ─────────────────────────────────────────
function render(d) {
  const h = JSON.stringify(d); if (h === S.hash) return; S.hash = h;
  if (!S.mounted) { buildPage(d, true); S.mounted = true; return; }
  updatePage(d);
}

// ── Model actions (start/stop/restart) ───────────────────────────
async function act(mid, action) {
  if (_pending[mid]) return;
  const m = S.cache.models.find(x => x.id === mid);
  if ((action === 'start' || action === 'restart') && m && m.parallel && m.parallel.enabled)
    return launchStart(mid, action);
  return actDirect(mid, action);
}

async function actDirect(mid, action) {
  if (_pending[mid]) return;
  if (action === 'stop') { const ok = await showModal({ title: `Stop ${mid}?`, body: 'GPU memory will be released.', buttons: [{ label: 'Cancel', type: 'ghost', value: false }, { label: 'Stop', type: 'danger', value: true }] }); if (!ok) return; }
  _pending[mid] = action;
  syncCard(mid);
  toast(`${action === 'stop' ? 'Stopping' : action === 'start' ? 'Starting' : 'Restarting'} ${mid}…`);
  const done = { stop: 'stopped', start: 'started', restart: 'restarted' };
  try {
    const r = await api(`/api/model/${mid}/${action}`, { method: 'POST' });
    if (r.ok) { toast(`${mid} ${done[action] || 'done'}`); refresh(); }
    else { _pending[mid] = null; syncCard(mid); showModal({ title: 'Error', body: r.output || 'Failed', buttons: [{ label: 'OK', type: 'ghost', value: true }] }); }
  } catch (e) { _pending[mid] = null; syncCard(mid); if (e.message !== 'auth') toast('Error: ' + e.message, 'err'); }
}

function launchStart(mid, action) {
  const m = S.cache.models.find(x => x.id === mid);
  const s = lsSel(m);
  if (action === 'restart' && m.running && s && !s.touched) {
    const ng = m.chosen_gpus || s.gpus, nc = m.chosen_ctx || s.ctx, np = m.chosen_parallel || s.par;
    if (ng !== s.gpus || nc !== s.ctx || np !== s.par) {
      const caps = launchCaps(m);
      if (caps) { const r = resolveLaunch(caps, 'gpus', { gpus: ng, ctx: nc, par: np }); if (r) { s.gpus = r.g; s.ctx = r.c; s.par = r.p; } }
      else { s.gpus = ng; s.ctx = nc; s.par = np; }
    }
  }
  _ddOpen = null;
  _pending[mid] = action; syncCard(mid);
  toast(`${action === 'restart' ? 'Restarting' : 'Starting'} ${m.short || mid}: ${s.gpus} GPU · ${fmtK(s.ctx)} · ${s.par} agent${s.par > 1 ? 's' : ''}…`);
  return (async () => {
    try {
      const r = await api(`/api/model/${mid}/${action}?gpus=${s.gpus}&ctx=${s.ctx}&parallel=${s.par}`, { method: 'POST' });
      if (r.ok) { s.touched = false; lsSave(mid); toast(`${m.short || mid} ${action === 'restart' ? 'restarted' : 'started'} · :${m.port}`); refresh(); }
      else { _pending[mid] = null; syncCard(mid); showModal({ title: 'Error', body: r.output || 'Failed', buttons: [{ label: 'OK', type: 'ghost', value: true }] }); }
    } catch (e) { _pending[mid] = null; syncCard(mid); if (e.message !== 'auth') toast('Error: ' + e.message, 'err'); }
  })();
}

// ── Stop-all ─────────────────────────────────────────────────────
let _stopAllBusy = false;
async function stopAll() {
  if (_stopAllBusy) return;
  const ok = await showModal({ title: 'Stop all running LLMs?', body: 'All running LLM models will be stopped and their GPU memory released. Media services are not affected.', buttons: [{ label: 'Cancel', type: 'ghost', value: false }, { label: 'Stop all', type: 'danger', value: true }] });
  if (!ok) return;
  _stopAllBusy = true;
  try {
    const r = await api('/api/stop-all', { method: 'POST' });
    if (!r || r.ok === false) { toast((r && (r.detail || r.output)) || 'Stop-all failed', 'err'); refresh(); return; }
    const n = (r.stopped || []).length;
    toast(n ? `Stopped ${n}: ${r.stopped.join(', ')}` : 'Nothing was running');
    for (const [id, msg] of Object.entries(r.errors || {})) toast(`${id}: ${msg}`, 'err');
    refresh();
  } catch (e) { if (e.message !== 'auth') toast('Stop-all failed: ' + e.message, 'err'); }
  finally { _stopAllBusy = false; }
}

// ── Switch-to ────────────────────────────────────────────────────
async function switchTo(mid) {
  if (_stopAllBusy) return;
  const models = (S.cache && S.cache.models) || [];
  const m = models.find(x => x.id === mid);
  if (!m) return;
  if (m.running) { toast((m.short || m.name) + ' is already running', 'warn'); return; }
  const others = models.filter(x => (x.backend === 'llama.cpp' || x.backend === 'vLLM') && x.running && x.id !== mid);
  const doStart = async () => {
    if (others.length) {
      const r = await api('/api/stop-all', { method: 'POST' });
      if (!r || r.ok === false) { toast((r && (r.detail || r.output)) || 'Stop-all failed', 'err'); refresh(); return false; }
      const n = (r.stopped || []).length;
      if (n) toast(`Stopped ${n}: ${r.stopped.join(', ')}`);
      for (const [id, msg] of Object.entries(r.errors || {})) toast(`${id}: ${msg}`, 'err');
      refresh();
    }
    await act(mid, 'start');
    return true;
  };
  if (!others.length) { await doStart(); return; }
  const ok = await showModal({
    title: `Switch to ${m.short || m.name}?`,
    body: `Stops <b>${others.map(x => x.short || x.name).join(', ')}</b> (frees their VRAM), then starts <b>${m.short || m.name}</b> on the freed GPUs. Media services are not affected.`,
    buttons: [{ label: 'Cancel', type: 'ghost', value: false }, { label: 'Switch', type: 'primary', value: true }]
  });
  if (!ok) return;
  _stopAllBusy = true;
  try { await doStart(); } catch (e) { if (e.message !== 'auth') toast('Switch failed: ' + e.message, 'err'); }
  finally { _stopAllBusy = false; }
}

// ── Benchmark ────────────────────────────────────────────────────
async function runBenchmark(mid) {
  const m = ((S.cache && S.cache.models) || []).find(x => x.id === mid);
  const name = (m && (m.short || m.name)) || mid;
  let el = showModal({
    title: `📊 Benchmark — ${name}`,
    body: `<div class="bench-run" id="bench-status">Running a fixed 256-token prompt and timing the stream…<div class="bench-spinner"></div></div>`,
    buttons: [{ label: 'Close', type: 'ghost', value: true }], wide: true
  });
  try {
    const r = await api('/api/model/' + encodeURIComponent(mid) + '/benchmark', { method: 'POST' });
    const st = $('bench-status');
    if (st) {
      if (!r || r.ok === false) {
        st.innerHTML = `<div class="bench-err">⚠ ${((r && (r.detail)) || 'benchmark failed')}</div>`;
      } else {
        st.innerHTML = `<div class="bench-grid">
          <div class="bench-cell"><div class="bench-k">Time to first token</div><div class="bench-v">${(r.ttft_s != null ? r.ttft_s : '—')} s</div></div>
          <div class="bench-cell"><div class="bench-k">Decode speed</div><div class="bench-v">${(r.tps != null ? r.tps : '—')} tok/s</div></div>
          <div class="bench-cell"><div class="bench-k">Tokens</div><div class="bench-v">${r.tokens != null ? r.tokens : '—'}</div></div>
          <div class="bench-cell"><div class="bench-k">Total time</div><div class="bench-v">${(r.total_s != null ? r.total_s : '—')} s</div></div>
        </div><div class="bench-note">Fixed prompt (count 1→256), temp=0, max_tokens=256. Decode speed = (tokens−1) ÷ (total − TTFT).</div>`;
      }
    }
  } catch (e) {
    if (e.message === 'auth') return;
    const st = $('bench-status');
    if (st) st.innerHTML = `<div class="bench-err">⚠ ${e.message}</div>`;
  }
  return el;
}

// ── Show logs ────────────────────────────────────────────────────
async function showLogs(mid) {
  try {
    const r = await fetch('/api/logs/' + mid + '?tail=200');
    if (r.status === 401) { loginView(); return; }
    const txt = await r.text();
    const o = document.createElement('div'); o.className = 'modal-overlay';
    const wrap = document.createElement('div'); wrap.className = 'logs-modal';
    const modal = document.createElement('div'); modal.className = 'modal';
    modal.innerHTML = '<h3>Logs \u2014 ' + mid + '</h3><p>Last 200 lines</p>';
    const pre = document.createElement('pre'); pre.className = 'log-box'; pre.textContent = txt.slice(-6000) || 'No logs yet';
    modal.appendChild(pre);
    const acts = document.createElement('div'); acts.className = 'modal-actions';
    const btn = document.createElement('button'); btn.className = 'btn btn-ghost'; btn.textContent = 'Close';
    btn.onclick = () => o.remove(); acts.appendChild(btn); modal.appendChild(acts);
    wrap.appendChild(modal); o.appendChild(wrap); $('modal-root').appendChild(o);
    o.addEventListener('click', e => { if (e.target === o) o.remove(); });
    const escH = e => { if (e.key === 'Escape') { document.removeEventListener('keydown', escH); o.remove(); } };
    document.addEventListener('keydown', escH);
  } catch (e) { toast('Failed: ' + e.message, 'err'); }
}

// ── Show API info ────────────────────────────────────────────────
function showApi(id, port, backend) {
  const base = `${serviceURL(port, '/v1')}`;
  const model = backend === 'Maya' ? (id === 'glm53-flash-maya-m-derisked' ? 'glm-5.3-flash-derisked' : 'glm-5.3-flash') : id.includes('gemma') ? (id.includes('uncensored') ? 'gemma-4-31b-uncensored' : 'gemma-4-31b') : 'qwen3.8-27b';
  const authArg = backend === 'Maya' ? ' -H "Authorization: Bearer $MAYA_API_KEY"' : '';
  const docsUrl = `${serviceURL(port, '/docs')}`;
  const body = `
<div style="text-align:left;font-family:JetBrains Mono,monospace;font-size:12px;line-height:1.6">
<div style="margin-bottom:12px"><b style="color:var(--fg)">Base URL:</b> <code style="background:var(--muted);padding:2px 6px;border-radius:6px">${base}</code> <span style="color:var(--fg3)">(${backend})</span></div>
<div style="margin-bottom:12px"><b>Model:</b> <code style="background:var(--muted);padding:2px 6px;border-radius:6px">${model}</code></div>
<div id="api-curl" style="background:var(--muted);border:1px solid var(--border);border-radius:10px;padding:12px;margin:10px 0;overflow-x:auto;white-space:pre">curl -X POST ${base}/chat/completions${authArg} \\
   -H "Content-Type: application/json" \\
   -d '{\n    "model": "${model}",\n    "messages": [{"role":"user","content":"Hello"}]\n  }'</div>
 <button class="btn btn-ghost" style="padding:5px 12px;font-size:11.5px" onclick="copyCurl()" title="Copy the curl command to the clipboard">Copy</button>
<div style="display:flex;gap:8px;flex-wrap:wrap;margin-top:12px">
  <a href="${base}/models" target="_blank" style="color:var(--fg);background:var(--muted);border:1px solid var(--border);padding:6px 12px;border-radius:8px;text-decoration:none;font-size:12px">GET /v1/models</a>
  <a href="${docsUrl}" target="_blank" style="color:var(--fg);background:var(--muted);border:1px solid var(--border);padding:6px 12px;border-radius:8px;text-decoration:none;font-size:12px">/docs (vLLM)</a>
  <a href="${serviceURL(port, '/health')}" target="_blank" style="color:var(--fg);background:var(--muted);border:1px solid var(--border);padding:6px 12px;border-radius:8px;text-decoration:none;font-size:12px">/health</a>
</div>
<div style="margin-top:10px;color:var(--fg3);font-size:11px">OpenAI-compatible. Use any OpenAI SDK with the baseURL above. Chat UI — the 💬 Chat button.${backend === 'Maya' ? ' Maya requires a private API key in MAYA_API_KEY; Open WebUI, OpenCode and pi are already configured.' : ''}</div>
</div>`;
  showModal({ title: `API — ${id} :${port}`, body: body, buttons: [{ label: 'Close', type: 'ghost', value: true }] });
}

function copyCurl() {
  const el = $('api-curl');
  if (!el) return;
  const txt = el.textContent;
  const fb = () => { try { window.prompt('Copy to clipboard (text is preselected — Ctrl+C):', txt); } catch (e) {} };
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(txt).then(() => toast('Copied curl command'), fb);
    else fb();
  } catch (e) { fb(); }
}
