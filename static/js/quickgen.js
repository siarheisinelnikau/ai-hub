// ── Comfy quick media generation (via hub proxy) ──
// ComfyUI (aiohttp) rejects foreign Origin headers, so calls are proxied through
// /api/comfy/* on ai-hub.

const QGEN = { pollMs: 3000, timeoutMs: 15 * 60 * 1000 };

const COMFY_TEMPLATES = [
  {
    id: 'wan-t2v',
    family: 'Wan 2.1',
    mode: 'video',
    label: 'Wan 2.1 T2V (1.3B)',
    ready: true,
    note: 'Ready in hub now',
    defaults: { width: 832, height: 480, frames: 49, steps: 20, negative: '' },
    build: ({ prompt, negative, width, height, frames, steps, seed }) =>
      wanWorkflow(prompt, negative, width, height, frames, 16, steps, seed),
  },
  {
    id: 'flux2-dev',
    family: 'FLUX.2',
    mode: 'image',
    label: 'FLUX.2-dev (FluxGuidance + Turbo LoRA)',
    ready: true,
    note: 'Ready in MCP pipeline, now exposed in hub UI',
    defaults: { width: 768, height: 768, steps: 8, cfg: 3.5, negative: '' },
    build: ({ prompt, width, height, steps, cfg, seed }) =>
      flux2Workflow(prompt, width, height, steps, seed, cfg),
  },
  {
    id: 'sdxl-t2i',
    family: 'SDXL',
    mode: 'image',
    label: 'SDXL T2I + inpaint/ outpaint variants',
    ready: false,
    reason: 'Templates need repair/rebind to current checked-weighted workflow set',
  },
  {
    id: 'ltx25-t2v',
    family: 'LTX-2.5',
    mode: 'video',
    label: 'LTX-2.5 T2V / I2V',
    ready: false,
    reason: 'Workflow variants exist but require post-repair mapping + fixture tests',
  },
  {
    id: 'minimax-t2v',
    family: 'MiniMax-H3',
    mode: 'video',
    label: 'MiniMax-H3 T2V / I2V / R2V',
    ready: false,
    reason: 'Needs explicit inpaint/R2V/family variant wiring before UI-safe launch',
  },
];

function getTemplateById(id) {
  return COMFY_TEMPLATES.find(t => t.id === id);
}

function wanWorkflow(prompt, negative, width, height, frames, fps, steps, seed) {
  return {
    "1": { class_type: "CLIPLoader", inputs: { clip_name: "umt5_xxl_fp8.safetensors", type: "wan" } },
    "2": { class_type: "UNETLoader", inputs: { unet_name: "wan2.1_t2v_1.3B_bf16.safetensors", weight_dtype: "default" } },
    "3": { class_type: "VAELoader", inputs: { vae_name: "wan_2.1_vae.safetensors" } },
    "4": { class_type: "CLIPTextEncode", inputs: { text: prompt, clip: ["1", 0] } },
    "5": { class_type: "CLIPTextEncode", inputs: { text: negative, clip: ["1", 0] } },
    "6": { class_type: "WanFirstLastFrameToVideo", inputs: {
      positive: ["4", 0], negative: ["5", 0], vae: ["3", 0],
      width, height, length: frames, batch_size: 1,
    } },
    "7": { class_type: "KSampler", inputs: {
      model: ["2", 0], seed: seed, steps: steps, cfg: 5.0,
      sampler_name: "euler", scheduler: "simple",
      positive: ["6", 0], negative: ["6", 1], latent_image: ["6", 2], denoise: 1,
    } },
    "8": { class_type: "VAEDecode", inputs: { samples: ["7", 0], vae: ["3", 0] } },
    "9": { class_type: "SaveWEBM", inputs: { images: ["8", 0], filename_prefix: "hub_vid", codec: "vp9", fps: 16, crf: 30 } },
  };
}

