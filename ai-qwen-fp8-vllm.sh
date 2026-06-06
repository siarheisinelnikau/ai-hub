#!/usr/bin/env bash
# ai-qwen-fp8-vllm.sh - Qwen 3.8 27B FP8 (vLLM), N=1..4 GPU, ctx from hub state.
# State: /srv/ai-hub/state/qwen-fp8-vllm.json  {"gpus": N, "ctx": C}
set -uo pipefail

STATE_DIR=/srv/ai-hub/state
STATE=$STATE_DIR/qwen-fp8-vllm.json
IMAGE=vllm/vllm-openai:v0.28.0
MODELS=/home/user/models-optim/Qwen3.8-27B-Uncensored-FP8
PORT=18023
NAME=qwen-fp8-vllm

mkdir -p "$STATE_DIR"

read_state(){
  python3 - "$1" "$2" <<PY
import json, sys
key, default = sys.argv[1], sys.argv[2]
try:
    d = json.load(open("$STATE"))
    print(d.get(key, default))
except Exception:
    print(default)
PY
}

get_gpus(){
  local n
  n=$(read_state gpus 4)
  case "$n" in
    [0-9]*) : ;;
    *) n=4 ;;
  esac
  [ "$n" -lt 1 ] && n=1
  [ "$n" -gt 4 ] && n=4
  echo "$n"
}

get_ctx(){
  local c
  c=$(read_state ctx 262144)
  case "$c" in
    [0-9]*) : ;;
    *) c=262144 ;;
  esac
  echo "$c"
}

get_parallel(){
  local p
  p=$(read_state parallel 1)
  case "$p" in
    [0-9]*) : ;;
    *) p=1 ;;
  esac
  [ "$p" -lt 1 ] && p=1
  [ "$p" -gt 8 ] && p=8
  echo "$p"
}

cmd_up(){
  local N CTX PAR
  N=$(get_gpus); CTX=$(get_ctx); PAR=$(get_parallel)
  echo "qwen-fp8-vllm up: gpus=$N ctx=$CTX parallel=$PAR tp=$N port=$PORT"
  if docker ps -a --format "{{.Names}}" | grep -qx "$NAME"; then
    if [ "$(docker inspect -f "{{.State.Running}}" "$NAME" 2>/dev/null)" = "true" ]; then
      echo "$NAME already running (:$PORT)"
      return 0
    fi
    docker rm -f "$NAME" >/dev/null 2>&1 || true
  fi
  docker run -d --name "$NAME" \
    --gpus all --ipc=host --shm-size 32g \
    -p "$PORT:8000" \
    -v "$MODELS":/model:ro \
    -v /home/user/vllm-cache:/root/.cache/vllm \
    --restart no \
    -e NCCL_P2P_DISABLE=1 \
    -e VLLM_WORKER_MULTIPROC_METHOD=spawn \
    -e VLLM_COMPILE_CACHE=1 \
    -e VLLM_CACHE_ROOT=/root/.cache/vllm \
    "$IMAGE" \
    --model /model \
    --served-model-name qwen3.8-27b \
    --tensor-parallel-size "$N" \
    --gpu-memory-utilization 0.95 \
    --max-model-len "$CTX" \
    --disable-custom-all-reduce \
    --trust-remote-code \
    --max-num-seqs "$PAR" \
    --reasoning-parser qwen3 \
    --enable-auto-tool-choice \
    --tool-call-parser qwen3_xml \
    --speculative-config '{"method":"mtp","num_speculative_tokens":3}'
  echo "started $NAME (gpus=$N tp=$N :$PORT ctx=$CTX parallel=$PAR)"
}

cmd_down(){
  if docker ps -a --format "{{.Names}}" | grep -qx "$NAME"; then
    docker stop -t 15 "$NAME" >/dev/null 2>&1 || true
    docker rm -f "$NAME" >/dev/null 2>&1 || true
    echo "removed $NAME"
  fi
}

cmd_status(){
  local N
  N=$(get_gpus)
  echo "gpus=$N tp=$N"
  local run=no
  [ "$(docker inspect -f "{{.State.Running}}" "$NAME" 2>/dev/null)" = "true" ] && run=yes
  echo "container $NAME running=$run"
}

case "${1:-status}" in
  up) cmd_up ;;
  down) cmd_down ;;
  status) cmd_status ;;
  *) echo "usage: $0 {up|down|status}"; exit 2 ;;
esac
