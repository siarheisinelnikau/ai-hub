"""Build and start the panel; configure a private login token on first launch."""
import getpass
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys

from env import ROOT, env_or, load_env


def main():
    os.chdir(ROOT)
    if not os.environ.get("AIHUB_ENV_FILE") and not (ROOT / ".env").exists():
        shutil.copyfile(ROOT / ".env.example", ROOT / ".env")
        (ROOT / ".env").chmod(0o600)
    path = load_env()
    root = Path(env_or("AIHUB", str(ROOT))).resolve()
    token_path = Path(env_or("AIHUB_TOKEN_FILE", "token"))
    if not token_path.is_absolute():
        token_path = root / token_path
    configured = os.environ.get("AIHUB_TOKEN", "")
    legacy = token_path.read_text(encoding="utf-8").strip() if token_path.is_file() else ""
    if not configured and (not legacy or legacy == "dev-token-123") and sys.stdin.isatty():
        token = getpass.getpass("Set ai-hub login token (Enter = generate): ")
        if token and getpass.getpass("Repeat token: ") != token:
            raise ValueError("Tokens do not match; nothing saved")
        if token == "dev-token-123" or token != token.strip():
            raise ValueError("Choose a private token without leading or trailing whitespace")
        token = token or secrets.token_urlsafe(24)
        # Preserve an explicitly empty process override for the Go process too.
        os.environ["AIHUB_TOKEN"] = token
        if path is not None:
            text = path.read_text(encoding="utf-8-sig") if path.exists() else ""
            assignment = "AIHUB_TOKEN=" + json.dumps(token, ensure_ascii=False)
            pattern = r"(?m)^(?:export )?AIHUB_TOKEN\s*=.*$"
            text = re.sub(pattern, lambda _: assignment, text) if re.search(pattern, text) else text.rstrip() + "\n" + assignment + "\n"
            path.write_text(text, encoding="utf-8", newline="\n")
            path.chmod(0o600)
            print(f"Login token saved to {path}. Use AIHUB_TOKEN to sign in.")
        else:
            token_path.write_text(token + "\n", encoding="utf-8")
            token_path.chmod(0o600)
            print(f"Login token saved to {token_path}.")
    binary = ROOT / "bin" / ("aihub.exe" if os.name == "nt" else "aihub")
    binary.parent.mkdir(exist_ok=True)
    subprocess.run(["go", "build", "-o", str(binary), "./cmd/aihub"], check=True)
    subprocess.run([str(binary)], check=True)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        pass
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"ai-hub: {error}", file=sys.stderr)
        sys.exit(1)
