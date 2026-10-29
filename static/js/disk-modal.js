/* ═══════════════════════════════════════════════════════════════════
   disk-modal.js — Disk Usage modal (full, live disk breakdown)
   Extracted from page.js so rollback only touches this file + CSS.
   ═══════════════════════════════════════════════════════════════════ */

// "240G" / "2.1M" / "48K" → GB number (du -sh / docker size strings).
function diskGB(s) {
  if (typeof s === 'number') return s;
  const m = String(s == null ? '' : s).trim().match(/^([\d.]+)\s*([KMGTP]?)B?$/i);
  if (!m) return 0;
  const mult = { K: 1 / 1048576, M: 1 / 1024, G: 1, T: 1024, P: 1048576 }[(m[2] || 'G').toUpperCase()] || 1;
  return (parseFloat(m[1]) || 0) * mult;
}
function diskFmt(gb) {
  if (gb >= 1024) return (gb / 1024).toFixed(2) + ' TB';
  if (gb >= 10) return gb.toFixed(gb >= 100 ? 0 : 1) + ' GB';
  if (gb >= 1) return gb.toFixed(1) + ' GB';
  if (gb >= 1 / 1024) return (gb * 1024).toFixed(0) + ' MB';
  if (gb > 0) return (gb * 1048576).toFixed(0) + ' KB';
  return '0';
}
function diskBar(pct, cls) {
  const w = Math.max(0, Math.min(100, pct || 0));
  return `<span class="db-bar"><span class="db-bar-fill ${cls || ''}" style="width:${w.toFixed(1)}%"></span></span>`;
}
function diskBadge(text, cls) { return `<span class="disk-badge ${cls || ''}">${text}</span>`; }

const DISK_SWATCH = { llm: 'cat-llm', media: 'cat-media', containerd: 'cat-conta', dockerroot: 'cat-docker', usr: 'cat-usr', snapd: 'cat-snap', log: 'cat-log' };

function diskDirRow(x, pctOf) {
  const cls = DISK_SWATCH[x.key] || 'cat-sys';
  const pct = pctOf(x.gb);
  return `<div class="db-row">
    <span class="db-label"><span class="db-swatch ${cls}"></span>${x.label}<span class="db-sub">${x.path}</span></span>
    <span class="db-val">${diskFmt(x.gb)}</span>
    <span class="db-pct">${x.gb > 0 && pct < 1 ? '<1' : Math.round(pct)}%</span>
  </div>`;
}

// One-level breakdown of a big directory (`du -sh <dir>/*` from /api/disk).
function renderSubGroups(rep) {
  return (rep.subdirs || []).map(g => {
    const items = (g.items || []).map(x => Object.assign({}, x, { gb: diskGB(x.size) })).sort((a, b) => b.gb - a.gb);
    if (!items.length) return '';
    const sum = items.reduce((a, x) => a + x.gb, 0);
    const rows = items.map(x => {
      const pct = sum ? 100 * x.gb / sum : 0;
      return `<div class="disk-row disk-subrow">
        <div class="disk-cell-main"><span class="disk-name" title="${x.path}">${x.name}</span></div>
        <div class="disk-cell-bar">${diskBar(pct, 'cat-llm')}</div>
        <div class="disk-cell-size">${diskFmt(x.gb)}</div>
        <div class="disk-cell-pct">${x.gb > 0 && pct < 1 ? '<1' : Math.round(pct)}%</div>
      </div>`;
    }).join('');
    return `<div class="disk-section-title">${g.label} — by directory <span class="disk-section-sub">${diskFmt(sum)} · ${items.length} items · % of group</span></div>
      <div class="disk-list disk-sublist">${rows}</div>`;
  }).join('');
}

// Render reclaimable summary — actionable "easy to free" items.
function renderReclaimable(rep) {
  const rs = rep.reclaimable || {};
  if (!rs.items || !rs.items.length) return '';
  const totalTxt = rs.total_gb >= 10 ? rs.total_gb.toFixed(0) + 'G' : rs.total_gb.toFixed(1) + 'G';
  const rows = rs.items.map(it => {
    const size = it.size_gb >= 10 ? it.size_gb.toFixed(0) + 'G' : it.size_gb.toFixed(1) + 'G';
    return `<div class="reclaim-row">
      <span class="reclaim-icon">🗑</span>
      <span class="reclaim-name">${it.name}</span>
      <span class="reclaim-size">${size}</span>
      <span class="reclaim-reason">${it.reason}</span>
    </div>`;
  }).join('');
  return `<div class="reclaim-section">
    <div class="reclaim-header">
      <span class="reclaim-title">⚡ Easy to free</span>
      <span class="reclaim-total">${totalTxt} reclaimable</span>
    </div>
    <div class="reclaim-list">${rows}</div>
  </div>`;
}

