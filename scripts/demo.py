"""Serve the actual UI with synthetic data for documentation; never runs models.

Usage: python scripts/demo.py --port 18087
All actions are rejected. The listener is restricted to localhost.
"""
import argparse
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import time
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]


def sample_status():
    models = []
    for index, (family, name, engine, quant, context) in enumerate([
        ("qwen", "Qwen · Local assistant", "llama.cpp", "Q4_K_M", "64k"),
        ("gemma", "Gemma · Vision & language", "llama.cpp", "Q4_K_M", "32k"),
        ("glm", "GLM · Reasoning", "Maya", "IQ2_S", "256k"),
        ("nemotron", "Nemotron · Agent workflows", "SGLang", "NVFP4", "128k"),
    ]):
        models.append(dict(id=family+"-demo", name=name, short=family.title(), category="LLM",
            backend=engine, quant=quant, port=18021+index, ctx=context, vram="Model dependent",
            tps="", running=index == 0, healthy=index == 0, enabled=True, **{"in": ["text"], "out": ["text"]},
            parallel={}, sub=[], desc="Synthetic documentation example."))
    return dict(models=models, services=[], profiles=[], ts=time.time(), stale=False, age_s=0,
        cpu=dict(socket0=8, socket1=6, cpu_count=32, mem_used=24576, mem_total=131072, load1=1.14, load5=0.92, load15=0.73),
        disk=dict(disk_free="420 GB", disk_total="1.8 TB"),
        gpus=[dict(index=i, name="NVIDIA RTX 3090 (demo)", mem_used=18000 if i == 0 else 900,
            mem_total=24576, util=67 if i == 0 else 0, temp=56 if i == 0 else 34,
            power=210 if i == 0 else 28, vram_temp=64 if i == 0 else 42) for i in range(2)])


class Handler(SimpleHTTPRequestHandler):
    def do_GET(self):
        path = unquote(urlsplit(self.path).path)
        if "\\" in path or any(part in (".", "..") for part in path.split("/")):
            self.send_error(404)
            return
        data = None
        if path == "/runtime-config.js":
            raw = b'window.AIHUB_CONFIG={"demoMode":true};\n'
            self.send_response(200)
            self.send_header("Content-Type", "application/javascript")
            self.end_headers()
            self.wfile.write(raw)
            return
        if path == "/api/status":
            data = sample_status()
        elif path.startswith("/api/"):
            data = {"history": [], "models": [], "gpus": [], "live": None}
        if data is not None:
            raw = json.dumps(data).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(raw)
            return
        if path == "/":
            self.path = "/static/index.html"
        elif path not in ("/favicon.svg", "/favicon.ico", "/favicon_light.svg", "/bg.svg") and not path.startswith("/static/"):
            self.send_error(404)
            return
        resolved = (ROOT / self.path.split("?", 1)[0].lstrip("/")).resolve()
        if not resolved.is_relative_to(ROOT) or not resolved.is_file():
            self.send_error(404)
            return
        super().do_GET()

    def do_POST(self):
        self.send_error(403, "Documentation demo: actions are disabled")

    do_DELETE = do_POST


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=18087)
    args = parser.parse_args()
    print(f"Synthetic UI preview: http://127.0.0.1:{args.port} (actions disabled)", flush=True)
    server = ThreadingHTTPServer(("127.0.0.1", args.port), partial(Handler, directory=str(ROOT)))
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
