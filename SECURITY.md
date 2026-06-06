# Security policy

ai-hub is a privileged, single-administrator control plane. Registry files,
provider configuration, model repositories and launch wrappers are trusted
operator input. It is not a multi-tenant isolation boundary.

- Run a maintained Go toolchain and keep dependencies updated.
- Bind to loopback; use HTTPS plus a trusted network or access gateway.
- Keep direct inference endpoints and the Docker socket private.
- Use a random token; keep `.env`, tokens and backend keys outside Git.
- Set `AIHUB_SECURE_COOKIES=true` behind HTTPS termination.
- Restrict MCP file roots and run media/inference processes without panel privileges.
- Review model licenses, provenance and checksums before executing model software.

Management APIs require sessions. Inference and metrics also accept bearer
tokens. MCP HTTP requires a bearer token. Login attempts are limited by socket
peer IP, state changes use Go cross-origin protection, and sessions expire
after 30 days or restart. The hub token is stripped before forwarding inference
requests. These controls do not replace host hardening or a security review.

## Reporting

Use the repository host's private vulnerability reporting feature when enabled.
Otherwise request a private channel from a maintainer without posting exploit
details, credentials, prompts or private host information in a public issue.
Include affected versions, a minimal reproduction, impact and mitigation.

Only the current maintained release is eligible for fixes. Response times are
not guaranteed. Rotate exposed credentials immediately; deleting a file or
rewriting Git history does not revoke a credential.
