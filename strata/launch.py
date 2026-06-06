#!/usr/bin/env python3
"""Run Strata using the GPU/context selection saved by ai-hub."""
import json
import os
from pathlib import Path
import subprocess

MID = os.environ.get("AIHUB_MODEL_ID", "qwen38-flash-next-strata")
PORT = int(os.environ.get("STRATA_PORT", "18029"))
CONFIG = os.environ.get("STRATA_INSTALL_CONFIG", "strata-unsloth-ud-iq4_xs.json")
ROOT = Path("/srv/ai/strata")
STATE = Path("/srv/ai-hub/state") / (MID + ".json")

def prepare_config(state, config, available, key):
    count = int(state.get("gpus", 4))
    context = int(state.get("ctx", 262144))
    parallel = int(state.get("parallel", 1))
    if count not in (1, 2, 3, 4) or context not in (65536, 131072, 196608, 262144) or parallel != 1:
        raise ValueError("Strata supports 1–4 GPUs, 64k/128k/192k/256k context and one request at a time")
    # Avoid occupied GPUs. The engine then sizes its expert cache to free VRAM.
    candidates = sorted((i for i, free in available if free >= 20000))
    if len(candidates) < count:
        raise ValueError(f"Need {count} GPUs with 20000 MiB free; available: {candidates}")
    ids = candidates[:count]
    config["gpu"] = ids if count > 1 else ids[0]
    if count > 1:
        config["layer_split"] = "auto"
    else:
        config.pop("layer_split", None)
    config["parallel"] = parallel
    config["host"] = "0.0.0.0"
    config["port"] = PORT
    config["api_key"] = key
    config["open_browser"] = False
    args = config["args"]
    args[args.index("--max-context") + 1] = str(context)
    # The shipped CJK subset contains almost no Cyrillic; retain English/code
    # and let the draft layer propose Russian tokens too. The target stays full.
    flag = "--mtp-draft-vocab"
    vocab = str(ROOT / "data/draft_vocab_cyrillic.bin")
    if flag in args:
        args[args.index(flag) + 1] = vocab
    else:
        args.extend([flag, vocab])
    config["draft_vocab"] = "cyrillic"
    state.update(gpus=count, ctx=context, parallel=parallel, gpu_ids=ids)
    return state, config

def main():
    state = json.loads(STATE.read_text()) if STATE.exists() else {}
    config = json.loads((ROOT / CONFIG).read_text())
    key = (Path("/srv/ai-hub/keys") / MID).read_text().strip()
    if len(key) < 32:
        raise ValueError("Missing private Strata API key")
    rows = subprocess.check_output(["nvidia-smi", "--query-gpu=index,memory.free", "--format=csv,noheader,nounits"], text=True)
    available = [tuple(map(int, line.split(","))) for line in rows.splitlines()]
    state, config = prepare_config(state, config, available, key)
    STATE.write_text(json.dumps(state, indent=2) + "\n")
    runtime = ROOT / (MID + ".runtime.json")
    # The runtime config contains the key; create it private before writing.
    fd = os.open(runtime, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.fchmod(fd, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(config, f, indent=2)
    print(f"Strata: GPUs {state['gpu_ids']}, ctx={state['ctx']}, parallel={state['parallel']}, port={PORT}", flush=True)
    python = str(ROOT / ".venv/bin/python")
    os.chdir(ROOT)
    os.execv(python, [python, str(ROOT / "serve/server.py"), "--engine", "strata", "--config", str(runtime), "--port", str(PORT)])

if __name__ == "__main__":
    main()
