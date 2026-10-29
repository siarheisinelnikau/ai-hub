"""Create a curated, history-free source snapshot; never overwrite or publish."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
FORBIDDEN_NAMES = {".git", ".env", "token", "server_token.txt", "auth.json", "node_modules", "__pycache__"}
SECRET_PATTERNS = (
    re.compile(rb"-----BEGIN (?:RSA |OPENSSH |EC )?PRIVATE KEY-----"),
    re.compile(rb"(?:hf_[A-Za-z0-9]{25,}|sk-[A-Za-z0-9_-]{24,}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,})"),
)


def source_files():
    manifest = json.loads((ROOT / "scripts/public-manifest.json").read_text(encoding="utf-8"))
    selected = {}
    for pattern in manifest["include"]:
        matches = [path for path in ROOT.glob(pattern) if path.is_file()]
        if not matches:
            raise ValueError(f"Manifest pattern matched no files: {pattern}")
        for path in matches:
            relative = path.relative_to(ROOT)
            if path.is_symlink() or not path.resolve().is_relative_to(ROOT):
                raise ValueError(f"Symlink or path escape: {relative}")
            if FORBIDDEN_NAMES.intersection(relative.parts) or path.suffix.lower() in {".key", ".pem", ".exe", ".log", ".pyc"}:
                raise ValueError(f"Forbidden publication path: {relative}")
            data = path.read_bytes()
            if any(pattern.search(data) for pattern in SECRET_PATTERNS):
                raise ValueError(f"Potential credential in {relative}; inspect privately before export")
            destination = manifest.get("rename", {}).get(relative.as_posix(), relative.as_posix())
            if Path(destination).is_absolute() or ".." in Path(destination).parts:
                raise ValueError("Unsafe manifest destination")
            selected[destination] = path
    license_path = ROOT / "LICENSE"
    if license_path.is_file():
        selected["LICENSE"] = license_path
    return selected


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", default="release/ai-hub-public")
    parser.add_argument("--list", action="store_true", help="list selected paths without copying")
    args = parser.parse_args()
    files = source_files()
    if args.list:
        print("\n".join(sorted(files)))
        print(f"{len(files)} files selected", file=sys.stderr)
        return
    destination = Path(args.output).resolve()
    if destination.exists():
        raise ValueError("Destination already exists; choose a new empty path")
    destination.mkdir(parents=True)
    hashes = []
    for name, source in sorted(files.items()):
        target = destination / name
        target.parent.mkdir(parents=True, exist_ok=True)
        data = source.read_bytes()
        # Normalize source text; binary documentation screenshots retain their bytes.
        if source.suffix.lower() not in {".jpg", ".png", ".webp", ".ico"}:
            data = data.replace(b"\r\n", b"\n")
        target.write_bytes(data)
        if source.suffix == ".sh":
            target.chmod(0o755)
        hashes.append({"path": name, "sha256": hashlib.sha256(data).hexdigest()})
    (destination / "SOURCE_MANIFEST.json").write_text(json.dumps({"files": hashes}, indent=2) + "\n", encoding="utf-8")
    print(f"Exported {len(files)} files to {destination}")
    print("No Git history or remotes copied. Run validation and choose a license before publication.")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError) as error:
        print(f"Export failed: {error}", file=sys.stderr)
        sys.exit(1)
