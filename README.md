# gostash

A small, self-hosted **read-it-later** article archiver — paste a URL, get a clean,
Instapaper-style readable copy saved forever on your own machine.

Built in Go. No external services, no tracking, no signup.

## Features

- **One-click archiving** — paste a URL, use the bookmarklet, or share to the
  installed PWA (Android share target)
- **Async archiving** — submissions queue instantly; the reader shows progress
  and a **Try again** button if a fetch fails
- **Clean reader view** — strips ads, nav, and clutter; extracts the article body;
  images are downloaded locally (content-addressed, garbage-collected) with a
  hotlink-busting image proxy as fallback
- **Full-text search** — SQLite FTS5 across title, author, excerpt, and body
- **Reading controls** (persisted per-browser): font family, size, line spacing,
  column width, Light/Sepia/Dark themes
- **Speed reader (RSVP)** — Spritz-style flashing words with focal-point (ORP)
  highlighting, adjustable WPM (150–900), smart punctuation pauses, and keyboard
  shortcuts (`Space` play/pause, `←/→` slower/faster, `Esc` close)
- **Read aloud** — natural device voices by default (Siri on iPhone, macOS
  system voices; no setup); optional Azure Neural upgrade for background
  playback (`READLATER_TTS_KEY`/`READLATER_TTS_REGION`)
- **Table of contents**, reading-progress bar, and per-browser resume-where-you-left-off
- **Reading list** with excerpts, archive dates, read/unread, and delete
- **Offline-friendly PWA** — visited articles stay readable without a network
- **SQLite storage** — everything in one file, no server to run

## Quick start

```bash
git clone https://github.com/ramuthumu/gostash.git
cd gostash
go run .
```

Then open <http://localhost:8090>.

### Configuration (env vars)

| Variable         | Default                | Description                         |
|------------------|------------------------|-------------------------------------|
| `READLATER_ADDR` | `127.0.0.1:8090`       | Address to listen on (loopback only by default; set to `:8090` to expose on all interfaces) |
| `READLATER_DATA` | `~/.readlater`         | Directory for the SQLite database and downloaded media |
| `READLATER_PUBLIC_URL` | _(empty)_            | Public base URL for the bookmarklet, e.g. `https://later.example.com` (use behind a reverse proxy) |

```bash
READLATER_ADDR=:9000 READLATER_DATA=./data go run .
```

### Archiving lifecycle

1. You submit a URL (form, bookmarklet, or share target). The article row is
   created immediately and the reader page polls until it's ready.
2. A background worker fetches the page with a normal browser User-Agent.
3. [go-readability](https://github.com/go-shiori/go-readability) (a Go port of
   Mozilla's Readability.js, the engine behind Firefox Reader View) extracts the
   title, byline, excerpt, and clean article HTML.
4. Images are downloaded into the media dir (SHA-256 content-addressed) and the
   article is stored in SQLite. URLs are canonicalized (fragment stripped, host
   lowercased) so the same page never archives twice; re-submitting a URL
   refreshes its content.
5. Failed fetches keep the error message on the article page with a retry button.

## The bookmarklet

On the reading list page (`/`), drag the **📑 Read Later** link to your browser's
bookmarks bar. While reading any article on the web, click it to archive the page
and jump straight to the saved copy.

## Deployment

- **Docker**: `docker build -t gostash .` — multi-stage, multi-arch
  (linux/amd64 + linux/arm64 images are published to GHCR by CI on tags)
- **systemd**: `scripts/deploy.sh user@host [--arch arm64]` cross-compiles,
  ships the binary, and installs `deploy/gostash.service`
- **HTTPS**: put Caddy/nginx in front (`deploy/Caddyfile.example`)

The binary reports its version at startup (`-ldflags "-X main.version=..."`,
stamped by CI and the deploy script).

## Project layout

```
gostash/
├── main.go                      # entry point, config, graceful shutdown
├── go.mod
└── internal/
    ├── db/db.go                 # SQLite store: articles, FTS5, lifecycle statuses
    ├── archive/
    │   ├── archive.go           # fetch + readability + SSRF-guarded dialer
    │   └── media.go             # local image download, media GC, URL canonicalization
    ├── srcset/srcset.go         # shared HTML-spec srcset tokenizer
    └── server/
        ├── server.go            # HTTP handlers, async archiving workers
        ├── templates/           # list.html, reader.html (go:embed)
        └── static/              # style.css, reader.js, sw.js (go:embed)
```

## Tech stack

| Layer              | Choice                                              |
|--------------------|-----------------------------------------------------|
| Language           | Go 1.25+                                            |
| HTTP / routing     | `net/http` (Go 1.22+ path patterns)                 |
| Content extraction | `github.com/go-shiori/go-readability`               |
| Storage            | SQLite via `modernc.org/sqlite` (pure Go, no CGO)   |
| Frontend           | Server-rendered HTML templates + vanilla JS/CSS     |
| Asset bundling     | `go:embed`                                          |

## Limitations / roadmap

- Heavily JS-rendered pages (SPAs) may not extract well — a headless browser
  (Chromedp) fallback would help.
- No tags/folders, no highlights/notes, no export/import yet.
- Reading position and display prefs are per-browser (localStorage).
- Single-user, no auth: bind to localhost or put behind an authenticated reverse
  proxy (see `deploy/`). POST mutations are guarded by a same-origin check, and
  the `/img` proxy blocks loopback/link-local/private addresses (SSRF).

## License

MIT
