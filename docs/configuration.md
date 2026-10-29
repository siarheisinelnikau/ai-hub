# Configuration

Copy `.env.example` to `.env`; keep the real file private. Values are literal:
shell commands and variable expansion are never evaluated. Process environment
variables override the file, including explicitly empty values.

Go loads the first available file: explicit `AIHUB_ENV_FILE`, `AIHUB/.env`, the
current directory's `.env`, or `.env` beside the executable. Set
`AIHUB_ENV_FILE=-` to disable loading. Scripts resolve the checkout's `.env`.
Restart after changes.

| Variable | Default / purpose |
| --- | --- |
| `AIHUB` | Installation root; current directory with registry or executable directory |
| `AIHUB_HOST`, `AIHUB_PORT` | `127.0.0.1`, `8080` |
| `AIHUB_TOKEN`, `AIHUB_TOKEN_FILE` | Private token, or file (default `token`, relative to root) |
| `AIHUB_SECURE_COOKIES` | `false`; set `true` behind an HTTPS reverse proxy |
| `AIHUB_ALLOW_UNAUTHENTICATED_API` | `false`; legacy opt-out for `/v1/*` and `/metrics` only |
| `AIHUB_REGISTRY`, `AIHUB_CATALOG` | First-run `.env` selects empty YAML files in `examples/` |
| `AIHUB_SERVER_IP` | Hostname/IP for direct service links and deployment |
| `AIHUB_PUBLIC_URL` | Full browser-facing hub URL; empty uses page origin |
| `AIHUB_WEBUI_URL`, `AIHUB_MCP_URL` | Full external URLs; otherwise derived from server/ports |
| `AIHUB_CASE_URL` | Optional full URL for a hardware visualization; empty hides it |
| `AIHUB_STATE_DIR` | Launch selections; default `/srv/ai-hub/state` |
| `AIHUB_DOWNLOADS_DIR` | Download manifest/logs; default `/srv/ai-hub/downloads` |
| `AIHUB_KEYS_DIR` | Native backend keys; default `/srv/ai-hub/keys` |
| `AIHUB_CLOUD_CONFIG` | Private cloud provider configuration file |
| `AIHUB_WEBHOOKS_FILE`, `AIHUB_WEBHOOK_URLS` | Private notification settings |
| `AIHUB_COMFYUI_URL`, `AIHUB_VOICE_URL` | Defaults: localhost ports 8188 and 8010 |
| `AIHUB_MUSIC_URL`, `AIHUB_THREED_URL` | Defaults: localhost ports 8017 and 8020 |
| `AIHUB_MCP_LISTEN` | Empty = stdio; e.g. `127.0.0.1:8090` = HTTP |
| `AIHUB_MCP_CORS` | Empty denies browser origins; otherwise explicit comma-separated allowlist |
| `AIHUB_MCP_OUTDIR`, `AIHUB_MCP_THREED_OUTDIR` | Output and 3D fallback directories |
| `HF_TOKEN`, `AIHUB_DOWNLOAD_CMD` | Hugging Face credential and optional `hf` executable path |
| `AIHUB_SSH_USER`, `AIHUB_SSH_PORT` | Deployment account and SSH port |
| `AIHUB_REMOTE_DIR`, `AIHUB_SERVICE`, `AIHUB_MCP_SERVICE` | Deployment root and units |
| `AIHUB_OPENCODE_OUTPUT` | Output path for the legacy OpenCode generator |

Explicit registry/catalog paths must exist and validate; relative paths resolve
against the installation root. Unset paths retain the historical registry and
built-in catalog, including their installation-specific paths and installers.
Only public links are exposed by `/runtime-config.js`; never put credentials
in those URLs. Internal provider URLs are trusted administrator configuration.

## MCP file access

Run MCP under a dedicated unprivileged account and restrict accessible files:

```sh
./bin/mcp -http 127.0.0.1:8090 -outdir /srv/ai/artifacts \
  -read-roots /srv/ai/artifacts,/srv/ai/inputs
```

Do not allow directories containing credentials. CORS is independent of bearer
authentication; the API compatibility opt-out does not disable MCP auth.

## Upgrade notes

New defaults are loopback binding, authenticated inference/metrics/MCP HTTP,
and no MCP browser origins. Update clients and monitoring to send the hub
token before deploying. Set secure cookies behind HTTPS. Existing `.env` values
are preserved. Old localStorage login tokens are removed at the next login
screen. Sessions expire after 30 days and are invalidated by a process restart.
