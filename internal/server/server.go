package server

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ramuthumu/gostash/internal/archive"
	"github.com/ramuthumu/gostash/internal/db"
)

const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// matches src="https?://..." (absolute URLs only; leaves relative/data: alone)
var imgSrcRe = regexp.MustCompile(`(?i)(\bsrc=")(https?://[^"]+)(")`)

// proxyImgs rewrites absolute <img src> URLs to go through /img?url=... so that
// hotlink-protected CDNs (e.g. eenadu.net) load via a server-side fetch with a
// spoofed Referer. Returns template.HTML so it isn't re-escaped.
func proxyImgs(htmlStr string) template.HTML {
	return template.HTML(imgSrcRe.ReplaceAllStringFunc(htmlStr, func(m string) string {
		sub := imgSrcRe.FindStringSubmatch(m)
		return sub[1] + "/img?url=" + url.QueryEscape(sub[2]) + sub[3]
	}))
}

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
		"safe":      func(s string) template.HTML { return template.HTML(s) },
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

	srv := &http.Server{Addr: addr, Handler: logRequests(mux)}
	return srv.ListenAndServe()
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
		"Articles": articles,
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
			"Bookmarklet": "javascript:void(0)",
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
		http.NotFound(w, r)
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
		http.NotFound(w, r)
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

// bookmarkletURL returns a JS bookmarklet string that POSTs the current page to /save.
// Using a GET redirect keeps it simple and works without a separate JS file.
func bookmarkletURL(r *http.Request) string {
	host := r.Host
	if host == "" {
		host = "localhost:8090"
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	_ = os.Getenv("READLATER_PUBLIC_URL") // allow override in future
	base := scheme + "://" + host
	js := "javascript:void(location.href='" + base + "/save?url='+encodeURIComponent(location.href));"
	return js
}

// handleImageProxy fetches an external image server-side with a spoofed Referer
// (the image host's own origin) so hotlink-protected CDNs serve it, then streams
// the bytes back to the browser. Gated to image/* content types. Personal/tailnet
// only, so SSRF risk is limited to trusted users.
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
	req.Header.Set("User-Agent", browserUA)
	// The image's own origin is the referer most CDNs' hotlink checks accept.
	req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	client := &http.Client{Timeout: 20 * time.Second}
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
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 25*1024*1024))
}