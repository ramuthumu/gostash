package server

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/microcosm-cc/bluemonday"
	"github.com/ramuthumu/gostash/internal/archive"
	"github.com/ramuthumu/gostash/internal/db"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type Server struct {
	store *db.Store
	tmpl  *template.Template
}

func New(store *db.Store) *Server {
	t := template.Must(template.New("").Funcs(template.FuncMap{
		"proxyImgs": proxyImgs,
	}).ParseFS(templateFS, "templates/*.html"))
	return &Server{store: store, tmpl: t}
}

func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleList)
	mux.HandleFunc("POST /", s.handleSubmit)
	mux.HandleFunc("GET /save", s.handleSaveBookmarklet)
	mux.HandleFunc("GET /img", s.handleImageProxy)
	mux.HandleFunc("GET /article/{id}", s.handleReader)
	mux.HandleFunc("POST /article/{id}/read", s.handleToggleRead)
	mux.HandleFunc("POST /article/{id}/delete", s.handleDelete)

	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	srv := &http.Server{
		Addr:         addr,
		Handler:      securityHeaders(sameOrigin(logRequests(mux))),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return srv.ListenAndServe()
}

// sameOrigin blocks cross-site form POSTs (CSRF). Same-origin POSTs carry an
// Origin header whose host matches r.Host; a cross-site form POST carries the
// attacker's origin. Personal/tailnet-only, so this is a lightweight guard.
func sameOrigin(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if origin := r.Header.Get("Origin"); origin != "" {
				if u, err := url.Parse(origin); err == nil && u.Host != "" && u.Host != r.Host {
					http.Error(w, "cross-site request blocked", http.StatusForbidden)
					return
				}
			}
		}
		h.ServeHTTP(w, r)
	})
}

// securityHeaders sets baseline browser-isolation headers on every response.
// X-Frame-Options: DENY prevents clickjacking of destructive POSTs (Delete /
// Mark read) from a framed instance — the sameOrigin guard passes for POSTs
// whose framed document origin is ours, so framing must be blocked outright.
// X-Content-Type-Options: nosniff stops browsers MIME-sniffing proxied/downloaded
// content into an executable type.
func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		h.ServeHTTP(w, r)
	})
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.RequestURI())
		h.ServeHTTP(w, r)
	})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	articles, err := s.store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "list.html", map[string]any{
		"Articles":    articles,
		"Bookmarklet": bookmarkletURL(r),
	})
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimSpace(r.FormValue("url"))
	if url == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	id, err := s.archiveURL(url)
	if err != nil {
		log.Printf("archive error: %v", err)
		articles, _ := s.store.List()
		s.render(w, "list.html", map[string]any{
			"Articles":    articles,
			"Error":       err.Error(),
			"Bookmarklet": bookmarkletURL(r),
		})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/article/%d", id), http.StatusSeeOther)
}

func (s *Server) handleSaveBookmarklet(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimSpace(r.URL.Query().Get("url"))
	if url == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	id, err := s.archiveURL(url)
	if err != nil {
		log.Printf("bookmarklet archive error: %v", err)
		http.Error(w, "Could not archive: "+err.Error(), http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/article/%d", id), http.StatusSeeOther)
}

func (s *Server) handleReader(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			log.Printf("get article %d: %v", id, err)
			http.Error(w, "database error", http.StatusInternalServerError)
		}
		return
	}
	if !a.Read {
		_ = s.store.SetRead(id, true)
		a.Read = true
	}
	s.render(w, "reader.html", map[string]any{"A": a})
}

