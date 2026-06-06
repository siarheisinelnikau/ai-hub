"""Explicit Linux deployment using .env; never uploads local secrets."""
import argparse
import os
import re
import shlex
import subprocess
import sys
import uuid

from env import ROOT, env_or, load_env


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mcp", action="store_true", help="deploy the MCP binary")
    parser.add_argument("--dry-run", action="store_true", help="print the destination without building or connecting")
    args = parser.parse_args()
    load_env()
    host = os.environ.get("AIHUB_SERVER_IP")
    if not host or host.startswith("-") or not re.fullmatch(r"[A-Za-z0-9_.:-]+", host):
        raise ValueError("Set AIHUB_SERVER_IP in .env to the deployment host")
    user = env_or("AIHUB_SSH_USER", "user")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", user):
        raise ValueError("Invalid AIHUB_SSH_USER")
    port = int(env_or("AIHUB_SSH_PORT", "22"))
    if not 1 <= port <= 65535:
        raise ValueError("AIHUB_SSH_PORT must be between 1 and 65535")
    dest = env_or("AIHUB_REMOTE_DIR", "/srv/ai-hub")
    if not re.fullmatch(r"/[A-Za-z0-9_./-]+", dest) or dest == "/" or ".." in dest.split("/"):
        raise ValueError("AIHUB_REMOTE_DIR must be a specific absolute Linux directory without spaces")
    service = env_or("AIHUB_MCP_SERVICE" if args.mcp else "AIHUB_SERVICE", "ai-hub-mcp" if args.mcp else "ai-hub")
    if not re.fullmatch(r"[A-Za-z0-9_.@-]+", service) or service.startswith("-"):
        raise ValueError("Invalid service name")
    name = "mcp" if args.mcp else "aihub"
    print(f"Deploy {name} to {user}@{host}:{port}{dest}; service {service}", flush=True)
    if args.dry_run:
        return
    binary = ROOT / "bin" / f"{name}-linux"
    binary.parent.mkdir(exist_ok=True)
    env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
    subprocess.run(["go", "build", "-o", str(binary), f"./cmd/{name}"], cwd=ROOT, env=env, check=True)
    authority = f"[{host}]" if ":" in host else host
    target = f"{user}@{authority}"
    ssh = ["ssh", "-p", str(port), target]
    scp = ["scp", "-P", str(port)]
    stage = "/tmp/ai-hub-deploy-" + uuid.uuid4().hex
    q = shlex.quote
    subprocess.run([*ssh, f"mkdir -m 700 {q(stage)}"], check=True)
    try:
        subprocess.run([*scp, str(binary), f"{target}:{stage}/{name}"], check=True)
        if not args.mcp:
            subprocess.run([*scp, "-r", str(ROOT / "static"), f"{target}:{stage}/"], check=True)
        commands = [f"sudo install -m 755 {q(stage+'/'+name)} {q(dest+'/'+name)}"]
        if not args.mcp:
            commands += [f"sudo cp -a {q(stage+'/static/.')} {q(dest+'/static/')}"]
            # Fail before restart if a shipped asset is incomplete.
            commands += [f"cmp {q(stage+'/static/index.html')} {q(dest+'/static/index.html')}",
                         f"cmp {q(dest+'/static/css/main.css')} {q(dest+'/static/js/main.css.js')}"]
        commands += [f"sudo systemctl restart {q(service)}", f"systemctl is-active {q(service)}"]
        subprocess.run([*ssh, " && ".join(commands)], check=True)
    finally:
        # stage is a fixed /tmp prefix plus a UUID generated in this process.
        subprocess.run([*ssh, f"rm -rf -- {q(stage)}"], check=False)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"deploy: {error}", file=sys.stderr)
        sys.exit(1)