function flux2Workflow(prompt, width, height, steps, seed, cfg) {
  return {
    "1": { class_type: "CLIPLoader", inputs: { clip_name: "mistral_3_small_flux2_fp8.safetensors", type: "flux2" } },
    "2": { class_type: "UNETLoader", inputs: { unet_name: "flux2_dev_fp8mixed.safetensors", weight_dtype: "default" } },
    "3": { class_type: "LoraLoaderModelOnly", inputs: { model: ["2", 0], lora_name: "Flux2TurboComfyv2.safetensors", strength_model: 1.0 } },
    "4": { class_type: "VAELoader", inputs: { vae_name: "flux2-vae.safetensors" } },
    "5": { class_type: "CLIPTextEncode", inputs: { text: prompt, clip: ["1", 0] } },
    "6": { class_type: "FluxGuidance", inputs: { conditioning: ["5", 0], guidance: cfg } },
    "7": { class_type: "EmptyFlux2LatentImage", inputs: { width: width, height: height, batch_size: 1 } },
    "8": { class_type: "BasicGuider", inputs: { model: ["3", 0], conditioning: ["6", 0] } },
    "9": { class_type: "KSamplerSelect", inputs: { sampler_name: "euler" } },
    "10": { class_type: "Flux2Scheduler", inputs: { steps: steps, width: width, height: height } },
    "11": { class_type: "RandomNoise", inputs: { noise_seed: seed } },
    "12": { class_type: "SamplerCustomAdvanced", inputs: {
      noise: ["11", 0], guider: ["8", 0], sampler: ["9", 0], sigmas: ["10", 0], latent_image: ["7", 0],
    } },
    "13": { class_type: "VAEDecode", inputs: { samples: ["12", 0], vae: ["4", 0] } },
    "14": { class_type: "SaveImage", inputs: { images: ["13", 0], filename_prefix: "hub_flux2" } },
  };
}

async function comfyCall(path, opts) {
  const r = await fetch('/api/comfy/' + path, opts);
  if (r.status === 401) { loginView(); throw new Error('auth'); }
  const text = await r.text();
  let json = null; try { json = JSON.parse(text); } catch (e) { /* not JSON */ }
  return { ok: r.ok, status: r.status, json, text };
}

function comfyFindFile(outputs) {
  for (const id of Object.keys(outputs).sort()) {
    const node = outputs[id];
    if (!node) continue;
    for (const key of ['videos', 'gifs', 'images']) {
      const items = node[key];
      if (Array.isArray(items) && items.length && items[0] && items[0].filename) {
        return { filename: items[0].filename, subfolder: items[0].subfolder || '' };
      }
    }
  }
  return null;
}

function fmtComfyError(messages) {
  if (!Array.isArray(messages)) return 'generation failed';
  const parts = [];
  for (const m of messages) {
    if (!Array.isArray(m) || m.length < 2) continue;
    for (const e of m[1]) {
      if (typeof e === 'string') parts.push(e);
      else if (e && (e.message || e.error)) parts.push(e.message || e.error);
    }
  }
  return parts.join(' · ') || 'generation failed';
}

const sleep = ms => new Promise(r => setTimeout(r, ms));

async function comfyWait(id, onTick) {
  const t0 = Date.now();
  for (;;) {
    if (Date.now() - t0 > QGEN.timeoutMs) throw new Error('timed out after ' + Math.round(QGEN.timeoutMs / 60000) + ' min');
    await sleep(QGEN.pollMs);
    onTick && onTick(Math.round((Date.now() - t0) / 1000));
    let r;
    try { r = await comfyCall('history/' + encodeURIComponent(id)); } catch (e) { if (e.message === 'auth') throw e; continue; }
    if (!r.ok) continue;
    const entry = r.json && r.json[id];
    if (!entry) continue;
    if (entry.status && entry.status.status_str === 'error') throw new Error(fmtComfyError(entry.status.messages));
    if (entry.status && entry.status.completed && entry.outputs) return entry.outputs;
  }
}

// Start media service first if needed.
async function ensureService(mid) {
  const find = () => (((S.cache && S.cache.models) || []).find(x => x.id === mid));
  const m0 = find();
  if (m0 && m0.running && m0.healthy) return true;
  const verb = (m0 && m0.running) ? 'restart' : 'start';
  const ok = await showModal({
    title: `${mid} is not ready`,
    body: `${m0 && m0.running ? 'Restart' : 'Start'} <b>${mid}</b> and wait until it is healthy?`,
    buttons: [{ label: 'Cancel', type: 'ghost', value: false }, { label: `${m0 && m0.running ? 'Restart' : 'Start'} & open`, type: 'primary', value: true }],
  });
  if (!ok) return false;
  toast(`${verb === 'restart' ? 'Restarting' : 'Starting'} ${mid}…`);
  try { await act(mid, verb); } catch (e) { if (e.message === 'auth') return false; }
  const t0 = Date.now();
  while (Date.now() - t0 < 180000) {
    await new Promise(r => setTimeout(r, 3000));
    try { await refresh(); } catch (e) {}
    const m = find();
    if (m && m.running && m.healthy) { toast(`${mid} is up`); return true; }
  }
  toast(`${mid}: timed out waiting — it may still be loading`, 'warn');
  return false;
}

