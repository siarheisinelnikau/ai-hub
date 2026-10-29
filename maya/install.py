"""Reinstall pinned weights and prepare Maya; never starts or stops a model."""
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path('/srv/ai/maya')
DATA = Path('/home/user/models/GLM-5.3-Flash-Maya-M')
COMMIT = '451f9cb6a21ea65673bdde2e57b4a670ae2b8859'


def main():
    # Model manager runs as root; keep HF/Python and generated files owned by user.
    if os.geteuid() == 0:
        os.execvp('sudo', ['sudo', '-H', '-u', 'user', '/usr/bin/python3', str(Path(__file__).resolve())])
    if not ROOT.exists():
        subprocess.run(['git', 'clone', 'https://github.com/mw00/project-maya.git', str(ROOT)], check=True)
        subprocess.run(['git', '-C', str(ROOT), 'checkout', COMMIT], check=True)
    head = subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip()
    if head != COMMIT:
        raise RuntimeError('Maya source changed; review before rebuilding: ' + head)
    source = ROOT / 'maya.py'
    text = source.read_text()
    old = 'jobs = max(2, min((os.cpu_count() or 4) // 2, int(mem_gb()[0] // 4) or 2))'
    if old in text:
        assert text.count(old) == 2
        source.write_text(text.replace(old, 'jobs = max(2, min(8, (os.cpu_count() or 4) // 2, int(mem_gb()[0] // 4) or 2))'))
    subprocess.run(['/usr/bin/python3', '/srv/ai-hub/tools/download_glm53_maya_m.py'], check=True)
    vision = DATA / 'Maya-M/vision'
    if not vision.exists():
        vision.symlink_to('../vision', target_is_directory=True)
    env = dict(os.environ, PATH='/usr/local/cuda-13.0/bin:/usr/local/bin:/usr/bin:/bin',
               CUDA_HOME='/usr/local/cuda-13.0', HF_HUB_DISABLE_XET='1',
               XDG_CONFIG_HOME=str(ROOT / 'setup-state'))
    subprocess.run([str(ROOT / 'setup.sh'), '--yes', '--plain', '--model', 'Maya-M',
        '--gguf-dir', str(DATA / 'Maya-M'), '--models-dir', str(DATA), '--context', '262144',
        '--gpus', '0,1,2,3', '--parallel', '1', '--draft', 'mtp', '--drafting', 'on',
        '--port', '18037', '--host', '127.0.0.1', '--nvcc', '/usr/local/cuda-13.0/bin/nvcc',
        '--no-start'], cwd=ROOT, env=env, check=True)
    mark_ready()


def mark_ready():
    config = json.loads((ROOT / 'maya-maya-m.json').read_text())
    manifest = json.loads((DATA / 'verified.json').read_text())
    for name, metadata in manifest['files'].items():
        assert (DATA / name).stat().st_size == metadata['bytes'], name
    for name in ['dense.bin', 'index.txt', 'tokenizer/vocab.json']:
        assert (DATA / 'Maya-M/pack' / name).is_file(), name
    assert config.get('vision'), 'Vision build did not complete'
    assert Path(config['exe']).is_file()
    marker = {'source_commit': COMMIT, 'config': str(ROOT / 'maya-maya-m.json'),
              'weights_manifest': str(DATA / 'verified.json'), 'vision': True}
    (DATA / 'runtime-ready.json').write_text(json.dumps(marker, indent=2) + '\n')
    print('Maya weights, engine, pack and vision are ready', flush=True)


if __name__ == '__main__':
    main()
