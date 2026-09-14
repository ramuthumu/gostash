package server

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramuthumu/gostash/internal/db"
)

func renderingServer(t *testing.T) (*Server, *db.Store) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "articles.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return New(store, t.TempDir()), store
}

func TestReaderContentOutline(t *testing.T) {
	body, outline := readerContent(`<h2>First &amp; <em>second</em></h2><p>Saved body</p><h2>First &amp; second</h2><h3>తెలుగు</h3><img src="https://example.com/photo.jpg" onerror="alert(1)"><script>alert(2)</script>`)
	if len(outline) != 3 {
		t.Fatalf("outline: %+v", outline)
	}
	seen := map[string]bool{}
	for _, heading := range outline {
		if heading.ID == "" || seen[heading.ID] {
			t.Fatalf("duplicate/missing ID: %+v", outline)
		}
		seen[heading.ID] = true
		if !strings.Contains(string(body), `id="`+heading.ID+`"`) {
			t.Fatalf("missing anchor: %s", body)
		}
	}
	if outline[0].Title != "First & second" || outline[2].Title != "తెలుగు" {
		t.Fatalf("lost heading text: %+v", outline)
	}
	if strings.Contains(string(body), "<script") || strings.Contains(string(body), "onerror") {
		t.Fatalf("unsafe body: %s", body)
	}
	if !strings.Contains(string(body), "/img?url=") {
		t.Fatalf("image proxy lost: %s", body)
	}
}

func TestListFullPageAndFragment(t *testing.T) {
	s, store := renderingServer(t)
	_, err := store.Save(db.Article{URL: "https://example.com/go", Title: "Go templates", TextContent: "Go templates"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/?filter=all&q=Go", "/partials/list?filter=all&q=Go"} {
		w := httptest.NewRecorder()
		s.handleList(w, httptest.NewRequest("GET", path, nil))
		body := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(body, "Go templates") || !strings.Contains(body, `id="library"`) {
			t.Fatalf("bad rendered list: %d %s", w.Code, body)
		}
		full := strings.Contains(body, "<!DOCTYPE html>")
		if full != strings.HasPrefix(path, "/?") {
			t.Fatalf("wrong document/fragment for %s", path)
		}
		if !full && w.Header().Get("HX-Push-Url") != "/?filter=all&q=Go" {
			t.Fatalf("bad history URL: %s", w.Header())
		}
	}
}

func TestFragmentFilterLinksEncodeSearch(t *testing.T) {
	s, _ := renderingServer(t)
	w := httptest.NewRecorder()
	query := "Go & templates"
	s.handleList(w, httptest.NewRequest("GET", "/partials/list?filter=all&q="+url.QueryEscape(query), nil))
	if w.Code != 200 {
		t.Fatalf("status: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(html.UnescapeString(w.Body.String()), `hx-get="/partials/list?filter=archive&q=Go+%26+templates"`) {
		t.Fatalf("fragment links must encode query separators: %s", w.Body)
	}
}

func TestListMutationPreservesQueryAndSupportsPlainForms(t *testing.T) {
	s, store := renderingServer(t)
	id, err := store.Save(db.Article{URL: "https://example.com/go", Title: "Go templates", TextContent: "Go templates"})
	if err != nil {
		t.Fatal(err)
	}
	for _, htmx := range []bool{true, false} {
		r := httptest.NewRequest("POST", fmt.Sprintf("/article/%d/read", id), strings.NewReader("filter=all&q=Go"))
		r.SetPathValue("id", fmt.Sprint(id))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if htmx {
			r.Header.Set("HX-Target", "library")
		}
		w := httptest.NewRecorder()
		s.handleToggleRead(w, r)
		if htmx {
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Mark unread") || strings.Contains(w.Body.String(), "<!DOCTYPE") {
				t.Fatalf("bad mutation fragment: %d %s", w.Code, w.Body)
			}
		} else if w.Code != 303 || w.Header().Get("Location") != "/?filter=all&q=Go" {
			t.Fatalf("bad fallback: %d %s", w.Code, w.Header())
		}
	}
}

func TestReaderServerRendersOutlineWithoutJavaScript(t *testing.T) {
	s, store := renderingServer(t)
	id, err := store.Save(db.Article{URL: "https://example.com/article", Title: "Example", ContentHTML: "<h2>A section</h2><p>Readable without JavaScript.</p><h3>Another section</h3>"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", fmt.Sprintf("/article/%d", id), nil)
	r.SetPathValue("id", fmt.Sprint(id))
	w := httptest.NewRecorder()
	s.handleReader(w, r)
	body := w.Body.String()
	for _, want := range []string{`<details class="toc-dropdown"`, `href="#sec-a-section-0"`, `id="sec-a-section-0"`, "Readable without JavaScript."} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in server HTML", want)
		}
	}
}

func TestArchiveStatusLifecycle(t *testing.T) {
	s, store := renderingServer(t)
	id, err := store.Enqueue("https://example.com/pending")
	if err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", fmt.Sprintf("/article/%d/status", id), nil)
		r.SetPathValue("id", fmt.Sprint(id))
		w := httptest.NewRecorder()
		s.handleArticleStatus(w, r)
		return w
	}
	w := request()
	if w.Code != 200 || !strings.Contains(w.Body.String(), `hx-trigger="every 2s"`) || strings.Contains(w.Body.String(), "location.reload") {
		t.Fatalf("bad pending card: %d %s", w.Code, w.Body)
	}
	if err := store.Fail(id, "<script>upstream failure</script>"); err != nil {
		t.Fatal(err)
	}
	w = request()
	if strings.Contains(w.Body.String(), "hx-trigger") || strings.Contains(w.Body.String(), "<script>") || !strings.Contains(w.Body.String(), "Try again") {
		t.Fatalf("bad failed card: %s", w.Body)
	}
	if err := store.Complete(id, db.Article{Title: "Ready"}); err != nil {
		t.Fatal(err)
	}
	w = request()
	if w.Code != 204 || w.Header().Get("HX-Refresh") != "true" {
		t.Fatalf("reader must refresh when ready: %d %s", w.Code, w.Header())
	}
	a, _ := store.Get(id)
	if a.Read {
		t.Fatal("status poll must not mark article read")
	}
	for _, invalid := range []string{"bogus", "999999"} {
		r := httptest.NewRequest("GET", "/article/"+url.PathEscape(invalid)+"/status", nil)
		r.SetPathValue("id", invalid)
		w := httptest.NewRecorder()
		s.handleArticleStatus(w, r)
		if w.Code != 404 {
			t.Errorf("status %q: got %d", invalid, w.Code)
		}
	}
}
