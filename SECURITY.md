# Security policy

## Supported versions

Security fixes are provided for the latest release. Users should upgrade to
the newest available version before reporting an issue.

## Reporting a vulnerability

Do not open a public GitHub issue for a suspected vulnerability. Use GitHub's
private vulnerability reporting feature on the repository's Security page.
Include reproduction steps, affected versions, impact, and any suggested
mitigation. Remove credentials and private terminal output from the report.

If private vulnerability reporting is unavailable, contact the repository
maintainer through the private contact method listed on their GitHub profile.
Allow maintainers reasonable time to investigate and publish a fix before
public disclosure.

## Deployment guidance

webtmux exposes an interactive terminal and must be treated as privileged
remote access:

- Keep authentication enabled and use a strong, unique credential.
- Bind to localhost or a trusted private network by default.
- Use TLS directly or place webtmux behind a trusted TLS reverse proxy.
- Do not expose a session running with more privileges than necessary.
- Keep webtmux, tmux, the browser, and the host operating system updated.
