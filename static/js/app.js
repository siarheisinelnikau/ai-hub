/* ═══════════════════════════════════════════════════════════════════
   app.js — initialization, refresh loop, boot sequence
   ═══════════════════════════════════════════════════════════════════ */

// ── Build command palette commands ────────────────────────────────
function buildCommands() {
  const models = (S.cache && S.cache.models) || [];
  const llms = models.filter(m => m.category === 'LLM');
  const cmds = [];
  cmds.push({ id: 'stop-all', label: '■ Stop all running LLMs', hint: 'action', run: () => stopAll() });
  cmds.push({ id: 'compare', label: '⚖ Compare models & recommend', hint: 'view', run: () => openCompare() });
  cmds.push({ id: 'export', label: '⬇ Export GPU telemetry CSV', hint: 'export', run: () => exportCSV() });
  cmds.push({ id: 'theme', label: '🌗 Toggle theme', hint: 'view', run: () => toggleTheme() });
  for (const m of llms) {
    const name = m.short || m.name;
    if (!m.running) cmds.push({ id: 'start-' + m.id, label: `▶ Start ${name}`, hint: 'stop', run: () => act(m.id, 'start') });
    if (!m.running) cmds.push({ id: 'switch-' + m.id, label: `⤢ Switch to ${name}`, hint: 'stop', run: () => switchTo(m.id) });
    if (m.running) cmds.push({ id: 'stop-' + m.id, label: `■ Stop ${name}`, hint: 'run', run: () => act(m.id, 'stop') });
    if (m.running) cmds.push({ id: 'restart-' + m.id, label: `↻ Restart ${name}`, hint: 'run', run: () => act(m.id, 'restart') });
    if (m.running) cmds.push({ id: 'bench-' + m.id, label: `📊 Benchmark ${name}`, hint: 'run', run: () => runBenchmark(m.id) });
    cmds.push({ id: 'logs-' + m.id, label: `📜 Logs — ${name}`, hint: 'view', run: () => showLogs(m.id) });
  }
  return cmds;
}

