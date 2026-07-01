# Contributing to GitShelf

Thanks for your interest! GitShelf aims to be the best **read-only** Git
browsing layer — mature, single-binary, config-driven.

## Development

```bash
go build ./...
go test ./...
go run ./cmd/gitshelf -config gitshelf.toml
```

Requirements: Go 1.26+ and the `git` CLI on PATH.

## Architecture

Layered, with clean interfaces so implementations can be swapped:

- `internal/config`   — TOML config loading + validation.
- `internal/git`      — `Adapter` interface; CLI implementation (shells out to
  `git` with **argument arrays only**, never shell strings).
- `internal/render`   — `Registry` routing blobs to pluggable `Renderer`s.
- `internal/metadata` — `Provider` interface; `json-export` implementation.
- `internal/auth`     — optional auth with first-run setup + sessions.
- `internal/web`      — HTTP routing, templates, static assets.

## Guidelines

- Keep the product **generic**: no hard-coded paths, usernames, or tokens.
- Everything read-only — never write to or mutate user repositories.
- Sanitize all rendered HTML; keep the CSP strict.
- Add tests for new adapters, renderers, and providers.
- Conventional, descriptive commit messages.

## Adding a renderer

Implement `render.Renderer` and register it in `render.NewRegistry`. Renderers
are tried in order; return `CanRender(r) == true` for the file types you handle.
