#!/bin/bash
###############################################################################
# healthcheck.sh - Docker HEALTHCHECK for Hunyuan3D-2.1
#
# Checks:
#   1. Gradio HTTP endpoint responds (port 8020)
#   2. Python process is alive
#   3. GPU is accessible (nvidia-smi)
###############################################################################

PORT="${PORT:-8020}"

# Check 1: HTTP endpoint
HTTP_CODE=$(curl -sf -o /dev/null -w "%{http_code}" "http://127.0.0.1:${PORT}/" 2>/dev/null || echo "000")
if [ "${HTTP_CODE}" = "000" ]; then
    echo "HEALTHCHECK FAIL: Gradio HTTP on port ${PORT} not responding"
    exit 1
fi

# Check 2: Python process
if ! pgrep -f "gradio_app.py" > /dev/null 2>&1; then
    echo "HEALTHCHECK FAIL: gradio_app.py process not running"
    exit 1
fi

# Check 3: GPU access (optional, non-fatal)
if command -v nvidia-smi &> /dev/null; then
    GPU_MEM=$(nvidia-smi --query-gpu=memory.used --format=csv,noheader,nounits 2>/dev/null | head -1)
    if [ -z "${GPU_MEM}" ]; then
        echo "HEALTHCHECK WARN: nvidia-smi not responding (non-fatal)"
    fi
fi

echo "HEALTHCHECK OK (HTTP ${HTTP_CODE})"
exit 0
