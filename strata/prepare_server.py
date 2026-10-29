"""Stage the Strata service and private credentials on the ai-hub server."""
import json
import os
from pathlib import Path
import secrets
import pwd
import subprocess

MID = os.environ.get("AIHUB_MODEL_ID", "qwen38-flash-next-strata")
ROOT = Path("/srv/ai-hub")
STAGE = ROOT / "strata"
keys = ROOT / "keys"
user = pwd.getpwnam("user")
keys.mkdir(exist_ok=True)
os.chown(keys, user.pw_uid, user.pw_gid)
keys.chmod(0o700)
keyfile = keys / MID
if not keyfile.exists():
    fd = os.open(keyfile, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as f:
        f.write(secrets.token_urlsafe(48) + "\n")
keyfile.chmod(0o600)
os.chown(keyfile, user.pw_uid, user.pw_gid)
state = ROOT / "state" / (MID + ".json")
if not state.exists():
    state.write_text(json.dumps({"gpus":4,"ctx":262144,"parallel":1,"gpu_ids":[0,1,2,3]}, indent=2) + "\n")
os.chown(state, user.pw_uid, user.pw_gid)
for target in [Path("/usr/local/sbin") / ("ai-"+MID+".sh"), Path("/etc/systemd/system") / ("ai-"+MID+".service")]:
    source = STAGE / target.name
    target.write_bytes(source.read_bytes().replace(b"\r\n", b"\n"))
    target.chmod(0o755 if target.suffix == ".sh" else 0o644)
    assert target.read_bytes() == source.read_bytes().replace(b"\r\n", b"\n")
assert len(keyfile.read_text().strip()) >= 32
assert json.loads(state.read_text())["gpus"] in (1,2,3,4)
subprocess.run(["systemctl","daemon-reload"], check=True)
subprocess.run(["systemd-analyze","verify", "/etc/systemd/system/ai-"+MID+".service"], check=True)
print("Verified Strata unit, wrapper, launch state and private API key (not printed)")
