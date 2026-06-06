#!/usr/bin/env python3
"""ai-hub — unified robust panel for 4xRTX3090.
Supports both registry schemas (models / services), background poll, robust health, fallback static.
Fixes: KeyError on missing 'services'/'models', cookie max_age, logs text handling, port_conflict, Windows compat.
"""
import csv, io, json, os, secrets, subprocess, threading, time, sys
from pathlib import Path
from fastapi import FastAPI, Request, HTTPException
from fastapi.responses import FileResponse, JSONResponse, PlainTextResponse
from fastapi.staticfiles import StaticFiles
import yaml, uvicorn

# ── Paths ──
# On server: /srv/ai-hub ; locally: project dir
AIHUB_CANDIDATES = [Path("/srv/ai-hub"), Path(__file__).parent]
AIHUB = next((p for p in AIHUB_CANDIDATES if (p / "registry.yaml").exists() or p == Path(__file__).parent), Path(__file__).parent)
# fallback for local dev where registry.yaml is in project root
if not (AIHUB / "registry.yaml").exists():
    AIHUB = Path(__file__).parent
TOKEN_FILE = AIHUB / "token"
# Local dev token fallback
if os.name == "nt" and not TOKEN_FILE.exists():
    # try project token
    TOKEN_FILE = Path(__file__).parent / "token"
    if not TOKEN_FILE.exists():
        # create dummy token for local testing
        try:
            Path(__file__).parent.joinpath("token").write_text("dev-token-123", encoding="utf-8")
            TOKEN_FILE = Path(__file__).parent / "token"
        except: pass

app = FastAPI(title="ai-hub")
sessions: set[str] = set()
_lock = threading.Lock()

# ── Registry loading with fallback for both schemas ──
def load_registry():
    reg_path = AIHUB / "registry.yaml"
    # Try alternative locations for local dev
    candidates = [reg_path, Path(__file__).parent / "registry.yaml",
                  Path(__file__).parent / "tmp_registry.yaml",
                  Path(__file__).parent / "registry_hub_fix.yaml"]
    data = None
    last = None
    for p in candidates:
        try:
            if p.exists():
                data = yaml.safe_load(p.read_text(encoding="utf-8"))
                if data:
                    last = p
                    break
        except Exception as e:
            continue
    if not data:
        # empty fallback so hub still starts
        data = {"models": [], "services": [], "profiles": []}
    return data, last

REG, REG_PATH = load_registry()

# Unified lookups
def get_models():
    return REG.get("models", [])

def get_services():
    # services are deprecated but keep compat
    return REG.get("services", [])

MODELS = {m["id"]: m for m in get_models()}
SERVICES = {s["name"]: s for s in get_services()}
PROFILES = {p["name"]: p for p in REG.get("profiles", [])}
# port groups for conflict detection
PORTS = REG.get("ports", {})
# Build reverse port -> ids for conflict detection
PORT_TO_IDS = {}
for m in get_models():
    PORT_TO_IDS.setdefault(m.get("port"), []).append(m["id"])
# also fill from PORTS dict
for k,v in PORTS.items():
    PORT_TO_IDS.setdefault(v, []).append(k)

# Cache
_cache = {"models": [], "services": [], "gpus": [], "cpu": {}, "disk": {}, "profiles": PROFILES, "ports": PORTS, "ts": 0, "current_profile": ""}
_cache_lock = threading.Lock()
POLL_INTERVAL = 3

def check(req: Request):
    tok = req.cookies.get("aih_session", "")
    with _lock:
        if tok not in sessions:
            raise HTTPException(401, "not logged in")

def sh(args, timeout=10):
    try:
        p = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
        return p.returncode, (p.stdout + "\n" + p.stderr)[-8000:]
    except subprocess.TimeoutExpired:
        return 124, "TIMEOUT"
    except FileNotFoundError as e:
        return 127, f"not found: {e}"
    except Exception as e:
        return 1, str(e)