// ── Compare & recommend ──────────────────────────────────────────
function numFirst(s) { const m = String(s || '').match(/(\d+(?:\.\d+)?)/); return m ? parseFloat(m[1]) : 0; }
function numCtx(s) { const m = String(s || '').match(/(\d+)\s*k/i); return m ? parseInt(m[1], 10) * 1000 : 0; }
function vramTotal(m) {
  const x = String(m.vram || '').match(/(\d+(?:\.\d+)?)\s*GB\s*[×x]\s*(\d+)/i);
  if (x) return parseFloat(x[1]) * parseInt(x[2], 10);
  const y = String(m.vram || '').match(/(\d+(?:\.\d+)?)\s*GB/i);
  return y ? parseFloat(y[1]) : 0;
}
const QUANT_RANK = { Q8_0: 5, Q8_K_XL: 5, FP8: 5, Q4_K_M: 4, W4A16: 4, MXFP4: 3 };
const RECOMMENDERS = [
  { id: 'fast', label: '⚡ Fastest responses (max tok/s)', pick: l => l.filter(m => numFirst(m.tps) > 0).sort((a, b) => numFirst(b.tps) - numFirst(a.tps))[0], reason: m => 'highest decode speed (' + (m.tps || '—') + ')' },
  { id: 'ctx', label: '📚 Longest context window', pick: l => l.filter(m => numCtx(m.ctx) > 0).sort((a, b) => numCtx(b.ctx) - numCtx(a.ctx))[0], reason: m => 'largest context (' + (m.ctx || '—') + ')' },
  { id: 'quality', label: '🎯 Most precise / highest quality', pick: l => [...l].sort((a, b) => (QUANT_RANK[b.quant] || 0) - (QUANT_RANK[a.quant] || 0) || numCtx(b.ctx) - numCtx(a.ctx))[0], reason: m => 'highest-precision quant (' + (m.quant || '—') + ')' },
  { id: 'footprint', label: '🪶 Lowest VRAM footprint', pick: l => l.filter(m => vramTotal(m) > 0).sort((a, b) => vramTotal(a) - vramTotal(b))[0], reason: m => 'smallest total VRAM (' + (m.vram || '—') + ')' },
];
function recResult(rec, llms) {
  const m = rec.pick(llms);
  if (!m) return '<div class="rec-empty">no LLM matches this criterion</div>';
  const running = m.running ? '<span class="rec-run ok">● running</span>' : '<span class="rec-run off">○ stopped</span>';
  return `<div class="rec-pick">${running}<b>${m.short || m.name}</b><span class="rec-why">— ${rec.reason(m)}</span>
    <div class="rec-meta">${m.quant || ''} · ${m.backend || ''} · :${m.port} · ${(m.vram || '')} · ${(m.tps || '—')} · ctx ${m.ctx || '—'}</div>
    <div class="rec-actions">${m.running ? '' : '<button class="btn btn-primary" onclick="closeModal();act(\'' + m.id + '\',\'start\')">▶ Start it</button>'}
    <button class="btn btn-ghost" onclick="showLogs('${m.id}')">Logs</button></div></div>`;
}
function openCompare() {
  const models = (S.cache && S.cache.models) || [];
  const llms = models.filter(m => m.category === 'LLM');
  const bestTps = Math.max(0, ...llms.map(m => numFirst(m.tps)));
  const bestCtx = Math.max(0, ...llms.map(m => numCtx(m.ctx)));
  const rows = [...llms].sort((a, b) => numFirst(b.tps) - numFirst(a.tps) || numCtx(b.ctx) - numCtx(a.ctx)).map(m => {
    const t = numFirst(m.tps), c = numCtx(m.ctx);
    return `<tr class="${m.running ? 'mrow-run' : ''}"><td>${m.running ? '● ' : ''}${m.short || m.name}</td><td>${m.quant || '—'}</td><td>${m.backend || '—'}</td><td>:${m.port}</td><td>${m.vram || '—'}</td><td class="${t === bestTps && t > 0 ? 'best' : ''}">${m.tps || '—'}</td><td class="${c === bestCtx && c > 0 ? 'best' : ''}">${m.ctx || '—'}</td></tr>`;
  }).join('');
  const body = `<div class="cmp-section"><div class="cmp-title">Comparison (LLMs, ranked by speed then context)</div>
    <table class="cmp-table"><thead><tr><th>Model</th><th>Quant</th><th>Backend</th><th>Port</th><th>VRAM</th><th>TPS</th><th>Ctx</th></tr></thead><tbody>${rows}</tbody></table>
    <div class="cmp-note">green = best in that column · ● = running</div></div>
    <div class="cmp-section"><div class="cmp-title">Recommend me a model</div>
    <select id="rec-select" class="rec-select" onchange="recUpdate()">${RECOMMENDERS.map(r => `<option value="${r.id}">${r.label}</option>`).join('')}</select>
    <div id="rec-out">${recResult(RECOMMENDERS[0], llms)}</div></div>`;
  showModal({ title: '⚖ Compare & recommend', body, buttons: [{ label: 'Close', type: 'ghost', value: true }], wide: true });
}
function recUpdate() {
  const sel = $('rec-select'); if (!sel) return;
  const rec = RECOMMENDERS.find(r => r.id === sel.value) || RECOMMENDERS[0];
  const llms = ((S.cache && S.cache.models) || []).filter(m => m.category === 'LLM');
  const out = $('rec-out'); if (out) out.innerHTML = recResult(rec, llms);
}

