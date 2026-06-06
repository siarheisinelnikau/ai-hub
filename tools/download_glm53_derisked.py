"""Download pinned Maya-M-Derisked and reuse verified vision files; retain disk headroom."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

REPO = 'peasantsmith/GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-GGUF'
VISION_REPO = 'peasantsmith/GLM-5.3-Flash-Maya-GGUF'
VISION_REVISION = 'f16ee4ba50d8fd2bb726ea1862ca6ca14ba188da'
REVISION = 'b0a5474568ab4322ba4b74be5f34308fb4592d57'
DEST = Path('/home/user/models/GLM-5.3-Flash-Maya-M-Derisked')
RESERVE = 10 * 1024**3
FILES = {
    'GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-00001-of-00003.gguf':
        (47678327584, 'da6a9755946f85a68aed34e80e86a7cf942f4ca637e605be81481979c871591e'),
    'GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-00002-of-00003.gguf':
        (47820244896, 'afc0705798bb293495168889a63186193fcd84e271aea3cde53c4814e9c96449'),
    'GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-00003-of-00003.gguf':
        (20504990688, 'e397c79669b84e8f54b7c946def0ea94f59771eecd7575766a3f70abfdd12157'),
    'vision/GLM-5.3-Flash-vocab.gguf':
        (9426880, '8f53cb1bd2e631c14ef413e3284735d9e53f3c508d07a6f609e705b487105912'),
    'vision/mmproj-GLM-5.3-Flash-F16.gguf':
        (1128046944, '3627575df16bd152db0f3fd7e488d270b33f3a9e6c7fa3b1b8ac381faafde882'),
}


def write_json(name, value):
    target = DEST / name
    temp = target.with_suffix(target.suffix + '.tmp')
    temp.write_text(json.dumps(value, indent=2) + '\n')
    temp.replace(target)


def progress(stage, **extra):
    # HF local-dir downloads keep partial bytes here, with no second full cache copy.
    paths = [DEST / name for name in FILES]
    cache = DEST / '.cache/huggingface/download'
    paths.extend(cache.rglob('*.incomplete') if cache.exists() else [])
    downloaded = 0
    for path in paths:
        try:
            downloaded += path.stat().st_size
        except FileNotFoundError:
            pass  # HF may atomically move an incomplete file during this snapshot.
    write_json('download-status.json', {
        'stage': stage, 'updated': time.time(), 'pid': os.getpid(),
        'repo': REPO, 'revision': REVISION, 'destination': str(DEST),
        'total_bytes': sum(size for size, _ in FILES.values()),
        'downloaded_bytes': downloaded, 'free_bytes': shutil.disk_usage(DEST).free,
        'reserve_bytes': RESERVE, **extra,
    })


def wait_for_space(name):
    while shutil.disk_usage(DEST).free < RESERVE + 1024**3:
        progress('waiting-for-space', file=name)
        time.sleep(15)


def main():
    DEST.mkdir(parents=True, exist_ok=True)
    lock = (DEST / '.download.lock').open('a')
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    cli = shutil.which('hf')
    if not cli:
        raise RuntimeError('hf CLI is not installed')
    write_json('download-manifest.json', {
        'repo': REPO, 'revision': REVISION,
        'files': {name: {'bytes': size, 'sha256': sha,
            'repo': VISION_REPO if name.startswith('vision/') else REPO,
            'revision': VISION_REVISION if name.startswith('vision/') else REVISION}
            for name, (size, sha) in FILES.items()},
    })
    env = dict(os.environ, HF_HUB_DISABLE_XET='1', HF_HUB_DOWNLOAD_TIMEOUT='120',
               HF_HUB_DISABLE_PROGRESS_BARS='1', HF_HUB_DISABLE_TELEMETRY='1')
    verified = {}
    for name, (size, expected) in FILES.items():
        path = DEST / name
        if name.startswith('vision/') and not path.exists():
            shared = Path('/home/user/models/GLM-5.3-Flash-Maya-M') / name
            if shared.is_file() and shared.stat().st_size == size:
                path.parent.mkdir(parents=True, exist_ok=True)
                os.link(shared, path)
        repo = VISION_REPO if name.startswith('vision/') else REPO
        revision = VISION_REVISION if name.startswith('vision/') else REVISION
        attempt = 0
        while not path.exists() or path.stat().st_size != size:
            wait_for_space(name)
            attempt += 1
            print('Downloading', name, 'attempt', attempt, flush=True)
            with (DEST / 'download-hf.log').open('a') as log:
                child = subprocess.Popen(
                    [cli, 'download', repo, name, '--revision', revision,
                     '--local-dir', str(DEST)], env=env, stdout=log, stderr=subprocess.STDOUT)
                try:
                    while child.poll() is None:
                        progress('downloading', file=name, attempt=attempt)
                        if shutil.disk_usage(DEST).free < RESERVE:
                            child.terminate()
                            try:
                                child.wait(timeout=15)
                            except subprocess.TimeoutExpired:
                                child.kill()
                                child.wait()
                            break
                        time.sleep(5)
                finally:
                    if child.poll() is None:
                        child.terminate()
                        try:
                            child.wait(timeout=15)
                        except subprocess.TimeoutExpired:
                            child.kill()
                            child.wait()
            if child.returncode != 0:
                progress('retrying', file=name, attempt=attempt, returncode=child.returncode)
                time.sleep(30)
        progress('verifying-sha256', file=name)
        sha = hashlib.sha256()
        with path.open('rb') as stream:
            for block in iter(lambda: stream.read(16 * 1024**2), b''):
                sha.update(block)
        if sha.hexdigest() != expected:
            raise RuntimeError('SHA256 mismatch; retained for inspection: ' + name)
        verified[name] = {'bytes': size, 'sha256': expected, 'repo': repo, 'revision': revision,
                          'mtime_ns': path.stat().st_mtime_ns}
        print('Verified', name, expected, flush=True)
    write_json('verified.json', {
        'repo': REPO, 'revision': REVISION, 'verified_at': time.time(), 'files': verified,
    })
    progress('complete', verified_files=len(verified))
    print('ALL FIVE FILES SHA256 VERIFIED', flush=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        if DEST.exists():
            progress('error', error=str(error))
        raise
