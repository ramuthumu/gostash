package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newAuthTestHandler(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	hash, err := createPasswordHash("correct horse battery staple", 100_000)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := parseAuthConfig(hash, "dGVzdC1zZXNzaW9uLWtleS1hdC1sZWFzdC0zMi1ieXRlcyE")
	if err != nil {
		t.Fatal(err)
	}
	s := New(nil, t.TempDir(), auth)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /private", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("private"))
	})
	return s, s.authMiddleware(mux)
}

func TestAuthRequiresLoginAndSetsSignedCookie(t *testing.T) {
	s, handler := newAuthTestHandler(t)

	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/private?tab=unread", nil))
	if anonymous.Code != http.StatusSeeOther {
		t.Fatalf("anonymous status = %d, want %d", anonymous.Code, http.StatusSeeOther)
	}
	loginURL, err := url.Parse(anonymous.Header().Get("Location"))
	if err != nil || loginURL.Path != "/login" || loginURL.Query().Get("next") != "/private?tab=unread" {
		t.Fatalf("unexpected login redirect: %q", anonymous.Header().Get("Location"))
	}

	form := url.Values{"password": {"correct horse battery staple"}, "next": {"/private?tab=unread"}}
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, login)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/private?tab=unread" {
		t.Fatalf("login status/location = %d %q", w.Code, w.Header().Get("Location"))
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].HttpOnly ||
		!cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected session cookie: %#v", cookies)
	}

	private := httptest.NewRequest(http.MethodGet, "/private", nil)
	private.AddCookie(cookies[0])
	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, private)
	if allowed.Code != http.StatusOK || allowed.Body.String() != "private" {
		t.Fatalf("authenticated request = %d %q", allowed.Code, allowed.Body.String())
	}

	logout := httptest.NewRequest(http.MethodPost, "/logout", nil)
	logout.AddCookie(cookies[0])
	loggedOut := httptest.NewRecorder()
	handler.ServeHTTP(loggedOut, logout)
	if loggedOut.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d", loggedOut.Code)
	}
	cleared := loggedOut.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatalf("logout did not clear session cookie: %#v", cleared)
	}
	if s.validSession(private) != true {
		t.Fatal("issued cookie should remain valid until logout or expiry")
	}
}

func TestAuthRejectsWrongPasswordAndTamperedCookie(t *testing.T) {
	_, handler := newAuthTestHandler(t)
	form := url.Values{"password": {"wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "That password") {
		t.Fatalf("wrong password status/body = %d %q", w.Code, w.Body.String())
	}

	cookie := &http.Cookie{Name: sessionCookieName, Value: "9999999999.invalid"}
	protected := httptest.NewRequest(http.MethodGet, "/private", nil)
	protected.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, protected)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("tampered cookie status = %d, want redirect", response.Code)
	}
}

func TestSafeNextRejectsExternalRedirects(t *testing.T) {
	for _, candidate := range []string{"https://example.com/", "//example.com/", "/\\example.com/"} {
		if got := safeNext(candidate); got != "/" {
			t.Errorf("safeNext(%q) = %q, want /", candidate, got)
		}
	}
	if got := safeNext("/article/12?from=login"); got != "/article/12?from=login" {
		t.Errorf("safe local return path changed: %q", got)
	}
}

func TestPasswordHashAcceptsComposeEscapedDollars(t *testing.T) {
	hash, err := createPasswordHash("correct horse battery staple", 100_000)
	if err != nil {
		t.Fatal(err)
	}
	escaped := strings.ReplaceAll(hash, "$", "$$")
	auth, err := parseAuthConfig(escaped, "dGVzdC1zZXNzaW9uLWtleS1hdC1sZWFzdC0zMi1ieXRlcyE")
	if err != nil {
		t.Fatal(err)
	}
	s := New(nil, t.TempDir(), auth)
	if !s.verifyPassword("correct horse battery staple") {
		t.Fatal("could not verify password after Compose dollar escaping")
	}
}

func TestServiceWorkerDoesNotCacheNoStorePages(t *testing.T) {
	sw, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sw), "gostash-v5") ||
		strings.Count(string(sw), "Cache-Control')?.includes('no-store") != 3 {
		t.Fatal("service worker must clear the old cache version and skip no-store responses")
	}
}
