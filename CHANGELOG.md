# Changelog

All notable changes are documented here. Format based on
[Keep a Changelog](https://keepachangelog.com/); versions follow
[Semantic Versioning](https://semver.org/).

## v0.1.1 — 2026-07-01

First public release.

### Added
- **Browse MVP**: config loading, directory discovery, repository list.
- In-place (zero-copy) reading of existing bare/mirror repositories.
- File tree / blob / raw / archive (zip, tar.gz) download.
- Render pipeline: code highlighting (Chroma), Markdown (GFM, sanitized),
  images, **PDF**, audio/video, CSV/TSV, JSON, plain text; pluggable registry.
- Branches/tags selector, commit history with pagination, commit diff,
  compare, blame, code search (git grep).
- **Metadata fusion** via pluggable provider; `json-export` provider for
  issues / pull requests / releases (list + detail, Markdown bodies,
  comment timelines, filtering).
- Auth **on by default** with a first-run setup wizard (bcrypt, sessions).
- Markdown: inline HTML rendering (GitHub-like, bluemonday-sanitized); relative
  image/link URLs resolve to the repo's raw/src routes; copy buttons on code
  blocks.
- Dark / light / auto theme, responsive layout, strict CSP and HTML
  sanitization throughout.

### Metadata compatibility
- Accept `labels` / `assignees` / `reviews` as either bare arrays or GraphQL
  connection objects (`{nodes:[…]}`), the shape produced by `gh api graphql`
  exports — otherwise issues/PRs silently failed to load.

### Security
- Git invocations reject revs/paths that look like options (leading `-`) or
  contain NUL bytes, preventing argument/option injection.
- Raw downloads stream from git (constant memory) and honor `max_raw_bytes`;
  `Content-Disposition` filenames are quote-escaped.
- Session cookies are marked `Secure` behind a TLS-terminating proxy
  (`X-Forwarded-Proto`); expired sessions are pruned.
