"""Bundle only the tracked public checkout and both already-built Linux commands."""
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import tarfile

root = Path(__file__).resolve().parents[1]
source = (root / "internal/buildinfo/version.go").read_text(encoding="utf-8")
version = re.search(r'^const Version = "(\d+\.\d+\.\d+)"$', source, re.M).group(1)
if subprocess.check_output(["git", "status", "--porcelain"], cwd=root).strip():
    raise SystemExit("Release binaries must identify a clean public checkout")
prefix = f"ai-hub-v{version}"
tracked = subprocess.check_output(["git", "archive", "--format=tar", f"--prefix={prefix}/", "HEAD"], cwd=root)
directory = root / "dist"
directory.mkdir()
asset = directory / f"ai-hub-v{version}-linux-amd64.tar.gz"
with asset.open("wb") as stream, gzip.GzipFile(filename="", fileobj=stream, mode="wb", mtime=0) as compressed:
    with tarfile.open(fileobj=compressed, mode="w") as output:
        with tarfile.open(fileobj=io.BytesIO(tracked)) as archive:
            for item in archive:
                output.addfile(item, archive.extractfile(item) if item.isfile() else None)
        for name in ["aihub-linux", "mcp-linux"]:
            binary = Path(os.environ["RUNNER_TEMP"]) / name
            data = binary.read_bytes()
            if len(data) < 64 or data[:6] != b"\x7fELF\x02\x01":
                raise ValueError("Expected an ELF64 little-endian release command")
            info = tarfile.TarInfo(f"{prefix}/{name}")
            info.size, info.mode, info.mtime = len(data), 0o755, 0
            output.addfile(info, io.BytesIO(data))
digest = hashlib.sha256(asset.read_bytes()).hexdigest()
(directory / "SHA256SUMS.txt").write_text(f"{digest}  {asset.name}\n", encoding="utf-8", newline="\n")
