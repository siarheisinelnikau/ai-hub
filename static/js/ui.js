/* ═══════════════════════════════════════════════════════════════════
   ui.js — showModal, closeModal, toast, theme, login, command palette
   ═══════════════════════════════════════════════════════════════════ */

// ── Modal system ─────────────────────────────────────────────────
function showModal({ title, body, buttons, wide = false }) {
  return new Promise(resolve => {
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    const modalId = 'modal-' + Date.now();
    const titleId = modalId + '-title';
    const previousOverflow = document.body.style.overflow;
    const previousFocus = document.activeElement;
    overlay.innerHTML = `
      <div class="modal${wide ? ' modal-wide' : ''}" role="dialog" aria-modal="true" aria-labelledby="${titleId}">
        <button class="btn btn-ghost modal-close" aria-label="Close" style="position:absolute;top:8px;right:8px;font-size:14px;">✖</button>
        <h3 id="${titleId}">${title}</h3>
        ${body ? `<div class="modal-body">${body}</div>` : ''}
        <div class="modal-actions">
          ${buttons.map((b, i) => `<button class="btn ${b.type === 'danger' ? 'btn-danger' : b.type === 'primary' ? 'btn-primary' : 'btn-ghost'}" data-idx="${i}">${b.label}</button>`).join('')}
        </div>
      </div>`;
    $('modal-root').appendChild(overlay);
    document.body.style.overflow = 'hidden';
    const firstBtn = overlay.querySelector('button[data-idx]');
    if (firstBtn) firstBtn.focus();
    const close = (value) => {
      overlay.remove();
      document.body.style.overflow = previousOverflow;
      if (previousFocus && typeof previousFocus.focus === 'function') previousFocus.focus();
      document.removeEventListener('keydown', escHandler);
      resolve(value);
    };
    overlay.addEventListener('click', e => {
      if (e.target === overlay) { close(null); return; }
      const btn = e.target.closest('button');
      if (!btn) return;
      if (btn.dataset.idx !== undefined) close(buttons[+btn.dataset.idx].value);
      else if (btn.classList.contains('modal-close')) close(null);
    });
    const escHandler = e => { if (e.key === 'Escape' && overlay.isConnected) close(null); };
    document.addEventListener('keydown', escHandler);
  });
}

function closeModal() {
  const overlays = document.querySelectorAll('#modal-root .modal-overlay');
  const o = overlays[overlays.length - 1];
  if (o) { o.remove(); document.body.style.overflow = ''; }
}

// ── Toast ────────────────────────────────────────────────────────
function toast(msg, type = 'ok') {
  const t = document.createElement('div');
  t.className = 'toast';
  t.textContent = msg;
  t.style.borderColor = type === 'err' ? 'rgba(239,68,68,0.4)' : 'var(--border)';
  $('toast-root').appendChild(t);
  setTimeout(() => {
    t.classList.add('exiting');
    setTimeout(() => t.remove(), 300);
  }, 3000);
}

// ── Theme ────────────────────────────────────────────────────────
function toggleTheme() {
  const cur = document.documentElement.getAttribute('data-theme');
  const nxt = cur === 'light' ? 'dark' : 'light';
  if (nxt === 'light') {
    document.documentElement.setAttribute('data-theme', 'light');
    localStorage.setItem('theme', 'light');
    const b = document.getElementById('themeBtn'); if (b) b.textContent = '☀️';
  } else {
    document.documentElement.removeAttribute('data-theme');
    localStorage.setItem('theme', 'dark');
    const b = document.getElementById('themeBtn'); if (b) b.textContent = '🌙';
  }
}
(function () { const s = localStorage.getItem('theme'); if (s === 'light') document.documentElement.setAttribute('data-theme', 'light'); })();

