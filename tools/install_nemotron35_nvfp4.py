"""Download NVIDIA's pinned NVFP4 snapshot without a duplicate model cache."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

MANIFEST=Path(__file__).with_name('nemotron35-nvfp4-manifest.json')
DEST=Path('/home/user/models/Nemotron-3.5-Super-VL-120B-NVFP4')


def main():
    manifest=json.loads(MANIFEST.read_text())
    DEST.mkdir(parents=True,exist_ok=True)
    total=sum(x['size'] for x in manifest['files'])
    completed=sum(min(x['size'],(DEST/x['name']).stat().st_size) for x in manifest['files'] if (DEST/x['name']).is_file())
    partial=sum(p.stat().st_size for p in (DEST/'.cache/huggingface/download').glob('*.incomplete'))
    remaining=max(0,total-completed-partial)
    assert shutil.disk_usage(DEST).free>remaining+10*1024**3, 'Need remaining NVFP4 weights plus 10 GiB headroom'
    names=[x['name'] for x in manifest['files']]
    assert all(not Path(n).is_absolute() and '..' not in Path(n).parts for n in names)
    env=dict(os.environ,HF_HUB_DISABLE_XET='1',HF_HUB_DOWNLOAD_TIMEOUT='120')
    subprocess.run(['/usr/local/bin/hf','download',manifest['repo'],*names,'--revision',manifest['revision'],
                    '--local-dir',str(DEST),'--max-workers','4'],env=env,check=True)

    def verify(item):
        p=DEST/item['name'];assert p.stat().st_size==item['size'],item['name']
        if item.get('sha256'):
            h=hashlib.sha256()
            with p.open('rb') as f:
                for chunk in iter(lambda:f.read(16*1024**2),b''):h.update(chunk)
            assert h.hexdigest()==item['sha256'],item['name']
        print('Verified',item['name'],flush=True)
    with ThreadPoolExecutor(max_workers=2) as pool:
        list(pool.map(verify,manifest['files']))
    (DEST/'verified.json').write_text(json.dumps(manifest,indent=2)+'\n')
    print('NVFP4 SNAPSHOT VERIFIED',flush=True)


if __name__=='__main__':main()
