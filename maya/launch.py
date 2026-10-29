"""Launch Maya with the GPU/context selection persisted by the hub."""
import json
import os
from pathlib import Path
import subprocess

MID = 'glm53-flash-maya-m'
ROOT = Path('/srv/ai/maya')
DATA = Path('/home/user/models/GLM-5.3-Flash-Maya-M')
STATE = Path('/srv/ai-hub/state') / (MID + '.json')


def prepare_config(state, config, available, key):
    count = int(state.get('gpus', 4))
    context = int(state.get('ctx', 262144))
    parallel = int(state.get('parallel', 1))
    if count not in (1, 2, 3, 4) or context not in (32768, 65536, 131072, 262144) or parallel != 1:
        raise ValueError('Maya profile supports 1-4 GPUs, 32k/64k/128k/256k and one request')
    ids = sorted(index for index, free in available if free >= 20000)[:count]
    if len(ids) != count:
        raise ValueError(f'Need {count} free GPUs (20000 MiB each); found {ids}')
    config.update(gpu=ids, host='0.0.0.0', port=18037, api_key=key, parallel=parallel, open_browser=False)
    config['layer_split'] = 'auto'
    args = config['args']
    for flag, value in [('--max-context', context), ('--parallel', parallel)]:
        if flag in args:
            args[args.index(flag) + 1] = str(value)
        else:
            args.extend([flag, str(value)])
    state.update(gpus=count, ctx=context, parallel=parallel, gpu_ids=ids)
    return state, config


def main():
    assert (DATA / 'runtime-ready.json').is_file(), 'Install Maya through Model manager first'
    state = json.loads(STATE.read_text()) if STATE.exists() else {}
    config = json.loads((ROOT / 'maya-maya-m.json').read_text())
    key = (Path('/srv/ai-hub/keys') / MID).read_text().strip()
    if len(key) < 32:
        raise ValueError('Missing private Maya API key')
    output = subprocess.check_output(['nvidia-smi', '--query-gpu=index,memory.free',
                                      '--format=csv,noheader,nounits'], text=True)
    available = [tuple(map(int, row.split(','))) for row in output.splitlines()]
    state, config = prepare_config(state, config, available, key)
    STATE.write_text(json.dumps(state, indent=2) + '\n')
    runtime = ROOT / (MID + '.runtime.json')
    fd = os.open(runtime, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.fchmod(fd, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(config, stream, indent=2)
    print(f"Maya: GPUs {state['gpu_ids']}, ctx={state['ctx']}, parallel=1, port=18037", flush=True)
    python = str(ROOT / '.venv/bin/python')
    os.chdir(ROOT)
    os.execv(python, [python, str(ROOT / 'serve/server.py'), '--engine', 'strata',
                     '--config', str(runtime), '--port', '18037'])


if __name__ == '__main__':
    main()