// renderDiskReport builds the modal body from /api/disk (fresh df+du+docker)
// and /api/models (REAL on-disk model sizes, recomputed server-side on every
// call — so the numbers stay correct as models are downloaded/deleted).
function renderDiskReport(rep, inv) {
  const d = S.cache || {};
  const runMap = {}; for (const m of (d.models || [])) runMap[m.id] = !!m.running;
  const totalGB = diskGB(rep.total) || diskGB((d.disk || {}).disk_total);
  const usedGB = diskGB(rep.used) || diskGB((d.disk || {}).disk_used);
  const freeTxt = rep.free || (d.disk || {}).disk_free || '—';
  const pctOf = gb => totalGB ? 100 * gb / totalGB : 0;
  const usedPct = pctOf(usedGB);

  // 1. directories (real du -sh, sorted by size)
  const dirs = (rep.dirs || []).map(x => Object.assign({}, x, { gb: diskGB(x.size) })).sort((a, b) => b.gb - a.gb);
  const dirSum = dirs.reduce((a, x) => a + x.gb, 0);
  const unaccounted = Math.max(0, usedGB - dirSum);
  let dirRows = dirs.map(x => diskDirRow(x, pctOf)).join('');
  if (unaccounted > 0.5) dirRows += diskDirRow({ key: 'other', label: 'Unaccounted', path: 'misc + rounding', gb: unaccounted }, pctOf);

  // 2. docker images + volumes
  const dk = rep.docker || {}, t = dk.totals || {};
  const images = (dk.images || []).slice().sort((a, b) => diskGB(b.size) - diskGB(a.size));
  // Category summary strip
  const catCount = {}; const catSum = {};
  for (const im of images) {
    let cat = 'reclaimable';
    if (im.dangling) cat = 'dangling';
    else if (im.in_use) cat = 'in_use';
    else if (/^ai-stack-/.test(im.name)) cat = 'service';
    catCount[cat] = (catCount[cat] || 0) + 1;
    catSum[cat] = (catSum[cat] || 0) + diskGB(im.size);
  }
  const catStrip = Object.entries(catCount).map(([c, n]) => {
    const colors = { dangling: 'var(--bad)', in_use: 'var(--ok)', service: '', reclaimable: 'var(--warn)' };
    return `<span class="img-cat" style="color:${colors[c] || 'var(--fg2)'}">${n}</span>`;
  }).join('<span class="img-cat-sep">·</span>');
  const imgRows = images.map(im => {
    const isService = /^ai-stack-/.test(im.name);
    const badge = im.dangling ? diskBadge('dangling', 'bad')
      : im.in_use ? diskBadge('in use', 'ok')
      : isService ? diskBadge('service', '')
      : diskBadge('reclaimable', 'warn');
    const sub = [im.unique_size ? 'unique ' + im.unique_size : '',
                 im.shared_size && im.shared_size !== '0B' ? 'shared ' + im.shared_size : '',
                 im.containers ? im.containers + ' container' + (im.containers > 1 ? 's' : '') : ''].filter(Boolean).join(' · ');
    const p = pctOf(diskGB(im.size));
    return `<div class="disk-img">
      <div class="disk-img-main"><span class="disk-name">${im.name}</span>${badge}<div class="disk-sub2">${im.id}${sub ? ' · ' + sub : ''}</div></div>
      <div class="disk-cell-size">${im.size}</div>
      <div class="disk-cell-pct">${p < 1 ? '<1' : Math.round(p)}%</div>
    </div>`;
  }).join('');
  const vols = (dk.volumes || []).slice().sort((a, b) => diskGB(b.size) - diskGB(a.size));
  const volRows = vols.map(v => {
    const svc = v.service ? ` · <b>${v.service}</b>` : '';
    return `<div class="disk-img">
      <div class="disk-img-main"><span class="disk-name">${v.name}</span>${v.in_use ? diskBadge('in use', 'ok') : diskBadge('unused', 'warn')}<div class="disk-sub2">${v.links} link${v.links === 1 ? '' : 's'}${svc}</div></div>
      <div class="disk-cell-size">${v.size}</div>
      <div class="disk-cell-pct"></div>
    </div>`;
  }).join('');
  const dockerTotals = dk.ok
    ? `<div class="disk-note">Images <b>${t.images_total || '—'}</b> · reclaimable <b class="disk-reclaim">${t.images_reclaimable || '—'}</b> · containers ${t.containers || '—'} · volumes <b>${t.volumes || '—'}</b> (reclaimable ${t.volumes_reclaimable || '—'})</div>`
    : '<div class="disk-note">docker CLI unavailable</div>';

  // 3. models — REAL sizes from /api/models (on_disk / size_gb / download %)
  const models = (inv || []).slice().sort((a, b) => (b.on_disk - a.on_disk) || ((b.size_gb || 0) - (a.size_gb || 0)));
  const modelRows = models.map(m => {
    const gb = m.size_gb || 0, pct = pctOf(gb), dl = m.download || {};
    let badge;
    if (dl.status === 'downloading') badge = diskBadge('downloading ' + Math.round(dl.pct || 0) + '%', 'warn');
    else if (m.on_disk) badge = diskBadge('on disk', 'ok');
    else badge = diskBadge('not on disk', 'bad');
    const dot = runMap[m.id] ? '<span class="disk-running" title="running">●</span>' : '<span class="disk-stopped" title="stopped">○</span>';
    const sizeTxt = gb > 0 ? gb.toFixed(1) + ' GB' : (dl.status === 'downloading' ? (dl.downloaded_gb || 0).toFixed(1) + ' GB' : '—');
    return `<div class="disk-row">
      <div class="disk-cell-main">${dot}<span class="disk-name">${m.short || m.id}</span>${badge}</div>
      <div class="disk-cell-bar">${diskBar(pct, 'cat-llm')}</div>
      <div class="disk-cell-size">${sizeTxt}</div>
      <div class="disk-cell-pct">${gb > 0 ? (pct < 1 ? '<1' : Math.round(pct)) + '%' : ''}</div>
    </div>`;
  }).join('');
  const onDisk = models.filter(m => m.on_disk);
  const modelsTotal = onDisk.reduce((a, m) => a + (m.size_gb || 0), 0);

  const ts = rep.ts ? new Date(rep.ts * 1000) : null;
  const ageTxt = rep.age_s != null ? (rep.age_s < 2 ? 'just now' : Math.round(rep.age_s) + 's ago') : '';
  return `
  <div class="disk-summary">
    ${diskBar(usedPct, usedPct > 90 ? 'crit' : usedPct > 75 ? 'warn' : '')}
    <div class="disk-topmeta">
      <span><b>${rep.used || '—'}</b> used of <b>${rep.total || '—'}</b> · ${Math.round(usedPct)}%</span>
      <span class="disk-free">${freeTxt} free</span>
    </div>
    ${renderReclaimable(rep)}
    <div class="disk-actions">
      <span class="disk-note">scanned ${ageTxt}${ts ? ' · ' + ts.toLocaleTimeString() : ''}</span>
      <button class="btn btn-ghost disk-rescan" onclick="refreshDiskModal(this)">↻ Rescan</button>
    </div>
  </div>
  <div class="disk-section-title">Where the space goes <span class="disk-section-sub">du -sh · real disk usage</span></div>
  <div class="disk-breakdown">${dirRows}</div>
  ${renderSubGroups(rep)}
  <div class="disk-section-title">Container images <span class="disk-section-sub">docker system df · ${catStrip}</span></div>
  ${dockerTotals}
  <div class="disk-list">${imgRows || '<div class="disk-empty">no images</div>'}</div>
  ${volRows ? `<div class="disk-section-title">Docker volumes <span class="disk-section-sub">links → owning service</span></div><div class="disk-list">${volRows}</div>` : ''}
  <div class="disk-section-title">Models on disk <span class="disk-section-sub">${onDisk.length} on disk · ${diskFmt(modelsTotal)} total · real sizes from /api/models</span></div>
  <div class="disk-list">${modelRows || '<div class="disk-empty">no models</div>'}</div>`;
}

