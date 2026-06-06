"""Reinstall pinned weights and prepare Maya; never starts or stops a model."""
import json
import os
from pathlib import Path
import runpy
import subprocess
import sys

ROOT = Path('/srv/ai/maya')
DATA = Path('/home/user/models/GLM-5.3-Flash-Maya-M-Derisked')
COMMIT = '451f9cb6a21ea65673bdde2e57b4a670ae2b8859'


def verified_weights_unchanged():
    """Reuse the completed SHA256 pass only while every verified file is unchanged."""
    source = runpy.run_path('/srv/ai-hub/tools/download_glm53_derisked.py')
    try:
        manifest = json.loads((DATA / 'verified.json').read_text())
        assert manifest['repo'] == source['REPO'] and manifest['revision'] == source['REVISION']
        assert set(manifest['files']) == set(source['FILES'])
        checked_at = int(manifest['verified_at'] * 1e9)
        for name, (size, digest) in source['FILES'].items():
            seen = manifest['files'][name]
            stat = (DATA / name).stat()
            assert seen['bytes'] == stat.st_size == size and seen['sha256'] == digest
            assert stat.st_mtime_ns <= checked_at and stat.st_ctime_ns <= checked_at
            if 'mtime_ns' in seen:
                assert stat.st_mtime_ns == seen['mtime_ns']
            seen.update(mtime_ns=stat.st_mtime_ns,
                repo=source['VISION_REPO'] if name.startswith('vision/') else source['REPO'],
                revision=source['VISION_REVISION'] if name.startswith('vision/') else source['REVISION'])
    except (OSError, ValueError, KeyError, AssertionError):
        return False
    (DATA / 'verified.json').write_text(json.dumps(manifest, indent=2) + '\n')
    (DATA / 'download-manifest.json').write_text(json.dumps({
        'repo':manifest['repo'],'revision':manifest['revision'],'files':manifest['files']},indent=2)+'\n')
    print('Reusing completed SHA256 checks: sizes, timestamps and pinned hashes unchanged',flush=True)
    return True


def remember_checks_for_setup():
    """Give Maya's own size/mtime cache the pinned SHA256 results already obtained."""
    manifest = json.loads((DATA / 'verified.json').read_text())
    books = {}
    for name, seen in manifest['files'].items():
        path = DATA / name
        stat = path.stat()
        assert stat.st_size == seen['bytes']
        assert stat.st_mtime_ns <= int(manifest['verified_at'] * 1e9)
        books.setdefault(path.parent, {})[path.name] = {'size':stat.st_size,'mtime_ns':stat.st_mtime_ns}
    for directory, checked in books.items():
        path = directory / '.maya-verified.json'
        book = json.loads(path.read_text()) if path.exists() else {}
        book.update(checked)
        temporary = path.with_suffix('.json.new')
        temporary.write_text(json.dumps(book, indent=2) + '\n')
        temporary.replace(path)
        assert json.loads(path.read_text()) == book


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
    if not verified_weights_unchanged():
        subprocess.run(['/usr/bin/python3', '/srv/ai-hub/tools/download_glm53_derisked.py'], check=True)
    remember_checks_for_setup()
    env = dict(os.environ, PATH='/usr/local/cuda-13.0/bin:/usr/local/bin:/usr/bin:/bin',
               CUDA_HOME='/usr/local/cuda-13.0', HF_HUB_DISABLE_XET='1',
               XDG_CONFIG_HOME=str(ROOT / 'setup-state'))
    subprocess.run([str(ROOT / 'setup.sh'), '--yes', '--plain', '--model', 'Maya-M-Derisked',
        '--gguf-dir', str(DATA), '--models-dir', str(DATA), '--context', '1048576',
        '--gpus', '0,1,2,3', '--parallel', '1', '--draft', 'mtp', '--drafting', 'on',
        '--port', '18038', '--host', '127.0.0.1', '--nvcc', '/usr/local/cuda-13.0/bin/nvcc',
        '--no-start'], cwd=ROOT, env=env, check=True)
    mark_ready()


def mark_ready():
    config = json.loads((ROOT / 'maya-maya-m-derisked.json').read_text())
    config['model_name'] = 'glm-5.3-flash-derisked'
    (ROOT / 'maya-maya-m-derisked.json').write_text(json.dumps(config, indent=2) + '\n')
    assert json.loads((ROOT / 'maya-maya-m-derisked.json').read_text())['model_name'] == 'glm-5.3-flash-derisked'
    manifest = json.loads((DATA / 'verified.json').read_text())
    for name, metadata in manifest['files'].items():
        assert (DATA / name).stat().st_size == metadata['bytes'], name
    for name in ['dense.bin', 'index.txt', 'tokenizer/vocab.json']:
        assert (DATA / 'pack' / name).is_file(), name
    assert config.get('vision'), 'Vision build did not complete'
    assert Path(config['exe']).is_file()
    marker = {'source_commit': COMMIT, 'config': str(ROOT / 'maya-maya-m-derisked.json'),
              'weights_manifest': str(DATA / 'verified.json'), 'vision': True}
    (DATA / 'runtime-ready.json').write_text(json.dumps(marker, indent=2) + '\n')
    print('Maya weights, engine, pack and vision are ready', flush=True)


if __name__ == '__main__':
    main()
