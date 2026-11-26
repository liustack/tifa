# Security Policy

## Reporting a vulnerability

Please report security issues privately, not in a public issue.

Use [GitHub Security Advisories](https://github.com/liustack/tifa/security/advisories/new) to open a private report. That keeps the details between you and the maintainer until a fix is out.

Include what a bug report needs: the request or command, the full output, the Tifa and Go versions, and the smallest app or route that shows it. We will acknowledge the report, work on a fix, and credit you unless you prefer otherwise.

## Supported versions

Fixes land in the latest release of github.com/liustack/tifa. Upgrade to it before reporting.

## What to keep in mind

Tifa renders React pages inside the server process, keeps sessions and remembers rendered pages. What Tifa guards by default and what is left to the app is in [docs/guides/security.md](docs/guides/security.md). How the session cookie works is in [docs/pages-and-data/sessions.md](docs/pages-and-data/sessions.md), the security headers and Content-Security-Policy are in [docs/build-and-deploy/config.md](docs/build-and-deploy/headers-and-origins.md#security-headers), and what the render memo keys on is in [docs/pages-and-data/ssr-and-cache.md](docs/pages-and-data/ssr-and-cache.md#ssr-and-cache). Read them before reporting behavior that may be working as intended.
