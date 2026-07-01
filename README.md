<div align="center">

<img src="docs/img/logo.png" alt="GitShelf" width="110">

# GitShelf

**English** · [简体中文](README.zh-CN.md)

A self-hosted, read-only Git browser. Point it at a folder of bare/mirror
repos and read them like GitHub — no import, no copy.

<img src="docs/img/teaser.png" alt="GitShelf" width="100%">

[Features](#features) · [Quick start](#quick-start) · [Config](#configuration) · [Screenshots](#screenshots)

</div>

Typical use: browsing your nightly **GitHub mirror backups** (`git clone --mirror`) offline.

GitShelf reads existing `*.git` mirrors in place and gives you file trees,
rich previews (code, Markdown, PDF, images, audio/video, CSV, JSON), commit
history and diffs, blame, code search, and — if you export them — the
issues, PRs and releases that go with the code. It's deliberately read-only:
no pushing, no CI, no issue editing. Just the browsing layer.

## Why

cgit is bare-bones; Gitea and GitLab are heavy and insist on importing repos
into their own storage first. GitShelf sits in between — it reads what's
already on disk.

| | Gitea / Forgejo | cgit | GitLab CE | GitShelf |
|---|---|---|---|---|
| Reads existing bare repos in place | needs import | yes | needs import | **yes** |
| File previews (code, PDF, images…) | good | minimal | great | **good** |
| Shows exported issues / PRs | no | no | no | **yes** |
| Footprint | low | tiny | heavy | **single binary** |
| Scope | full forge | minimal | full forge | **read-only** |

## Features

- Reads your existing bare/mirror repos directly — nothing is imported or duplicated.
- File previews: syntax highlighting (Chroma), GitHub-flavored Markdown, images, PDF, audio/video, CSV/TSV, JSON. Renderers are pluggable.
- Branch/tag switching, commit history, diffs, compare, blame, `git grep` search, zip/tar.gz downloads.
- Optional issues / PRs / releases from exported JSON, shown next to the code.
- Auth is on by default — the first visit walks you through creating an admin account.
- Dark / light / auto theme, responsive, keyboard-friendly.
- One static binary, config-driven, binds `127.0.0.1` by default, no database.

## Quick start

Grab a prebuilt binary for your platform from the
[Releases](https://github.com/Loping151/GitShelf/releases) page (no Go needed —
just `git` on PATH):

```bash
cp gitshelf.example.toml gitshelf.toml   # then edit repo_source.path
./gitshelf -config gitshelf.toml         # http://127.0.0.1:8888
```

Or build from source (Go 1.26+):

```bash
go build -o gitshelf ./cmd/gitshelf
./gitshelf -config gitshelf.toml
```

Or Docker:

```bash
docker run -p 8888:8888 \
  -v /path/to/mirrors:/mirrors:ro \
  -v /path/to/gitshelf.toml:/etc/gitshelf.toml:ro \
  ghcr.io/loping151/gitshelf
```

A repo source is just a directory of bare repos:

```
/path/to/mirrors/
  project-a.git/      # from: git clone --mirror <url>
  project-b.git/
```

## Configuration

One TOML file drives everything; there are no hard-coded paths. Full reference
in [`gitshelf.example.toml`](gitshelf.example.toml).

```toml
[server]
bind = "127.0.0.1:8888"   # 0.0.0.0:8888 to expose on the LAN
theme = "auto"

[[repo_source]]
path = "/path/to/mirrors"
glob = "*.git"
namespace = "flat"        # flat: /<repo>   owner: /<owner>/<repo>

[metadata]                # optional
provider = "json-export"
path = "/path/to/meta"

[auth]
enabled = true            # first visit creates the admin account
```

**Auth** is enabled by default. On first launch GitShelf sends you to a one-time
`/setup` page to pick an admin username and password (stored as a bcrypt hash —
never in the config). Turn it off only behind a trusted reverse proxy or for a
purely local instance.

The metadata JSON layout is one folder per repo (`<meta>/<repo>/{summary.json,
issues/<n>.json, prs/<n>.json, releases/<tag>.json}`). Missing fields and files
are tolerated — a repo with no metadata just shows no issues tab.

## Screenshots

Browsing the bundled demo repo, dark and light:

<img src="docs/screenshots/02-repo-dark.png" width="49%"> <img src="docs/screenshots/02-repo-light.png" width="49%">

| Code | Commit diff |
|---|---|
| ![code](docs/screenshots/03-code-dark.png) | ![commit](docs/screenshots/05-commit-dark.png) |
| Issue timeline | CSV as a table |
| ![issue](docs/screenshots/06-issue-dark.png) | ![csv](docs/screenshots/07-csv-dark.png) |

## URLs

```
/                              repo list
/<repo>                        repo home (README + tree)
/<repo>/src/<rev>/<path>       tree or file
/<repo>/raw/<rev>/<path>       raw file
/<repo>/archive/<rev>.<fmt>    zip / tar.gz
/<repo>/commits/<rev>          history      /commit/<sha>   diff
/<repo>/compare/<a>...<b>      compare      /blame/<rev>/<path>
/<repo>/branches  /tags  /search?q=
/<repo>/issues[/<n>]  /pulls[/<n>]  /releases[/<tag>]
```

## Architecture

```
web        routing · templates · static · auth · CSP
 ├─ git        Adapter interface → git CLI (argument arrays, never shell)
 ├─ render     Registry → pluggable Renderer{code, markdown, image, pdf, …}
 ├─ metadata   Provider interface → json-export
 └─ config     TOML load + validation
```

The git layer shells out to `git`, so any mirror/bare/worktree works; a
`go-git` backend can slot in behind the same interface later. See
[CONTRIBUTING.md](CONTRIBUTING.md).

## Development

```bash
go test ./...
go vet ./...
make build
```

## License

[MIT](LICENSE) © 2026 Loping151
