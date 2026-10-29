#!/usr/bin/env bash
# =============================================================================
# ACE-Step (music) — pre-start safety check   [E-02]
#
# ACE-Step needs ~17 GB of FREE VRAM on a SINGLE dedicated GPU (DiT ~15 GB
# weights + LM KV cache + runtime). The stack compose default of
# MUSIC_GPUS=0,1,2,3 is a footgun: with the LLM (q4k) holding all 4 GPUs it
# would either OOM or steal VRAM from a running model.
#
# This script:
#   1. finds a GPU with >= MIN_FREE_GB free (default 18 GB),
#   2. prints the exact `MUSIC_GPUS=<n> docker compose up -d music` command,
#   3. exits non-zero if NO GPU has enough free VRAM (start the LLM stop first).
#
# Usage:
#   prestart-check.sh                 # just check + recommend
#   MIN_FREE_GB=20 prestart-check.sh  # stricter threshold
#   GPU=3 prestart-check.sh           # force-check a specific GPU
# =============================================================================
set -uo pipefail

MIN_FREE_GB="${MIN_FREE_GB:-18}"
FORCE_GPU="${GPU:-}"
TOTAL_GB=24

if ! command -v nvidia-smi >/dev/null 2>&1; then
  echo "ERROR: nvidia-smi not found (run on the ai-hub box)." >&2
  exit 2
fi

# nvidia-smi index -> free GB
mapfile -t rows < <(nvidia-smi --query-gpu=index,memory.used,memory.total --format=csv,noheader,nounits)

best_idx=""
best_free=-1
for r in "${rows[@]}"; do
  IFS=',' read -r idx used total <<< "$r"
  idx=$(echo "$idx" | tr -d ' '); used=$(echo "$used" | tr -d ' '); total=$(echo "$total" | tr -d ' ')
  free_gb=$(( (total - used) / 1024 ))
  printf '  GPU%s : %s/%s GiB used, %s GiB free %s\n' \
    "$idx" "$used" "$total" "$free_gb" "$([ "$free_gb" -ge "$MIN_FREE_GB" ] && echo '✓ OK' || echo '(< ${MIN_FREE_GB}G)')"
  if [ "$free_gb" -ge "$MIN_FREE_GB" ] && [ "$free_gb" -gt "$best_free" ]; then
    best_free=$free_gb; best_idx=$idx
  fi
done
echo "----"

if [ -n "$FORCE_GPU" ]; then
  # verify the forced GPU specifically
  ok=0
  for r in "${rows[@]}"; do
    IFS=',' read -r idx used total <<< "$r"
    idx=$(echo "$idx" | tr -d ' '); used=$(echo "$used" | tr -d ' '); total=$(echo "$total" | tr -d ' ')
    free_gb=$(( (total - used) / 1024 ))
    [ "$idx" = "$FORCE_GPU" ] && [ "$free_gb" -ge "$MIN_FREE_GB" ] && ok=1
  done
  if [ "$ok" -ne 1 ]; then
    echo "FAIL: GPU $FORCE_GPU does not have >= ${MIN_FREE_GB} GiB free. Pick another GPU or stop the LLM first." >&2
    exit 1
  fi
  echo "OK: GPU $FORCE_GPU has >= ${MIN_FREE_GB} GiB free."
  echo "Start with:"
  echo "  MUSIC_GPUS=$FORCE_GPU docker compose up -d music"
  exit 0
fi

if [ -z "$best_idx" ]; then
  echo "FAIL: no GPU has >= ${MIN_FREE_GB} GiB free VRAM. ACE-Step needs a single dedicated GPU." >&2
  echo "      Stop the running LLM first (with permission), or raise free VRAM, then re-run." >&2
  exit 1
fi

echo "OK: GPU $best_idx has ${best_free} GiB free (>= ${MIN_FREE_GB} GiB)."
echo "Start with a SINGLE dedicated GPU:"
echo "  MUSIC_GPUS=$best_idx docker compose up -d music"
echo "Verify:  docker compose ps music   (and check it is on GPU $best_idx)"
exit 0
