/* ═══════════════════════════════════════════════════════════════════
   cards.js — model card rendering, in-place updates, live stats patch
   ═══════════════════════════════════════════════════════════════════ */

// Original family artwork is served locally; no third-party image requests.
function modelArtwork(m) {
  const name = ((m.id || '') + ' ' + (m.name || '')).toLowerCase();
  const family = name.includes('qwen') ? 'qwen' : name.includes('gemma') ? 'gemma'
    : name.includes('glm') ? 'glm' : name.includes('nemotron') ? 'nemotron'
    : name.includes('gpt') ? 'gpt' : m.category === 'Image' ? 'image'
    : m.category === 'Voice' ? 'voice' : m.category === 'Music' ? 'music'
    : m.category === '3D' ? '3d' : 'generic';
  return `<img class="model-artwork" src="/static/img/models/${family}.svg" width="40" height="40" alt="" loading="lazy" decoding="async">`;
}

// ── Model card HTML ──────────────────────────────────────────────
function modelCard(m, cat, enter, idx) {
  const running = m.running, healthy = m.healthy;
  const pnd = _pending[m.id] || '';
  const dis = pnd ? 'disabled' : '';
  const cls = running ? 'running' : m.port_conflict ? 'conflict' : '';
  const portBadge = `<span style="font-size:11px;font-weight:600;color:var(--fg3);background:var(--muted);border:1px solid var(--border);padding:3px 8px;border-radius:8px;font-family:JetBrains Mono,monospace">:${m.port}</span>`;
  const uncensoredBadge = m.uncensored ? `<span class="uncensored-mark tooltip-bubble" role="img" aria-label="Uncensored model" data-tooltip="Uncensored / abliterated build — verified model source"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 7.5-2"/><path d="M12 15v2"/></svg></span>` : '';

  let specsHtml = '';
  if (cat === "LLM") {
    const accelLabel = m.accel ? m.accel : "—";
    const accelColor = (accelLabel.includes("MTP") || accelLabel.includes("DFlash2")) && !accelLabel.includes("(off)") ? "var(--ok)" : "var(--fg3)";
    specsHtml = `<div class="specs" style="grid-template-columns:0.7fr 0.8fr 1fr 1fr 1.2fr 0.9fr">
      <div class="spec"><span class="spec-label">Quant</span><span class="spec-val">${m.quant || '—'}</span></div>
      <div class="spec"><span class="spec-label">Engine</span><span class="spec-val">${m.backend || ''}</span></div>
      <div class="spec"><span class="spec-label">Speed @20k</span><span class="spec-val"><button class="context-speed" onclick="showContextBenchmark('${m.id}')" title="Measured decode speed around 20k input tokens, 4 GPUs, 1 request; click for the context graph">${m.benchmark_tps_20k > 0 ? m.benchmark_tps_20k + ' tok/s' : '—'} <small>↗</small></button></span></div>
      <div class="spec"><span class="spec-label">Context</span><span class="spec-val">${running && m.chosen_ctx ? fmtK(m.chosen_ctx) : m.ctx || '—'}</span></div>
      <div class="spec"><span class="spec-label">VRAM</span><span class="spec-val">${m.vram || '—'}</span></div>
      <div class="spec"><span class="spec-label">Accel</span><span class="spec-val" style="color:${accelColor}">${accelLabel}</span></div>
    </div>`;
  } else if (cat === "Cloud") {
    specsHtml = `<div class="specs" style="grid-template-columns:0.9fr 1.2fr 0.8fr 0.9fr 0.9fr">
      <div class="spec"><span class="spec-label">Engine</span><span class="spec-val">OpenAI OAuth</span></div>
      <div class="spec"><span class="spec-label">Model</span><span class="spec-val" style="font-size:11.5px;overflow:hidden;text-overflow:ellipsis" title="${m.id}">${m.id}</span></div>
      <div class="spec"><span class="spec-label">Context</span><span class="spec-val">${m.ctx || '—'}</span></div>
      <div class="spec"><span class="spec-label">Speed</span><span class="spec-val">${m.tps || '—'}</span></div>
      <div class="spec"><span class="spec-label">Billing</span><span class="spec-val">ChatGPT</span></div>
    </div>`;
  } else if (cat === "Image") {
    const outStr = (m.out || []).join(' & ') || 'image';
    specsHtml = `<div class="specs" style="grid-template-columns:repeat(5,1fr)">
      <div class="spec"><span class="spec-label">VRAM</span><span class="spec-val">${m.vram || '—'}</span></div>
      <div class="spec"><span class="spec-label">Output</span><span class="spec-val">${outStr}</span></div>
      <div class="spec"><span class="spec-label">Max Res</span><span class="spec-val">1024²</span></div>
      <div class="spec"><span class="spec-label">Video</span><span class="spec-val">5 sec</span><span class="spec-sub">720p</span></div>
      <div class="spec"><span class="spec-label">Models</span><span class="spec-val">${(m.sub || []).length} models</span></div>
    </div>`;
  } else if (cat === "Voice") {
    specsHtml = `<div class="specs" style="grid-template-columns:repeat(5,1fr)">
      <div class="spec"><span class="spec-label">VRAM</span><span class="spec-val">${m.vram || '—'}</span></div>
      <div class="spec"><span class="spec-label">Engines</span><span class="spec-val">${(m.sub || []).length || 2} models</span></div>
      <div class="spec"><span class="spec-label">Type</span><span class="spec-val">TTS & STT</span></div>
      <div class="spec"><span class="spec-label">Latency</span><span class="spec-val">realtime</span></div>
      <div class="spec"><span class="spec-label">Output</span><span class="spec-val">speech</span></div>
    </div>`;
  } else if (cat === "Music") {
    specsHtml = `<div class="specs" style="grid-template-columns:repeat(5,1fr)">
      <div class="spec"><span class="spec-label">VRAM</span><span class="spec-val">${m.vram || '—'}</span></div>
      <div class="spec"><span class="spec-label">Output</span><span class="spec-val">music</span></div>
      <div class="spec"><span class="spec-label">Length</span><span class="spec-val">up to 3 min</span></div>
      <div class="spec"><span class="spec-label">Quality</span><span class="spec-val">44.1 kHz</span></div>
      <div class="spec"><span class="spec-label">Model</span><span class="spec-val">ACE-Step</span></div>
    </div>`;
  } else if (cat === "3D") {
    specsHtml = `<div class="specs" style="grid-template-columns:repeat(5,1fr)">
      <div class="spec"><span class="spec-label">VRAM</span><span class="spec-val">${m.vram || '—'}</span></div>
      <div class="spec"><span class="spec-label">Input</span><span class="spec-val">image</span></div>
      <div class="spec"><span class="spec-label">Output</span><span class="spec-val">GLB / OBJ</span></div>
      <div class="spec"><span class="spec-label">Textured</span><span class="spec-val">yes</span></div>
      <div class="spec"><span class="spec-label">Model</span><span class="spec-val">Hunyuan3D</span></div>
    </div>`;
  } else {
    specsHtml = `<div class="specs"><div class="spec"><span class="spec-label">VRAM</span><span class="spec-val">${m.vram || '—'}</span></div><div class="spec"><span class="spec-label">Output</span><span class="spec-val">${(m.out || []).join(' & ')}</span></div></div>`;
  }

  const ioIn = (m.in && m.in.length) ? m.in : ['text'];
  const ioOut = (m.out && m.out.length) ? m.out : ['text'];
  const ioCompact = `<div class="io-compact" title="in: ${ioIn.join(', ')}  →  out: ${ioOut.join(', ')}">${ioIn.map(x => ioIcon(x)).join('')}<span class="sep">→</span>${ioOut.map(x => ioIcon(x)).join('')}</div>`;

  return `<div class="model${cls ? ' ' + cls : ''}${enter ? ' enter' : ''}" id="card-${m.id}"${enter ? ` style="animation-delay:${idx * 45}ms"` : ''}>
    <div class="model-head">
      ${modelArtwork(m)}
      <div class="model-main">
        <div class="title-row"><span class="model-name${running && cat === "LLM" ? ' name-click' : ''}"${running && cat === "LLM" ? ` onclick="showStats('${m.id}')" title="📈 Live stats — TPS · tokens · context"` : ''}>${m.name || m.short}</span>${uncensoredBadge} ${m.hf ? `<a href="${m.hf}" target="_blank" data-tooltip="Source — Hugging Face / GitHub" class="tooltip-bubble" style="display:inline-flex;align-items:center;justify-content:center;width:22px;height:22px;border-radius:6px;background:var(--muted);border:1px solid var(--border);font-size:12px;line-height:1;text-decoration:none">🔗</a>` : ''} ${pill(running, healthy, m.id)} ${portBadge}</div>
      </div>
      ${ioCompact}
    </div>
    ${specsHtml}
    ${m.sub && m.sub.length ? `<div class="subs">${m.sub.map(s => s.hf ? `<a href="${s.hf}" target="_blank" data-tooltip="Source — ${s.name}" class="tooltip-bubble sub-tag" style="text-decoration:none;color:var(--fg3)">${s.name} <span style="opacity:.6">· ${s.vram || ''}</span> 🔗</a>` : `<span class="sub-tag">${s.name} <span style="opacity:.6">· ${s.vram || ''}</span></span>`).join('')}</div>` : ''}
    ${m.port_conflict ? `<div class="hint">⏸ Shares port :${m.port} with ${(m.group_siblings || []).join(', ')} — starting will auto-stop the other</div>` : ''}
    ${running && m.parallel && m.parallel.enabled ? runningLine(m) : ''}
    ${!running && m.parallel && m.parallel.enabled ? launchControls(m) : ''}
    <div class="foot">
      <div class="lifecycle">
        ${running
      ? `<button class="btn btn-danger" onclick="act('${m.id}','stop')" ${dis}>${pnd === 'stop' ? '◌ Stopping…' : '■ Stop'}</button><button class="btn btn-ghost" onclick="act('${m.id}','restart')" ${dis}>${pnd === 'restart' ? '◌ Restarting…' : '↻ Restart'}</button>${cat === "LLM" ? `<button class="btn btn-ghost" onclick="runBenchmark('${m.id}')" title="Run a quick benchmark (TTFT + decode TPS) against this running model">📊 Bench</button>` : ''}`
      : `<button class="btn btn-primary" onclick="act('${m.id}','start')" ${dis}>${pnd === 'start' ? '◌ Starting…' : '▶ Start'}</button>${cat === "LLM" ? `<button class="btn btn-ghost" onclick="switchTo('${m.id}')" title="Stop all other running LLMs, then start this one (frees their VRAM)">⤢ Switch</button>` : ''}`}
        <button class="btn btn-ghost" onclick="showLogs('${m.id}')">Logs</button>
      </div>
      <div class="access">
        ${running && cat === "LLM" ? `<a class="btn btn-access" href="/chat?model=${encodeURIComponent(m.id)}&v=1" target="_blank">💬 Chat</a>` : ''}
        ${["Image", "Voice", "Music", "3D"].includes(cat) ? `<button class="btn btn-access tooltip-bubble" onclick="openServiceUI('${m.id}', '${serviceURL(m.id === "music" ? 8016 : m.port)}')" data-tooltip="Open the web UI of this service (starts it first if it is not running)">🌐 Open</button>` : ''}
        ${cat === "Image" ? `<button class="btn btn-access tooltip-bubble" onclick="openVideoGen()" data-tooltip="Open template-based Comfy generation (Wan 2.1 / FLUX.2 / SDXL / LTX2.5 / MiniMax-H3)">⚡ Quick Generate</button>` : ''}
        ${running && cat === "LLM" ? `<a class="btn btn-access tooltip-bubble" href="${webuiURL()}" target="_blank" data-tooltip="Open WebUI — full ChatGPT clone with history, branching, files, RAG">🌐 WebUI</a>` : ''}
        ${running ? `<button class="btn btn-access" onclick="showApi('${m.id}',${m.port},'${m.backend}')">API</button>` : ''}
        ${running && m.backend === 'vLLM' ? `<a class="btn btn-access" href="${serviceURL(m.port, '/docs')}" target="_blank">Docs</a>` : ''}
      </div>
    </div>
  </div>`;
}

