#!/usr/bin/env python
"""
T8 — Parity proof: run the Python app.py and the Go aihub side by side on the
same temporary installation and compare their shared HTTP API contract.
Go-only extensions are checked separately from the historical Python baseline.

Volatile fields (GPU counters, disk free, ts) are compared with tolerance /
structure rather than byte-equality.

Exit 0 = shared contract verified; exit 1 = mismatch.
"""
import json
import os
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

# repo root = two levels up from this file (tests/parity/run_parity.py)
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PY_PORT = 18091
GO_PORT = 18092
PY_BASE = f"http://127.0.0.1:{PY_PORT}"
GO_BASE = f"http://127.0.0.1:{GO_PORT}"
TOKEN = secrets.token_urlsafe(24)
FIXTURE = tempfile.TemporaryDirectory(prefix="ai-hub-parity-")
RUNTIME_ROOT = FIXTURE.name
# Keep credentials and the historical Python reference out of the real checkout.
with open(os.path.join(ROOT, "_wip", "app.py"), encoding="utf-8") as file:
    reference = file.read()
# Redirect only the reference's installation lookup into the disposable fixture.
reference = reference.replace('AIHUB_CANDIDATES = [Path("/srv/ai-hub"), Path(__file__).parent]',
                              'AIHUB_CANDIDATES = [Path(__file__).parent]')
with open(os.path.join(RUNTIME_ROOT, "app.py"), "w", encoding="utf-8") as file:
    file.write(reference)
# A nonexistent service/model avoids dependence on installed weights and never
# touches real model units. Real-registry behavior is covered by Go tests.
with open(os.path.join(RUNTIME_ROOT, "registry.yaml"), "w", encoding="utf-8") as file:
    json.dump({"models": [{"id": "parity-fixture", "name": "Parity fixture", "short": "Fixture",
        "category": "LLM", "backend": "llama.cpp", "port": 18998, "sysd": "ai-parity-fixture",
        "in": ["text"], "out": ["text"], "parallel": {"enabled": False}}],
        "ports": {"parity-fixture": 18998}, "profiles": []}, file)
shutil.copytree(os.path.join(ROOT, "static"), os.path.join(RUNTIME_ROOT, "static"))
shutil.copyfile(os.path.join(ROOT, "favicon.svg"), os.path.join(RUNTIME_ROOT, "static", "favicon.svg"))
with open(os.path.join(RUNTIME_ROOT, "token"), "w", encoding="utf-8") as file:
    file.write(TOKEN)
LOGDIR = os.path.join(tempfile.gettempdir(), "opencode")
os.makedirs(LOGDIR, exist_ok=True)
PY_LOG = os.path.join(LOGDIR, "parity_py.log")
GO_LOG = os.path.join(LOGDIR, "parity_go.log")

RESULTS = []
PROCS = []
CONN_ERRORS = {}  # base url -> list of connection error strings


def check(name, cond, extra=""):
    tag = "PASS" if cond else "FAIL"
    print(f"{tag} {name}" + (f"  {extra}" if extra else ""))
    RESULTS.append((name, cond))
    return cond


def http(method, url, body=None, ctype="application/json", cookie=None, raw=False):
    headers = {}
    if ctype:
        headers["Content-Type"] = ctype
    if cookie:
        headers["Cookie"] = cookie
    data = None
    if body is not None:
        data = body.encode() if isinstance(body, str) else body
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            content = r.read()
            setcookie = r.headers.get("Set-Cookie", "")
            return r.status, content, setcookie
    except urllib.error.HTTPError as e:
        return e.code, e.read(), e.headers.get("Set-Cookie", "")
    except OSError as e:
        from urllib.parse import urlsplit
        base = f"{urlsplit(url).scheme}://{urlsplit(url).netloc}"
        CONN_ERRORS.setdefault(base, []).append(f"{method} {url}: {e!r}")
        return 0, b"", ""