func (s *Server) handleToggleRead(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			log.Printf("get article %d: %v", id, err)
			http.Error(w, "database error", http.StatusInternalServerError)
		}
		return
	}
	_ = s.store.SetRead(id, !a.Read)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = s.store.Delete(id)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) archiveURL(url string) (int64, error) {
	a, err := archive.Fetch(url)
	if err != nil {
		return 0, err
	}
	return s.store.Save(a)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// bookmarkletURL returns an <a> element (as template.HTML) holding a JS
// bookmarklet that archives the current page via /save?url=... . The whole
// anchor is returned as safe HTML because html/template's URL filter would
// otherwise neutralize the javascript: scheme to "#ZgotmplZ" (verified).
// READLATER_PUBLIC_URL, if set, overrides the base (scheme://host) used in the
// bookmarklet — useful behind a reverse proxy where you want a stable URL.
func bookmarkletURL(r *http.Request) template.HTML {
	base := ""
	if pub := os.Getenv("READLATER_PUBLIC_URL"); pub != "" {
		base = strings.TrimRight(pub, "/")
	}
	if base == "" {
		host := r.Host
		if host == "" {
			host = "localhost:8090"
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + host
	}
	js := "javascript:void(location.href='" + base + "/save?url='+encodeURIComponent(location.href));"
	return template.HTML(`<a class="bookmarklet-link" href="` + html.EscapeString(js) + `">📑 Read Later</a>`)
}

// sanitizePolicy is a bluemonday allowlist for rendered article HTML. It strips
// XSS vectors — on* event handlers, <script>/<style>/<iframe>/<object>/<embed>/
// <link>/<meta>/<base>, and javascript:/vbscript:/data: URLs — while preserving
// readability's article formatting and the image attributes proxyImgs rewrites
// (src, srcset, data-src, data-srcset, sizes, loading). go-readability is an
// extractor, not a sanitizer (it leaves on* attributes and case-variant
// javascript: hrefs intact), so this pass is required before rendering.
var sanitizePolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// Keep responsive/lazy image attributes so proxyImgs can route them via /img.
	p.AllowAttrs("srcset", "sizes", "data-src", "data-srcset", "loading", "decoding").
		OnElements("img", "source")
	p.AllowAttrs("type", "media").OnElements("source")
	p.AllowElements("picture", "source")
	return p
}()

// proxyImgs rewrites absolute http(s) image URLs (src, srcset, data-src,
// data-srcset) in <img> and <source> elements to go through /img?url=... so
// that hotlink-protected CDNs load via a server-side fetch with a spoofed
// Referer. Relative, data:, and non-http(s) URLs are left untouched. Returns
// template.HTML so it isn't re-escaped.
func proxyImgs(htmlStr string) template.HTML {
	htmlStr = sanitizePolicy.Sanitize(htmlStr)
	nodes, err := xhtml.ParseFragment(strings.NewReader(htmlStr), &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil || len(nodes) == 0 {
		return template.HTML(htmlStr)
	}
	for _, n := range nodes {
		rewriteMedia(n)
	}
	var b strings.Builder
	for _, n := range nodes {
		_ = xhtml.Render(&b, n)
	}
	return template.HTML(b.String())
}

func rewriteMedia(n *xhtml.Node) {
	if n.Type == xhtml.ElementNode && (n.Data == "img" || n.Data == "source") {
		hasLoading := false
		hasDecoding := false
		for i := range n.Attr {
			switch n.Attr[i].Key {
			case "src", "data-src":
				if u := absHTTP(n.Attr[i].Val); u != "" {
					n.Attr[i].Val = "/img?url=" + url.QueryEscape(u)
				}
			case "srcset", "data-srcset":
				n.Attr[i].Val = rewriteSrcset(n.Attr[i].Val)
			case "loading":
				hasLoading = true
			case "decoding":
				hasDecoding = true
			}
		}
		if n.Data == "img" {
			if !hasLoading {
				n.Attr = append(n.Attr, xhtml.Attribute{Key: "loading", Val: "lazy"})
			}
			if !hasDecoding {
				n.Attr = append(n.Attr, xhtml.Attribute{Key: "decoding", Val: "async"})
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		rewriteMedia(c)
	}
}

// absHTTP returns s if it is an absolute http/https URL, else "". Scheme
// matching is case-insensitive per RFC 3986 (HTTP:// and HTTPS:// qualify).
func absHTTP(s string) string {
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ""
	}
	return s
}

// rewriteSrcset rewrites each absolute http(s) candidate URL in a srcset
// attribute to /img?url=..., preserving its descriptor (e.g. "2x", "300w").
//
// Candidate URLs may themselves contain commas — e.g. Substack's CDN uses
// /image/fetch/w_424,c_limit,f_webp,q_auto:good,.../https%3A... — so we cannot
// simply split on commas. Per the HTML spec, a srcset URL is a maximal run of
// non-whitespace characters (commas included); commas only separate candidates.
// Splitting on every comma shreds such URLs and leaves the browser fetching a
// truncated /img?url=... that 404s. parseSrcset implements the spec tokenization.
func rewriteSrcset(val string) string {
	parts := make([]string, 0, 4)
	for _, c := range parseSrcset(val) {
		if u := absHTTP(c.url); u != "" {
			s := "/img?url=" + url.QueryEscape(u)
			if c.desc != "" {
				s += " " + c.desc
			}
			parts = append(parts, s)
		} else {
			// Relative / data: / non-http(s) candidates: keep verbatim.
			s := c.url
			if c.desc != "" {
				s += " " + c.desc
			}
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// srcsetCandidate is one entry of a srcset attribute: a URL and its optional
// descriptor (e.g. "424w", "2x"). An empty descriptor means 1x.
type srcsetCandidate struct {
	url  string
	desc string
}

// parseSrcset tokenizes a srcset attribute per the HTML spec
// (https://html.spec.whatwg.org/multipage/images.html). Each candidate's URL is
// a maximal run of non-whitespace code points — commas are NOT delimiters for
// the URL, only whitespace is — so URLs containing commas (common with image
// CDNs) stay intact. The optional descriptor follows, separated by whitespace,
// and runs up to the next comma or whitespace. Whitespace and commas between
// candidates are skipped as separators.
func parseSrcset(val string) []srcsetCandidate {
	var out []srcsetCandidate
	isWS := func(b byte) bool {
		return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
	}
	i, n := 0, len(val)
	for i < n {
		// Skip whitespace and commas separating candidates.
		for i < n && (isWS(val[i]) || val[i] == ',') {
			i++
		}
		if i >= n {
			break
		}
		// URL: run of non-whitespace (commas included, per spec).
		urlStart := i
		for i < n && !isWS(val[i]) {
			i++
		}
		u := val[urlStart:i]
		// Trailing commas on the URL are candidate separators (a parse error per
		// spec); strip them. This candidate then has no descriptor.
		desc := ""
		if strings.HasSuffix(u, ",") {
			u = strings.TrimRight(u, ",")
		} else if i < n {
			// Optional descriptor: skip whitespace, then run to next ws/comma.
			for i < n && isWS(val[i]) {
				i++
			}
			if i < n && val[i] != ',' {
				descStart := i
				for i < n && !isWS(val[i]) && val[i] != ',' {
					i++
				}
				desc = val[descStart:i]
			}
		}
		if u != "" {
			out = append(out, srcsetCandidate{url: u, desc: desc})
		}
	}
	return out
}

// handleImageProxy fetches an external image server-side with a spoofed Referer
// (the image host's own origin) so hotlink-protected CDNs serve it, then streams
// the bytes back to the browser. Gated to image/* content types; rejects
// image/svg+xml and sets CSP: sandbox on the response. Personal/tailnet only,
// so SSRF risk is limited to trusted users; guardedDial additionally blocks
// loopback/link-local/private/unspecified targets (e.g. cloud metadata at
// 169.254.169.254). Public and CGNAT/tailnet (100.64/10) addresses are allowed.
func (s *Server) handleImageProxy(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "bad url", http.StatusBadRequest)
		return
	}

	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", archive.BrowserUA)
	// The image's own origin is the referer most CDNs' hotlink checks accept.
	req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	client := archive.SafeClient(20 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		http.Error(w, "not an image ("+ct+")", http.StatusUnsupportedMediaType)
		return
	}
	// image/svg+xml executes script when navigated as a top-level document in
	// our origin (can then fetch("/") and POST /article/{id}/delete). <img>-
	// embedded SVGs are scriptless, so only the direct-navigation case bites;
	// reject it outright as the only hard case.
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "image/svg") {
		http.Error(w, "svg images are not permitted", http.StatusUnsupportedMediaType)
		return
	}

	// sandbox disables scripts even if a future content type slips through the
	// image/ gate, so a directly-navigated /img?url=... URL cannot run same-origin
	// script. nosniff is set site-wide by securityHeaders.
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 25*1024*1024))
}
