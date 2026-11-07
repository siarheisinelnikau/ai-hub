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
| `AIHUB_PI_OUTPUT` | Optional pi models configuration path for generation/migration |
| `AIHUB_CLIENT_TOKEN`, `AIHUB_CLIENT_TOKEN_FILE` | Private hub credential for client generators; token overrides the file |

Explicit registry/catalog paths must exist and validate; relative paths resolve
against the installation root. Unset paths retain the historical registry and
built-in catalog, including their installation-specific paths and installers.
Only public links are exposed by `/runtime-config.js`; never put credentials
in those URLs. Internal provider URLs are trusted administrator configuration.

OpenCode/pi generators use one hub credential for Universal and every pinned model, read from
`AIHUB_CLIENT_TOKEN`, then `AIHUB_CLIENT_TOKEN_FILE` (default:
`~/.config/ai-hub/hub-api-key`), then `AIHUB_TOKEN`. Keep a remote server's
credential separate from a local development token. Generated client files
contain credentials and must remain private.

Clients use `/v1` with model `universal` to follow the active model, or
`/v1/model/<registry-profile-id>` with the existing upstream model ID to pin
a profile. Both support `/models` and `/chat/completions`; pinned discovery
requires the selected profile to be running. Streaming, tool calls, image
input and thinking parameters pass through unchanged. Pinning prevents
fallback to a different model. Unknown/non-LLM profiles return `404`, stopped
profiles `503`, and missing server-side native keys `502` with an explanation.

On another machine, Node.js 22+ client setup needs only the hub address and
hub token. Set `AIHUB_PUBLIC_URL` to the server's URL, and provide
`AIHUB_CLIENT_TOKEN` privately or place the token in
`~/.config/ai-hub/hub-api-key`. Set `AIHUB_ENV_FILE=-` to ignore checkout-local
server/development settings. No Strata/Maya key files are needed on clients.

```sh
node scripts/connect-clients.cjs  # migrate existing generated JSON configs
node gen-opencode.js             # create/replace OpenCode configuration
node strata/sync-pi.js            # create/merge pi providers
```

The migration changes only known AI-HUB provider URLs and credentials and
saves private backups beside the originals. Provider/model IDs, defaults,
reasoning settings and unrelated providers remain unchanged. Restart the
harness to reload its configuration. The output path variables above also
apply to migration. The API action on each model card shows its pinned URL,
model name and a command using `$AIHUB_TOKEN`.

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
