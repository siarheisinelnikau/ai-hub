# Changelog

## 0.1.0 — 2026-10-29

### Changed

- The panel listens on loopback by default. Inference and metrics require a
  hub bearer token or browser session. MCP HTTP requires a bearer token.
- Browser sessions expire on the server; login credentials are no longer saved
  in localStorage. Forwarded-IP headers cannot bypass login rate limits.
- Browser state changes use cross-origin protection. HTTP servers have header
  and idle timeouts while retaining long-running streamed responses.
- Fresh configuration selects empty registry/catalog examples. Custom YAML
  catalogs support portable paths and reject traversal in download filenames.
- Native systemd processes are supported independently of engine type.
- Minimum Go toolchain is 1.26.10. CI runs on Linux/Windows with pinned Actions,
  dependency updates, Linux race checks, vulnerability and secret scans.

### Added

- Shared release version and Git revision in `aihub --version`, `mcp --version`,
  panel `/healthz` and MCP `/health`; MCP initialization uses the release version.
- Real-server benchmark documentation distinguishing measured inference results
  from the synthetic data used to produce documentation screenshots.
- MIT license for the original project code and artwork.
- Authenticated Universal API credentials in OpenCode, pi and Open WebUI
  configuration generators, with separate remote-client token settings.
- English installation, model setup, operations, configuration, security,
  contribution and release documentation.
- Original local model-family illustrations, a first-run empty state, and
  desktop/mobile screenshots generated from synthetic preview data.
- A curated source exporter that omits private history, reports and credentials.
- Regression tests for authentication, CSRF, session expiry, upstream credential
  isolation, configuration, frontend behavior and a fresh executable installation.

### Upgrade

The seven reviewed Dependabot updates are included: FastAPI 0.162.0, Uvicorn
0.54.0, checkout 7.0.1, setup-go 7.0.0, setup-python 7.1.0, setup-node 7.1.0,
and upload-artifact 7.0.2. Actions remain pinned to full commit SHAs with a
read-only repository token. Python dependencies are for development/parity tests.

Update API/MCP/Prometheus credentials and HTTPS cookie configuration before
deploying. Existing host configuration and model services are not migrated by
the UI/binary deploy helper. See `docs/configuration.md` and `docs/operations.md`.
