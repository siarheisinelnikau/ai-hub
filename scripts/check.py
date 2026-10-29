"""Run the same local and CI validation without changing the Python environment."""
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]


def run(*args):
    print("+ " + " ".join(args), flush=True)
    subprocess.run(args, cwd=ROOT, env=dict(os.environ, AIHUB_ENV_FILE="-"), check=True)


def main():
    run("go", "vet", "./...")
    run("go", "build", "./...")
    run("go", "test", "-count=1", "-timeout", "120s", "./...")
    run(sys.executable, "-m", "unittest", "discover", "-s", "tests", "-p", "test_*.py")
    for path in sorted((ROOT / "static" / "js").glob("*.js")):
        if path.name != "main.css.js":
            run("node", "--check", str(path))
    run("node", "--test", "tests/frontend.test.cjs")
    if (ROOT / "static/css/main.css").read_bytes() != (ROOT / "static/js/main.css.js").read_bytes():
        raise ValueError("main.css and main.css.js must be identical")
    run(sys.executable, "tests/parity/run_parity.py")
    print("All checks passed.")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"Validation failed: {error}", file=sys.stderr)
        sys.exit(1)