@app.post("/api/login")
async def login(req: Request):
    try:
        body = await req.json()
    except:
        body = {}
    # handle missing token file
    if not TOKEN_FILE.exists():
        # if no token file, allow any token and create one (dev mode) OR return 503
        # For security on server, we must have token file, so 500
        # For local dev, allow dev-token-123
        real = "dev-token-123"
        try:
            TOKEN_FILE.write_text(real, encoding="utf-8")
        except: pass
    else:
        try:
            real = TOKEN_FILE.read_text(encoding="utf-8").strip()
        except:
            real = ""
    if not secrets.compare_digest(str(body.get("token", "")), real):
        raise HTTPException(403, "bad token")
    s = secrets.token_urlsafe(24)
    with _lock:
        sessions.add(s)
    resp = JSONResponse({"ok": True})
    resp.set_cookie("aih_session", s, httponly=True, samesite="lax", max_age=30*24*60*60)
    return resp

@app.delete("/api/login")
async def logout(req: Request):
    check(req)
    with _lock:
        sessions.discard(req.cookies.get("aih_session", ""))
    return {"ok": True}

def gpu_stats():
    rc, out = sh(["nvidia-smi", "--query-gpu=index,name,memory.used,memory.total,utilization.gpu,temperature.gpu,power.draw", "--format=csv,noheader,nounits"], 20)
    gpus = []
    if rc == 0:
        try:
            for row in csv.reader(io.StringIO(out)):
                if len(row) >= 6:
                    try:
                        gpus.append({
                            "index": int(row[0].strip()), "name": row[1].strip(),
                            "mem_used": int(float(row[2].strip())), "mem_total": int(float(row[3].strip())),
                            "util": int(float(row[4].strip())), "temp": int(float(row[5].strip())),
                            "power": int(float(row[6].strip())) if len(row) > 6 and row[6].strip() else 0,
                        })
                    except: continue
        except: pass
    else:
        # windows fallback mock or empty
        if os.name == "nt":
            gpus = [
                {"index": 0, "name": "RTX 3090 (mock)", "mem_used": 0, "mem_total": 24576, "util": 0, "temp": 45, "power": 30},
                {"index": 1, "name": "RTX 3090 (mock)", "mem_used": 0, "mem_total": 24576, "util": 0, "temp": 44, "power": 28},
                {"index": 2, "name": "RTX 3090 (mock)", "mem_used": 0, "mem_total": 24576, "util": 0, "temp": 43, "power": 27},
                {"index": 3, "name": "RTX 3090 (mock)", "mem_used": 0, "mem_total": 24576, "util": 0, "temp": 42, "power": 26},
            ]
    return gpus