// loadDiskModalData fetches /api/disk + /api/models and fills #disk-modal-root.
// fresh=true bypasses the server's 10s cache (Rescan button).
async function loadDiskModalData(fresh) {
  const root = () => document.getElementById('disk-modal-root');
  if (!root()) return;
  root().innerHTML = '<div class="disk-loading"><span class="spinner"></span> Scanning disk…</div>';
  try {
    const [rep, inv] = await Promise.all([
      api('/api/disk' + (fresh ? '?fresh=1' : '')),
      api('/api/models'),
    ]);
    const el = root();
    if (el) el.innerHTML = renderDiskReport(rep, (inv && inv.models) || []);
  } catch (e) {
    const el = root();
    if (el) el.innerHTML = `<div class="disk-empty">Failed to load disk report: ${e.message || e}</div>`;
  }
}
function refreshDiskModal(btn) {
  if (btn) { btn.disabled = true; btn.textContent = '↻ Scanning…'; }
  loadDiskModalData(true);
}
function openDiskModal() {
  showModal({
    title: 'Disk Usage — Full Breakdown',
    body: '<div id="disk-modal-root"><div class="disk-loading"><span class="spinner"></span> Scanning disk…</div></div>',
    buttons: [{ label: 'Close', type: 'ghost', value: true }],
    wide: true,
  });
  loadDiskModalData(false);
}
