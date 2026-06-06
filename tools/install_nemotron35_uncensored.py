"""Download AtomicChat's pinned highest-quality uncensored build, verifying SHA256."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

DEST = Path('/home/user/models/Nemotron-3.5-Super-VL-120B-Uncensored-AD-Q4_K_S')
MANIFEST = Path(__file__).with_name('nemotron35-uncensored-manifest.json')


def main():
    manifest = json.loads(MANIFEST.read_text())
    DEST.mkdir(parents=True, exist_ok=True)
    partials = list((DEST/'.cache/huggingface/download').rglob('*.incomplete'))
    missing = 0
    for f in manifest['files']:
        p = DEST/f['path']
        downloaded = p.stat().st_size if p.exists() else max(
            (part.stat().st_size for part in partials if '.'+f['sha256']+'.' in part.name), default=0)
        missing += max(0, f['size']-downloaded)
    assert shutil.disk_usage(DEST).free > missing + 8 * 1024**3, 'Need weights plus 8 GiB free headroom'
    env = dict(os.environ, HF_HUB_DISABLE_XET='1', HF_HUB_DOWNLOAD_TIMEOUT='120', HF_HUB_DISABLE_PROGRESS_BARS='1')

    def download(f):
        name = f['path']
        print('Downloading', name, f['size'], flush=True)
        for attempt in range(5):
            result = subprocess.run(['/usr/local/bin/hf', 'download', manifest['repo'], name,
                '--revision', manifest['revision'], '--local-dir', str(DEST)], env=env)
            if result.returncode == 0:
                break
            if attempt == 4:
                raise RuntimeError('Download failed: '+name)
            time.sleep(5)
        p = DEST/name
        assert p.stat().st_size == f['size'], name
        digest = hashlib.sha256()
        with p.open('rb') as source:
            for block in iter(lambda: source.read(16 * 1024**2), b''):
                digest.update(block)
        assert digest.hexdigest() == f['sha256'], 'SHA256 mismatch: '+name
        print('Verified', name, f['sha256'], flush=True)
        return f

    with ThreadPoolExecutor(max_workers=4) as pool:
        verified = list(pool.map(download, manifest['files']))
    result = dict(manifest, files=verified, verified_at=time.time())
    temporary = DEST/'verified.json.tmp'
    temporary.write_text(json.dumps(result, indent=2)+'\n')
    temporary.replace(DEST/'verified.json')
    print('ALL', len(verified), 'FILES VERIFIED', flush=True)


if __name__ == '__main__':
    main()
