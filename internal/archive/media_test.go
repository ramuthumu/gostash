package archive

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessAndDownloadMedia(t *testing.T) {
	// Mock HTTP image server
	pngData := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15c4\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82")

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/img1.png":
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(pngData)
		case "/img2.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("fake-jpeg-data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	mediaDir := filepath.Join(t.TempDir(), "media")

	inputHTML := `
		<div>
			<p>Article text</p>
			<img src="` + mockServer.URL + `/img1.png" alt="Test 1">
			<img data-src="` + mockServer.URL + `/img2.jpg" srcset="` + mockServer.URL + `/img1.png 2x, ` + mockServer.URL + `/img2.jpg 1x">
		</div>
	`

	outHTML := ProcessAndDownloadMediaWithClient(inputHTML, serverURL, mediaDir, mockServer.Client())

	// Verify URLs rewritten to /media/
	if !strings.Contains(outHTML, `src="/media/`) {
		t.Errorf("expected src to be rewritten to /media/, got: %s", outHTML)
	}
	if !strings.Contains(outHTML, `loading="lazy"`) {
		t.Errorf("expected loading=lazy injected, got: %s", outHTML)
	}
	if !strings.Contains(outHTML, `decoding="async"`) {
		t.Errorf("expected decoding=async injected, got: %s", outHTML)
	}

	// Verify files actually exist on disk in mediaDir
	files, err := os.ReadDir(mediaDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 media files downloaded, found %d", len(files))
	}
}