// ── Running line (chosen params + charts) ────────────────────────
function runningLine(m) {
  const used = (m.gpus_used && m.gpus_used.length) ? m.gpus_used : (m.chosen_gpus ? [...Array(m.chosen_gpus).keys()] : []);
  const g = m.chosen_gpus || used.length || 1;
  const c = m.chosen_ctx ? fmtK(m.chosen_ctx) : '?';
  const p = m.chosen_parallel || 1;
  const dec = (typeof m.tps_live === 'number' && m.tps_live > 0) ? Math.round(m.tps_live) : null;
  const pr = (typeof m.prompt_tps_live === 'number' && m.prompt_tps_live > 0) ? Math.round(m.prompt_tps_live) : null;
  const ctxv = (typeof m.ctx_used_live === 'number' && m.ctx_used_live > 0) ? m.ctx_used_live : (m.chosen_ctx || null);
  const headVal = (id, val, unit, color) => ` <b id="${id}" class="mc-val" style="color:${color}">${val == null ? '—' : val + ' ' + unit}</b>`;
  const charts = `<div class="mcharts">
    <div class="mc-col">
      <div class="mc-head"><span class="lt"><i style="background:#f59e0b"></i>Decode</span>${headVal('mdecv-' + m.id, dec, 'tok/s', '#f59e0b')}</div>
      <canvas class="mc-canvas" id="mdec-${m.id}" height="64"></canvas>
    </div>
    <div class="mc-col">
      <div class="mc-head"><span class="lt"><i style="background:#22d3ee"></i>Prompt</span>${headVal('mprmv-' + m.id, pr, 'tok/s', '#22d3ee')}</div>
      <canvas class="mc-canvas" id="mprm-${m.id}" height="64"></canvas>
    </div>
    <div class="mc-col">
      <div class="mc-head"><span class="lt"><i style="background:#22c55e"></i>Context</span>${headVal('mctxv-' + m.id, ctxv != null ? fmtK(ctxv) : null, 'tok', '#22c55e')}</div>
      <canvas class="mc-canvas" id="mctx-${m.id}" height="64"></canvas>
    </div>
  </div>`;
  const eff = modelEfficiency(m);
  return `<div class="running-line"><span class="rl-dot"></span><span class="rl-text">Running on <b>${g} GPU</b>${used.length ? ` (${used.join(',')})` : ''} · <b>${c} ctx</b> · <b>${p} agent${p > 1 ? 's' : ''}</b></span>${eff ? `<span class="rl-eff" id="meff-${m.id}" title="decode ÷ GPU count, and decode ÷ power draw of this model's GPUs">⟐ ${eff}</span>` : ''}</div>${charts}`;
}

