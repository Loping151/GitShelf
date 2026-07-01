# Security Policy

## Reporting a vulnerability

Please report security issues privately via a GitHub Security Advisory on this
repository, or by email to the maintainer. Do not open public issues for
vulnerabilities. We aim to acknowledge reports within 72 hours.

## Security model

GitShelf is **read-only** by design:

- It never writes to, pushes to, or mutates the repositories it serves.
- All git invocations use argument arrays (no shell string interpolation).
- Paths and refs are validated; archive/raw filenames are sanitized.
- Markdown / SVG / all rendered HTML is passed through a strict allow-list
  sanitizer (bluemonday) and served under a strict Content-Security-Policy.
- The `raw` endpoint sets `X-Content-Type-Options: nosniff` and forces
  downloads for content that is unsafe to display inline.
- Optional auth stores only a bcrypt password hash; sessions are signed,
  http-only cookies.

## Supported versions

Until 1.0, only the latest release receives security fixes.
