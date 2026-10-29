"""Download the pinned Nemotron 3.5 Super VL Q4_K_M and verify every byte."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

REPO = 'lmstudio-community/NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-GGUF'
REVISION = '716398014e94528b38bf58871dbd6eb996bd7ad9'
DEST = Path('/home/user/models/Nemotron-3.5-Super-VL-120B-Q4_K_M')
FILES = {
    'NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Q4_K_M-00001-of-00002.gguf': (39730340416, '7e8ccf2ec6290679294b6940aa1f4e3e93808f714879446db5086e7eee0fb214'),
    'NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Q4_K_M-00002-of-00002.gguf': (30372951904, '6d256028e7feebe04bdf2d3bb423cc2528f060d2334f021917d2b4d73628ed02'),
    'mmproj-NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-BF16.gguf': (1733695872, 'a4c4bd40685fe77f224e1547189dacf8b120184ce93b1aa6a5a15730bed11d3c'),
}


def main():
    DEST.mkdir(parents=True, exist_ok=True)
    missing = sum(max(0, size - ((DEST/name).stat().st_size if (DEST/name).exists() else 0)) for name, (size, _) in FILES.items())
    # Local-dir downloads do not retain a second full copy in the HF cache.
    assert shutil.disk_usage(DEST).free > missing + 10 * 1024**3, 'Need weights plus 10 GiB free headroom'
    env = dict(os.environ, HF_HUB_DISABLE_XET='1', HF_HUB_DOWNLOAD_TIMEOUT='120')
    verified = {}
    for name, (size, digest) in FILES.items():
        print('Downloading', name, 'bytes', size, flush=True)
        for attempt in range(5):
            result = subprocess.run(['/usr/local/bin/hf', 'download', REPO, name, '--revision', REVISION, '--local-dir', str(DEST)], env=env)
            if result.returncode == 0:
                break
            if attempt == 4:
                raise RuntimeError('Download failed: ' + name)
            time.sleep(10)
        p = DEST/name
        assert p.stat().st_size == size, name
        sha = hashlib.sha256()
        with p.open('rb') as f:
            for block in iter(lambda: f.read(16 * 1024**2), b''):
                sha.update(block)
        assert sha.hexdigest() == digest, 'SHA256 mismatch: ' + name
        verified[name] = {'bytes': size, 'sha256': digest}
        print('Verified', name, digest, flush=True)
    manifest = {'repo': REPO, 'revision': REVISION, 'files': verified}
    (DEST/'verified.json').write_text(json.dumps(manifest, indent=2)+'\n')
    print('ALL THREE FILES VERIFIED', flush=True)


if __name__ == '__main__':
    main()
