"""Download pinned Qwen 122B weights, merge the MTP donor, and audit every tensor.

Invoked by Model manager. The donor is a separate repository, not a second
full target model. Input files are removed only after the merged output passes
metadata, shape, quantization and tensor-byte checks.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

GGUF_LIB = '/srv/ai/strata/third_party/llama.cpp/gguf-py'
sys.path.insert(0, GGUF_LIB)
import gguf

ROOT = Path('/home/user/models/Qwen3.5-122B-A10B-Uncensored-HauhauCS')
BASE = 'Qwen3.5-122B-A10B-Uncensored-HauhauCS-Aggressive-Q4_K_M.gguf'
DONOR = 'Qwen3.5-122B-A10B-MTP-Donor-Q4_K_M.gguf'
OUTPUT = BASE.replace('.gguf', '-MTP.gguf')
PROJ = 'mmproj-Qwen3.5-122B-A10B-Uncensored-HauhauCS-Aggressive-f16.gguf'
SOURCES = [
    ('HauhauCS/Qwen3.5-122B-A10B-Uncensored-HauhauCS-Aggressive', 'f4cb0b62dd608c596f0edacd200363676a826abc', BASE, 74208240224, 'ee482a86498a6815ba5cbf1f9fb354d3298d578b64f585f36d619f4f2b50550e'),
    ('LaikaFramework/Qwen3.5-122B-A10B-MTP-Donor', '0afcb6a3441b5dbd498acc81bfdc4fc4c0ef7e36', DONOR, 1629595264, '1507faebaf1c397ee4d77b8a6101180304e1d53df49417b0daf4056a913ae730'),
    ('HauhauCS/Qwen3.5-122B-A10B-Uncensored-HauhauCS-Aggressive', 'f4cb0b62dd608c596f0edacd200363676a826abc', PROJ, 908724288, '9d3c48d525143a639c5ca7067a142fee416f48d6e85adb4125dc1a21012ea0b6'),
]


def sha_file(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as f:
        while chunk := f.read(32 << 20):
            h.update(chunk)
    return h.hexdigest()


def sha_tensor(tensor):
    h = hashlib.sha256()
    data = memoryview(tensor.data).cast('B')
    for start in range(0, len(data), 32 << 20):
        h.update(data[start:start + (32 << 20)])
    return h.hexdigest()


def fetch_source(source):
    repo, revision, name, size, digest = source
    path = ROOT / name
    if path.exists() and path.stat().st_size == size:
        if sha_file(path) == digest:
            return
        path.unlink()  # a complete but corrupt source cannot be resumed
    remaining = max(0, size - (path.stat().st_size if path.exists() else 0))
    if shutil.disk_usage(ROOT).free < remaining + (1 << 30):
        raise RuntimeError('Insufficient disk space for ' + name)
    subprocess.run(['aria2c', '--continue=true', '--auto-file-renaming=false', '--allow-overwrite=true', '--file-allocation=none', '--max-connection-per-server=8', '--split=8', '--min-split-size=16M', '--dir', str(ROOT), '--out', name, f'https://huggingface.co/{repo}/resolve/{revision}/{name}'], check=True)
    assert path.stat().st_size == size and sha_file(path) == digest, name


def merge(base_path, donor_path, output_path):
    base, donor = gguf.GGUFReader(base_path), gguf.GGUFReader(donor_path)
    arch = base.get_field('general.architecture').contents()
    assert arch == 'qwen35moe' == donor.get_field('general.architecture').contents()
    blocks = base.get_field(arch + '.block_count').contents()
    assert blocks == 48
    assert donor.get_field(arch + '.block_count').contents() == 49
    assert donor.get_field(arch + '.nextn_predict_layers').contents() == 1
    assert len(donor.tensors) == 20
    assert all(t.name.startswith('blk.48.') for t in donor.tensors)
    tensors = list(base.tensors) + list(donor.tensors)
    assert len({t.name for t in tensors}) == len(tensors)
    writer = gguf.GGUFWriter(output_path, arch)
    overrides = {arch + '.block_count': 49, arch + '.nextn_predict_layers': 1}
    for field in base.fields.values():
        if field.name == 'general.architecture' or field.name.startswith('GGUF.'):
            continue
        kind = field.types[0]
        sub = field.types[-1] if kind == gguf.GGUFValueType.ARRAY else None
        writer.add_key_value(field.name, overrides.pop(field.name, field.contents()), kind, sub_type=sub)
    for key, value in overrides.items():
        writer.add_key_value(key, value, gguf.GGUFValueType.UINT32)
    for t in tensors:
        writer.add_tensor_info(t.name, t.data.shape, t.data.dtype, t.n_bytes, t.tensor_type)
    writer.write_header_to_file()
    writer.write_kv_data_to_file()
    writer.write_ti_data_to_file()
    for t in tensors:
        writer.write_tensor_data(t.data, tensor_endianess=base.endianess)
    writer.close()
    actual = gguf.GGUFReader(output_path)
    assert len(actual.tensors) == len(tensors)
    assert actual.get_field(arch + '.block_count').contents() == 49
    assert actual.get_field(arch + '.nextn_predict_layers').contents() == 1
    for key, field in base.fields.items():
        if key.startswith('GGUF.') or key in (arch + '.block_count', arch + '.nextn_predict_layers'):
            continue
        assert actual.get_field(key).contents() == field.contents(), key
    audit = []
    for source, copied in zip(tensors, actual.tensors):
        assert source.name == copied.name
        assert list(source.shape) == list(copied.shape), source.name
        assert source.tensor_type == copied.tensor_type, source.name
        assert source.n_bytes == copied.n_bytes, source.name
        original_hash = sha_tensor(source)
        assert original_hash == sha_tensor(copied), source.name
        audit.append({'name':source.name, 'bytes':source.n_bytes, 'sha256':original_hash})
    return audit


def main():
    ROOT.mkdir(parents=True, exist_ok=True)
    output = ROOT / OUTPUT
    marker = ROOT / 'mtp-merge-verified.json'
    if output.exists() and marker.exists():
        recorded = json.loads(marker.read_text())
        if sha_file(output) == recorded['sha256']:
            fetch_source(SOURCES[2])  # repair a missing projector without downloading 76 GB again
            print('Verified existing merged model and projector', flush=True)
            return
    # Merging writes a separate audited copy. Account for both inputs and output.
    temp = ROOT / (OUTPUT + '.merge-tmp')
    if temp.exists():
        temp.unlink()
    inputs = sum(s[3] for s in SOURCES)
    have = sum(min((ROOT / s[2]).stat().st_size, s[3]) for s in SOURCES if (ROOT / s[2]).exists())
    needed = inputs + SOURCES[0][3] + SOURCES[1][3] + (2 << 30) - have
    free = shutil.disk_usage(ROOT).free
    if free < needed:
        raise RuntimeError(f'Need {needed / 2**30:.1f} GiB free for download and audited MTP merge; available {free / 2**30:.1f} GiB. No weights downloaded.')
    for source in SOURCES:
        fetch_source(source)
    audit = merge(ROOT / BASE, ROOT / DONOR, temp)
    report = {'sources':SOURCES, 'tensor_count':len(audit), 'tensors':audit, 'size':temp.stat().st_size, 'sha256':sha_file(temp)}
    os.replace(temp, output)
    marker_tmp = marker.with_suffix('.json.tmp')
    marker_tmp.write_text(json.dumps(report, indent=2) + '\n')
    os.replace(marker_tmp, marker)
    for name in (BASE, DONOR):
        (ROOT / name).unlink()
    print(f'Verified merged checkpoint: {len(audit)} tensors; source inputs removed', flush=True)


if __name__ == '__main__':
    main()
