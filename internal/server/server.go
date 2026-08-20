package server

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

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
		"safe": func(s string) template.HTML { return template.HTML(s) },
	}).ParseFS(templateFS, "templates/*.html"))
	return &Server{store: store, tmpl: t}
}

func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleList)
	mux.HandleFunc("POST /", s.handleSubmit)
	mux.HandleFunc("GET /save", s.handleSaveBookmarklet)
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