function ensureReadyTemplate(selId) {
  const t = getTemplateById(selId);
  if (!t || !t.ready) {
    return null;
  }
  return t;
}

async function openServiceUI(mid, url) {
  if (await ensureService(mid)) window.open(url, '_blank');
}

async function openVideoGen() {
  if (await ensureService('comfyui')) showMediaGenModal();
}

function templateOptionsHtml() {
  return COMFY_TEMPLATES.map(t => {
    const disabled = t.ready ? '' : 'disabled';
    const badge = t.ready ? ' (ready)' : ` (${t.reason || 'not ready'})`;
    const cls = t.ready ? '': ' style="color:var(--fg3)"';
    return `<option value="${t.id}" ${disabled}${cls}>${t.label} · ${t.family}${badge}</option>`;
  }).join('');
}

function modeInputsHtml(template) {
  if (template.mode === 'image') {
    return `
      <div style="display:flex;gap:14px;margin-top:10px;flex-wrap:wrap;align-items:end">
        <label style="font-size:12px;color:var(--fg3)">Resolution
          <select id="qg-res" style="display:block;margin-top:4px;background:var(--muted);color:var(--fg);border:1px solid var(--border);border-radius:8px;padding:6px 8px">
            <option value="512x512">512×512</option>
            <option value="768x768" selected>768×768</option>
            <option value="1024x1024">1024×1024</option>
            <option value="1024x1536">1024×1536</option>
            <option value="1536x1024">1536×1024</option>
          </select>
        </label>
        <label style="font-size:12px;color:var(--fg3)">Steps
          <input id="qg-steps" type="number" value="${template.defaults.steps || 8}" min="2" max="60" step="1" style="width:120px;display:block;margin-top:4px;background:var(--muted);color:var(--fg);border:1px solid var(--border);border-radius:8px;padding:6px 8px" />
        </label>
        <label style="font-size:12px;color:var(--fg3)">Guidance
          <input id="qg-cfg" type="number" value="${template.defaults.cfg || 3.5}" min="1" max="10" step="0.1" style="width:100px;display:block;margin-top:4px;background:var(--muted);color:var(--fg);border:1px solid var(--border);border-radius:8px;padding:6px 8px" />
        </label>
      </div>
      <div style="display:flex;gap:14px;margin-top:10px;flex-wrap:wrap;align-items:end">
        <label style="flex:1;min-width:200px;font-size:12px;color:var(--fg3)">Negative (optional)
          <input id="qg-neg" placeholder="low quality, blurry, watermark" style="display:block;margin-top:4px;width:100%;box-sizing:border-box;background:var(--muted);color:var(--fg);border:1px solid var(--border);border-radius:8px;padding:6px 8px">
        </label>
      </div>`;
  }

  return `
      <div style="display:flex;gap:14px;margin-top:10px;flex-wrap:wrap;align-items:end">
        <label style="font-size:12px;color:var(--fg3)">Resolution
          <select id="qg-res" style="display:block;margin-top:4px;background:var(--muted);color:var(--fg);border:1px solid var(--border);border-radius:8px;padding:6px 8px">
            <option value="832x480" selected>832×480 (16:9)</option>
            <option value="480x832">480×832 (9:16)</option>
            <option value="1280x720">1280×720 (720p)</option>
          </select>
        </label>
        <label style="font-size:12;color:var(--fg3)">Length
          <select id="qg-len" style="display:block;margin-top:4px;background:var(--muted);color:var(--fg);border:1px solid var(--border);border-radius:8px;padding:6px 8px">
            <option value="49" selected>49 frames ≈ 3 s</option>
            <option value="81">81 frames ≈ 5 s</option>
            <option value="121">121 frames ≈ 8 s</option>
          </select>
        </label>
        <label style="font-size:12px;color:var(--fg3)">Steps
          <input id="qg-steps" type="number" value="${template.defaults.steps || 20}" min="4" max="80" step="1" style="width:90px;display:block;margin-top:4px;background:var(--muted);color:var(--fg);border:1px solid var(--border);border-radius:8px;padding:6px 8px" />
        </label>
      </div>
      <div style="display:flex;gap:14px;margin-top:10px;align-items:end;flex-wrap:wrap">
        <label style="flex:1;min-width:200px;font-size:12px;color:var(--fg3)">Negative (optional)
          <input id="qg-neg" placeholder="low quality, blurry, watermark" style="display:block;margin-top:4px;width:100%;box-sizing:border-box;background:var(--muted);color:var(--fg);border:1px solid var(--border);border-radius:8px;padding:6px 8px">
        </label>
      </div>`;
}

