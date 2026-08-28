# Public release checklist

## Versioned releases from the public repository

For subsequent releases, keep the existing public history. Update the shared
version in `internal/buildinfo/version.go`, the changelog and README release link.
Refresh `SOURCE_MANIFEST.json` using the canonical LF bytes of every published
file, excluding the manifest itself. Run `python scripts/check.py`, review the
diff and scan the history before committing. Require successful Linux/Windows CI.

Create an annotated `v<version>` tag on the reviewed commit. Build both commands
from that clean checkout with `CGO_ENABLED=0`, `GOOS=linux`, `GOARCH=amd64` and
`go build -trimpath -buildvcs=true`. Do not put build outputs inside the checkout
before the second build: untracked files can mark its revision as modified.
Verify `--version` on both binaries and distribute them with SHA-256 checksums
and release notes. Keep deployment credentials and local configuration private.

After deployment, both health endpoints must identify the tagged commit with
`modified: false`. Preserve a rollback snapshot and verify authenticated model
status, streaming and MCP access as described in [operations](operations.md).

## Creating the initial public snapshot

Use a curated snapshot when development history includes private operational
records. `scripts/export_public.py` copies reviewed manifest paths into a new
directory, excludes `.git` and secrets, and refuses to overwrite a destination.
The original checkout remains intact.

```sh
python scripts/export_public.py --output release/ai-hub-public
cd release/ai-hub-public
python scripts/check.py
```

Inspect the files, verify the MIT license and author identity, then create
a new repository with an English initial commit. Never copy the private `.git`,
add the old repository as a remote, or push old refs into the public repository.

```sh
git init -b main
git add .
git diff --cached --stat
git commit -m "feat: introduce the ai-hub control plane"
go run github.com/zricethezav/gitleaks/v8@v8.30.1 git . --redact --no-banner
```

Before publishing:

1. Include `LICENSE` (MIT) and review third-party notices.
2. Run all checks and secret-scan files and new Git history.
3. Verify a fresh checkout starts with private configuration and no models.
4. Validate installation, restart, rollback, lifecycle and streaming on a
   disposable Linux host with representative GPUs.
5. Review screenshots/docs for personal data and unsupported performance claims.
6. Enable private vulnerability reporting, branch protection and CI.
7. Publish a reviewed tag and only the curated history to the intended repository.

Rotate exposed credentials even when old commits are omitted. See GitHub's
[sensitive-data removal guide](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository).
A clean pattern scan is not proof that no secret or personal data exists.
