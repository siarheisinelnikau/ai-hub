"""A fresh installation starts privately without model engines or production state."""
import http.cookiejar
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time
import unittest
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


class InstallationTest(unittest.TestCase):
    def test_empty_first_start_authenticates_and_hides_credentials(self):
        with tempfile.TemporaryDirectory(prefix="ai-hub-install-") as directory:
            root = Path(directory)
            binary = root / ("aihub.exe" if os.name == "nt" else "aihub")
            subprocess.run(["go", "build", "-o", str(binary), "./cmd/aihub"], cwd=ROOT, check=True, capture_output=True)
            shutil.copytree(ROOT / "examples", root / "examples")
            shutil.copytree(ROOT / "static", root / "static")
            with socket.socket() as sock:
                sock.bind(("127.0.0.1", 0))
                port = sock.getsockname()[1]
            (root / ".env").write_text(
                f"AIHUB_HOST=127.0.0.1\nAIHUB_PORT={port}\nAIHUB_REGISTRY=examples/registry.yaml\n"
                f"AIHUB_CATALOG=examples/catalog.yaml\nAIHUB_DOWNLOADS_DIR={root.as_posix()}/downloads\n",
                encoding="utf-8")
            env = {key: value for key, value in os.environ.items() if not key.startswith("AIHUB")}
            env.update(AIHUB=str(root), AIHUB_ENV_FILE=str(root / ".env"))
            with (root / "process.log").open("w", encoding="utf-8") as log:
                process = subprocess.Popen([str(binary)], cwd=root, env=env, stdout=log, stderr=log)
                try:
                    base = f"http://127.0.0.1:{port}"
                    client = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
                    deadline = time.monotonic() + 30
                    while True:
                        try:
                            with client.open(base + "/healthz", timeout=1) as response:
                                self.assertEqual(response.status, 200)
                            break
                        except OSError:
                            if process.poll() is not None or time.monotonic() > deadline:
                                self.fail("fresh installation failed to become healthy")
                            time.sleep(0.1)
                    token = (root / "token").read_text().strip()
                    self.assertEqual(len(token), 32)
                    if os.name != "nt":
                        self.assertEqual((root / "token").stat().st_mode & 0o777, 0o600)
                    for path, expected in [("/v1/models", 401), ("/metrics", 401), ("/.env", 404), ("/static/token", 404)]:
                        with self.assertRaises(urllib.error.HTTPError) as caught:
                            client.open(base + path, timeout=5)
                        self.assertEqual(caught.exception.code, expected)
                        caught.exception.close()
                    with client.open(base + "/runtime-config.js", timeout=5) as response:
                        self.assertNotIn(token.encode(), response.read())
                    login = urllib.request.Request(base + "/api/login", data=json.dumps({"token": token}).encode(), headers={"Content-Type": "application/json"})
                    with client.open(login, timeout=5) as response:
                        self.assertEqual(response.status, 200)
                        self.assertIn("HttpOnly", response.headers["Set-Cookie"])
                    with client.open(base + "/api/status", timeout=5) as response:
                        self.assertEqual(json.load(response)["models"], [])
                    with client.open(base + "/api/models", timeout=5) as response:
                        self.assertEqual(json.load(response)["models"], [])
                    with client.open(base + "/v1/models", timeout=5) as response:
                        self.assertEqual(json.load(response)["data"][0]["id"], "universal")
                finally:
                    process.terminate()
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)