// ── Model manager ────────────────────────────────────────────────
async function fetchModels() { const r = await api('/api/models'); return (r.models || []); }
let _mmTimer = null, _mmSig = '';
function mmProgText(d) {
  const gb = v => (v >= 10 ? v.toFixed(1) : (v || 0).toFixed(2)) + ' GB';
  let s = (d.total_gb > 0) ? ((d.pct || 0).toFixed(1) + '% · ' + gb(d.downloaded_gb) + ' / ' + gb(d.total_gb)) : (gb(d.downloaded_gb) + ' downloaded');
  if (d.speed_mbps > 0) s += ' · ' + d.speed_mbps.toFixed(1) + ' MB/s';
  return s;
}
// Updates the Model manager dialog in place every 2s while it is open:
// progress bars/percent/GB/speed stream live, and only a real state change
// (downloading -> complete/error, on-disk toggle) re-renders the list.
async function refreshModelsLive(force) {
  const bodyEl = document.getElementById('mm-body');
  if (!bodyEl) { if (_mmTimer) { clearInterval(_mmTimer); _mmTimer = null; } return; }
  let models;
  try { models = await fetchModels(); } catch (e) { return; }
  const sig = models.map(m => { const d = m.download || {}; return m.id + ':' + d.status + ':' + (m.on_disk ? 1 : 0) + ':' + (m.size_gb || 0); }).join('|');
  if (force || sig !== _mmSig) { _mmSig = sig; bodyEl.innerHTML = renderModels(models); return; }
  for (const m of models) {
    const d = m.download || {};
    if (d.status !== 'downloading') continue;
    const bar = document.getElementById('mmbar-' + m.id);
    const txt = document.getElementById('mmtxt-' + m.id);
    if (bar) bar.style.width = Math.min(100, d.pct || 0).toFixed(1) + '%';
    if (txt) txt.textContent = mmProgText(d);
  }
}
async function openModels() {
  if (_mmTimer) { clearInterval(_mmTimer); _mmTimer = null; }
  let models = [];
  try { models = await fetchModels(); } catch (e) { toast('Could not load models: ' + e.message, 'err'); return; }
  _mmSig = '';
  showModal({ title: '📦 Model manager — download (resumable) + hub connection', body: `<div id="mm-body">${renderModels(models)}</div>`,
    buttons: [{ label: 'Refresh', type: 'primary', value: 1 }, { label: 'Close', type: 'ghost', value: 0 }], wide: true })
    .then(v => { if (_mmTimer) { clearInterval(_mmTimer); _mmTimer = null; } if (v === 1) openModels(); });
  _mmTimer = setInterval(refreshModelsLive, 2000);
}
function renderModels(models) {
  const esc = s => String(s == null ? '' : s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');
  const row = (m) => {
    const dl = m.download || {};
    const onDisk = m.on_disk ? `<span class="mm-ok">✓ ${(m.size_gb || 0)} GB</span>` : `<span class="mm-bad">✗ not on disk</span>`;
    let dlst;
    if (dl.status === 'downloading') dlst = `<span class="mm-dl">⬇ downloading…</span><div class="mm-prog"><div class="mm-prog-bar" id="mmbar-${m.id}" style="width:${Math.min(100, dl.pct || 0).toFixed(1)}%"></div></div><div class="mm-prog-txt" id="mmtxt-${m.id}">${mmProgText(dl)}</div>`;
    else if (dl.status === 'complete') dlst = '<span class="mm-ok">✓ complete</span>';
    else if (dl.status === 'error') dlst = '<span class="mm-bad">✗ error</span>';
    else dlst = '<span class="mm-idle">idle</span>';
    const conn = m.connection || {};
    const cc = (v, lab) => `<span class="${v ? 'mm-ok' : 'mm-bad'}">${v ? '✓' : '✗'} ${lab}</span>`;
    const busy = dl.status === 'downloading';
    const dlBtn = !m.hf_repo
      ? `<span class="mm-idle" title="ships inside the Docker image / syv-ai repo — no plain-HF download">image-based</span>`
      : busy ? `<button class="btn btn-ghost mm-btn" disabled>⏳ in progress</button>`
      : dl.status === 'error' ? `<button class="btn mm-btn" onclick="mmDownload('${m.id}')" title="${esc(dl.error || '')}">↻ Retry download</button>`
      : m.on_disk ? `<button class="btn btn-ghost mm-btn mm-subtle" onclick="mmDownload('${m.id}')" title="Model is already on disk — this re-verifies its files from HuggingFace">↻ Re-sync</button>`
      : `<button class="btn mm-btn" onclick="mmDownload('${m.id}')" title="Download from HuggingFace (resumable, survives reboot)">⬇ Download${m.size_gb ? ' (' + m.size_gb + ' GB)' : ''}</button>`;
    return `<div class="mm-row"><div class="mm-id"><b>${esc(m.short || m.id)}</b><span class="mm-meta">${esc(m.quant || '')} · ${esc(m.backend || '')} · :${m.port} · <code>${esc(m.id)}</code></span></div><div class="mm-cell">${onDisk}</div><div class="mm-cell">${dlst}${dl.error ? `<div class="mm-err">${esc(dl.error)}</div>` : ''}</div><div class="mm-cell">${cc(conn.registry, 'reg')} ${cc(conn.service, 'svc')} ${cc(conn.webui, 'webui')}</div><div class="mm-actions">${dlBtn}<button class="btn btn-ghost mm-btn" onclick="mmConnect('${m.id}')">🔌 Connect</button><button class="btn btn-ghost mm-btn mm-del" onclick="mmDelete('${m.id}')" title="Remove weights; keep this model available for download and reuse.">🗑 Delete weights</button></div></div>`;
  };
  return `<div class="mm-list">${models.map(row).join('')}</div>
    <div class="mm-note"><b>⬇ Download</b> = pull from HuggingFace (only when not on disk). <b>↻ Re-sync</b> = already on disk, re-verifies files. <b>🔌 Connect</b> = check hub touchpoints. <b>🗑 Delete weights</b> = free disk space; keep the download source and launch settings. All downloads are resumable.</div>`;
}
async function mmDownload(id) {
  try { const r = await api('/api/models/download', { method: 'POST', body: JSON.stringify({ id }) }); if (!r.ok) { toast('Download: ' + (r.detail || '?'), 'err'); return; } toast('Download started (resumable after reboot)', 'ok'); refreshModelsLive(true); } catch (e) { toast('Download failed: ' + e.message, 'err'); }
}
async function mmConnect(id) {
  try {
    const r = await api('/api/models/connect', { method: 'POST', body: JSON.stringify({ id }) });
    const c = r.connection || {};
    const line = (v, lab) => (v ? '✓ ' : '✗ ') + lab;
    if (r.ok) showModal({ title: '🔌 ' + id + ' — connected', body: `<div class="mm-conn">${line(c.registry, 'registry')} · ${line(c.service, 'service')} · ${line(c.webui, 'webui')}</div>`, buttons: [{ label: 'Close', type: 'ghost', value: true }] });
    else { const missing = (c.notes && c.notes.length) ? c.notes.join(' · ') : 'see details'; showModal({ title: '🔌 ' + id + ' — not fully connected', body: `<div class="mm-conn bad">Missing: ${missing}</div>`, buttons: [{ label: 'Close', type: 'ghost', value: true }] }); }
  } catch (e) { toast('Connect check failed: ' + e.message, 'err'); }
}
async function mmDelete(id) {
  const esc = s => String(s == null ? '' : s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');
  const steps = (st) => (st || []).map(x => `<div style="padding:4px 0;border-bottom:1px solid var(--border)">${x.done ? '<span style="color:var(--ok)">✓</span>' : '<span style="color:var(--err)">✗</span>'} <b>${esc(x.what)}</b>${x.note ? ` <span style="color:var(--fg3);font-size:11px">${esc(x.note)}</span>` : ''}</div>`).join('');
  let prev;
  try { const r = await api('/api/models/delete', { method: 'POST', body: JSON.stringify({ id, hub: false, dry_run: true }) }); if (!r.ok) { toast('Preview failed: ' + (r.detail || '?'), 'err'); return; } prev = r; } catch (e) { toast('Preview failed: ' + e.message, 'err'); return; }
  showModal({ title: '🗑 Delete ' + id + ' ?',
    body: `<div style="font-size:13px;color:var(--fg2);margin-bottom:10px">Weights will be removed. You can download this model again. Preview:</div><div style="max-height:260px;overflow:auto;margin-bottom:10px">${steps(prev.steps)}</div>`,
    buttons: [{ label: '🗑 Delete now', type: 'danger', value: 1 }, { label: 'Cancel', type: 'ghost', value: 0 }] }).then(async v => {
    if (v !== 1) return;
    try {
      const r = await api('/api/models/delete', { method: 'POST', body: JSON.stringify({ id, hub: false, dry_run: false }) });
      if (!r.ok) { toast('Delete failed: ' + (r.detail || '?'), 'err'); return; }
      showModal({ title: '🗑 Deleted ' + id, body: `<div style="font-size:13px;color:var(--fg2);margin-bottom:10px">Done.</div>${steps(r.steps)}`, buttons: [{ label: 'OK', type: 'primary', value: 1 }] }).then(() => openModels());
    } catch (e) { toast('Delete failed: ' + e.message, 'err'); }
  });
}

// ── Settings (webhooks + cloud models) ─────────────────────────
let _cloud = null;
let _cloudList = [];
function _esc(s) { return String(s == null ? '' : s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;'); }
// Display defaults for known ChatGPT/Codex models (priority: GPT-6 Astra).
const CLOUD_DEFAULTS = {
  'gpt-6-astra': { name: 'GPT-6 Astra', short: 'Astra', desc: 'GPT-6 · ChatGPT Work / Codex', tps: '—', ctx: '—', priority: true },
  'gpt-5.6-sol': { name: 'GPT-5.6 Sol', short: 'Sol' },
  'gpt-5.6-terra': { name: 'GPT-5.6 Terra', short: 'Terra' },
  'gpt-5.6-luna': { name: 'GPT-5.6 Luna', short: 'Luna' },
  'gpt-5.5': { name: 'GPT-5.5', short: 'GPT-5.5' },
  'gpt-image-2': { name: 'GPT Image 2', short: 'Image 2', desc: 'Image generation / editing' }
};

async function openSettings() {
  let urls = [], source = 'file';
  try { const s = await api('/api/settings'); urls = (s.webhooks && s.webhooks.urls) || []; source = (s.webhooks && s.webhooks.source) || 'file'; } catch (e) {}
  try { _cloud = await api('/api/cloud'); } catch (e) { _cloud = null; }
  const urlList = urls.length ? urls.join('\n') : '(none configured)';
  const body = `<div class="set-sec">
    <div class="set-sec-title">🔔 Notifications · webhooks</div>
    ${source === 'env' ? '<div class="set-note warn">AIHUB_WEBHOOK_URLS is set via environment — file edits below are ignored.</div>' : ''}
    <div class="set-sub">Telegram bot (recommended)</div>
    <div class="set-bot"><input id="bot-token" class="set-input" type="text" placeholder="Bot token" autocomplete="off" spellcheck="false"><input id="bot-chat" class="set-input" type="text" placeholder="Chat ID" autocomplete="off" spellcheck="false"><button class="btn btn-ghost" onclick="addBotURL()">+ Add bot URL</button></div>
    <div class="set-sub set-sub2">All webhook URLs (one per line)</div>
    <textarea id="set-urls" class="set-textarea" rows="6" spellcheck="false">${urlList}</textarea>
    <div class="set-actions"><button class="btn btn-primary" onclick="saveSettings()">💾 Save</button><button class="btn btn-ghost" onclick="testSettings()">📡 Send test</button><span class="set-result" id="set-result"></span></div>
    <div id="set-test-results" class="set-test-results"></div>
  </div>${cloudSection()}`;
  showModal({ title: '⚙ Settings', body, buttons: [{ label: 'Close', type: 'ghost', value: true }], wide: true });
}

// ── Cloud models section (OpenAI OAuth mapping) ─────────────────
function cloudSection() {
  if (!_cloud) return `<div class="set-sec" style="margin-top:22px;border-top:1px solid var(--border);padding-top:18px"><div class="set-sec-title">☁️ Cloud models · OpenAI OAuth</div><div class="set-note warn">Could not load cloud settings.</div></div>`;
  const c = _cloud;
  return `<div class="set-sec" style="margin-top:22px;border-top:1px solid var(--border);padding-top:18px">
    <div class="set-sec-title">☁️ Cloud models · OpenAI OAuth (ChatGPT)</div>
    <div class="set-note">Map ChatGPT/Codex models (e.g. <b>GPT-6 Astra</b>) to hub cards. The hub talks to an OpenAI-compatible <code>openai-oauth</code> proxy — set its base URL below. Mapped models render in the <b>Cloud</b> section.</div>
    <label style="display:flex;align-items:center;gap:9px;font-size:13px;color:var(--fg2);font-weight:600;margin:4px 0 12px;cursor:pointer"><input type="checkbox" id="cl-enabled" ${c.enabled ? 'checked' : ''} style="width:16px;height:16px;accent-color:var(--fg);cursor:pointer"> Enable cloud models</label>
    <div class="set-bot">
      <input id="cl-base" class="set-input" placeholder="http://127.0.0.1:10531/v1" value="${_esc(c.base_url)}" spellcheck="false">
      <input id="cl-svc" class="set-input" placeholder="ai-openai-oauth" value="${_esc(c.service)}" spellcheck="false" style="flex:0 1 170px">
      <button class="btn btn-ghost" onclick="cloudFetch()">↻ Fetch models</button>
      <button class="btn btn-ghost" onclick="cloudAction('start')">▶ Start</button>
      <button class="btn btn-ghost" onclick="cloudAction('stop')">■ Stop</button>
    </div>
    <div class="set-sub" id="cl-status" style="margin-top:12px">${cloudStatusText(c)}</div>
    <div id="cloud-body">${cloudRender()}</div>
    <div class="set-actions"><button class="btn btn-primary" onclick="cloudSave()">💾 Save cloud mapping</button><span class="set-result" id="cloud-result"></span></div>
  </div>`;
}
function cloudStatusText(c) {
  const dot = c.healthy ? '<span style="color:var(--ok)">● healthy</span>' : (c.running ? '<span style="color:var(--warn)">◐ running</span>' : '<span style="color:var(--fg3)">○ offline</span>');
  const unit = c.installed ? 'local unit' : 'external provider';
  const extra = c.remote_err ? ` · <span style="color:var(--warn)">${_esc(c.remote_err)}</span>` : '';
  return `provider: ${dot} · ${unit} · :${c.port}${extra}`;
}
function cloudRender() {
  const c = _cloud;
  const byId = {};
  for (const m of (c.models || [])) byId[m.id] = m;
  const ids = [], seen = {};
  for (const r of (c.remote || [])) { if (r && r.id && !seen[r.id]) { seen[r.id] = 1; ids.push(r.id); } }
  for (const m of (c.models || [])) { if (m && m.id && !seen[m.id]) { seen[m.id] = 1; ids.push(m.id); } }
  _cloudList = ids.map(id => {
    const m = byId[id] || {}, d = CLOUD_DEFAULTS[id] || {};
    return {
      id,
      name: m.name || d.name || id,
      short: m.short || d.short || id,
      enabled: m.enabled !== undefined ? !!m.enabled : !!d.priority,
      category: m.category || 'Cloud',
      quant: m.quant || '', tps: m.tps || d.tps || '', ctx: m.ctx || d.ctx || '',
      desc: m.desc || d.desc || '', accel: m.accel || '', hf: m.hf || '',
      in: m.in || ['text'], out: m.out || ['text']
    };
  });
  if (!_cloudList.length) return `<div class="set-note" style="margin-top:0">No models yet — set the base URL and click <b>↻ Fetch models</b>.</div>`;
  const rows = _cloudList.map((m, i) => `<div style="display:flex;align-items:center;gap:8px;padding:7px 0;border-bottom:1px solid var(--border)">
    <input type="checkbox" class="cl-en" data-i="${i}" ${m.enabled ? 'checked' : ''} style="width:15px;height:15px;accent-color:var(--fg);cursor:pointer">
    <code style="flex:0 0 168px;font-size:11.5px;color:var(--fg3);overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${_esc(m.id)}">${_esc(m.id)}</code>
    <input class="set-input cl-name" data-i="${i}" value="${_esc(m.name)}" placeholder="Name" style="flex:1 1 140px">
    <input class="set-input cl-short" data-i="${i}" value="${_esc(m.short)}" placeholder="Short" style="flex:0 1 120px">
  </div>`).join('');
  const en = _cloudList.filter(m => m.enabled).length;
  return `<div style="max-height:260px;overflow:auto">${rows}</div><div style="font-size:11px;color:var(--fg3);margin-top:8px">${en} of ${_cloudList.length} enabled · cards appear in the hub's Cloud section</div>`;
}
function cloudCollect() {
  for (const el of document.querySelectorAll('.cl-en')) { const i = +el.dataset.i; if (_cloudList[i]) _cloudList[i].enabled = el.checked; }
  for (const el of document.querySelectorAll('.cl-name')) { const i = +el.dataset.i; if (_cloudList[i]) _cloudList[i].name = el.value.trim(); }
  for (const el of document.querySelectorAll('.cl-short')) { const i = +el.dataset.i; if (_cloudList[i]) _cloudList[i].short = el.value.trim(); }
}
async function cloudFetch() {
  const res = $('cloud-result'); if (res) res.textContent = 'Fetching…';
  try {
    const base = ($('cl-base') || {}).value || '';
    const r = await api('/api/cloud/refresh', { method: 'POST', body: JSON.stringify({ base_url: base }) });
    if (_cloud) { _cloud.remote = r.models || []; _cloud.remote_err = r.error || ''; _cloud.base_url = base; }
    const b = $('cloud-body'); if (b) b.innerHTML = cloudRender();
    const s = $('cl-status'); if (s && _cloud) s.innerHTML = cloudStatusText(_cloud);
    if (res) res.textContent = r.ok ? ((r.models || []).length + ' model(s) found') : ('Fetch failed: ' + (r.error || '?'));
    if (!r.ok) toast('Cloud fetch failed: ' + (r.error || '?'), 'err');
  } catch (e) { if (res) res.textContent = 'Error: ' + e.message; }
}
async function cloudSave() {
  if (!_cloud) return;
  cloudCollect();
  const res = $('cloud-result'); if (res) res.textContent = '…';
  const payload = {
    enabled: !!($('cl-enabled') || {}).checked,
    base_url: ($('cl-base') || {}).value || '',
    service: ($('cl-svc') || {}).value || '',
    port: _cloud.port || 10531,
    models: _cloudList
  };
  try {
    const r = await api('/api/cloud', { method: 'POST', body: JSON.stringify(payload) });
    if (res) res.textContent = r.ok ? ('Saved ' + r.saved + ' model(s) ✓') : ('Save failed: ' + (r.detail || '?'));
    if (r.ok) toast('Cloud mapping saved', 'ok'); else toast('Cloud save failed', 'err');
  } catch (e) { if (res) res.textContent = 'Error: ' + e.message; }
}
async function cloudAction(action) {
  const res = $('cloud-result'); if (res) res.textContent = action + '…';
  try {
    const r = await api('/api/cloud/action', { method: 'POST', body: JSON.stringify({ action }) });
    if (res) res.textContent = r.ok ? (r.state || action) : (r.output || 'failed');
    toast('Cloud provider: ' + (r.state || action), r.ok ? 'ok' : 'err');
  } catch (e) { if (res) res.textContent = 'Error: ' + e.message; }
}
function addBotURL() {
  const tok = ($('bot-token') || {}).value || '', chat = ($('bot-chat') || {}).value || '';
  if (!tok.trim() || !chat.trim()) { toast('Enter both token and chat ID', 'err'); return; }
  const url = 'https://api.telegram.org/bot' + encodeURIComponent(tok.trim()) + '/sendMessage?chat_id=' + encodeURIComponent(chat.trim());
  const ta = $('set-urls'); if (!ta) return;
  const lines = ta.value.split('\n').map(x => x.trim()).filter(Boolean);
  if (lines[0] === '(none configured)') lines.shift();
  if (!lines.includes(url)) lines.push(url);
  ta.value = lines.join('\n');
  toast('Bot URL added — press Save', 'ok');
}
async function saveSettings() {
  const ta = $('set-urls'); if (!ta) return;
  const urls = ta.value.split('\n').map(x => x.trim()).filter(x => x && x !== '(none configured)');
  const res = $('set-result'); if (res) res.textContent = '…';
  try { const r = await api('/api/settings', { method: 'POST', body: JSON.stringify({ webhooks: { urls } }) }); if (res) res.textContent = r.ok ? ('Saved ' + urls.length + ' URL(s) ✓') : 'Save failed: ' + (r.detail || '?'); if (r.ok) toast('Webhooks saved', 'ok'); else toast('Save failed', 'err'); } catch (e) { if (res) res.textContent = 'Error: ' + e.message; }
}
async function testSettings() {
  const res = $('set-result'), box = $('set-test-results');
  if (res) res.textContent = 'Sending…'; if (box) box.innerHTML = '';
  const ta = $('set-urls');
  const urls = ta ? ta.value.split('\n').map(x => x.trim()).filter(x => x && x !== '(none configured)') : [];
  try {
    const r = await api('/api/settings/test', { method: 'POST', body: JSON.stringify({ webhooks: { urls: urls.length ? urls : null } }) });
    const results = (r.results || []);
    if (!results.length) { if (res) res.textContent = r.detail || 'no URLs'; return; }
    let okc = 0;
    const rows = results.map(x => { const st = x.ok ? ('✓ HTTP ' + x.status) : ('✗ ' + (x.err || 'HTTP ' + (x.status || '?'))); if (x.ok) okc++; return `<div class="set-tr ${x.ok ? 'ok' : 'bad'}"><span class="set-tr-url">${String(x.url).slice(0, 60)}</span><span class="set-tr-st">${st}</span></div>`; });
    if (box) box.innerHTML = rows.join('');
    if (res) res.textContent = okc + ' / ' + results.length + ' delivered';
  } catch (e) { if (res) res.textContent = 'Error: ' + e.message; }
}

// ── Connection banner ────────────────────────────────────────────
let _connFails = 0;
function hideConnBanner() { const b = $('conn-banner'); if (b) b.remove(); }
function showConnBanner() {
  if (document.hidden || $('conn-banner')) return;
  const b = document.createElement('div');
  b.className = 'conn-banner'; b.id = 'conn-banner';
  b.textContent = '⚠ Lost connection to hub — retrying…';
  document.body.appendChild(b);
}

// ── Main refresh loop ────────────────────────────────────────────
async function refresh() {
  try {
    const d = await api('/api/status');
    _connFails = 0; hideConnBanner(); S.cache = d;
    render(d); pollGpuTemps(); pollModelStats(); pollSysHealth(); renderAlertBanner();
  } catch (e) {
    if (e.message !== 'auth' && S.cache) { _connFails++; if (_connFails >= 2) showConnBanner(); render(S.cache); }
  }
}

// ── Pause poller on hidden tab ───────────────────────────────────
document.addEventListener('visibilitychange', () => {
  if (document.hidden) { if (S.timer) { clearInterval(S.timer); S.timer = null; } }
  else if (S.timer === null && S.mounted && !_loggedOut) { refresh(); S.timer = setInterval(refresh, 5000); }
});

// ── Boot ─────────────────────────────────────────────────────────
$('app').innerHTML = '<div class="loading"><div class="spinner"></div><div>Connecting to ai-hub…</div></div>';
(async () => { try { await api('/api/status'); await refresh(); S.timer = setInterval(refresh, 5000); } catch (e) { loginView(); } })();
