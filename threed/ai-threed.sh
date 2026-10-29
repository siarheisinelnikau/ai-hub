#!/usr/bin/env bash
# =============================================================================
# ai-threed.sh — start Hunyuan3D-2.1 on a GPU that actually has free VRAM.
#
# Hunyuan3D-2.1 needs ~16 GiB VRAM on ONE GPU (shape ~10G + texture ~6G with
# LOW_VRAM). The compose default THREED_GPUS=0,1,2,3 lets PyTorch pick cuda:0,
# which is usually held by comfyui / a running LLM -> torch.OutOfMemoryError.
#
# This wrapper picks the GPU with the MOST free VRAM (must be >= MIN_FREE_GB)
# and exports THREED_GPUS=<n> for docker compose, so the hub panel Start button
# works no matter which GPUs the LLMs currently occupy.
#
# Usage:
#   ai-threed.sh                     # auto-pick the freest GPU
#   THREED_GPUS=3 ai-threed.sh       # explicit pin wins
#   THREED_MIN_FREE_GB=20 ai-threed.sh
# =============================================================================
set -euo pipefail

COMPOSE_FILE="${THREED_COMPOSE_FILE:-/srv/ai/stack/docker-compose.yml}"
MIN_FREE_GB="${THREED_MIN_FREE_GB:-16}"

if [ -n "${THREED_GPUS:-}" ]; then
  echo "ai-threed: THREED_GPUS=${THREED_GPUS} explicitly set — using it."
else
  if ! command -v nvidia-smi >/dev/null 2>&1; then
    echo "ai-threed: ERROR: nvidia-smi not found." >&2
    exit 1
  fi

  best_idx=""
  best_free=0
  while IFS=',' read -r idx free; do
    idx="${idx//[[:space:]]/}"
    free="${free//[[:space:]]/}"
    [ -z "$idx" ] && continue
    if [ "$free" -ge "$((MIN_FREE_GB * 1024))" ] && [ "$free" -gt "$best_free" ]; then
      best_idx="$idx"
      best_free="$free"
    fi
  done < <(nvidia-smi --query-gpu=index,memory.free --format=csv,noheader,nounits)

  if [ -z "$best_idx" ]; then
    echo "ai-threed: ERROR: no GPU has >= ${MIN_FREE_GB} GiB free VRAM for Hunyuan3D." >&2
    nvidia-smi --query-gpu=index,memory.used,memory.free --format=csv >&2 || true
    echo "ai-threed: stop a running model first, or lower THREED_MIN_FREE_GB." >&2
    exit 1
  fi

  echo "ai-threed: selected GPU ${best_idx} ($((best_free / 1024)) GiB free; min ${MIN_FREE_GB} GiB)."
  export THREED_GPUS="$best_idx"
fi

exec docker compose -f "$COMPOSE_FILE" up -d threed
