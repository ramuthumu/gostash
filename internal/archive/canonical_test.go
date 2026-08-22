package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalizeURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"example.com", "https://example.com"},
		{"  example.com  ", "https://example.com"},
		{"http://Example.COM/a", "http://example.com/a"},
		{"https://example.com/page#section", "https://example.com/page"},
		{"https://example.com#top", "https://example.com"},
		{"http://example.com:80/x?y=1#z", "http://example.com/x?y=1"},
		{"https://example.com:443/", "https://example.com/"},
		{"https://example.com:8443/x", "https://example.com:8443/x"},
		{"https://example.com/path?keep=1#frag", "https://example.com/path?keep=1"},
	}
	for _, c := range cases {
		if got := CanonicalizeURL(c.in); got != c.want {
			t.Errorf("CanonicalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := CanonicalizeURL(""); got != "" {
		t.Errorf(`CanonicalizeURL("") = %q, want empty`, got)
	}
}

func TestPruneMedia(t *testing.T) {
	dir := t.TempDir()
	unreferenced := strings.Repeat("a", 64) + ".png"
	referencedName := strings.Repeat("b", 64) + ".jpg"
	tmpFile := strings.Repeat("c", 64) + ".tmp"
	foreign := "notes.txt"

	for _, name := range []string{unreferenced, referencedName, tmpFile, foreign} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	refs := map[string]bool{referencedName: true}
	removed, err := PruneMedia(dir, refs)
	if err != nil {
		t.Fatalf("PruneMedia failed: %v", err)
	}
	if len(removed) != 1 || removed[0] != unreferenced {
		t.Errorf("removed = %v, want [%s]", removed, unreferenced)
	}

	for name, wantGone := range map[string]bool{
		unreferenced:   true,
		referencedName: false,
		tmpFile:        false,
		foreign:        false,
	} {
		_, statErr := os.Stat(filepath.Join(dir, name))
		gone := statErr != nil
		if gone != wantGone {
			t.Errorf("%s: gone=%v, want removed=%v", name, gone, wantGone)
		}
	}
	if _, err := os.Stat(subdir); err != nil {
		t.Errorf("subdir should survive: %v", err)
	}
}

func TestPruneMediaMissingDir(t *testing.T) {
	removed, err := PruneMedia(filepath.Join(t.TempDir(), "nope"), nil)
	if err != nil || removed != nil {
		t.Errorf("expected nil,nil for missing dir; got %v,%v", removed, err)
	}
}