// ── Login ────────────────────────────────────────────────────────
let _loggedOut = false;
function loginView() {
  // Remove credentials saved by older versions. Only the HttpOnly session cookie persists.
  localStorage.removeItem('aih_token');
  $('app').innerHTML = `<div class="login-wrap"><div class="login-box"><h2>ai-hub</h2><p class="sub">Your models. Your hardware.</p><label for="tok" style="display:block;text-align:left;margin-bottom:8px">Access token</label><input id="tok" type="password" placeholder="Enter your access token" autocomplete="current-password" onkeydown="if(event.key==='Enter')doLogin()"><div style="font-size:12px;color:var(--fg3);margin-top:12px;line-height:1.6">Use the token from your server configuration. Your session lasts up to 30 days.</div><button id="login-submit" onclick="doLogin()" style="margin-top:16px">Connect</button><p id="lerr" role="alert" style="color:var(--bad);font-size:12px;margin-top:8px;min-height:16px"></p></div></div>`;
  $('tok')?.focus();
}
async function doLogin() {
  const button = $('login-submit');
  if (button?.disabled) return;
  if (button) button.disabled = true;
  try {
    const token = $('tok')?.value || '';
    const r = await fetch('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) });
    if (r.ok) { _loggedOut = false; location.reload(); return; }
    $('lerr').textContent = r.status === 429 ? 'Too many attempts. Please try again shortly.' : 'Invalid access token.';
  } catch (_) {
    if ($('lerr')) $('lerr').textContent = 'Cannot reach ai-hub. Check your connection and try again.';
  } finally {
    if (button) button.disabled = false;
  }
}
async function logout() {
  _loggedOut = true;
  if (S.timer) { clearInterval(S.timer); S.timer = null; }
  localStorage.removeItem('aih_token');
  try {
    const r = await fetch('/api/login', { method: 'DELETE' });
    if (!r.ok && r.status !== 401) throw new Error('logout failed');
    loginView();
  } catch (_) {
    _loggedOut = false;
    toast('Could not sign out. Check your connection and try again.');
    if (S.mounted && !S.timer) S.timer = setInterval(refresh, 5000);
  }
}

// ── Pill (online/offline/starting state) ─────────────────────────
function pill(running, healthy, mid) {
  const pending = mid && _pending[mid];
  if (pending === 'stop' && !running) { _pending[mid] = null; return '<span class="pill pill-off">○ Offline</span>'; }
  if (pending === 'stop') return '<span class="pill pill-stopping">◼ Stopping</span>';
  if (pending === 'start' && !running) return '<span class="pill pill-warn">◐ Starting</span>';
  if (!running) { if (pending) _pending[mid] = null; return '<span class="pill pill-off">○ Offline</span>'; }
  if (healthy) { if (pending) _pending[mid] = null; return '<span class="pill pill-ok">● Online</span>'; }
  if (pending === 'start') return '<span class="pill pill-warn">◐ Starting</span>';
  return '<span class="pill pill-warn">◐ Starting</span>';
}

// ── Quant badge ──────────────────────────────────────────────────
function quantBadge(q) { return q ? `<span class="badge quant">${q}</span>` : ''; }

// ── I/O icon map ─────────────────────────────────────────────────
function ioIcon(t) {
  t = (t || '').toLowerCase();
  if (t === 'text') return '📝'; if (t === 'image') return '🖼';
  if (t === 'video') return '🎬'; if (t === 'audio') return '🎧';
  if (t === 'speech') return '🗣'; if (t === 'music') return '🎵';
  if (t === '3d') return '🧊'; return '•';
}

// ── Command palette (Ctrl/Cmd+K) ────────────────────────────────
function openPalette() {
  if ($('cmdk-ov')) return;
  const ov = document.createElement('div');
  ov.className = 'cmdk-overlay'; ov.id = 'cmdk-ov';
  ov.innerHTML = `<div class="cmdk" role="dialog" aria-modal="true">
    <input id="cmdk-input" type="text" placeholder="Type a command…  (start · stop · bench · switch · compare · export · …)" autocomplete="off" spellcheck="false">
    <div class="cmdk-list" id="cmdk-list"></div>
    <div class="cmdk-foot"><span>↑↓ navigate</span><span>↵ run</span><span>esc close</span></div></div>`;
  $('modal-root').appendChild(ov);
  const input = $('cmdk-input');
  const all = buildCommands();
  let filtered = all, sel = 0;
  const close = () => { ov.remove(); document.removeEventListener('keydown', onKey); };
  const render = () => {
    $('cmdk-list').innerHTML = filtered.length ?
      filtered.map((c, i) => `<div class="cmdk-item${i === sel ? ' sel' : ''}" data-i="${i}"><span class="cmdk-label">${c.label}</span><span class="cmdk-hint">${c.hint}</span></div>`).join('') :
      '<div class="cmdk-empty">no matching commands</div>';
  };
  const doSel = () => { const c = filtered[sel]; if (!c) return; close(); c.run(); };
  const filter = (q) => { q = q.trim().toLowerCase(); filtered = q ? all.filter(c => c.label.toLowerCase().includes(q) || c.id.includes(q)) : all; sel = 0; render(); };
  function onKey(e) {
    if (e.key === 'Escape') { close(); return; }
    if (e.key === 'ArrowDown') { if (filtered.length) { sel = Math.min(sel + 1, filtered.length - 1); render(); } e.preventDefault(); return; }
    if (e.key === 'ArrowUp') { sel = Math.max(sel - 1, 0); render(); e.preventDefault(); return; }
    if (e.key === 'Enter') { doSel(); return; }
  }
  input.addEventListener('input', () => filter(input.value));
  ov.addEventListener('click', e => { if (e.target === ov) close(); const it = e.target.closest('.cmdk-item'); if (it) { sel = +it.dataset.i; doSel(); } });
  document.addEventListener('keydown', onKey);
  render();
  setTimeout(() => input.focus(), 0);
}
document.addEventListener('keydown', e => { if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); openPalette(); } });

// ── Button ripple (CSS keyframes in animations.css) ─────────────
document.addEventListener('click', e => {
  const btn = e.target.closest && e.target.closest('.btn');
  if (!btn || btn.disabled) return;
  const rect = btn.getBoundingClientRect();
  const size = Math.max(rect.width, rect.height);
  const r = document.createElement('span');
  r.className = 'ripple';
  r.style.cssText = `width:${size}px;height:${size}px;left:${e.clientX - rect.left - size / 2}px;top:${e.clientY - rect.top - size / 2}px`;
  btn.appendChild(r);
  setTimeout(() => r.remove(), 500);
});
