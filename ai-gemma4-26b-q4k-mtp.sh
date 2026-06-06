#!/usr/bin/env bash
# Gemma4 26B-A4B Q4_K_M, N=1..4 GPU, ctx+parallel from hub state.
# Plain decode + repetition penalty for the 256k profile.
# MTP-1 loops at 192k; MTP-2 is slower. Plain without a penalty loops at 64k.
# State: /srv/ai-hub/state/gemma4-26b-q4k-mtp.json  {"gpus": N, "ctx": C, "parallel": P}
set -uo pipefail

STATE_DIR=/srv/ai-hub/state
STATE=$STATE_DIR/gemma4-26b-q4k-mtp.json
IMAGE=ghcr.io/ggml-org/llama.cpp:server-cuda
MODELS=/home/user/models/gemma4-26b-uncensored
PORT=18031
NAME=gemma4-26b-q4k-mtp

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
  echo "gemma4-26b-q4k-mtp up: gpus=$N ctx=$CTX parallel=$PAR port=$PORT"
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
    --model /models/Gemma4-26B-A4B-Uncensored-HauhauCS-Balanced-Q4_K_M.gguf \
    --mmproj /models/mmproj-Gemma4-26B-A4B-Uncensored-HauhauCS-Balanced-f16.gguf \
    --flash-attn on \
    --repeat-penalty 1.05 --repeat-last-n 64 \
    --ctx-size "$CTX" \
    --n-gpu-layers 99 \
    --parallel "$PAR" \
    --metrics --host 0.0.0.0 --port 8080 --threads 16 --jinja
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
