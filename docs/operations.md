# Linux installation and operations

Deployment model: one Linux host with systemd and a trusted administrator.
Docker is needed for container profiles. GPU containers need NVIDIA drivers
and Container Toolkit; native inference needs its own engine/runtime.

## Fresh installation

From a reviewed checkout, using Go 1.26.10 or newer:

```sh
go build -trimpath -o bin/aihub ./cmd/aihub
go build -trimpath -o bin/mcp ./cmd/mcp
sudo install -d -m 0755 /srv/ai-hub
sudo install -m 0755 bin/aihub /srv/ai-hub/aihub
sudo install -m 0755 bin/mcp /srv/ai-hub/mcp
sudo cp -a static examples /srv/ai-hub/
sudo install -m 0644 favicon.svg favicon_light.svg bg.svg /srv/ai-hub/
sudo install -m 0600 .env.example /srv/ai-hub/.env
sudo install -m 0644 ai-hub.service /etc/systemd/system/ai-hub.service
```

The configuration-copy commands are for a **new installation**. Preserve private
configuration and state on updates. Edit `/srv/ai-hub/.env`; keep the loopback
listener and select this host's URLs, registry and catalog. Adjust unit paths
if you install elsewhere.

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now ai-hub
sudo systemctl status ai-hub --no-pager
curl --fail http://127.0.0.1:8080/healthz
```

If no token was configured, retrieve `/srv/ai-hub/token` through your trusted
administrator session and store it privately. Then [connect a model](models.md).

The panel runs as root because the existing implementation controls
systemd/Docker and host diagnostics. Treat it as privileged administration.
Inference and MCP should use separate unprivileged service accounts.

## HTTPS access

Keep panel, MCP and direct backend ports on loopback or a firewalled network.
Put a TLS reverse proxy/access gateway in front. Example Caddy configuration
(replace the hostname and configure DNS):

```caddyfile
aihub.example.com {
    request_body {
        max_size 32MB
    }
    reverse_proxy 127.0.0.1:8080 {
        flush_interval -1
    }
}
```

Set `AIHUB_PUBLIC_URL=https://aihub.example.com` and
`AIHUB_SECURE_COOKIES=true`. Preserve Host, do not cache APIs or buffer SSE,
and apply rate/request-size limits at the gateway. The panel uses transport
peer IPs for login limits; arbitrary forwarded-IP headers are not trusted.
TLS on the panel does not protect an independently exposed inference port.

## Monitoring and rollback

`/healthz` is unauthenticated process liveness, not model readiness.
`/api/status` reports authenticated model health and snapshot freshness.
`/metrics` requires a bearer token or browser session. For Prometheus, use
`authorization.credentials_file` with a private token file. MCP `/health`
reports transport liveness. Missing optional sensors do not prove hardware health.

Inspect the installed release with `./aihub --version` and `./mcp --version`.
Both health endpoints also return `version`, `revision`, and `modified` fields.
Release builds from a clean Git checkout report the full commit and
`modified: false`; builds from source archives report `revision: "unknown"`.
These fields identify the deployed code, not the health of model engines.

Before updating, back up executables, static assets, units, registry/catalog,
and private configuration to an administrator-only location. Record engine
versions/image digests and run the checks. Preview the target with
`python scripts/deploy.py --dry-run`; deploy explicitly afterward. The helper
does not install model services, migrate state, or upload credentials.

After deployment verify served assets/cache versions, `/healthz`, authenticated
status, a model start/stop cycle, and streaming inference. Restore the saved
executables/UI and compatible configuration to roll back. Restart invalidates
browser sessions. Do not roll back to a known insecure configuration.

| Symptom | Check |
| --- | --- |
| Empty model list | First-run configuration is intentionally empty; set your registry |
| Missing service | Install its unit; `sysd` must omit `.service` |
| API 401 | Supply the hub bearer token, not the backend token |
| API 503 | Start a model; inspect backend `/health` and journal |
| Simulated GPU status | Windows can fall back to development fixtures; Linux polls hardware |
| Download error | Exact repository/file, disk space, `hf` PATH and gated access |
| Buffered streaming | Disable reverse-proxy response buffering |

```sh
sudo journalctl -u ai-hub -n 100 --no-pager
sudo journalctl -u ai-local-llm -n 100 --no-pager
```

Review logs before sharing; backend output can include prompts and credentials.
