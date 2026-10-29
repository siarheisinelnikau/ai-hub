/* ═══════════════════════════════════════════════════════════════════
   main.css — all component/layout styles (no keyframes, no media queries)
   ═══════════════════════════════════════════════════════════════════ */
*{box-sizing:border-box;margin:0;padding:0}
:root{
  --bg:#07080a;--bg2:#101114;--surface:#15161a;--card:#1a1c20;--card-hover:#22242b;--border:#2a2d36;--border-hover:#343845;
  --fg:#f8fafc;--fg2:#cbd5e1;--fg3:#94a3b8;--muted:#1e2028;
  --ok:#22c55e;--warn:#eab308;--bad:#ef4444;--accent:#f1f5f9;
  --radius:16px;--radius-lg:20px;--radius-sm:12px;
}
[data-theme="light"]{
  --bg:#fafaf9;--bg2:#ffffff;--surface:#ffffff;--card:#ffffff;--card-hover:#fafaf9;--border:#e7e5e4;--border-hover:#d6d3d1;
  --fg:#1c1917;--fg2:#57534e;--fg3:#a8a29e;--muted:#f5f5f4;
  --ok:#16a34a;--warn:#ca8a04;--bad:#dc2626;--accent:#1c1917;
}
html{font-family:'Inter',-apple-system,BlinkMacSystemFont,'Segoe UI',system-ui,sans-serif;font-size:15px;line-height:1.55}
body{background:var(--bg);color:var(--fg);min-height:100vh;position:relative;overflow-x:clip}
body::after{content:'';position:fixed;inset:0;background-image:url('/bg.svg');background-size:600px 400px;background-repeat:repeat;opacity:0.18;pointer-events:none;z-index:0}
[data-theme="light"] body::after{opacity:0.07}
body::before{content:'';position:fixed;inset:0;background:radial-gradient(ellipse 90% 60% at 50% -15%, rgba(148,163,184,0.08), transparent 55%), radial-gradient(ellipse 70% 50% at 90% 90%, rgba(99,102,241,0.05), transparent 60%);pointer-events:none;z-index:0}
.shell{position:relative;z-index:1;max-width:1360px;margin:0 auto;padding:24px 24px 48px}
#alert-banner-wrap{position:relative;padding:0 0 8px}

/* ── Header ────────────────────────────────────────────────────── */
header{display:flex;align-items:center;gap:8px;padding:8px 0 20px;border-bottom:1px solid var(--border);margin-bottom:20px}
.logo{font-size:22px;font-weight:800;letter-spacing:-0.7px}
.logo span{color:var(--fg3);font-weight:500;font-size:13px;margin-left:10px;letter-spacing:0}
.hw{display:flex;align-items:center;gap:8px;color:var(--fg3);font-size:13px}
.hw .dot{width:8px;height:8px;border-radius:50%;background:var(--ok);box-shadow:0 0 8px rgba(34,197,94,0.5)}
.spacer{flex:1}
.hsep{width:1px;height:22px;background:var(--border);margin:0 6px;flex:none}
/* Unified top-bar controls: one height, one radius, one type, one hover */
header .btn-logout,header .icon-btn,header .icon-btn.wide,header .exp-select,header .model-filter{height:34px;box-sizing:border-box;border-radius:10px;flex:none;display:inline-flex;align-items:center;justify-content:center;font-size:12.5px;font-weight:600;line-height:1;white-space:nowrap;font-family:'Inter',system-ui,sans-serif;transition:border-color .15s,background .15s,color .15s,width .2s}
header .btn-logout{background:var(--card);border:1px solid var(--border);color:var(--fg2);cursor:pointer;padding:0 12px;gap:6px}
header .btn-logout:hover{border-color:var(--border-hover);color:var(--fg);background:var(--card-hover)}
header .icon-btn{width:34px;padding:0;background:var(--card);border:1px solid var(--border);color:var(--fg2);cursor:pointer}
header .icon-btn:hover{border-color:var(--border-hover);color:var(--fg);background:var(--card-hover)}
header .icon-btn.wide{width:auto;min-width:34px;padding:0 12px;gap:6px}
header .exp-select{background:var(--muted);border:1px solid var(--border);color:var(--fg2);padding:0 10px;cursor:pointer;outline:none}
header .exp-select:hover{border-color:var(--border-hover);color:var(--fg)}
header .model-filter{background:var(--muted);border:1px solid var(--border);color:var(--fg);outline:none;width:190px;padding:0 12px}
header .model-filter:focus{border-color:var(--fg3);width:230px}
header .hg{font-size:14px;line-height:1;flex:none}
/* base classes kept for non-header usage (modals, cards) */
.icon-btn{width:36px;height:36px;border-radius:10px;border:1px solid var(--border);background:var(--card);color:var(--fg2);cursor:pointer;display:flex;align-items:center;justify-content:center;font-size:16px;transition:all .15s}
.icon-btn:hover{border-color:var(--border-hover);color:var(--fg);background:var(--card-hover)}
.icon-btn.wide{width:auto;min-width:36px;padding:0 13px;gap:7px;white-space:nowrap;font-size:13px;font-weight:600;font-family:'JetBrains Mono',monospace}
.btn-logout{background:var(--card);border:1px solid var(--border);color:var(--fg2);border-radius:10px;padding:8px 14px;font-size:13px;font-weight:500;cursor:pointer}
.btn-logout:hover{border-color:var(--border-hover);color:var(--fg)}
.exp-select{background:var(--muted);border:1px solid var(--border);color:var(--fg2);border-radius:10px;padding:0 8px;font-size:12px;font-family:'JetBrains Mono',monospace;height:36px;cursor:pointer;outline:none}
.exp-select:hover{border-color:var(--border-hover);color:var(--fg)}
.model-filter{background:var(--muted);border:1px solid var(--border);color:var(--fg);border-radius:10px;padding:8px 12px;font-size:12.5px;outline:none;width:190px;transition:border-color .15s, width .2s}
.model-filter:focus{border-color:var(--fg3);width:230px}

