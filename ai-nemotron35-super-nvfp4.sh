#!/usr/bin/env bash
# Verified Nemotron NVFP4 / SGLang v0.5.20 Ampere profile.
set -euo pipefail
NAME=nemotron35-super-nvfp4
STATE=/srv/ai-hub/state/$NAME.json

up() {
  local CTX MEM
  read -r CTX MEM < <(python3 - "$STATE" <<'PY'
import json, sys
try:
    state = json.load(open(sys.argv[1]))
except FileNotFoundError:
    state = {}
assert int(state.get('gpus', 4)) == 4, 'This profile requires all four GPUs'
ctx = int(state.get('ctx', 393114))
memory = float(state.get('memory_fraction', 0.99))
assert 32768 <= ctx <= 393114, 'Context exceeds the measured NVFP4 capacity'
assert 0.90 <= memory <= 0.99, 'Unsupported static memory fraction'
assert int(state.get('parallel', 1)) == 1, 'This profile supports one concurrent request'
print(ctx, memory)
PY
  )
  if [ "$(docker inspect -f '{{.State.Running}}' "$NAME" 2>/dev/null || true)" = true ]; then
    echo "$NAME already running on 4 GPUs, $CTX context, 1 slot"
    return 0
  fi
  docker rm "$NAME" >/dev/null 2>&1 || true
  AIHUB_MODEL_ID="$NAME" AIHUB_CTX="$CTX" AIHUB_MEM_FRACTION="$MEM" bash /srv/ai-hub/tools/launch_nemotron35_sglang_test.sh
}

down() {
  if docker inspect "$NAME" >/dev/null 2>&1; then
    docker stop --time 20 "$NAME" >/dev/null
    docker rm "$NAME" >/dev/null
  fi
}

case "${1:-status}" in
  up) up ;;
  down) down ;;
  status) docker inspect -f '{{.State.Status}}' "$NAME" ;;
  *) echo "Usage: $0 {up|down|status}"; exit 2 ;;
esac
