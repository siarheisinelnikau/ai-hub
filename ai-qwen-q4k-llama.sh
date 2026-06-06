#!/usr/bin/env bash
# ai-qwen-q4k-llama.sh - Qwen 3.8 27B Q4_K_M (llama.cpp), N=1..4 GPU, ctx+parallel from hub state.
# Native MTP reuses the loaded checkpoint; avoid loading the full GGUF twice.
# State: /srv/ai-hub/state/qwen-q4k-llama.json  {"gpus": N, "ctx": C, "parallel": P}
set -uo pipefail

STATE_DIR=/srv/ai-hub/state
STATE=$STATE_DIR/qwen-q4k-llama.json
IMAGE=ghcr.io/ggml-org/llama.cpp:server-cuda
MODELS=/home/user/models/Qwen3.8-27B-Uncensored-GGUF
PORT=18021
NAME=qwen-q4k-llama

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

dev_list(){
  local n=$1 i lst=
  for i in $(seq 0 $((n-1))); do
    if [ -n "$lst" ]; then lst="$lst,"; fi
    lst="$lst$i"
  done
  echo "$lst"
}

cmd_up(){
  local N CTX PAR
  N=$(get_gpus); CTX=$(get_ctx); PAR=$(get_parallel)
  echo "qwen-q4k-llama up: gpus=$N ctx=$CTX parallel=$PAR port=$PORT"
  if docker ps -a --format "{{.Names}}" | grep -qx "$NAME"; then
    if [ "$(docker inspect -f "{{.State.Running}}" "$NAME" 2>/dev/null)" = "true" ]; then
      echo "$NAME already running (:$PORT)"
      return 0
    fi
    docker rm -f "$NAME" >/dev/null 2>&1 || true
  fi
  docker run -d --name "$NAME" \
    --runtime nvidia -e NVIDIA_VISIBLE_DEVICES=$(dev_list $N) \
    -p "$PORT:8080" \
    -v "$MODELS":/models:ro \
    --restart no \
    "$IMAGE" \
    --model /models/Qwen3.8-27B-Uncensored-Q4_K_M.gguf \
    --mmproj /models/mmproj-Qwen3.8-27B-Q8_0.gguf  \
    --flash-attn on \
    --spec-type draft-mtp \
    --spec-draft-n-max 4 \
    --ctx-size "$CTX" \
    --n-gpu-layers 99 \
    --parallel "$PAR" \
    --threads 88 \
    --metrics --host 0.0.0.0 --port 8080
  echo "started $NAME (gpus=$N :$PORT ctx=$CTX parallel=$PAR)"
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
  echo "gpus=$N"
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