/* ── System grid ───────────────────────────────────────────────── */
.sys{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:10px;margin-bottom:16px}
.sys-card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius-sm);padding:12px 14px;box-shadow:0 1px 2px rgba(0,0,0,0.3)}
.sys-card.clickable{cursor:pointer}
.sys-card.clickable:hover{border-color:var(--border-hover)}
.sys-label{font-size:10px;font-weight:700;letter-spacing:0.8px;text-transform:uppercase;color:var(--fg3);margin-bottom:5px}
.sys-val{font-size:19px;font-weight:800;letter-spacing:-0.4px;font-family:'JetBrains Mono',monospace}
.sys-sub{font-size:11.5px;color:var(--fg3);margin-top:2px}
.sys-val .dim{font-size:12.5px;font-weight:600;color:var(--fg3)}
.mem-dot{display:inline-block;width:9px;height:9px;border-radius:50%;background:var(--ok);margin-left:5px;vertical-align:middle;box-shadow:0 0 6px currentColor}
.mem-dot.warn{background:var(--warn)}
.mem-dot.crit{background:var(--err,#ef4444)}
.lw-mini{display:inline-flex;align-items:center;gap:3px;font-size:12px;font-weight:700;color:var(--fg3);margin-left:6px;cursor:pointer;vertical-align:middle}
.lw-mini:hover{color:var(--fg)}
.rh-grid{display:grid;grid-template-columns:1fr 1fr;gap:4px 12px}
.rh{display:flex;align-items:baseline;gap:5px;font-size:15px;font-weight:800;font-family:'JetBrains Mono',monospace;color:var(--ok);white-space:nowrap;min-width:0}
.rh .k{font-size:9.5px;font-weight:700;letter-spacing:.3px;color:var(--fg3);flex:0 0 auto}
.rh b{font-weight:800;overflow:hidden;text-overflow:ellipsis}
.rh.warn{color:var(--warn)}.rh.crit{color:var(--err,#ef4444)}
.rh.warn .k,.rh.crit .k{color:inherit;opacity:.7}

/* ── Last-words ────────────────────────────────────────────────── */
.lw-strip{display:flex;align-items:center;gap:10px;flex-wrap:wrap;background:var(--card);border:1px solid var(--border);border-radius:var(--radius-sm);padding:9px 12px;margin-bottom:16px;box-shadow:0 1px 2px rgba(0,0,0,0.3)}
.lw-strip-label{font-size:10px;font-weight:700;letter-spacing:.5px;text-transform:uppercase;color:var(--fg3);flex:0 0 auto}
.lw-strip-items{display:flex;gap:6px;flex-wrap:wrap;flex:1 1 auto;min-width:0}
.lw-chip{font-size:10.5px;font-family:'JetBrains Mono',monospace;color:var(--fg2);background:var(--muted);border:1px solid var(--border);border-radius:7px;padding:3px 8px;cursor:pointer;white-space:nowrap;transition:all .15s;max-width:100%;overflow:hidden;text-overflow:ellipsis}
.lw-chip:hover{border-color:var(--border-hover);color:var(--fg);background:var(--card-hover)}
.lw-all{display:flex;gap:8px;flex-wrap:wrap}
.lw-all .lw-chip{font-size:11.5px}
.lw-pre{background:var(--muted);border:1px solid var(--border);border-radius:8px;padding:12px;font-size:11px;font-family:'JetBrains Mono',monospace;white-space:pre-wrap;max-height:60vh;overflow:auto;color:var(--fg2);line-height:1.5}

/* ── GPU drop banner ───────────────────────────────────────────── */
.gpu-drop-banner{display:flex;align-items:center;gap:11px;background:rgba(239,68,68,.12);border:1px solid rgba(239,68,68,.45);border-left:4px solid var(--err,#ef4444);border-radius:11px;padding:11px 14px;margin-bottom:16px;color:var(--fg);font-size:13px;font-weight:550;line-height:1.45}
.gpu-drop-banner .gdb-icon{font-size:17px;flex:0 0 auto}
.gpu-drop-banner .gdb-msg{flex:1;min-width:0}
.gpu-drop-banner .gdb-dismiss{flex:0 0 auto;background:rgba(239,68,68,.16);border:1px solid rgba(239,68,68,.45);color:var(--err,#ef4444);border-radius:8px;padding:5px 12px;font-size:12px;font-weight:700;cursor:pointer;transition:background .15s}
.gpu-drop-banner .gdb-dismiss:hover{background:rgba(239,68,68,.28)}

/* ── Settings ──────────────────────────────────────────────────── */
.set-sec{font-size:13px}
.set-sec-title{font-size:14px;font-weight:750;letter-spacing:-0.2px;margin-bottom:12px}
.set-sub{font-size:11px;font-weight:700;letter-spacing:.4px;text-transform:uppercase;color:var(--fg3);margin-bottom:8px}
.set-sub2{margin-top:14px}
.set-bot{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
.set-input{flex:1 1 160px;min-width:0;background:var(--muted);border:1px solid var(--border);border-radius:9px;padding:8px 11px;font-size:12.5px;font-family:'JetBrains Mono',monospace;color:var(--fg);outline:none}
.set-input:focus{border-color:var(--fg3)}
.set-textarea{width:100%;background:var(--muted);border:1px solid var(--border);border-radius:10px;padding:10px 12px;font-size:12px;font-family:'JetBrains Mono',monospace;color:var(--fg);outline:none;resize:vertical;line-height:1.6}
.set-textarea:focus{border-color:var(--fg3)}
.set-actions{display:flex;gap:10px;align-items:center;margin-top:14px;flex-wrap:wrap}
.set-result{font-size:12px;font-weight:600;color:var(--fg2)}
.set-note{font-size:12px;padding:8px 11px;border-radius:9px;margin-bottom:10px;background:var(--muted);border:1px solid var(--border);color:var(--fg2)}
.set-note.warn{background:rgba(245,158,11,.1);border-color:rgba(245,158,11,.4);color:var(--warn)}
.set-hint{font-size:11.5px;color:var(--fg3);margin-top:12px;line-height:1.5}
.set-test-results{margin-top:10px;display:flex;flex-direction:column;gap:5px}
.set-tr{display:flex;justify-content:space-between;gap:10px;align-items:center;padding:6px 10px;border-radius:8px;font-size:11.5px;font-family:'JetBrains Mono',monospace;border:1px solid var(--border)}
.set-tr.ok{background:rgba(34,197,94,.08);border-color:rgba(34,197,94,.35)}
.set-tr.bad{background:rgba(239,68,68,.08);border-color:rgba(239,68,68,.35)}
.set-tr-url{color:var(--fg2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:70%}
.set-tr-st{font-weight:700;flex:0 0 auto}
.set-tr.ok .set-tr-st{color:var(--ok)}
.set-tr.bad .set-tr-st{color:var(--err,#ef4444)}

/* ── Model manager ─────────────────────────────────────────────── */
.mm-list{display:flex;flex-direction:column;gap:9px}
.mm-row{display:grid;grid-template-columns:minmax(180px,1.4fr) 110px minmax(120px,1fr) minmax(150px,1fr) auto;gap:12px;align-items:center;background:var(--card);border:1px solid var(--border);border-radius:10px;padding:11px 13px}
.mm-id{display:flex;flex-direction:column;gap:3px;min-width:0}
.mm-meta{font-size:10.5px;color:var(--fg3);font-family:'JetBrains Mono',monospace}
.mm-cell{font-size:12px;display:flex;flex-direction:column;gap:3px}
.mm-ok{color:var(--ok);font-weight:700;font-size:12px}
.mm-bad{color:var(--err,#ef4444);font-weight:700;font-size:12px}
.mm-dl{color:var(--info,#38bdf8);font-weight:700;font-size:12px}
.mm-prog{height:6px;border-radius:4px;background:var(--muted);border:1px solid var(--border);overflow:hidden;margin-top:2px;min-width:110px}
.mm-prog-bar{height:100%;width:0;border-radius:4px;background:linear-gradient(90deg,#38bdf8,#22c55e);transition:width .4s ease}
.mm-prog-txt{font-size:10.5px;color:var(--fg3);font-family:'JetBrains Mono',monospace;white-space:nowrap}
.mm-idle{color:var(--fg3);font-size:11.5px;font-style:italic}
.mm-err{font-size:10px;color:var(--err,#ef4444);max-width:170px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.mm-actions{display:flex;gap:7px;flex-wrap:wrap;justify-content:flex-end}
.mm-btn{font-size:11.5px;padding:6px 10px;white-space:nowrap}
.mm-btn.mm-subtle{color:var(--fg3);border-color:transparent;background:transparent}
.mm-btn.mm-subtle:hover{color:var(--fg2);background:var(--muted)}
.mm-btn.mm-del{color:var(--err,#ef4444)}
.mm-btn.mm-del:hover{background:rgba(239,68,68,.12);color:var(--err,#ef4444)}
.mm-conn{font-size:13px;font-weight:600;margin-bottom:8px}
.mm-conn.bad{color:var(--err,#ef4444)}
.mm-note{font-size:11.5px;color:var(--fg3);margin-top:12px;line-height:1.55}
.mm-note code{background:var(--muted);padding:1px 5px;border-radius:4px;font-size:10.5px}

/* ── System history ────────────────────────────────────────────── */
.sys-history{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:14px}
.sh-card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius-sm);padding:12px 14px;box-shadow:0 1px 2px rgba(0,0,0,0.3)}
.sh-label{font-size:10px;font-weight:700;letter-spacing:.4px;text-transform:uppercase;color:var(--fg3);margin-bottom:6px}
.sh-val{font-size:20px;font-weight:800;letter-spacing:-0.4px;font-family:'JetBrains Mono',monospace;color:var(--fg)}
.sh-sub{font-size:11px;color:var(--fg3);margin-top:4px}
.sh-trend{background:var(--card);border:1px solid var(--border);border-radius:var(--radius-sm);padding:12px 14px;margin-bottom:20px;box-shadow:0 1px 2px rgba(0,0,0,0.3)}
.sh-trend-head{display:flex;gap:10px;align-items:center;margin-bottom:8px;font-size:10px;font-weight:700;letter-spacing:.4px;text-transform:uppercase;color:var(--fg3)}
.sh-trend-head .lt{display:inline-flex;align-items:center;gap:4px}
.sh-trend-head .lt i{width:9px;height:3px;border-radius:2px;display:inline-block}
.sh-trend canvas{display:block;width:100%;height:72px;border-radius:6px;background:rgba(148,163,184,0.04)}

/* ── GPU cards ─────────────────────────────────────────────────── */
.gpus{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;margin-bottom:28px}
.gpu{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:14px;box-shadow:0 1px 3px rgba(0,0,0,0.35)}
.gpu-head{display:flex;justify-content:space-between;align-items:center;margin-bottom:10px}
.gpu-name{font-weight:700;font-size:13px;letter-spacing:-0.2px}
.gpu-meta{font-size:12px;color:var(--fg3);font-family:'JetBrains Mono',monospace;display:flex;gap:8px;align-items:center;flex-wrap:wrap;justify-content:flex-end}
.gpu-meta b{color:var(--fg);font-weight:700}
.gpu-meta .tm-core b{color:#f59e0b}
.gpu-meta .tm-vram b{color:#22d3ee}
.gpu-meta .tm-vram.na{color:var(--fg3)}.gpu-meta .tm-vram.na b{color:var(--fg3)}
.gpu-charts{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:10px}
.gc{display:flex;flex-direction:column;gap:4px;min-width:0}
.gc-head{display:flex;gap:8px;font-size:9px;font-weight:700;letter-spacing:.4px;text-transform:uppercase;color:var(--fg3)}
.gc-head .lt{display:inline-flex;align-items:center;gap:3px}
.gc-head .lt i{width:8px;height:3px;border-radius:2px;display:inline-block}
.gc canvas{display:block;width:100%;height:56px;border-radius:6px;background:rgba(148,163,184,0.04)}
.gpu-bar{height:6px;background:var(--muted);border-radius:6px;overflow:hidden;margin:8px 0}
.gpu-fill{height:100%;border-radius:6px;transition:width .6s ease;background:var(--fg)}
.gpu-fill.warn{background:var(--warn)}
.gpu-fill.crit{background:var(--bad)}
.gpu-foot{display:flex;justify-content:space-between;font-size:12px;color:var(--fg3);font-family:'JetBrains Mono',monospace}
.gpu-tenants{display:flex;gap:4px;flex-wrap:wrap;margin-top:6px;justify-content:flex-end}
.tenant-chip{font-size:10px;font-weight:700;letter-spacing:.3px;padding:2px 7px;border-radius:6px;background:rgba(34,197,94,.1);color:var(--ok);border:1px solid rgba(34,197,94,.25);font-family:'JetBrains Mono',monospace;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}

/* ── Category ──────────────────────────────────────────────────── */
.cat{margin:28px 0 0}
.cat-head{display:flex;align-items:center;gap:12px;padding:4px 2px 12px;border-bottom:1px solid var(--border);margin-bottom:14px}
.cat-icon{width:36px;height:36px;border-radius:11px;background:var(--card);border:1px solid var(--border);display:flex;align-items:center;justify-content:center;font-size:16px}
.cat-title{font-size:15px;font-weight:700;letter-spacing:-0.2px}
.cat-count{font-size:11px;font-weight:700;background:var(--muted);border:1px solid var(--border);color:var(--fg3);padding:3px 9px;border-radius:20px}
.cat-desc{margin-left:auto;font-size:12px;color:var(--fg3);display:none}
.cat-grid{display:grid;grid-template-columns:1fr;gap:12px}

/* ── Model card ────────────────────────────────────────────────── */
.model{position:relative;background:var(--card);border:1px solid var(--border);border-radius:var(--radius-lg);padding:14px 16px;transition:border-color .18s, background .18s, box-shadow .18s, transform .18s;box-shadow:0 1px 3px rgba(0,0,0,0.4), 0 6px 16px rgba(0,0,0,0.2)}
.model:hover{border-color:var(--border-hover);background:var(--card-hover);box-shadow:0 4px 12px rgba(0,0,0,0.5), 0 12px 32px rgba(0,0,0,0.35);transform:translateY(-1px)}
.model.running{border-color:rgba(34,197,94,0.35);background:var(--card)}
.model.running:hover{border-color:rgba(34,197,94,0.5)}
.model.conflict{border-color:rgba(234,179,8,0.4)}
.model-head{display:flex;justify-content:space-between;gap:14px;align-items:flex-start}
.io-compact{flex:0 0 auto;display:inline-flex;align-items:center;gap:7px;font-size:15px;line-height:1;background:var(--muted);border:1px solid var(--border);border-radius:10px;padding:5px 11px;white-space:nowrap;margin-top:1px;cursor:default}
.io-compact .sep{color:var(--fg3);font-size:13px;opacity:.8}
.model-main{flex:1;min-width:0}
.title-row{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.model-name{font-size:16px;font-weight:750;letter-spacing:-0.3px;line-height:1.2}
/* running model: title is a button to the live stats page */
.model-name.name-click{cursor:pointer;border-radius:6px;padding:1px 4px;margin-left:-4px;transition:background .15s,color .15s}
.model-name.name-click:hover{background:rgba(34,197,94,.1);color:var(--ok);text-decoration:underline;text-underline-offset:3px}
.uncensored-mark{display:inline-flex;align-items:center;justify-content:center;flex-shrink:0;width:24px;height:24px;border:1px solid rgba(245,158,11,.3);border-radius:7px;color:#d97706;background:rgba(245,158,11,.09)}
.pill{font-size:10px;font-weight:800;letter-spacing:0.6px;padding:4px 10px;border-radius:20px;border:1px solid;text-transform:uppercase;white-space:nowrap}
.pill-ok{background:rgba(34,197,94,0.12);color:var(--ok);border-color:rgba(34,197,94,0.25)}
.pill-off{background:var(--muted);color:var(--fg3);border-color:var(--border)}
.pill-warn{background:rgba(234,179,8,0.12);color:var(--warn);border-color:rgba(234,179,8,0.25)}
.pill-stopping{background:rgba(239,68,68,0.12);color:var(--bad);border-color:rgba(239,68,68,0.25)}
.badges{display:flex;gap:6px;flex-wrap:wrap;margin-top:9px}
.badge{font-size:11px;font-weight:600;padding:4px 9px;border-radius:8px;border:1px solid var(--border);background:var(--muted);color:var(--fg2);display:inline-flex;align-items:center;gap:5px}
.badge strong{color:var(--fg);font-weight:700}
.badge.quant{background:var(--card);border-color:var(--border)}
.specs{display:grid;gap:8px 12px;align-items:center;margin-top:8px;background:var(--muted);border:1px solid var(--border);border-radius:10px;padding:8px 12px}
.spec{display:flex;flex-direction:column;gap:2px;min-width:0}
.spec-label{font-size:11px;font-weight:700;letter-spacing:0.6px;text-transform:uppercase;color:var(--fg3)}
.spec-val{font-size:13px;font-weight:600;font-family:'JetBrains Mono',monospace;color:var(--fg);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.spec-sub{font-size:11px;color:var(--fg3);margin-left:2px}
.io{display:flex;gap:8px;margin-top:10px;align-items:center}
.io-box{flex:1;display:flex;align-items:center;gap:10px;background:var(--muted);border:1px solid var(--border);border-radius:12px;padding:10px 12px;min-width:0}
.io-label{font-size:10px;font-weight:800;letter-spacing:0.7px;text-transform:uppercase;color:var(--fg3);border:1px solid var(--border);background:var(--card);padding:4px 7px;border-radius:7px;white-space:nowrap}
.io-items{display:flex;gap:6px;flex-wrap:wrap;align-items:center}
.io-item{font-size:12px;font-weight:550;color:var(--fg2);display:inline-flex;align-items:center;gap:5px;background:var(--card);border:1px solid var(--border);padding:4px 8px;border-radius:8px}
.io-arrow{display:flex;align-items:center;justify-content:center;width:32px;height:32px;border-radius:10px;background:var(--muted);border:1px solid var(--border);color:var(--fg2);font-weight:700;font-size:14px;flex-shrink:0;align-self:center}
.subs{display:flex;gap:6px;flex-wrap:wrap;margin-top:8px}
.sub-tag{font-size:11px;font-weight:500;color:var(--fg3);background:var(--muted);border:1px solid var(--border);padding:4px 8px;border-radius:8px;display:inline-flex;gap:5px;align-items:center}
.hint{margin-top:12px;font-size:12px;color:#a16207;background:rgba(234,179,8,0.09);border:1px solid rgba(234,179,8,0.22);border-radius:10px;padding:8px 12px;display:flex;gap:8px;align-items:center}
[data-theme="light"] .hint{color:#854d0e;background:#fefce8;border-color:#fde68a}
.foot{display:flex;justify-content:space-between;gap:10px;align-items:center;margin-top:10px;padding-top:10px;border-top:1px solid var(--border);flex-wrap:wrap}
.lifecycle{display:flex;gap:8px;align-items:center}
.access{display:flex;gap:8px;align-items:center;margin-left:auto;flex-wrap:wrap;justify-content:flex-end}
.desc{margin-top:6px;color:var(--fg3);font-size:11.5px;line-height:1.4;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}

/* ── Buttons ───────────────────────────────────────────────────── */
.btn{font-size:13px;font-weight:600;padding:8px 14px;border-radius:10px;border:1px solid;cursor:pointer;display:inline-flex;align-items:center;gap:6px;transition:all .15s;text-decoration:none;line-height:1}
.btn-primary{background:var(--fg);color:var(--bg);border-color:var(--fg)}
.btn-primary:hover{opacity:0.9;transform:translateY(-1px)}
.btn-ghost{background:var(--card);color:var(--fg2);border-color:var(--border)}
.btn-ghost:hover{border-color:var(--border-hover);color:var(--fg);background:var(--card-hover)}
.btn-danger{background:transparent;color:var(--bad);border-color:rgba(239,68,68,0.28)}
.btn-danger:hover{background:rgba(239,68,68,0.08);border-color:rgba(239,68,68,0.4)}
.btn-access{background:var(--card);color:var(--fg2);border-color:var(--border)}
.btn-access:hover{border-color:var(--border-hover);color:var(--fg)}
.btn-access[href]{text-decoration:none}
.btn-stats{background:var(--card);color:var(--ok);border-color:rgba(34,197,94,0.25)}
.btn-stats:hover{border-color:rgba(34,197,94,0.4);background:rgba(34,197,94,0.06)}

/* ── Tooltips ──────────────────────────────────────────────────── */
.tooltip-bubble{position:relative;cursor:help}
.tooltip-bubble[data-tooltip]:hover::after{content:attr(data-tooltip);position:absolute;bottom:calc(100% + 8px);left:50%;transform:translateX(-50%);background:var(--card);border:1px solid var(--border);color:var(--fg);padding:8px 11px;border-radius:10px;font-size:11px;font-weight:500;white-space:normal;max-width:300px;min-width:160px;width:max-content;text-align:center;line-height:1.35;word-wrap:break-word;z-index:20;box-shadow:0 8px 24px rgba(0,0,0,0.35);pointer-events:none;letter-spacing:0}
.tooltip-bubble[data-tooltip]:hover::before{content:'';position:absolute;bottom:calc(100% + 2px);left:50%;transform:translateX(-50%);border:6px solid transparent;border-top-color:var(--border);z-index:20;pointer-events:none}
.divider{height:1px;background:var(--border);margin:32px 0 0}

/* ── Modal & Toast ─────────────────────────────────────────────── */
.modal-overlay{position:fixed;inset:0;background:rgba(0,0,0,0.55);backdrop-filter:blur(6px);z-index:1000;display:flex;align-items:flex-start;justify-content:center;padding:40px 20px;overflow-y:auto}
.modal{position:relative;background:var(--bg2);border:1px solid var(--border);border-radius:var(--radius-lg);padding:24px;min-width:360px;max-width:520px;box-shadow:0 20px 60px rgba(0,0,0,0.5);width:100%;margin:auto}
.modal h3{font-size:15px;font-weight:700;margin-bottom:8px}
.modal p{color:var(--fg2);font-size:13px;line-height:1.6;margin-bottom:18px}
.modal-body{color:var(--fg2);font-size:13px;line-height:1.6;margin-bottom:18px}
.modal-actions{display:flex;gap:8px;justify-content:flex-end;margin-top:16px}
.modal-wide{min-width:680px;max-width:860px;padding:26px 30px}
.toast{position:fixed;top:20px;right:20px;z-index:2000;background:var(--card);border:1px solid var(--border);border-radius:12px;padding:12px 16px;font-size:13px;font-weight:500;box-shadow:0 10px 30px rgba(0,0,0,0.3)}

/* ── Connection / Alert banners ────────────────────────────────── */
.conn-banner{position:fixed;top:0;left:0;right:0;z-index:3000;background:rgba(239,68,68,.14);border-bottom:1px solid rgba(239,68,68,.4);color:var(--bad);font-size:12.5px;font-weight:600;padding:8px 16px;display:flex;align-items:center;gap:8px;backdrop-filter:blur(6px);animation:fade-in .2s ease}
#alert-banner{display:flex;flex-wrap:wrap;align-items:center;gap:8px;padding:8px 16px;font-size:12.5px;font-weight:600;backdrop-filter:blur(8px);box-shadow:0 6px 20px rgba(0,0,0,.35);animation:fade-in .2s ease}
#alert-banner.warn{background:rgba(245,158,11,.16);border:1px solid rgba(245,158,11,.5);border-radius:var(--radius)}
#alert-banner.crit{background:rgba(239,68,68,.2);border:1px solid rgba(239,68,68,.55);border-radius:var(--radius)}
#alert-banner .alert-title{font-family:'JetBrains Mono',monospace;letter-spacing:.5px;padding:2px 10px;border-radius:8px;font-weight:700}
#alert-banner.warn .alert-title{background:rgba(245,158,11,.2);color:#fbbf24}
#alert-banner.crit .alert-title{background:rgba(239,68,68,.22);color:#fca5a5}
#alert-banner .alert-item{font-family:'JetBrains Mono',monospace;font-size:12px;padding:3px 10px;border-radius:8px;white-space:nowrap}
#alert-banner.warn .alert-item{background:rgba(245,158,11,.14);color:#fcd34d}
#alert-banner.crit .alert-item{background:rgba(239,68,68,.16);color:#fecaca}

/* ── Login ─────────────────────────────────────────────────────── */
.login-wrap{display:flex;align-items:center;justify-content:center;min-height:100vh;padding:20px}
.login-box{background:var(--card);border:1px solid var(--border);border-radius:20px;padding:32px;width:400px;text-align:center;box-shadow:0 12px 40px rgba(0,0,0,0.25)}
.login-box h2{font-size:20px;font-weight:800;letter-spacing:-0.5px;margin-bottom:4px}
.login-box .sub{color:var(--fg3);font-size:13px;margin-bottom:18px}
.login-box input{width:100%;background:var(--muted);border:1px solid var(--border);color:var(--fg);border-radius:10px;padding:11px 14px;font-size:14px;outline:none}
.login-box input:focus{border-color:var(--fg3)}
.login-box button{width:100%;margin-top:12px;background:var(--fg);color:var(--bg);border:1px solid var(--fg);border-radius:10px;padding:11px;font-size:14px;font-weight:700;cursor:pointer}
.log-box{background:var(--muted);border:1px solid var(--border);border-radius:12px;padding:14px;font-family:'JetBrains Mono',monospace;font-size:12.5px;color:var(--fg3);max-height:300px;overflow-y:auto;line-height:1.6;white-space:pre-wrap;word-break:break-all;margin-top:14px}

/* ── Stats modal ───────────────────────────────────────────────── */
.stats-modal{min-width:950px;max-width:1250px;width:100%}
.stats-modal .modal{padding:35px 42px 30px;min-width:950px;max-width:1250px}
.stats-header{display:flex;align-items:center;gap:15px;margin-bottom:22px}
.stats-header span:first-child{font-size:21px}
.stats-body{display:flex;gap:17px;align-items:stretch}
.stats-kpi{display:flex;gap:15px;flex-direction:column;flex-shrink:0;width:238px}
.stats-kpi-item{background:var(--muted);border:1px solid var(--border);border-radius:12px;padding:13px 22px;flex:1;min-width:0}
.stats-kpi-label{font-size:14px;font-weight:700;letter-spacing:0.6px;text-transform:uppercase;color:var(--fg3);margin-bottom:4px}
.stats-kpi-val{font-size:28px;font-weight:800;font-family:'JetBrains Mono',monospace;color:var(--fg);letter-spacing:-0.3px}
.stats-kpi-unit{font-size:14px;color:var(--fg3);margin-left:2px}
.stats-charts{flex:1;display:flex;flex-direction:column;gap:10px;min-width:0}
.stats-chart-box{background:var(--muted);border:1px solid var(--border);border-radius:14px;padding:10px 18px 8px;flex:1;min-height:0}
.stats-chart-title{font-size:13px;font-weight:800;letter-spacing:0.6px;text-transform:uppercase;margin-bottom:5px;display:flex;align-items:center;gap:7px}
.stats-chart-title .dot{width:10px;height:10px;border-radius:50%;display:inline-block}
.stats-chart-box canvas{width:100%;height:115px;display:block}
.stats-modal .btn{font-size:16px;padding:10px 18px}
.logs-modal{min-width:950px;max-width:1250px;width:100%}
.logs-modal .modal{padding:35px 42px 30px;min-width:950px;max-width:1250px}
.logs-modal .log-box{font-size:15px;max-height:520px}
.logs-modal .btn{font-size:16px;padding:10px 18px}

/* ── Compare / Recommend ───────────────────────────────────────── */
.cmp-section{margin-bottom:22px}
.cmp-title{font-size:12px;font-weight:700;letter-spacing:.4px;text-transform:uppercase;color:var(--fg3);margin-bottom:12px}
.cmp-table{width:100%;border-collapse:collapse;font-size:12.5px}
.cmp-table th{text-align:left;font-size:10px;font-weight:700;letter-spacing:.4px;text-transform:uppercase;color:var(--fg3);padding:8px 10px;border-bottom:1px solid var(--border)}
.cmp-table td{padding:9px 10px;border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;color:var(--fg2)}
.cmp-table td:first-child{color:var(--fg);font-family:inherit;font-weight:600}
.cmp-table tr.mrow-run td:first-child{color:var(--ok)}
.cmp-table td.best{color:var(--ok);font-weight:700}
.cmp-note{font-size:11px;color:var(--fg3);margin-top:8px}
.rec-select{width:100%;background:var(--muted);border:1px solid var(--border);color:var(--fg);border-radius:10px;padding:10px 12px;font-size:13px;outline:none;cursor:pointer;margin-bottom:14px}
.rec-pick{background:var(--card);border:1px solid var(--border);border-radius:var(--radius-sm);padding:14px 16px;box-shadow:0 1px 2px rgba(0,0,0,0.3)}
.rec-pick b{font-size:15px;color:var(--fg);margin-left:4px}
.rec-run{font-size:11px;font-family:'JetBrains Mono',monospace;font-weight:700}
.rec-run.ok{color:var(--ok)}.rec-run.off{color:var(--fg3)}
.rec-why{font-size:12.5px;color:var(--fg2);margin-left:8px}
.rec-meta{font-size:11.5px;color:var(--fg3);font-family:'JetBrains Mono',monospace;margin-top:8px}
.rec-actions{display:flex;gap:8px;margin-top:12px}
.rec-empty{color:var(--fg3);font-size:13px;padding:12px}

/* ── Benchmark ─────────────────────────────────────────────────── */
.bench-run{display:flex;align-items:center;gap:12px;font-size:13.5px;color:var(--fg2)}
.bench-spinner{width:18px;height:18px;border:2px solid var(--border);border-top-color:var(--ok);border-radius:50%;animation:spin .7s linear infinite;flex-shrink:0}
.bench-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:12px}
.bench-cell{background:var(--card);border:1px solid var(--border);border-radius:var(--radius-sm);padding:14px 16px}
.bench-k{font-size:10px;font-weight:700;letter-spacing:.4px;text-transform:uppercase;color:var(--fg3);margin-bottom:6px}
.bench-v{font-size:24px;font-weight:800;font-family:'JetBrains Mono',monospace;color:var(--fg);letter-spacing:-0.5px}
.bench-note{font-size:11px;color:var(--fg3);margin-top:12px}
.bench-err{font-size:13.5px;color:var(--warn);padding:8px 0}

/* ── Command palette ───────────────────────────────────────────── */
.cmdk-overlay{position:fixed;inset:0;z-index:4000;background:rgba(0,0,0,.55);backdrop-filter:blur(4px);display:flex;justify-content:center;align-items:flex-start;padding-top:12vh;animation:fade-in .12s ease}
.cmdk{width:100%;max-width:560px;background:var(--bg2);border:1px solid var(--border);border-radius:14px;box-shadow:0 24px 70px rgba(0,0,0,.55);overflow:hidden;animation:pop-in .16s cubic-bezier(.2,.9,.3,1.1)}
.cmdk-input{width:100%;box-sizing:border-box;border:none;border-bottom:1px solid var(--border);background:transparent;color:var(--fg);font-size:15px;padding:15px 16px;outline:none;font-family:inherit}
.cmdk-list{max-height:52vh;overflow:auto;padding:6px}
.cmdk-item{display:flex;justify-content:space-between;align-items:center;gap:10px;padding:10px 12px;border-radius:9px;cursor:pointer;font-size:13.5px;color:var(--fg2)}
.cmdk-item.sel{background:var(--muted);color:var(--fg)}
.cmdk-item .cmdk-label{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.cmdk-hint{font-size:10px;font-family:'JetBrains Mono',monospace;color:var(--fg3);text-transform:uppercase;letter-spacing:.4px;flex-shrink:0}
.cmdk-empty{padding:18px;text-align:center;color:var(--fg3);font-size:13px}
.cmdk-foot{display:flex;gap:14px;padding:8px 14px;border-top:1px solid var(--border);font-size:10.5px;color:var(--fg3);font-family:'JetBrains Mono',monospace}

/* ── Launch options ────────────────────────────────────────────── */
.launch{display:flex;gap:10px;align-items:center;margin-top:10px;padding:8px 12px;background:var(--muted);border:1px solid var(--border);border-radius:10px;flex-wrap:wrap}
.launch-title{font-size:10px;font-weight:800;letter-spacing:.7px;text-transform:uppercase;color:var(--fg3);white-space:nowrap}
.dd{position:relative;display:inline-block}
.dd-btn{display:inline-flex;align-items:center;gap:7px;background:var(--card);border:1px solid var(--border);color:var(--fg);border-radius:9px;padding:6px 10px;font-size:12.5px;font-family:'JetBrains Mono',monospace;cursor:pointer;transition:border-color .15s,background .15s,color .15s,box-shadow .15s}
.dd-btn:hover{border-color:var(--border-hover);background:var(--card-hover)}
.dd.open .dd-btn{border-color:var(--fg3);box-shadow:0 0 0 3px rgba(148,163,184,.12)}
.dd.hl .dd-btn{border-color:var(--warn);background:rgba(234,179,8,.12);color:var(--warn)}
.dd-chev{font-size:9px;color:var(--fg3);transition:transform .18s}
.dd.open .dd-chev{transform:rotate(180deg)}
.dd-pop{position:absolute;top:calc(100% + 6px);left:0;min-width:110px;width:max-content;background:var(--bg2);border:1px solid var(--border);border-radius:11px;box-shadow:0 14px 34px rgba(0,0,0,.5);padding:5px;z-index:60;display:none}
.dd.open .dd-pop{display:block;animation:pop-in .16s cubic-bezier(.2,.9,.3,1.1)}
.model.dd-card-elev,.model:has(.dd.open){position:relative;z-index:1000;box-shadow:0 4px 12px rgba(0,0,0,0.5),0 14px 34px rgba(0,0,0,0.4)}
.dd-opt{display:flex;justify-content:space-between;align-items:center;gap:14px;padding:6px 10px;border-radius:8px;font-size:12.5px;font-family:'JetBrains Mono',monospace;color:var(--fg2);cursor:pointer;white-space:nowrap;transition:background .12s,color .12s}
.dd-opt:hover{background:var(--muted);color:var(--fg)}
.dd-opt.sel{color:var(--fg);font-weight:700}
.dd-check{opacity:0;color:var(--ok);font-size:11px}
.dd-opt.sel .dd-check{opacity:1}
.dd.right .dd-pop{left:auto;right:0}
.dd-proj{margin-left:auto;font-size:11px;font-weight:700;font-family:'JetBrains Mono',monospace;white-space:nowrap}
.dd-proj.ok{color:var(--ok)}.dd-proj.warn{color:var(--warn)}
.launch-preview{font-size:11.5px;color:var(--fg3);font-family:'JetBrains Mono',monospace;flex-basis:100%;margin-top:3px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.launch-presets{display:flex;gap:6px;flex-wrap:wrap;align-items:center}
.preset-btn{background:var(--card);border:1px solid var(--border);color:var(--fg2);border-radius:8px;padding:5px 9px;font-size:11px;font-weight:700;font-family:'JetBrains Mono',monospace;cursor:pointer;transition:all .15s}
.preset-btn:hover{border-color:var(--border-hover);color:var(--fg);background:var(--card-hover)}
.preset-btn:active{transform:scale(.96)}

/* ── Running line ──────────────────────────────────────────────── */
.running-line{display:flex;gap:10px;align-items:center;margin-top:10px;padding:8px 12px;background:rgba(34,197,94,0.06);border:1px solid rgba(34,197,94,0.2);border-radius:10px;flex-wrap:wrap}
.rl-dot{width:8px;height:8px;border-radius:50%;background:var(--ok);box-shadow:0 0 8px rgba(34,197,94,.6);animation:pill-pulse 1.5s ease-in-out infinite;flex-shrink:0}
.rl-text{font-size:12.5px;color:var(--fg2)}
.rl-text b{color:var(--fg);font-family:'JetBrains Mono',monospace;font-weight:700}
.rl-eff{font-size:12px;font-family:'JetBrains Mono',monospace;color:var(--fg2);background:rgba(139,92,246,0.1);border:1px solid rgba(139,92,246,0.3);padding:3px 9px;border-radius:8px;white-space:nowrap}
.rl-eff b,.rl-eff{color:#c4b5fd}
.tps-live{font-size:11px;font-weight:800;font-family:'JetBrains Mono',monospace;color:var(--ok);background:rgba(34,197,94,.08);border:1px solid rgba(34,197,94,.22);padding:3px 8px;border-radius:8px;white-space:nowrap}
.mc-val{font-family:'JetBrains Mono',monospace;font-size:12px;font-weight:800;margin-left:2px;white-space:nowrap}
.mcharts{margin-top:10px;display:flex;gap:10px;flex-wrap:wrap}
.mc-col{flex:1 1 0;min-width:110px;display:flex;flex-direction:column;gap:6px}
.mc-head{display:flex;gap:6px;align-items:center;font-size:10px;font-weight:700;letter-spacing:.4px;text-transform:uppercase;color:var(--fg2);white-space:nowrap}
.mc-head .lt{display:inline-flex;align-items:center;gap:4px}
.mc-head .lt i{width:9px;height:3px;border-radius:2px;display:inline-block}
.mc-canvas{display:block;width:100%;height:64px;border-radius:8px;background:rgba(148,163,184,0.04)}

/* ── D-01: page sections (overview / models / connections+ops) ── */
.sec{margin-bottom:4px}
.sec-head{display:flex;align-items:baseline;gap:10px;margin:18px 2px 12px;padding-bottom:8px;border-bottom:1px solid var(--border)}
.sec-head .sec-title{font-size:15px;font-weight:800;letter-spacing:-0.2px}
.sec-head .sec-sub{font-size:11.5px;color:var(--fg3)}
.sec-ops{background:var(--card);border:1px solid var(--border);border-radius:var(--radius-sm);padding:14px 16px;margin-bottom:16px;display:grid;grid-template-columns:1.1fr 1fr 1fr;gap:18px}
.sec-ops h4{margin:0 0 8px;font-size:10.5px;font-weight:700;letter-spacing:0.8px;text-transform:uppercase;color:var(--fg3)}
.ops-row{display:flex;justify-content:space-between;gap:10px;font-size:12px;padding:3.5px 0;font-family:'JetBrains Mono',monospace;color:var(--fg)}
.ops-row a{color:var(--fg);text-decoration:none}
.ops-row a:hover{text-decoration:underline}
.ops-fresh{display:block;font-size:12px;color:var(--ok);margin-bottom:6px;font-weight:600}
.ops-fresh.stale{color:#f59e0b}
@media(max-width:900px){.sec-ops{grid-template-columns:1fr}}



/* ── D-02: progressive launch disclosure ── */
.launch-collapsed{cursor:pointer}
.launch-collapsed:hover{border-color:var(--border-hover);background:rgba(255,255,255,.02)}
.launch-sum{flex:1;font-size:12px;color:var(--fg2);font-family:'JetBrains Mono',monospace;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.launch-chev{color:var(--fg3);font-size:12px;flex:0 0 auto}
.launch-open{position:relative}
.launch-collapse{margin-left:auto;flex:0 0 auto;background:none;border:1px solid var(--border);border-radius:7px;color:var(--fg3);cursor:pointer;font-size:11px;line-height:1;padding:4px 8px}
.launch-collapse:hover{color:var(--fg);border-color:var(--border-hover)}

/* Local model illustrations and first-run guidance. */
.model-artwork{flex:0 0 40px;width:40px;height:40px;border-radius:12px;align-self:flex-start}
.empty-models{padding:32px;border:1px dashed var(--border);border-radius:16px;background:var(--card);margin:12px 0 24px}
.empty-models strong{font-size:18px;letter-spacing:-.3px}
.empty-models p{color:var(--fg2);max-width:640px;line-height:1.65;margin:10px 0}
.empty-models span{font-size:12px;color:var(--fg3)}

.demo-banner{padding:9px 16px;margin-top:16px;border:1px solid var(--border);border-radius:10px;color:var(--fg3);font-size:12px;text-align:center;background:var(--card)}