def get_json(method, url, **kw):
    code, content, _ = http(method, url, **kw)
    try:
        return code, json.loads(content.decode() or "null")
    except Exception:
        return code, content.decode(errors="replace")


def get_text(method, url, **kw):
    code, content, _ = http(method, url, **kw)
    return code, content.decode(errors="replace")


def login(base):
    code, content, setcookie = http("POST", base + "/api/login", body=json.dumps({"token": TOKEN}))
    m = re.search(r"aih_session=([^;]+)", setcookie or "")
    cookie = f"aih_session={m.group(1)}" if m else None
    return code, json.loads(content.decode() or "{}"), cookie, setcookie


def wait_ready(url, timeout=40):
    deadline = time.time() + timeout
    last = ""
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=3) as r:
                if r.status == 200:
                    return True
        except Exception as e:
            last = str(e)
        time.sleep(0.5)
    print(f"  (wait_ready timeout for {url}: {last})")
    return False


def start_python():
    env = dict(os.environ)
    env["AIHUB_PORT"] = str(PY_PORT)
    env["AIHUB_ENV_FILE"] = "-"
    logf = open(PY_LOG, "w", encoding="utf-8")
    p = subprocess.Popen(
        [sys.executable, "app.py"],
        cwd=RUNTIME_ROOT, env=env,
        stdout=logf, stderr=subprocess.STDOUT,
        creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
    )
    p._logf = logf
    p._log = PY_LOG
    PROCS.append(p)
    return p


