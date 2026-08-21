package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ProcessAndDownloadMedia parses HTML content, downloads all images to mediaDir,
// content-addresses them by SHA-256 hash, and rewrites the HTML image tags to
// point to /media/<hash>.<ext>.
func ProcessAndDownloadMedia(htmlStr string, pageURL *url.URL, mediaDir string) string {
	return ProcessAndDownloadMediaWithClient(htmlStr, pageURL, mediaDir, SafeClient(15*time.Second))
}

// ProcessAndDownloadMediaWithClient downloads media using a specified http.Client.
func ProcessAndDownloadMediaWithClient(htmlStr string, pageURL *url.URL, mediaDir string, client *http.Client) string {
	if mediaDir == "" || strings.TrimSpace(htmlStr) == "" {
		return htmlStr
	}
	if client == nil {
		client = SafeClient(15 * time.Second)
	}
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return htmlStr
	}

	nodes, err := xhtml.ParseFragment(strings.NewReader(htmlStr), &xhtml.Node{
		Type:     xhtml.ElementNode,
		Data:     "body",
		DataAtom: atom.Body,
	})
	if err != nil || len(nodes) == 0 {
		return htmlStr
	}

	// 1. Collect all candidate image URLs
	urlsToFetch := make(map[string]bool)
	for _, n := range nodes {
		collectMediaURLs(n, pageURL, urlsToFetch)
	}

	if len(urlsToFetch) == 0 {
		return htmlStr
	}

	// 2. Concurrently download images
	var mu sync.Mutex
	urlToMedia := make(map[string]string) // absoluteURL -> /media/<hash>.<ext>

	var wg sync.WaitGroup
	sem := make(chan struct{}, 6) // up to 6 concurrent downloads

	for rawURL := range urlsToFetch {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mediaPath, err := downloadImage(client, u, pageURL.String(), mediaDir)
			if err == nil && mediaPath != "" {
				mu.Lock()
				urlToMedia[u] = mediaPath
				mu.Unlock()
			}
		}(rawURL)
	}
	wg.Wait()

	// 3. Rewrite HTML nodes
	for _, n := range nodes {
		rewriteNodeMedia(n, pageURL, urlToMedia)
	}

	var b strings.Builder
	for _, n := range nodes {
		_ = xhtml.Render(&b, n)
	}
	return b.String()
}

func collectMediaURLs(n *xhtml.Node, base *url.URL, out map[string]bool) {
	if n.Type == xhtml.ElementNode && (n.Data == "img" || n.Data == "source") {
		for _, attr := range n.Attr {
			switch attr.Key {
			case "src", "data-src":
				if resolved := resolveHTTP(attr.Val, base); resolved != "" {
					out[resolved] = true
				}
			case "srcset", "data-srcset":
				for _, cand := range parseSrcsetURLs(attr.Val) {
					if resolved := resolveHTTP(cand, base); resolved != "" {
						out[resolved] = true
					}
				}
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectMediaURLs(c, base, out)
	}
}

func rewriteNodeMedia(n *xhtml.Node, base *url.URL, urlToMedia map[string]string) {
	if n.Type == xhtml.ElementNode && (n.Data == "img" || n.Data == "source") {
		hasLoading := false
		hasDecoding := false
		for i := range n.Attr {
			switch n.Attr[i].Key {
			case "src", "data-src":
				if resolved := resolveHTTP(n.Attr[i].Val, base); resolved != "" {
					if local, ok := urlToMedia[resolved]; ok {
						n.Attr[i].Val = local
					}
				}
			case "srcset", "data-srcset":
				n.Attr[i].Val = rewriteSrcsetWithMedia(n.Attr[i].Val, base, urlToMedia)
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
		rewriteNodeMedia(c, base, urlToMedia)
	}
}

func downloadImage(client *http.Client, imgURL string, referer string, mediaDir string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, imgURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", BrowserUA)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	// 25MB max per image
	data, err := io.ReadAll(io.LimitReader(resp.Body, 25*1024*1024))
	if err != nil || len(data) == 0 {
		return "", fmt.Errorf("empty or read error")
	}

	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// Detect extension
	ext := detectExtension(resp.Header.Get("Content-Type"), imgURL, data)
	filename := hashStr + ext
	destPath := filepath.Join(mediaDir, filename)

	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		tmpPath := destPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
			return "", err
		}
		if err := os.Rename(tmpPath, destPath); err != nil {
			return "", err
		}
	}

	return "/media/" + filename, nil
}

func detectExtension(contentType string, rawURL string, data []byte) string {
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		switch mediaType {
		case "image/jpeg", "image/jpg":
			return ".jpg"
		case "image/png":
			return ".png"
		case "image/webp":
			return ".webp"
		case "image/gif":
			return ".gif"
		case "image/svg+xml":
			return ".svg"
		case "image/avif":
			return ".avif"
		}
	}
	// Fallback to URL path extension
	if u, err := url.Parse(rawURL); err == nil {
		if e := strings.ToLower(path.Ext(u.Path)); e != "" && len(e) <= 5 {
			return e
		}
	}
	return ".jpg"
}

func resolveHTTP(s string, base *url.URL) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "data:") || strings.HasPrefix(s, "javascript:") {
		return ""
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s
	}
	if base == nil {
		return ""
	}
	rel, err := url.Parse(s)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(rel)
	if resolved.Scheme == "http" || resolved.Scheme == "https" {
		return resolved.String()
	}
	return ""
}

type srcsetCandidate struct {
	rawURL     string
	descriptor string
}

func parseSrcsetURLs(val string) []string {
	var urls []string
	for _, c := range parseSrcsetCandidates(val) {
		if c.rawURL != "" {
			urls = append(urls, c.rawURL)
		}
	}
	return urls
}

func rewriteSrcsetWithMedia(val string, base *url.URL, urlToMedia map[string]string) string {
	var parts []string
	for _, c := range parseSrcsetCandidates(val) {
		resolved := resolveHTTP(c.rawURL, base)
		u := c.rawURL
		if local, ok := urlToMedia[resolved]; ok {
			u = local
		}
		if c.descriptor != "" {
			parts = append(parts, u+" "+c.descriptor)
		} else {
			parts = append(parts, u)
		}
	}
	return strings.Join(parts, ", ")
}

func parseSrcsetCandidates(s string) []srcsetCandidate {
	var res []srcsetCandidate
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		// skip leading commas or spaces
		s = strings.TrimLeft(s, " \t\r\n,")
		if s == "" {
			break
		}
		// find next whitespace
		spaceIdx := strings.IndexAny(s, " \t\r\n")
		var rawURL, rest string
		if spaceIdx == -1 {
			rawURL = strings.TrimRight(s, ",")
			rest = ""
		} else {
			rawURL = s[:spaceIdx]
			rest = strings.TrimSpace(s[spaceIdx:])
		}
		var desc string
		if rest != "" {
			commaIdx := strings.IndexByte(rest, ',')
			if commaIdx == -1 {
				desc = strings.TrimSpace(rest)
				s = ""
			} else {
				desc = strings.TrimSpace(rest[:commaIdx])
				s = rest[commaIdx+1:]
			}
		} else {
			s = ""
		}
		res = append(res, srcsetCandidate{rawURL: rawURL, descriptor: desc})
	}
	return res
}
