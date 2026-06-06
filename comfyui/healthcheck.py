#!/usr/bin/env python3
"""
ComfyUI Health Check — verifies the server is responding on port 8188.

Usage:
    python healthcheck.py

Returns:
    0 if healthy, 1 if unhealthy.

ComfyUI does NOT expose a /health endpoint by default. This script checks:
  1. TCP connectivity to localhost:8188
  2. HTTP response from the ComfyUI web UI
  3. (Optional) GPU availability via nvidia-smi
"""

import sys
import socket
import urllib.request
import urllib.error
import json
import subprocess

HOST = "localhost"
PORT = 8188
TIMEOUT = 5  # seconds


def check_tcp(host: str, port: int) -> bool:
    """Check if TCP port is open."""
    try:
        with socket.create_connection((host, port), timeout=TIMEOUT):
            return True
    except (ConnectionRefusedError, OSError, TimeoutError):
        return False


def check_http(host: str, port: int) -> bool:
    """Check if ComfyUI responds to HTTP requests."""
    try:
        url = f"http://{host}:{port}/system_stats"
        req = urllib.request.Request(url, method="GET")
        with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
            return resp.status == 200
    except (urllib.error.URLError, urllib.error.HTTPError, OSError, TimeoutError):
        # Fallback: try the root page
        try:
            url = f"http://{host}:{port}/"
            req = urllib.request.Request(url, method="HEAD")
            with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
                return resp.status in (200, 307)  # 307 = redirect to /UI
        except Exception:
            return False


def check_gpu() -> bool:
    """Check if NVIDIA GPU is accessible (optional, non-fatal)."""
    try:
        result = subprocess.run(
            ["nvidia-smi", "--query-gpu=name", "--format=csv,noheader"],
            capture_output=True, text=True, timeout=5
        )
        return result.returncode == 0 and len(result.stdout.strip()) > 0
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return False


def main() -> int:
    """Run all health checks. Exit 0 if all pass, 1 otherwise."""
    errors = []

    # 1. TCP check
    if not check_tcp(HOST, PORT):
        errors.append(f"TCP connection to {HOST}:{PORT} failed")
    else:
        # 2. HTTP check (only if TCP passed)
        if not check_http(HOST, PORT):
            errors.append(f"HTTP request to {HOST}:{PORT} failed")

    # 3. GPU check (warning only — ComfyUI can run on CPU)
    if not check_gpu():
        print("WARN: nvidia-smi not available — running in CPU-only mode",
              file=sys.stderr)

    if errors:
        for err in errors:
            print(f"FAIL: {err}", file=sys.stderr)
        return 1

    # Print system stats for logging
    try:
        url = f"http://{HOST}:{PORT}/system_stats"
        with urllib.request.urlopen(url, timeout=TIMEOUT) as resp:
            stats = json.loads(resp.read())
            devices = stats.get("devices", [])
            gpu_info = [f"{d.get('name', '?')} ({d.get('vram_total', 0) // (1024**2)} MB)"
                        for d in devices if d.get("type") == "cuda"]
            if gpu_info:
                print(f"OK — GPUs: {', '.join(gpu_info)}")
            else:
                print("OK — ComfyUI is running (CPU mode)")
    except Exception:
        print("OK — ComfyUI is running")

    return 0


if __name__ == "__main__":
    sys.exit(main())
