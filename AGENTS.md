# Contributor agent instructions

Write project prose, UI, comments and CLI output in English. Preserve original
languages in model output and Unicode tests. Keep credentials and machine
configuration in ignored local files.

Read files before and after editing. Run `python scripts/check.py`; add targeted
regression checks and use the Linux race detector when appropriate. Local work
does not authorize deployment or model actions on a real server.

For UI changes, keep `main.css` and `main.css.js` identical, bump changed asset
versions and inspect desktop/mobile rendering. For requested deployments,
verify installed content and served assets before claiming completion.

Keep registry/catalog, units, wrappers/state, clients and benchmarks consistent.
Deleting weights retains downloadable entries unless permanent removal was
requested. Report actual GPU counts and concurrency. Unit tests alone do not
establish hardware compatibility or production readiness.
