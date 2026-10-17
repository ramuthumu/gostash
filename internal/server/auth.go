package server

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookieName = "gostash_session"
	sessionLifetime   = 7 * 24 * time.Hour
)

type passwordVerifier struct {
	iterations int
	salt       []byte
	hash       []byte
}

// AuthConfig holds the password verifier and signing key for the single-user
// login. Create it with AuthConfigFromEnv in the application entry point.
type AuthConfig struct {
	password     passwordVerifier
	sessionKey   []byte
	cookieSecure bool
	enabled      bool
}

// AuthConfigFromEnv reads GOSTASH_PASSWORD_HASH and GOSTASH_SESSION_KEY. The
// hash format is pbkdf2-sha256$iterations$salt$hash, with raw URL-safe base64
// for salt and hash. The session key is raw URL-safe base64 with at least
// 32 random bytes.
func AuthConfigFromEnv() (AuthConfig, error) {
	rawHash := os.Getenv("GOSTASH_PASSWORD_HASH")
	if rawHash == "" {
		return AuthConfig{}, errors.New("GOSTASH_PASSWORD_HASH is required")
	}
	rawKey := os.Getenv("GOSTASH_SESSION_KEY")
	if rawKey == "" {
		return AuthConfig{}, errors.New("GOSTASH_SESSION_KEY is required")
	}
	return parseAuthConfig(rawHash, rawKey)
}

func parseAuthConfig(rawHash, rawKey string) (AuthConfig, error) {
	// Compose may preserve escaped dollars in the resolved env-file value.
	rawHash = strings.ReplaceAll(rawHash, "$$", "$")
	parts := strings.Split(rawHash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return AuthConfig{}, errors.New("GOSTASH_PASSWORD_HASH has an unsupported format")
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 100_000 || iterations > 1_000_000 {
		return AuthConfig{}, errors.New("GOSTASH_PASSWORD_HASH iterations must be between 100000 and 1000000")
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) < 16 {
		return AuthConfig{}, errors.New("GOSTASH_PASSWORD_HASH salt is invalid")
	}
	hash, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(hash) != sha256.Size {
		return AuthConfig{}, errors.New("GOSTASH_PASSWORD_HASH digest is invalid")
	}
	key, err := base64.RawURLEncoding.DecodeString(rawKey)
	if err != nil || len(key) < 32 {
		return AuthConfig{}, errors.New("GOSTASH_SESSION_KEY must contain at least 32 random bytes in raw URL-safe base64")
	}
	return AuthConfig{
		password:     passwordVerifier{iterations: iterations, salt: salt, hash: hash},
		sessionKey:   key,
		cookieSecure: os.Getenv("GOSTASH_COOKIE_SECURE") != "false",
		enabled:      true,
	}, nil
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	if !s.auth.enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w = noStoreWriter{ResponseWriter: w}
		w.Header().Set("Cache-Control", "private, no-store")
		if r.URL.Path == "/login" {
			next.ServeHTTP(w, r)
			return
		}
		if s.validSession(r) {
			next.ServeHTTP(w, r)
			return
		}
		target := safeNext(r.URL.RequestURI())
		loginURL := "/login?next=" + url.QueryEscape(target)
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", loginURL)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, loginURL, http.StatusSeeOther)
	})
}

type noStoreWriter struct{ http.ResponseWriter }

func (w noStoreWriter) WriteHeader(status int) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.ResponseWriter.WriteHeader(status)
}

func (w noStoreWriter) Write(data []byte) (int, error) {
	w.Header().Set("Cache-Control", "private, no-store")
	return w.ResponseWriter.Write(data)
}

func (w noStoreWriter) Flush() {
	w.Header().Set("Cache-Control", "private, no-store")
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w noStoreWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

type loginPageData struct {
	Next  string
	Error string
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.URL.Query().Get("next"))
	if r.Method == http.MethodGet {
		if s.validSession(r) {
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}
		s.renderLogin(w, http.StatusOK, loginPageData{Next: next})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		s.renderLogin(w, http.StatusBadRequest, loginPageData{Next: next, Error: "Invalid sign-in request."})
		return
	}
	next = safeNext(r.Form.Get("next"))
	if !s.verifyPassword(r.Form.Get("password")) {
		s.renderLogin(w, http.StatusUnauthorized, loginPageData{Next: next, Error: "That password didn't match. Try again."})
		return
	}
	s.setSession(w)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) renderLogin(w http.ResponseWriter, status int, data loginPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tmpl.ExecuteTemplate(w, "login.html", data); err != nil {
		http.Error(w, "Unable to render sign-in page", http.StatusInternalServerError)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: s.auth.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) verifyPassword(password string) bool {
	if !s.auth.enabled || password == "" {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, s.auth.password.salt, s.auth.password.iterations, len(s.auth.password.hash))
	return err == nil && subtle.ConstantTimeCompare(got, s.auth.password.hash) == 1
}

func (s *Server) setSession(w http.ResponseWriter) {
	expires := time.Now().Add(sessionLifetime).Unix()
	payload := strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, s.auth.sessionKey)
	_, _ = mac.Write([]byte(payload))
	value := payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: value, Path: "/", Expires: time.Unix(expires, 0),
		MaxAge: int(sessionLifetime.Seconds()), HttpOnly: true, Secure: s.auth.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) validSession(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	now := time.Now().Unix()
	if err != nil || expires <= now || expires > now+int64(sessionLifetime.Seconds())+60 {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, s.auth.sessionKey)
	_, _ = mac.Write([]byte(parts[0]))
	return hmac.Equal(signature, mac.Sum(nil))
}

func safeNext(candidate string) string {
	u, err := url.Parse(candidate)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") ||
		strings.HasPrefix(u.Path, "//") || strings.Contains(u.Path, "\\") {
		return "/"
	}
	return u.RequestURI()
}

func createPasswordHash(password string, iterations int) (string, error) {
	if password == "" || iterations < 100_000 || iterations > 1_000_000 {
		return "", errors.New("password and a bounded PBKDF2 iteration count are required")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, iterations, sha256.Size)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iterations,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(hash)), nil
}
