# gostash

A small, self-hosted **read-it-later** article archiver — paste a URL, get a clean,
Instapaper-style readable copy saved forever on your own machine.

Built in Go. No external services, no tracking, no signup.

## Features

- **One-click archiving** — paste a URL or use the bookmarklet
- **Clean reader view** — strips ads, nav, and clutter; extracts the article body
- **Reading controls** (persisted per-browser):
  - Font family: Serif / Iowan-Palatino / Sans / Mono
  - Font size, line spacing
  - Themes: Light / Sepia / Dark
- **Speed reader (RSVP)** — Spritz-style flashing words with focal-point (ORP)
  highlighting, adjustable WPM (150–900), smart punctuation pauses, and keyboard
  shortcuts (`Space` play/pause, `←/→` slower/faster, `Esc` close)
- **Reading list** with excerpts, archive dates, read/unread, and delete
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
| `READLATER_DATA` | `~/.readlater`         | Directory for the SQLite database   |
| `READLATER_PUBLIC_URL` | _(empty)_            | Public base URL for the bookmarklet, e.g. `https://later.example.com` (use behind a reverse proxy) |

```bash
READLATER_ADDR=:9000 READLATER_DATA=./data go run .
```

## The bookmarklet

On the reading list page (`/`), drag the **📑 Read Later** link to your browser's
bookmarks bar. While reading any article on the web, click it to archive the page
and jump straight to the saved copy.

## How it works

1. You submit a URL (form, or `/save?url=...` from the bookmarklet).
2. The backend fetches the page with a normal browser User-Agent.
3. [go-readability](https://github.com/go-shiori/go-readability) (a Go port of
   Mozilla's Readability.js, the engine behind Firefox Reader View) parses the
   HTML and extracts the title, byline, excerpt, and clean article HTML.
4. The article is stored in SQLite (URL is unique, re-archiving refreshes it).
5. The reader view renders the stored HTML with a calm stylesheet; font/size/
   spacing/theme prefs live in `localStorage`, and the speed reader streams words
   from the rendered article text.

## Project layout

```
gostash/
├── main.go                      # entry point, config, wiring
├── go.mod
├── internal/
│   ├── db/db.go                 # SQLite store (articles table)
│   ├── archive/archive.go       # fetch + readability extraction
│   └── server/
│       ├── server.go            # HTTP handlers (Go 1.22+ routing)
│       ├── templates/           # list.html, reader.html (go:embed)
│       └── static/              # style.css, reader.js (go:embed)
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
- No full-text search yet (easy to add with SQLite FTS5).
- No tags/folders, no per-article reading position, no auth (single-user, local).
- Single-user, no auth: bind to localhost or put behind an authenticated reverse
  proxy (see `deploy/`). POST mutations are guarded by a same-origin check, and
  the `/img` proxy blocks loopback/link-local/private addresses (SSRF).

## License

MIT