/* ═══════════════════════════════════════════════════════════════════
   api.js — fetch wrapper, auth state, global shared state
   ═══════════════════════════════════════════════════════════════════ */

// ── DOM helper ───────────────────────────────────────────────────
const $ = id => document.getElementById(id);

// ── Global state (shared across all modules) ─────────────────────
const S = { hash: '', cache: null, timer: null, mounted: false, cardKey: {} };

const HUB_CONFIG = window.AIHUB_CONFIG || {};
function serviceURL(port, path = '/') {
  const url = new URL(window.location.origin);
  if (HUB_CONFIG.serverIP) url.hostname = HUB_CONFIG.serverIP.includes(':') ? `[${HUB_CONFIG.serverIP.replace(/^\[|\]$/g, '')}]` : HUB_CONFIG.serverIP;
  url.protocol = 'http:';
  url.port = String(port);
  url.pathname = path;
  url.search = ''; url.hash = '';
  return url.href;
}
function hubURL(path = '/') { return new URL(path, HUB_CONFIG.hubURL || window.location.origin).href; }
function webuiURL() { return HUB_CONFIG.webuiURL || serviceURL(3000); }
function mcpURL() { return HUB_CONFIG.mcpURL || serviceURL(8090, '/mcp'); }

// ── Pending model actions (start/stop) for correct pill state ────
const _pending = {};  // mid -> 'start'|'stop'|null

// ── Fetch wrapper with auth handling ─────────────────────────────
async function api(path, opts = {}) {
  opts.headers = Object.assign({ 'Content-Type': 'application/json' }, opts.headers || {});
  const r = await fetch(path, opts);
  if (r.status === 401) { loginView(); throw new Error('auth'); }
  return r.json();
}

// ── Export CSV ───────────────────────────────────────────────────
function exportCSV() {
  const h = ($('exp-hours') && $('exp-hours').value) || 24;
  window.location.href = '/api/export?what=gpu&hours=' + h;
}