function resolvePromptValues(template) {
  const prompt = document.getElementById('qg-prompt').value.trim();
  const res = document.getElementById('qg-res').value || template.defaults.width + 'x' + template.defaults.height;
  const [w, h] = res.split('x').map(Number);
  const steps = parseInt(document.getElementById('qg-steps').value, 10) || template.defaults.steps;
  const negative = (document.getElementById('qg-neg') && document.getElementById('qg-neg').value || '').trim() || template.defaults.negative || '';
  const seed = Math.floor(Math.random() * 1e9);

  if (template.mode === 'image') {
    return {
      prompt,
      width: w,
      height: h,
      steps,
      cfg: parseFloat(document.getElementById('qg-cfg')?.value || template.defaults.cfg || 3.5),
      negative,
      seed,
    };
  }

  return {
    prompt,
    width: w,
    height: h,
    frames: parseInt(document.getElementById('qg-len').value, 10) || template.defaults.frames,
    steps,
    negative,
    seed,
  };
}

function showMediaGenModal() {
  const defaultTemplate = COMFY_TEMPLATES.find(t => t.ready) || COMFY_TEMPLATES[0];
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.innerHTML = `
    <div class="modal modal-wide" role="dialog" aria-modal="true" aria-label="Comfy quick generation">
      <button class="btn btn-ghost modal-close" style="position:absolute;top:8px;right:8px;font-size:14px;">✖</button>
      <h3>⚡ Quick Comfy templates <span style="color:var(--fg3);font-size:12px;font-weight:normal">Wan2.1 / FLUX.2 / SDXL / LTX2.5 / MiniMax-H3</span></h3>
      <div class="modal-body">
        <div id="qg-form">
          <label style="display:block;font-size:12px;color:var(--fg3)">Template</label>
          <select id="qg-template" style="margin-top:4px;display:block;background:var(--muted);color:var(--fg);border:1px solid var(--border);border-radius:8px;padding:6px 8px; width:100%;box-sizing:border-box;max-width:100%;">
            ${templateOptionsHtml()}
          </select>
          <p id="qg-template-note" style="margin:8px 0 0 0;font-size:12px;color:var(--fg3)"></p>
          <textarea id="qg-prompt" rows="3" placeholder="A cat in a spacesuit walking on the moon, cinematic lighting, slow motion" style="margin-top:10px;width:100%;box-sizing:border-box;background:var(--muted);color:var(--fg);border:1px solid var(--border);border-radius:10px;padding:10px;font-size:13px;resize:vertical"></textarea>
          <div id="qg-mode-fields"></div>
          <div style="margin-top:14px;display:flex;gap:10px">
            <button class="btn btn-primary" id="qg-go">▶ Generate</button>
            <button class="btn btn-ghost" id="qg-close">Close</button>
          </div>
        </div>
        <div id="qg-status" style="display:none">
          <div style="display:flex;align-items:center;gap:12px;padding:18px 4px">
            <div class="bench-spinner"></div>
            <div><div id="qg-status-text" style="font-size:14px">Queuing…</div>
            <div id="qg-elapsed" style="font-size:12px;color:var(--fg3)">0 s elapsed — generation continues on the server even if you close this dialog</div></div>
          </div>
        </div>
        <div id="qg-result" style="display:none"></div>
      </div>
    </div>`;

  $('modal-root').appendChild(overlay);
  document.body.style.overflow = 'hidden';

  const $id = sel => overlay.querySelector(sel);
  const show = (sel, on) => { $id(sel).style.display = on ? '' : 'none'; };
  const close = () => {
    overlay.remove();
    document.body.style.overflow = '';
    document.removeEventListener('keydown', escHandler);
  };

  const drawFields = () => {
    const t = getTemplateById($id('#qg-template').value);
    const note = $id('#qg-template-note');
    if (t.ready) {
      note.textContent = t.note;
      note.style.color = 'var(--fg3)';
      $id('#qg-go').disabled = false;
    } else {
      note.textContent = `Template blocked: ${t.reason || 'not ready yet'}`;
      note.style.color = '#fca5a5';
      $id('#qg-go').disabled = true;
    }
    $id('#qg-mode-fields').innerHTML = modeInputsHtml(t);
  };

  $id('.modal-close').onclick = close;
  $id('#qg-close').onclick = close;
  overlay.addEventListener('click', e => { if (e.target === overlay) close(); });
  const escHandler = e => { if (e.key === 'Escape' && overlay.isConnected) close(); };
  document.addEventListener('keydown', escHandler);
  $id('#qg-template').addEventListener('change', drawFields);
  $id('#qg-prompt').focus();
  $id('#qg-template').value = defaultTemplate.id;
  drawFields();

  const go = async () => {
    const template = getTemplateById($id('#qg-template').value);
    if (!template || !template.ready) {
      toast(template ? `${template.label}: not ready yet` : 'Choose a template', 'warn');
      return;
    }

    const cfg = resolvePromptValues(template);
    if (!cfg.prompt) { toast('Write a prompt first', 'warn'); return; }

    $id('#qg-go').disabled = true;
    try {
      show('#qg-form', false); show('#qg-status', true);
    const t0 = Date.now();
      const payload = template.build(cfg);
      if (!payload) throw new Error('template builder returned empty payload');

      const body = template.id === 'flux2-dev'
        ? { prompt: payload }
        : { prompt: payload };
      const res = await comfyCall('prompt', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error((res.json && (res.json.error || res.json.detail)) || res.text || ('HTTP ' + res.status));
      const id = res.json.prompt_id;
      if (!id) throw new Error('ComfyUI accepted the job but returned no prompt_id');

      $id('#qg-status-text').textContent = 'Rendering…';
      const outputs = await comfyWait(id, s => { $id('#qg-elapsed').textContent = s + ' s elapsed — generation continues on the server even if you close this dialog'; });
      const f = comfyFindFile(outputs);
      if (!f) throw new Error('finished, but no file in the outputs');
      const isVideo = template.mode === 'video';
      const url = '/api/comfy/view?filename=' + encodeURIComponent(f.filename) + '&subfolder=' + encodeURIComponent(f.subfolder) + '&type=output';
      const secs = Math.round((Date.now() - t0) / 1000);
      const resBox = $id('#qg-result');

      if (isVideo) {
        resBox.innerHTML = `
          <video src="${url}" controls autoplay loop style="width:100%;max-height:60vh;border-radius:12px;background:#000"></video>
          <div style="display:flex;gap:10px;margin-top:12px;align-items:center;flex-wrap:wrap">
            <button class="btn btn-primary" id="qg-again">↻ Generate another</button>
            <a class="btn btn-ghost" href="${url}" target="_blank" style="text-decoration:none">Open in new tab</a>
            <span style="font-size:12px;color:var(--fg3)">${cfg.width}×${cfg.height} · ${cfg.frames || '—'} frames · ${secs} s</span>
          </div>`;
      } else {
        resBox.innerHTML = `
          <img src="${url}" style="width:100%;max-height:60vh;border-radius:12px;background:#111" />
          <div style="display:flex;gap:10px;margin-top:12px;align-items:center;flex-wrap:wrap">
            <button class="btn btn-primary" id="qg-again">↻ Generate another</button>
            <a class="btn btn-ghost" href="${url}" target="_blank" style="text-decoration:none">Open in new tab</a>
            <span style="font-size:12px;color:var(--fg3)">${cfg.width}×${cfg.height} · ${cfg.steps} steps · ${secs} s</span>
          </div>`;
      }
      show('#qg-status', false); show('#qg-result', true);
      toast(template.mode === 'video' ? ('🎬 ' + template.label + ' ready (' + secs + ' s)') : ('🖼️ ' + template.label + ' ready (' + secs + ' s)'), 'ok');
      $id('#qg-again').onclick = () => { show('#qg-result', false); show('#qg-form', true); $id('#qg-go').disabled = false; $id('#qg-prompt').focus(); };
    } catch (e) {
      if (e.message === 'auth') return;
      show('#qg-status', false);
      const resBox = $id('#qg-result');
      resBox.innerHTML = `
        <div style="background:rgba(220,80,80,.12);border:1px solid rgba(220,80,80,.4);border-radius:10px;padding:12px;font-size:13px">⚠ ${String(e.message).replace(/</g, '&lt;')}</div>
        <div style="display:flex;gap:10px;margin-top:12px">
          <button class="btn btn-primary" id="qg-retry">↻ Try again</button>
          <button class="btn btn-ghost" id="qg-close2">Close</button>
        </div>`;
      show('#qg-result', true);
      $id('#qg-retry').onclick = () => { show('#qg-result', false); show('#qg-form', true); $id('#qg-go').disabled = false; };
      $id('#qg-close2').onclick = close;
      toast('Generation failed: ' + e.message, 'err');
    } finally {
      if (getTemplateById($id('#qg-template').value)?.ready) {
        $id('#qg-go').disabled = false;
      }
    }
  };

  $id('#qg-go').onclick = go;
  $id('#qg-prompt').addEventListener('keydown', e => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) go(); });
}