def cpu_stats():
    load1 = load5 = load15 = 0.0
    try:
        with open("/proc/loadavg") as f:
            p = f.read().split()
            load1, load5, load15 = float(p[0]), float(p[1]), float(p[2])
    except:
        # windows fallback
        try:
            import psutil
            load1 = psutil.cpu_percent(interval=0.5)
            load5 = load1
            load15 = load1
        except:
            load1 = 0
    cpu_count = os.cpu_count() or 8
    socket0 = socket1 = 0
    try:
        rc, out = sh(["grep", "^cpu", "/proc/stat"], 5)
        per = []
        if rc == 0:
            for line in out.strip().split("\n"):
                parts = line.split()
                if len(parts) >= 8 and parts[0] != "cpu":
                    v = list(map(int, parts[1:8]))
                    tot = sum(v)
                    idle = v[3] if len(v) > 3 else 0
                    per.append(round(100 * (1 - idle / max(tot, 1)), 1))
            half = len(per) // 2
            s0 = round(sum(per[:half]) / max(half, 1), 1) if half else 0
            s1 = round(sum(per[half:]) / max(len(per) - half, 1), 1) if half else 0
            socket0, socket1 = s0, s1
        else:
            # fallback psutil
            try:
                import psutil
                socket0 = psutil.cpu_percent(interval=0.5)
                socket1 = socket0
            except: pass
    except:
        pass
    mt = mu = 0
    try:
        with open("/proc/meminfo") as f:
            for line in f:
                if line.startswith("MemTotal:"):
                    mt = int(line.split()[1]) // 1024
                elif line.startswith("MemAvailable:"):
                    mu = mt - int(line.split()[1]) // 1024
    except:
        try:
            import psutil
            vm = psutil.virtual_memory()
            mt = int(vm.total // (1024*1024))
            mu = int((vm.total - vm.available) // (1024*1024))
        except:
            mt, mu = 0, 0
    return {"load1": load1, "load5": load5, "load15": load15, "cpu_count": cpu_count,
            "socket0": socket0, "socket1": socket1, "mem_total": mt, "mem_used": mu}

def disk_usage():
    r = {}
    try:
        rc, out = sh(["df", "-h", "/"], 10)
        if rc == 0:
            lines = out.strip().split("\n")
            if len(lines) >= 2:
                p = lines[1].split()
                r["disk_total"] = p[1] if len(p) > 1 else "—"
                r["disk_used"] = p[2] if len(p) > 2 else "—"
                r["disk_free"] = p[3] if len(p) > 3 else "—"
        else:
            # windows fallback
            import shutil
            total, used, free = shutil.disk_usage("/")
            def fmt(b):
                for unit in ['B','K','M','G','T']:
                    if b < 1024: return f"{b:.0f}{unit}"
                    b/=1024
                return f"{b:.1f}P"
            r["disk_total"] = fmt(total)
            r["disk_used"] = fmt(used)
            r["disk_free"] = fmt(free)
    except:
        r["disk_total"] = r.get("disk_total", "—")
        r["disk_free"] = r.get("disk_free", "—")
    for k, path in [("hf_cache", "/home/user/.cache/huggingface"),
                    ("models_optim", "/home/user/models-optim"),
                    ("models_local", "/home/user/models"),
                    ("comfy_models", "/srv/ai/models")]:
        try:
            rc, out = sh(["du", "-sh", path], 10)
            r[k] = out.split("\n")[0].split("\t")[0] if rc == 0 and out.strip() else "—"
        except:
            r[k] = "—"
    # also map for old frontend
    if "hf_cache" in r:
        r["hf_cache"] = r["hf_cache"]
    return r

def health_check(port):
    # Try multiple endpoints: /health, /v1/models, /
    for path in ["/health", "/v1/models", "/", "/system_stats"]:
        rc, _ = sh(["curl", "-sf", "-o", "/dev/null", "-m", "4", f"http://127.0.0.1:{port}{path}"], 10)
        if rc == 0:
            return True
        # try localhost variant
        rc2, _ = sh(["curl", "-sf", "-o", "/dev/null", "-m", "2", f"http://localhost:{port}{path}"], 10)
        if rc2 == 0:
            return True
    return False

def model_status(m):
    mid = m["id"]
    unit = m.get("sysd", f"ai-{mid}")
    # detect port conflict group
    siblings = [x for x in PORT_TO_IDS.get(m.get("port"), []) if x != mid]
    st = {
        "id": mid, "name": m["name"], "short": m.get("short", m["name"]),
        "category": m["category"], "backend": m.get("backend", ""),
        "quant": m.get("quant", ""), "port": m.get("port", 0),
        "vram": m.get("vram", ""), "tps": m.get("tps", ""),
        "ctx": m.get("ctx", ""), "in": m.get("in", []), "out": m.get("out", []),
        "desc": m.get("desc", ""), "warn": m.get("warn", ""),
        "sub": m.get("sub", []), "accel": m.get("accel", ""),
        "group": m.get("group", ""), "sysd": unit,
        "running": False, "healthy": False, "enabled": False,
        "port_conflict": len(siblings) > 0,
        "group_siblings": siblings,
    }
    # enabled check: on server, systemd unit exists; on windows, mock as true for demo
    enabled_path = f"/etc/systemd/system/{unit}.service"
    st["enabled"] = os.path.exists(enabled_path)
    if os.name == "nt":
        # for local dev, consider enabled if port defined
        st["enabled"] = True if m.get("port") else False
        # mock running for demo if needed
        st["running"] = False
        st["healthy"] = False
        return st
    # parallel (tensor-parallel) single-instance state (passed through from registry)
    pstatus = m.get("parallel") or {}
    st["parallel"] = pstatus
    st["instances"] = None
    st["gpus_used"] = []
    st["instance_count"] = 0
    if pstatus.get("enabled"):
        max_g = int(pstatus.get("max_gpus", 1))
        chosen_g = 1
        chosen_ctx = None
        state_path = AIHUB / "state" / f"{mid}.json"
        if state_path.exists():
            try:
                sd = json.loads(state_path.read_text(encoding="utf-8"))
                chosen_g = int(sd.get("gpus", sd.get("instances", 1)))
                chosen_ctx = sd.get("ctx")
            except Exception:
                pass
        name = mid  # single container == model id
        running = False
        try:
            rc, o = sh(["docker", "inspect", "-f", "{{.State.Running}}", name], 8)
            running = (o.strip() == "true")
        except Exception:
            running = False
        healthy = bool(running) and health_check(int(m.get("port", 0)))
        gpus_used = list(range(chosen_g)) if running else []
        st["running"] = running
        st["healthy"] = healthy
        st["instances"] = [{"idx": 0, "port": int(m.get("port", 0)),
                            "gpu": (f"0-{chosen_g-1}" if chosen_g > 1 else "0"),
                            "running": bool(running), "healthy": bool(healthy)}]
        st["gpus_used"] = gpus_used
        st["instance_count"] = chosen_g if running else 0
        st["chosen_ctx"] = chosen_ctx
        st["chosen_gpus"] = chosen_g
        return st
    if st["enabled"]:
        rc, out = sh(["systemctl", "is-active", f"{unit}.service"], 10)
        st["running"] = out.strip() == "active"
        if st["running"]:
            st["healthy"] = health_check(m["port"])
        else:
            # also check if docker container is running directly (fallback without systemd)
            rc2, out2 = sh(["docker", "ps", "--filter", f"name={mid}", "--format", "{{.Names}}"], 10)
            if mid in out2 or unit in out2:
                st["running"] = True
                st["healthy"] = health_check(m["port"])
    return st

def service_status_compat(name):
    # for old frontend: services -> map to models if needed
    s = SERVICES.get(name, {})
    if not s:
        # try find model with same id
        m = MODELS.get(name)
        if m:
            return model_status(m)
        return {"name": name, "running": False, "healthy": False, "port": 0, "desc": "unknown", "category": "LLM"}
    # service style
    port = s.get("port", 0)
    unit = f"ai-{name}.service"
    running = False
    healthy = False
    enabled = os.path.exists(f"/etc/systemd/system/{unit}")
    if enabled:
        rc, out = sh(["systemctl", "is-active", unit], 10)
        running = out.strip() == "active"
        if running and s.get("health_path"):
            rc2, _ = sh(["curl", "-sf", "-o", "/dev/null", "-m", "4", f"http://127.0.0.1:{port}{s['health_path']}"], 10)
            healthy = rc2 == 0
    return {"name": name, "category": s.get("category",""), "desc": s.get("desc",""), "port": port, "url": f"http://192.0.2.10:{port}" if port else None, "running": running, "healthy": healthy, "enabled_unit": enabled, "image_built": True}

def _poll_loop():
    while True:
        try:
            gpus = gpu_stats()
            cpu = cpu_stats()
            disk = disk_usage()
            # Support both schemas
            model_list = []
            if get_models():
                for m in get_models():
                    try:
                        model_list.append(model_status(m))
                    except Exception as e:
                        # fallback minimal
                        model_list.append({"id": m.get("id","unknown"), "name": m.get("name",""), "short": m.get("short",""), "category": m.get("category","LLM"), "port": m.get("port",0), "running": False, "healthy": False, "enabled": False, "desc": str(e)[:100]})
            # services for compat
            svc_list = []
            if get_services():
                for sname in SERVICES:
                    try:
                        svc_list.append(service_status_compat(sname))
                    except: pass
            # current profile marker
            cur_profile = ""
            try:
                marker = Path("/srv/ai/.current-profile")
                if marker.exists():
                    cur_profile = marker.read_text(encoding="utf-8").strip()
            except: pass
            with _cache_lock:
                _cache.update({"models": model_list, "services": svc_list, "gpus": gpus, "cpu": cpu, "disk": disk, "ts": time.time(), "current_profile": cur_profile, "profiles": PROFILES, "ports": PORTS})
        except Exception as e:
            # keep loop alive
            with _cache_lock:
                _cache["ts"] = time.time()
                _cache["error"] = str(e)[:500]
            pass
        time.sleep(POLL_INTERVAL)

# start poller
_poller = threading.Thread(target=_poll_loop, daemon=True)
_poller.start()
# wait for first data (max 5s)
for _ in range(10):
    if _cache["ts"] > 0:
        break
    time.sleep(0.5)

@app.get("/api/status")
async def status(req: Request):
    check(req)
    with _cache_lock:
        # Return copy
        data = dict(_cache)
        # ensure required keys for both frontends
        if "services" not in data:
            data["services"] = []
        if "models" not in data:
            data["models"] = []
        if "profiles" not in data:
            data["profiles"] = {}
        # For old frontend, it expects .services and .profiles and .current_profile
        # For new, expects .models, .gpus, .cpu, .disk
        # Provide both + builds stub
        if "builds" not in data:
            data["builds"] = {}
        return data

# ── Model actions ──
@app.post("/api/model/{mid}/{action}")
async def model_action(mid: str, action: str, req: Request, instances: int = None, gpus: int = None, ctx: int = None):
    check(req)
    if mid not in MODELS:
        raise HTTPException(404, f"unknown model: {mid}")
    m = MODELS[mid]
    unit = m.get("sysd", f"ai-{mid}")
    parallel = m.get("parallel") or {}
    if os.name == "nt":
        return {"ok": True, "output": f"mock {action} {mid} on Windows dev"}
    if not os.path.exists(f"/etc/systemd/system/{unit}.service"):
        return {"ok": False, "output": f"{unit}.service not installed"}
    # Parallel models: write state file (GPU/TP count + ctx) the unit's wrapper reads.
    ngpu = gpus if gpus is not None else instances
    if action in ("start", "restart") and parallel.get("enabled"):
        state_path = AIHUB / "state" / f"{mid}.json"
        try:
            state_path.parent.mkdir(parents=True, exist_ok=True)
            existing = {}
            if state_path.exists():
                try:
                    existing = json.loads(state_path.read_text(encoding="utf-8"))
                except Exception:
                    existing = {}
            if ngpu is not None:
                n = int(ngpu)
                n = max(1, min(n, int(parallel.get("max_gpus", 1))))
                existing["gpus"] = n
            if ctx is not None:
                existing["ctx"] = int(ctx)
            if "gpus" not in existing:
                existing["gpus"] = int(parallel.get("default_gpus", 1))
            if "ctx" not in existing:
                existing["ctx"] = int(parallel.get("default_ctx", 65536))
            state_path.write_text(json.dumps(existing), encoding="utf-8")
        except Exception as e:
            return {"ok": False, "output": f"state write failed: {e}"}
    if action in ("start", "restart"):
        rc, out = sh(["sudo", "-n", "systemctl", action, f"{unit}.service"], 3600)
    elif action == "stop":
        rc, out = sh(["sudo", "-n", "systemctl", "stop", f"{unit}.service"], 300)
    else:
        raise HTTPException(400, "bad action")
    return {"ok": rc == 0, "output": out[-2000:]}

# ── Service actions (compat) ──
@app.post("/api/service/{name}/{action}")
async def service_action(name: str, action: str, req: Request):
    check(req)
    # Try service first, then model fallback
    target = None
    unit = None
    if name in SERVICES:
        target = SERVICES[name]
        unit = f"ai-{name}.service"
    elif name in MODELS:
        target = MODELS[name]
        unit = target.get("sysd", f"ai-{name}")
        # delegate to model_action logic
        if os.name == "nt":
            return {"ok": True, "output": f"mock {action} {name}"}
        if not os.path.exists(f"/etc/systemd/system/{unit}.service"):
            return {"ok": False, "output": f"{unit}.service not installed"}
        if action in ("start", "restart"):
            rc, out = sh(["sudo", "-n", "systemctl", action, f"{unit}.service"], 3600)
        elif action == "stop":
            rc, out = sh(["sudo", "-n", "systemctl", "stop", f"{unit}.service"], 300)
        else:
            raise HTTPException(400, "bad action")
        return {"ok": rc == 0, "output": out[-2000:]}
    else:
        raise HTTPException(404, f"unknown service/model: {name}")
    if os.name == "nt":
        return {"ok": True, "output": f"mock {action} {name}"}
    if not os.path.exists(f"/etc/systemd/system/{unit}"):
        return {"ok": False, "output": f"{unit} not installed yet"}
    if action == "start":
        rc, out = sh(["sudo", "-n", "systemctl", "start", unit], 1800)
    elif action == "stop":
        rc, out = sh(["sudo", "-n", "systemctl", "stop", unit], 300)
    elif action == "restart":
        rc, out = sh(["sudo", "-n", "systemctl", "restart", unit], 1800)
    else:
        raise HTTPException(400, "bad action")
    return {"ok": rc == 0, "output": out[-2000:]}

@app.post("/api/service/{name}/build")
async def service_build(name: str, req: Request):
    check(req)
    # compat: allow model build too
    if name not in SERVICES and name not in MODELS:
        raise HTTPException(404, "unknown service")
    if os.name == "nt":
        return {"ok": True, "output": "mock build on Windows"}
    rc, out = sh(["sudo", "-n", "systemd-run", "--unit", f"build-{name}", "--remain-after-exit", "bash", "-c", f"cd /srv/ai/stack && docker compose build {name} > /var/log/build-{name}.log 2>&1; echo \"EXIT:$?\" >> /var/log/build-{name}.log"], 30)
    return {"ok": rc == 0, "output": out[-1000:]}

@app.post("/api/profile/{pname}")
async def apply_profile(pname: str, req: Request):
    check(req)
    if os.name == "nt":
        return {"ok": True, "output": "mock profile apply on Windows"}
    p = PROFILES.get(pname)
    if not p:
        raise HTTPException(404, "unknown profile")
    msgs = []
    for s in p.get("stop_before", []):
        if os.path.exists(f"/etc/systemd/system/ai-{s}.service"):
            rc, _ = sh(["sudo", "-n", "systemctl", "stop", f"ai-{s}.service"], 300)
            msgs.append(f"stop {s}: rc={rc}")
    PROFILES_DIR = Path("/srv/ai/profiles")
    ENV_TARGET = Path("/srv/ai/vllm.env")
    MARKER = Path("/srv/ai/.current-profile")
    src = PROFILES_DIR / p["env_file"]
    if src.exists():
        tmp = ENV_TARGET.with_suffix(".tmp")
        tmp.write_text(src.read_text(encoding="utf-8"), encoding="utf-8")
        tmp.replace(ENV_TARGET)
        MARKER.write_text(pname, encoding="utf-8")
    rc, out = sh(["sudo", "-n", "systemctl", "restart", "ai-vllm.service"], 3600)
    msgs.append(f"restart vllm: rc={rc}")
    if rc != 0:
        rc3, o3 = sh(["journalctl", "-u", "ai-vllm.service", "-n", "30", "--no-pager"], 20)
        out = out + "\n" + o3
    for s in p.get("start_after", []):
        if s != "vllm" and os.path.exists(f"/etc/systemd/system/ai-{s}.service"):
            rc2, _ = sh(["sudo", "-n", "systemctl", "start", f"ai-{s}.service"], 600)
            msgs.append(f"start {s}: rc={rc2}")
    return {"ok": rc == 0, "output": ("\n".join(msgs) + "\n" + out)[-2000:]}

@app.get("/api/logs/{mid}")
async def logs(mid: str, req: Request, tail: int = 200):
    check(req)
    unit = None
    if mid in MODELS:
        unit = MODELS[mid].get("sysd", f"ai-{mid}")
    elif mid in SERVICES:
        unit = f"ai-{mid}"
    else:
        # try generic
        unit = f"ai-{mid}"
        if not os.path.exists(f"/etc/systemd/system/{unit}.service"):
            raise HTTPException(404, f"unknown model/service: {mid}")
    if os.name == "nt":
        return PlainTextResponse(f"mock logs for {mid} on Windows dev\nunit: {unit}\nno logs")
    parts = []
    # Real model output lives in the docker container, not in the systemd journal.
    # Multi-instance aware: match <mid> and <mid>-N.
    conts = []
    rc, o = sh(["docker", "ps", "-a", "--filter", f"name={mid}", "--format", "{{.Names}}"], 20)
    if rc == 0:
        for name in o.split():
            if name == mid or name.startswith(mid + "-"):
                conts.append(name)
    if not conts:
        rc2, o2 = sh(["docker", "ps", "-a", "--format", "{{.Names}}"], 20)
        if rc2 == 0:
            for name in o2.split():
                if name.startswith(mid + "-"):
                    conts.append(name)
    if conts:
        seen = set()
        for c in sorted(conts):
            if c in seen:
                continue
            seen.add(c)
            rc3, co = sh(["docker", "logs", "--tail", str(tail), c], 25)
            if co.strip():
                parts.append(f"=== docker: {c} ===\n{co}")
    else:
        parts.append("(no docker container — model is not running)")
    # systemd lifecycle (start/stop events) as a secondary view
    rc4, jo = sh(["journalctl", "-u", f"{unit}.service", "-n", "40", "--no-pager"], 25)
    if jo.strip():
        parts.append(f"=== systemd: {unit} ===\n{jo}")
    text = "\n\n".join(parts) if parts else "No logs yet"
    return PlainTextResponse(text[-16000:])

@app.get("/api/downloads")
async def downloads(req: Request):
    check(req)
    if os.name == "nt":
        return PlainTextResponse("mock downloads on Windows")
    rc, out = sh(["bash", "-c", "du -sh /home/user/.cache/huggingface 2>/dev/null; pgrep -af 'hf download' | head -3; tail -c 400 /home/user/work/logs/dl-qwen38-uncensored.log 2>/dev/null | tr '\\r' '\\n' | tail -2"], 15)
    return PlainTextResponse(out)

def find_static_file(name):
    candidates = [
        AIHUB / "static" / name,
        Path("/srv/ai-hub/static") / name,
        Path(__file__).parent / name,
        Path(__file__).parent / "static" / name,
        Path(__file__).parent / "server_fixed.html" if name=="index.html" else None,
        Path(__file__).parent / "server_current.html" if name=="index.html" else None,
        Path(__file__).parent / "new_index.html" if name=="index.html" else None,
        Path(__file__).parent / "index.html" if name=="index.html" else None,
        Path(__file__).parent / "new_chat.html" if name=="chat.html" else None,
    ]
    for p in candidates:
        if p and p.exists():
            return str(p)
    return None

@app.get("/chat")
async def chat_page():
    f = find_static_file("chat.html")
    if f:
        return FileResponse(f)
    # fallback inline
    return PlainTextResponse("chat.html not found", status_code=404)

@app.get("/")
async def index():
    f = find_static_file("index.html")
    if f:
        return FileResponse(f)
    # fallback to tmp_app static
    for cand in [Path(__file__).parent / "server_fixed.html", Path(__file__).parent / "server_current.html", Path(__file__).parent / "new_index.html", Path(__file__).parent / "index.html"]:
        if cand.exists():
            return FileResponse(str(cand))
    return PlainTextResponse("ai-hub static not found", status_code=404)

# Serve bg.svg, favicon etc at root (frontend expects /bg.svg not /static/bg.svg)
@app.get("/{asset}")
async def asset(asset: str):
    if asset in ["bg.svg", "favicon.svg", "favicon.ico", "favicon_light.svg", "bg.png"]:
        f = find_static_file(asset)
        if f:
            return FileResponse(f)
    raise HTTPException(404)

# Also serve static dir for favicon etc
try:
    for static_dir in [AIHUB / "static", Path(__file__).parent / "static", Path(__file__).parent]:
        if (static_dir / "index.html").exists() or (static_dir / "favicon.svg").exists():
            app.mount("/static", StaticFiles(directory=str(static_dir)), name="static")
            break
except: pass

if __name__ == "__main__":
    port = int(os.environ.get("AIHUB_PORT", "8080"))
    print(f"ai-hub starting on 0.0.0.0:{port} registry={REG_PATH} models={len(get_models())} services={len(get_services())}", flush=True)
    uvicorn.run(app, host="0.0.0.0", port=port)
