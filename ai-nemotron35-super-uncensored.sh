#!/usr/bin/env bash
# Nemotron 3.5 Super VL, pinned llama.cpp b11669; hub state controls ctx/parallel.
set -euo pipefail
NAME=nemotron35-super-uncensored
STATE=/srv/ai-hub/state/$NAME.json
MODELS=/home/user/models/Nemotron-3.5-Super-VL-120B-Uncensored-AD-Q4_K_S
IMAGE=ghcr.io/ggml-org/llama.cpp@sha256:e4cfff28678327b0ae7eda129e700e56a0fa55aa23db2f016256ea155c15c2b5
PORT=18036

up() {
  local N CTX PAR MTP UB CK CV DCK DCV VC TS OV
  read -r N CTX PAR MTP UB CK CV DCK DCV VC TS OV < <(python3 - "$STATE" <<'PY'
import json, sys
try:
    s = json.load(open(sys.argv[1]))
except FileNotFoundError:
    s = {}
n, ctx, par, mtp, ub = [int(s.get(k, d)) for k, d in [('gpus',4),('ctx',1048576),('parallel',1),('mtp',3),('ubatch',64)]]
assert n == 4, 'This 120B profile requires all four GPUs'
assert 65536 <= ctx <= 1048576 and ctx % 1024 == 0 and par == 1, 'Unsupported context/parallel selection'
assert mtp in [0,1,2,3,4], 'Unsupported MTP draft count'
assert ub in [64,128,256,512,768,1024], 'Unsupported prefill microbatch'
caches = [s.get(k, 'q8_0') for k in ['cache_k','cache_v','draft_cache_k','draft_cache_v']]
assert all(v in ['f16','q8_0','q5_0','q4_0'] for v in caches), 'Unsupported KV cache format'
split = s.get('tensor_split','0.238889,0.266667,0.266667,0.227778')
parts = [float(v) for v in split.split(',')]
assert len(parts)==4 and all(0 < v <= 1 for v in parts), 'Invalid GPU split'
overrides = s.get('overrides',[r'^blk\.88\.ffn_down_exps\.weight$=CUDA0'])
assert isinstance(overrides,list) and all(isinstance(v,str) and ';' not in v and ' ' not in v and '\n' not in v for v in overrides), 'Invalid tensor override'
print(n, ctx, par, mtp, ub, *caches, int(s.get('vision_cpu',True)), split, ';'.join(overrides) or '-')
PY
  )
  test -f "$MODELS/verified.json"
  if [ "$(docker inspect -f '{{.State.Running}}' "$NAME" 2>/dev/null || true)" = true ]; then
    echo "$NAME already running"
    return 0
  fi
  docker rm "$NAME" >/dev/null 2>&1 || true
  local SPEC=()
  local EXTRA=() RULES=()
  if [ "$MTP" -gt 0 ]; then SPEC=(--spec-type draft-mtp --spec-draft-n-max "$MTP"); fi
  if [ "$VC" -eq 1 ]; then EXTRA+=(--no-mmproj-offload); fi
  if [ "$OV" != - ]; then
    IFS=';' read -r -a RULES <<< "$OV"
    for rule in "${RULES[@]}"; do EXTRA+=(--override-tensor "$rule"); done
  fi
  docker run -d --name "$NAME" \
    --runtime nvidia -e NVIDIA_VISIBLE_DEVICES=0,1,2,3 \
    -p "$PORT:8080" -v "$MODELS:/models:ro" --restart no \
    "$IMAGE" \
    --model /models/NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Uncensored-AD-Q4_K_S/NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Uncensored-AD-Q4_K_S-00001-of-00059.gguf \
    --mmproj /models/mmproj-NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Uncensored-BF16.gguf \
    --alias nemotron-3.5-super-vl --jinja \
    --n-gpu-layers 999 --split-mode layer --tensor-split "$TS" \
    --fit off --flash-attn on --ctx-size "$((CTX * PAR))" --parallel "$PAR" \
    --batch-size 2048 --ubatch-size "$UB" --threads 16 \
    --cache-type-k "$CK" --cache-type-v "$CV" \
    --cache-type-k-draft "$DCK" --cache-type-v-draft "$DCV" \
    --temp 1.0 --top-p 0.95 --top-k 0 --min-p 0.0 --repeat-penalty 1.0 \
    "${SPEC[@]}" "${EXTRA[@]}" --metrics --slots --host 0.0.0.0 --port 8080 --verbosity 3
  echo "Started $NAME: $N GPUs, $CTX context per slot, $PAR slot, MTP $MTP"
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
