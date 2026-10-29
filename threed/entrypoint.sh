#!/bin/bash
###############################################################################
# entrypoint.sh - Hunyuan3D-2.1 entrypoint for ai-hub
#
# Features:
#   - Pre-downloads models on first run (with progress)
#   - Configurable via environment variables
#   - Graceful shutdown handling
#
# Environment variables:
#   HF_HOME              - HuggingFace cache directory (default: /models/huggingface)
#   MODEL_PATH           - Model repo path (default: tencent/Hunyuan3D-2.1)
#   SUBFOLDER            - Shape model subfolder (default: hunyuan3d-dit-v2-1)
#   TEXGEN_MODEL_PATH    - Texture model path (default: tencent/Hunyuan3D-2.1)
#   LOW_VRAM             - Enable low VRAM mode (default: true)
#   DISABLE_TEX          - Disable texture generation (default: false)
#   ENABLE_T23D          - Enable text-to-3D (default: false)
#   ENABLE_FLASHVDM      - Enable FlashVDM acceleration (default: false)
#   PORT                 - Listen port (default: 8020)
###############################################################################

set -e

PYTHON="/workspace/miniconda3/envs/hunyuan3d/bin/python"
MODEL_PATH="${MODEL_PATH:-tencent/Hunyuan3D-2.1}"
SUBFOLDER="${SUBFOLDER:-hunyuan3d-dit-v2-1}"
TEXGEN_MODEL_PATH="${TEXGEN_MODEL_PATH:-tencent/Hunyuan3D-2.1}"
LOW_VRAM="${LOW_VRAM:-true}"
DISABLE_TEX="${DISABLE_TEX:-false}"
ENABLE_T23D="${ENABLE_T23D:-false}"
ENABLE_FLASHVDM="${ENABLE_FLASHVDM:-false}"
PORT="${PORT:-8020}"

echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║          Hunyuan3D-2.1 — ai-hub 3D Service                  ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo ""
echo "  Model:         ${MODEL_PATH}/${SUBFOLDER}"
echo "  TexGen:        ${TEXGEN_MODEL_PATH}"
echo "  Low VRAM:      ${LOW_VRAM}"
echo "  Texture:       $([ "${DISABLE_TEX}" = 'true' ] && echo 'disabled' || echo 'enabled')"
echo "  Text-to-3D:    ${ENABLE_T23D}"
echo "  FlashVDM:      ${ENABLE_FLASHVDM}"
echo "  Port:          ${PORT}"
echo "  HF Cache:      ${HF_HOME}"
echo ""

# ── Pre-download models (first run) ─────────────────────────────────────────
echo "Checking model cache..."
SHAPE_CACHE="${HF_HOME}/hub/models--${MODEL_PATH}/snapshots"
if [ ! -d "${SHAPE_CACHE}" ] || [ -z "$(ls -A ${SHAPE_CACHE} 2>/dev/null)" ]; then
    echo "⚠  Model not cached. Downloading shape model (~15 GB)..."
    echo "   This may take 10-30 minutes on first run."
    $PYTHON -c "
from huggingface_hub import snapshot_download
snapshot_download('${MODEL_PATH}', ignore_patterns=['*.bin', '*.safetensors', '*.ot', '*.msgpack'])
print('Shape model downloaded.')
"
else
    echo "✓ Model cache found."
fi

# Download texture subfolder if not cached
TEX_CACHE="${HF_HOME}/hub/models--${TEXGEN_MODEL_PATH}/snapshots"
if [ ! -d "${TEX_CACHE}" ] || [ -z "$(ls -A ${TEX_CACHE} 2>/dev/null)" ]; then
    if [ "${DISABLE_TEX}" != 'true' ]; then
        echo "Downloading texture model..."
        $PYTHON -c "
from huggingface_hub import snapshot_download
snapshot_download('${TEXGEN_MODEL_PATH}', ignore_patterns=['*.bin', '*.safetensors', '*.ot', '*.msgpack'])
print('Texture model downloaded.')
"
    fi
fi

# ── Build command ────────────────────────────────────────────────────────────
CMD_ARGS=(
    "--port" "${PORT}"
    "--host" "0.0.0.0"
    "--model_path" "${MODEL_PATH}"
    "--subfolder" "${SUBFOLDER}"
    "--texgen_model_path" "${TEXGEN_MODEL_PATH}"
)

if [ "${LOW_VRAM}" = 'true' ]; then
    CMD_ARGS+=("--low_vram_mode")
fi

if [ "${DISABLE_TEX}" = 'true' ]; then
    CMD_ARGS+=("--disable_tex")
fi

if [ "${ENABLE_T23D}" = 'true' ]; then
    CMD_ARGS+=("--enable_t23d")
fi

if [ "${ENABLE_FLASHVDM}" = 'true' ]; then
    CMD_ARGS+=("--enable_flashvdm")
fi

echo ""
echo "Starting Hunyuan3D-2.1 Gradio app on port ${PORT}..."
echo "  Command: $PYTHON gradio_app.py ${CMD_ARGS[*]}"
echo ""

# ── Graceful shutdown ────────────────────────────────────────────────────────
cleanup() {
    echo ""
    echo "Shutting down gracefully..."
    kill -TERM "$PID" 2>/dev/null
    wait "$PID" 2>/dev/null
    exit 0
}
trap cleanup SIGTERM SIGINT

# ── Launch ───────────────────────────────────────────────────────────────────
$PYTHON gradio_app.py "${CMD_ARGS[@]}" &
PID=$!
wait "$PID"