// ── In-place card operations ─────────────────────────────────────
function modelKey(m) {
  let p = _pending[m.id] || '';
  if (p && m.running && m.healthy) p = '';
  return [!!m.running, !!m.healthy, !!m.port_conflict, p, m.name, m.short, m.hf, !!m.uncensored, m.tps, m.ctx, m.vram, m.quant, m.accel, m.benchmark_tps_20k, m.benchmark_points, m.benchmark_status].join('|');
}

function syncOpenCardElevate() {
  document.querySelectorAll('.model.dd-card-elev').forEach(el => el.classList.remove('dd-card-elev'));
  if (_ddOpen) {
    const mid = String(_ddOpen).split(':')[0];
    const card = document.getElementById('card-' + mid);
    if (card) card.classList.add('dd-card-elev');
  }
}

function replaceCard(m) {
  const el = $('card-' + m.id);
  if (!el) return;
  const tmp = document.createElement('div');
  tmp.innerHTML = modelCard(m, m.category);
  el.replaceWith(tmp.firstElementChild);
  if (S.filter) applyFilter(S.filter);
  syncOpenCardElevate();
}

function syncCard(mid) {
  if (!S.mounted || !S.cache) return;
  const m = S.cache.models.find(x => x.id === mid);
  if (!m) return;
  if (_pending[mid] && m.running && m.healthy) _pending[mid] = null;
  S.cardKey[mid] = modelKey(m);
  replaceCard(m);
}

// ── Patch live stats in chart headers (no full card re-render) ───
function patchLiveStats(m) {
  const card = $('card-' + m.id);
  if (!card) return;
  const dec = (typeof m.tps_live === 'number' && m.tps_live > 0) ? Math.round(m.tps_live) : null;
  const pr = (typeof m.prompt_tps_live === 'number' && m.prompt_tps_live > 0) ? Math.round(m.prompt_tps_live) : null;
  const ctxv = (typeof m.ctx_used_live === 'number' && m.ctx_used_live > 0) ? m.ctx_used_live : (m.chosen_ctx || null);
  const set = (id, val, unit) => {
    const el = document.getElementById(id);
    if (!el) return;
    el.textContent = (val == null) ? '—' : `${val} ${unit}`;
  };
  set('mdecv-' + m.id, dec, 'tok/s');
  set('mprmv-' + m.id, pr, 'tok/s');
  set('mctxv-' + m.id, ctxv != null ? fmtK(ctxv) : null, 'tok');
}
