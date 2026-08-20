package archive

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-shiori/go-readability"

	"github.com/ramuthumu/gostash/internal/db"
)

// Fetch downloads the page at rawURL and extracts a readable article.
func Fetch(rawURL string) (db.Article, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return db.Article{}, fmt.Errorf("empty url")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return db.Article{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return db.Article{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return db.Article{}, fmt.Errorf("fetch %s: HTTP %d", rawURL, resp.StatusCode)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return db.Article{}, fmt.Errorf("parse url: %w", err)
	}
	body := io.LimitReader(resp.Body, 20*1024*1024) // 20 MB safety cap
	art, err := readability.FromReader(body, parsed)
	if err != nil {
		return db.Article{}, fmt.Errorf("extract: %w", err)
	}

	title := strings.TrimSpace(art.Title)
	if title == "" {
		title = rawURL
	}

	return db.Article{
		URL:         rawURL,
		Title:       title,
		Author:      strings.TrimSpace(art.Byline),
		Excerpt:     strings.TrimSpace(art.Excerpt),
		ContentHTML: art.Content,
		TextContent: art.TextContent,
	}, nil
}