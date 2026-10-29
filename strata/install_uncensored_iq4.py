"""Resume, verify and prepare uncensored weights using the Unsloth UD-IQ4_XS recipe."""
import hashlib
import fcntl
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import shutil
import subprocess
import sys
import time

ROOT = Path('/srv/ai/strata')
DATA = Path('/home/user/models/Strata-uncensored-iq4-data')
RESULTS = Path('/srv/ai-hub/strata/results')
MID = 'qwen38-flash-next-uncensored-strata'
REPO = 'Dyluhn/Qwen3.8-Flash-Next-Uncensored-R9V-IQ4_XS'
REVISION = '02829d67fb9b022e1024cefeceb2008658fb521d'
FILES = [
    ('Qwen3.8-Flash-Next-Uncensored-UD-IQ4_XS-00001-of-00003.gguf', 10945792, '7542a55ece3f5453f37944055bb2a15440b48686412ddeb94e31830049d72795'),
    ('Qwen3.8-Flash-Next-Uncensored-UD-IQ4_XS-00002-of-00003.gguf', 46907803200, '38f8b07593f17dbeefaf1289089c27059dd0b7157617194d0bcecc78ee129d1b'),
    ('Qwen3.8-Flash-Next-Uncensored-UD-IQ4_XS-00003-of-00003.gguf', 46763834432, '207b5369d8c50800f64fb5fcb27c4202d589472957a5347ee85e784ff4b8e71a'),
]
SIZE = sum(size for _, size, _ in FILES)
STATUS = RESULTS / 'uncensored-iq4-install.json'

def phase(name, **extra):
    STATUS.write_text(json.dumps({'stage': name, 'updated': time.time(), 'repo': REPO,
                                'revision': REVISION, 'total_bytes': SIZE, **extra}, indent=2) + '\n')
    print('Stage:', name, flush=True)

def main():
    DATA.mkdir(parents=True, exist_ok=True)
    models = DATA / 'models' / 'UD-IQ4_XS'
    models.mkdir(parents=True, exist_ok=True)
    present = sum((models / name).stat().st_size if (models / name).exists() else (models / (name + '.part')).stat().st_size if (models / (name + '.part')).exists() else 0 for name, _, _ in FILES)
    required = max(0, SIZE - present) + 5 * 1024**3
    assert shutil.disk_usage(DATA).free > required, 'Not enough free space for weights and prepared pack'
    shared = Path('/home/user/models/Strata-shared/mtp')
    assert (shared / 'rt').is_dir(), 'Shared MTP runtime is missing'
    if not (DATA / 'mtp').exists():
        (DATA / 'mtp').symlink_to(shared, target_is_directory=True)
    sys.path.insert(0, str(ROOT))
    import setup
    verified = []
    phase('downloading', paths=[str(models / name) for name, _, _ in FILES], files=[])
    def download(item):
        name, _, _ = item
        setup.download(f'https://huggingface.co/{REPO}/resolve/{REVISION}/target/{name}?download=true&aihub=uncensored-ud-iq4-full', models / name)
    with ThreadPoolExecutor(max_workers=2) as pool:
        list(pool.map(download, FILES))
    for name, size, expected_sha in FILES:
        target = models / name
        assert target.stat().st_size == size, 'Downloaded size mismatch: ' + name
        phase('verifying-sha256', path=str(target), files=verified)
        digest = hashlib.sha256()
        with target.open('rb') as stream:
            while block := stream.read(16 * 1024**2):
                digest.update(block)
        assert digest.hexdigest() == expected_sha, 'Downloaded checksum mismatch: ' + name
        verified.append({'file': str(target), 'bytes': size, 'sha256': expected_sha})
    phase('preparing-native-pack', files=verified)
    first = models / FILES[0][0]
    pack = DATA / 'packs' / 'dyluhn-ud-iq4_xs'
    subprocess.run([sys.executable, str(ROOT / 'tools/iq_pack.py'), '--gguf', str(first),
                    '--out', str(pack), '--compat-bf16'], cwd=ROOT, check=True)
    assert (pack / 'native_experts.txt').is_file() and (pack / 'tokenizer/vocab.json').is_file()
    sys.path.insert(0, str(ROOT / 'tools'))
    from gguf_reader import GGUFFile
    ple_shards = [models/name for name, _, _ in FILES
                  if any(t.name == 'per_layer_token_embd.weight' for t in GGUFFile(models/name).tensors)]
    assert len(ple_shards) == 1, 'Expected one PLE tensor across all target shards'
    ple = ple_shards[0]
    config = {'exe': str(ROOT / 'engine/strata'), 'cwd': str(ROOT),
              'args': ['--pack', str(pack), '--native', str(first), '--ple-gguf', str(ple),
                       '--expert-profile', str(ROOT / 'data/expert-profile.bin'), '--expert-cache', 'auto',
                       '--prefill', 'auto', '--spec', '4', '--spec-min-p', '0.5',
                       '--mtp', str(shared / 'rt'), '--max-context', '262144', '--kv', 'int8',
                       '--kv-resident', '32768', '--resident-budget-gib', '55'],
              'tokenizer': str(pack / 'tokenizer'), 'model_name': 'qwen3.8-flash-next-uncensored-ud-iq4_xs',
              'log': str(ROOT / (MID + '.log')), 'lib_dirs': ['/usr/local/cuda-13.0/bin', '/usr/local/cuda-13.0/lib64'],
              'host': '127.0.0.1', 'port': 18030, 'gpu': [0, 1, 2, 3], 'gpus_asked': True,
              'layer_split': 'auto', 'open_browser': False}
    config_path = ROOT / 'strata-dyluhn-uncensored-ud-iq4_xs.json'
    config_path.write_text(json.dumps(config, indent=2) + '\n')
    assert json.loads(config_path.read_text()) == config
    phase('prepared', files=verified, config=str(config_path), pack=str(pack), quant='UD-IQ4_XS', ple_gguf=str(ple))
    verification = {'verified_at': time.time(), 'revision': REVISION, 'repo': REPO, 'files': verified}
    (Path('/srv/ai-hub/strata') / 'weights-verified-uncensored-iq4.json').write_text(json.dumps(verification, indent=2) + '\n')

if __name__ == '__main__':
    lock = Path('/srv/ai/strata/uncensored-iq4-install.lock').open('a')
    fcntl.flock(lock, fcntl.LOCK_EX)
    try:
        main()
    except BaseException as error:
        phase('error', error=str(error))
        raise
