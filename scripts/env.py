"""Read literal .env values without shell evaluation. Shared by local launch/deploy."""
import json
import os
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parent.parent


def load_env():
    explicit = os.environ.get("AIHUB_ENV_FILE")
    if explicit == "-":
        return None
    candidates = [Path(explicit)] if explicit else [
        *([Path(os.environ["AIHUB"]) / ".env"] if os.environ.get("AIHUB") else []),
        ROOT / ".env",
    ]
    path = next((p for p in candidates if p.is_file()), None)
    if path is None:
        if explicit:
            raise ValueError("AIHUB_ENV_FILE does not exist")
        return ROOT / ".env"
    for number, line in enumerate(path.read_text(encoding="utf-8-sig").splitlines(), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        line = line.removeprefix("export ")
        key, separator, value = line.partition("=")
        key, value = key.strip(), value.strip()
        if not separator or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key):
            raise ValueError(f"Invalid .env assignment at line {number}")
        if value.startswith('"'):
            try:
                value = json.loads(value)
            except ValueError:
                raise ValueError(f"Invalid .env quote at line {number}") from None
        elif value.startswith("'"):
            if len(value) < 2 or not value.endswith("'"):
                raise ValueError(f"Invalid .env quote at line {number}")
            value = value[1:-1]
        else:
            value = value.split(" #", 1)[0].strip()
        os.environ.setdefault(key, value)
    return path


def env_or(key, fallback):
    return os.environ.get(key) or fallback