def build_go():
    exe = os.path.join(RUNTIME_ROOT, "parity-aihub.exe")
    r = subprocess.run(["go", "build", "-o", exe, "./cmd/aihub"], cwd=ROOT,
                       capture_output=True, text=True,
                       creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
    if r.returncode != 0:
        print("GO BUILD FAILED:\n", r.stdout, r.stderr)
        sys.exit(1)
    return exe


def start_go(exe):
    env = dict(os.environ)
    env["AIHUB_PORT"] = str(GO_PORT)
    env["AIHUB"] = RUNTIME_ROOT
    env["AIHUB_ENV_FILE"] = "-"
    env["AIHUB_TOKEN"] = TOKEN
    env["AIHUB_TOKEN_FILE"] = os.path.join(RUNTIME_ROOT, "token")
    logf = open(GO_LOG, "w", encoding="utf-8")
    p = subprocess.Popen([exe], cwd=ROOT, env=env,
                         stdout=logf, stderr=subprocess.STDOUT,
                         creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
    p._logf = logf
    p._log = GO_LOG
    PROCS.append(p)
    return p


def log_tail(path, n=25):
    try:
        lines = open(path, encoding="utf-8", errors="replace").read().splitlines()
        return "\n".join(lines[-n:])
    except Exception as e:
        return f"(cannot read {path}: {e!r})"


def human_size(s):
    return bool(re.fullmatch(r"\d+[BKMGTP]|\d+\.\dP", s or ""))


# The deployment target is Linux (both servers read /proc). On the Windows dev
# box Go has no /proc (cpu fields -> 0) while app.py falls back to psutil (real
# values), so the live cpu values are platform-implementation-specific there.
IS_LINUX = os.name == "posix" and os.path.exists("/proc/loadavg")


def close(a, b, tol, rel=0.5):
    """Tolerant numeric compare: pass if |a-b| <= max(tol, rel*max(|a|,|b|)).

    Both servers read /proc at different instants (Go poller 1s, app.py 3s),
    so live values (load, per-core %, mem_used) legitimately differ. This is
    the same treatment gpus[].volatile gets, applied to the cpu dict.
    """
    a, b = float(a or 0), float(b or 0)
    return abs(a - b) <= max(tol, rel * max(abs(a), abs(b)))


def cpu_equal(pc, gc):
    """Compare the cpu dict.

    Both sides must expose the same keys and the same cpu_count (platform-stable:
    Go runtime.NumCPU() == Python os.cpu_count()). On Linux the live fields
    (load*, socket*, mem_*) are compared with tolerance (both read /proc, just at
    different instants). On Windows the live fields are implementation-specific
    (Go: no /proc -> 0; app.py: psutil -> real), so only the stable fields are
    asserted there.
    """
    pc, gc = pc or {}, gc or {}
    if set(pc) != set(gc):
        return False
    if pc.get("cpu_count") != gc.get("cpu_count"):
        return False
    if not IS_LINUX:
        return True
    if pc.get("mem_total") != gc.get("mem_total"):
        return False
    for k in ("load1", "load5", "load15"):
        if not close(pc.get(k), gc.get(k), tol=5.0):
            return False
    for k in ("socket0", "socket1"):
        if not close(pc.get(k), gc.get(k), tol=30.0):
            return False
    # mem_used: 2GB floor + 10% relative — wide enough for the 1s/3s poll-gap
    # drift under LLM load, tight enough to catch a 0-vs-GB or unit bug.
    return close(pc.get("mem_used"), gc.get("mem_used"), tol=2048.0, rel=0.10)


def compare_status(py, go):
    # Go-only model extensions (see models[i].fields): strip from the raw
    # list-equality check too, so it stays consistent with the detailed check.
    GO_ONLY = {"tps_live", "prompt_tps_live", "ctx_used_live", "parallel", "hf", "uncensored", "instances", "instance_count", "gpus_used"}
    def strip_models(lst):
        return [{k: v for k, v in m.items() if k not in GO_ONLY} for m in (lst or [])]
    for k in ("models", "services", "cpu", "profiles", "ports", "current_profile", "builds"):
        pv, gv = py.get(k), go.get(k)
        if k == "models":
            pv, gv = strip_models(pv), strip_models(gv)
        if k == "cpu":
            # live fields (load*, socket*, mem_used) drift between the two
            # servers' cached reads -> tolerant compare, not exact equality.
            check(f"status.{k}", cpu_equal(pv, gv),
                  f"py={json.dumps(pv, sort_keys=True)[:100]} go={json.dumps(gv, sort_keys=True)[:100]}")
            continue
        check(f"status.{k}", pv == gv,
              f"py={json.dumps(pv, sort_keys=True)[:100]} go={json.dumps(gv, sort_keys=True)[:100]}")
    # ts: both float epochs
    check("status.ts", isinstance(py.get("ts"), (int, float)) and isinstance(go.get("ts"), (int, float))
          and py["ts"] > 1_700_000_000 and go["ts"] > 1_700_000_000,
          f"py={py.get('ts')} go={go.get('ts')}")
    # gpus: structure + tolerant counters
    pg, gg = py.get("gpus") or [], go.get("gpus") or []
    check("status.gpus.len", len(pg) == len(gg), f"py={len(pg)} go={len(gg)}")
    for i, (a, b) in enumerate(zip(pg, gg)):
        check(f"gpus[{i}].stable", a.get("index") == b.get("index") and a.get("name") == b.get("name")
              and a.get("mem_total") == b.get("mem_total"),
              f"py={a} go={b}")
        check(f"gpus[{i}].volatile", abs(a.get("mem_used", 0) - b.get("mem_used", 0)) <= 1024
              and abs(a.get("temp", 0) - b.get("temp", 0)) <= 5
              and abs(a.get("power", 0) - b.get("power", 0)) <= 30,
              f"py={a} go={b}")
    # disk
    pd, gd = py.get("disk") or {}, go.get("disk") or {}
    check("disk.keys", set(pd) == set(gd), f"py={sorted(pd)} go={sorted(gd)}")
    check("disk.total", pd.get("disk_total") == gd.get("disk_total"),
          f"py={pd.get('disk_total')} go={gd.get('disk_total')}")
    for k in ("disk_used", "disk_free"):
        same = pd.get(k) == gd.get(k)
        check(f"disk.{k}", same or (human_size(pd.get(k)) and human_size(gd.get(k))),
              f"py={pd.get(k)} go={gd.get(k)}")
    for k in ("hf_cache", "models_optim", "models_local", "comfy_models"):
        check(f"disk.{k}", pd.get(k) == gd.get(k), f"py={pd.get(k)} go={gd.get(k)}")
    # models detail: full field parity (running=false deterministic on this box)
    pm, gm = py.get("models") or [], go.get("models") or []
    check("models.len", len(pm) == len(gm), f"py={len(pm)} go={len(gm)}")
    # Go-only extensions (absent on the Python ref): strip before exact compare.
    # parallel/hf: launch-config + HF source URL — needed by the Go UI, added
    # after app.py; Go is a strict superset so we drop them before comparing.
    GO_ONLY = {"tps_live", "prompt_tps_live", "ctx_used_live", "parallel", "hf", "uncensored", "instances", "instance_count", "gpus_used"}
    for i, (a, b) in enumerate(zip(pm, gm)):
        a = {k: v for k, v in a.items() if k not in GO_ONLY}
        b = {k: v for k, v in b.items() if k not in GO_ONLY}
        check(f"models[{i}].fields", a == b,
              f"py={json.dumps(a, sort_keys=True)[:140]} go={json.dumps(b, sort_keys=True)[:140]}")


def main():
    print("== T8 parity: python app.py (:{} ) vs go aihub (:{}) ==\n".format(PY_PORT, GO_PORT))
    exe = build_go()
    start_python()
    start_go(exe)
    try:
        print("waiting for python ...", end=" ", flush=True)
        py_ok = wait_ready(PY_BASE + "/")
        print("ok" if py_ok else "FAIL")
        print("waiting for go ...", end=" ", flush=True)
        go_ok = wait_ready(GO_BASE + "/healthz")
        print("ok" if go_ok else "FAIL")
        if not (py_ok and go_ok):
            check("both-servers-up", False)
            return
        # extra settle time for python poller first data
        time.sleep(4)

        # ---- login flow parity ----
        for name, base in (("py", PY_BASE), ("go", GO_BASE)):
            code, body, cookie, setc = login(base)
            check(f"{name}.login", code == 200 and body == {"ok": True}, f"{code} {body}")
            check(f"{name}.login.cookie32", bool(re.search(r"aih_session=[A-Za-z0-9_-]{32}", setc or "")), setc[:60])
            globals()[name + "_cookie"] = f"aih_session={re.search(r'aih_session=([^;]+)', setc).group(1)}"
        # bad token
        for name, base in (("py", PY_BASE), ("go", GO_BASE)):
            code, content, _ = http("POST", base + "/api/login", body=json.dumps({"token": "wrong"}))
            check(f"{name}.login-bad", code == 403 and json.loads(content.decode()) == {"detail": "bad token"}, f"{code}")
        # status no auth
        for name, base, ck in (("py", PY_BASE, "aih_session=none"), ("go", GO_BASE, "aih_session=none")):
            code, body = get_json("GET", base + "/api/status", cookie=ck)
            check(f"{name}.status-noauth", code == 401 and body == {"detail": "not logged in"}, f"{code} {body}")

        # ---- /api/status parity ----
        pyc = globals()["py_cookie"]; goc = globals()["go_cookie"]
        code_p, st_p = get_json("GET", PY_BASE + "/api/status", cookie=pyc)
        code_g, st_g = get_json("GET", GO_BASE + "/api/status", cookie=goc)
        check("go.status-freshness", isinstance(st_g.get("stale"), bool) and isinstance(st_g.get("age_s"), (int,float)))
        check("status.codes", code_p == 200 and code_g == 200, f"py={code_p} go={code_g}")
        check("status.same-keyset", set(st_p) == (set(st_g) - {"age_s", "stale"}), f"py={sorted(st_p)} go={sorted(st_g)}")
        compare_status(st_p, st_g)

        # Historical lifecycle replies are Windows-only mocks. On Linux the
        # Go unit/E2E tests inject command runners; parity must not operate units.
        if os.name == "nt":
            # ---- model endpoints ----
            # (use an existing registry model; qwen-fp8-vllm was removed 2026-09-01)
            mid = "parity-fixture"
            for name, base, ck in (("py", PY_BASE, pyc), ("go", GO_BASE, goc)):
                for act in ("start", "stop", "restart"):
                    code, body = get_json("POST", f"{base}/api/model/{mid}/{act}", cookie=ck, body="{}")
                    exp = {"ok": True, "output": f"mock {act} {mid} on Windows dev"}
                    check(f"{name}.model-{act}", code == 200 and body == exp, f"{code} {body}")
                code, body = get_json("POST", f"{base}/api/model/{mid}/flush", cookie=ck, body="{}")
                # windows: mock before action validation (parity!)
                check(f"{name}.model-badaction", code == 200 and body == {"ok": True, "output": f"mock flush {mid} on Windows dev"}, f"{code} {body}")
                code, body = get_json("POST", f"{base}/api/model/nope/start", cookie=ck, body="{}")
                check(f"{name}.model-unknown", code == 404 and body == {"detail": "unknown model: nope"}, f"{code} {body}")
                # stats
                # interval reflects each server's actual poll rate (app.py 3s, Go 1s
                # for live-TPS responsiveness) — accept either, not an exact match.
                code, body = get_json("GET", f"{base}/api/model/{mid}/stats", cookie=ck)
                if name == "py":
                    # The archived Python reference predates live statistics.
                    check("py.model-stats-absent", code == 405, f"{code}")
                else:
                    check("go.model-stats", code == 200 and body["id"] == mid and body["live"] is None
                          and body["history"] == [] and body["interval"] == 1, f"{code}")
                code, body = get_json("GET", f"{base}/api/model/nope/stats", cookie=ck)
                check(f"{name}.model-stats-unknown", (code == 405 if name == "py" else
                      code == 404 and body == {"detail": "unknown model: nope"}), f"{code} {body}")
                # logs
                code, text = get_text("GET", f"{base}/api/logs/{mid}", cookie=ck)
                check(f"{name}.logs", code == 200 and text == f"mock logs for {mid} on Windows dev\nunit: ai-{mid}\nno logs", repr(text)[:80])
                code, text = get_text("GET", f"{base}/api/logs/nope", cookie=ck)
                check(f"{name}.logs-unknown", code == 404, f"{code} {text[:60]}")
                # downloads
                code, text = get_text("GET", f"{base}/api/downloads", cookie=ck)
                check(f"{name}.downloads", code == 200 and text == "mock downloads on Windows", repr(text)[:60])
                # service (build is shadowed by {action} route in Python -> service_action mock)
                code, body = get_json("POST", f"{base}/api/service/{mid}/build", cookie=ck, body="{}")
                check(f"{name}.service-build", code == 200 and body == {"ok": True, "output": f"mock build {mid}"}, f"{code} {body}")
                code, body = get_json("POST", f"{base}/api/service/nope/start", cookie=ck, body="{}")
                check(f"{name}.service-unknown", code == 404 and body == {"detail": "unknown service/model: nope"}, f"{code} {body}")
                code, body = get_json("POST", f"{base}/api/service/{mid}/start", cookie=ck, body="{}")
                check(f"{name}.service-model", code == 200 and body == {"ok": True, "output": f"mock start {mid}"}, f"{code} {body}")
                # profile
                code, body = get_json("POST", f"{base}/api/profile/whatever", cookie=ck, body="{}")
                check(f"{name}.profile", code == 200 and body == {"ok": True, "output": "mock profile apply on Windows"}, f"{code} {body}")

            # ---- cross-endpoint body equality (python vs go) ----
            pairs = [
                ("model-start", "POST", f"/api/model/{mid}/start"),
            ]
            for name, method, path in pairs:
                cp, bp = get_json(method, PY_BASE + path, cookie=pyc, body="{}" if method == "POST" else None)
                cg, bg = get_json(method, GO_BASE + path, cookie=goc, body="{}" if method == "POST" else None)
                # interval differs by design (Py 3s vs Go 1s poller) — normalize it
                # out before the cross-endpoint byte comparison.
                if isinstance(bp, dict) and "interval" in bp:
                    bp = {k: v for k, v in bp.items() if k != "interval"}
                if isinstance(bg, dict) and "interval" in bg:
                    bg = {k: v for k, v in bg.items() if k != "interval"}
                check(f"cross.{name}", bp == bg, f"py={json.dumps(bp)[:100]} go={json.dumps(bg)[:100]}")

        # ---- static parity ----
        # 200 paths: byte-identical; 404 paths: same code + same body
        for path, marker, expect404 in (
            ("/", "/runtime-config.js", False),
            ("/chat", None, False),
            ("/favicon.svg", None, False),
            ("/static/index.html", "/runtime-config.js", False),
            ("/index.html", None, True),
            ("/app.js", None, True),
        ):
            rp = http("GET", PY_BASE + path)
            rg = http("GET", GO_BASE + path)
            if expect404:
                same = rp[0] == rg[0] == 404 and rp[1] == rg[1]
            else:
                same = rp[1] == rg[1] and rp[0] == rg[0] == 200
            extra = "" if same else f"py={rp[0]} len={len(rp[1])} go={rg[0]} len={len(rg[1])}"
            check(f"static{path}", same, extra)
            if marker and same:
                check(f"static{path}.marker", marker in rp[1].decode(errors="replace"))

        # ---- logout parity ----
        for name, base, ck in (("py", PY_BASE, pyc), ("go", GO_BASE, goc)):
            code, content, _ = http("DELETE", base + "/api/login", cookie=ck)
            check(f"{name}.logout", code == 200 and json.loads(content.decode()) == {"ok": True}, f"{code}")
            code, body = get_json("GET", base + "/api/status", cookie=ck)
            check(f"{name}.status-after-logout", code == 401, f"{code}")
    finally:
        exit_codes = {}
        for p in PROCS:
            try:
                p.terminate()
            except Exception:
                pass
        time.sleep(1)
        for p in PROCS:
            try:
                p.kill()
            except Exception:
                pass
            try:
                p.wait(timeout=5)
            except Exception:
                pass
            logname = getattr(p, "_log", None)
            if logname:
                exit_codes[logname] = p.returncode
            try:
                p._logf.close()
            except Exception:
                pass
        for f in ("parity-aihub.exe",):
            try:
                os.remove(os.path.join(RUNTIME_ROOT, f))
            except Exception:
                pass

    print()
    passed = sum(1 for _, c in RESULTS if c)
    total = len(RESULTS)
    failed = [n for n, c in RESULTS if not c]
    print(f"== PARITY RESULT: {passed}/{total} checks passed ==")
    if CONN_ERRORS:
        for base, errs in CONN_ERRORS.items():
            print(f"CONNECTION ERRORS on {base} ({len(errs)}), first 5:")
            for e in errs[:5]:
                print(f"  {e}")
        print(f"--- go log tail ---\n{log_tail(GO_LOG)}")
        print(f"--- py log tail ---\n{log_tail(PY_LOG)}")
    if failed:
        print("FAILED:", failed)
        sys.exit(1)
    if CONN_ERRORS:
        sys.exit(1)
    print("SHARED API CONTRACT CONFIRMED; current Go extensions checked separately")
    sys.exit(0)


if __name__ == "__main__":
    main()
