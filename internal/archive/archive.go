package archive

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-shiori/go-readability"

	"github.com/ramuthumu/gostash/internal/db"
)

// BrowserUA is the desktop Chrome User-Agent used for all outbound fetches so
// that sites serve their normal (non-bot) HTML and images.
const BrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// GuardedDial is an http.Transport.DialContext that blocks requests to
// loopback, link-local, RFC1918 private, and unspecified addresses so neither
// archive.Fetch nor the /img image proxy can be turned into an SSRF vector
// (e.g. cloud metadata at 169.254.169.254, or services on the host itself).
// Public and CGNAT/tailnet (100.64/10) addresses are allowed.
//
// It resolves the host once, validates every returned IP, and dials a validated
// IP literal directly — never re-resolving the hostname — so DNS-rebinding
// cannot bypass the check (a malicious resolver can't return a public IP for
// the validation lookup and an internal IP for the actual dial). If the first
// validated address is unreachable (e.g. IPv6 with no route), it tries the rest.
// The request's Host header and TLS SNI are still derived from the original
// URL, not the dial address.
func GuardedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
			return nil, fmt.Errorf("refused: %s resolves to internal address %s", host, ip)
		}
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	var lastErr error
	for _, ip := range ips {
		c, derr := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if derr == nil {
			return c, nil
		}
		lastErr = derr
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no usable address for %s", host)
}

// SafeClient returns an *http.Client whose transport blocks internal (SSRF)
// destinations via GuardedDial. timeout bounds the whole request.
func SafeClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: GuardedDial},
	}
}

// Fetch downloads the page at rawURL and extracts a readable article.
func Fetch(rawURL string) (db.Article, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return db.Article{}, fmt.Errorf("empty url")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	client := SafeClient(30 * time.Second)
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return db.Article{}, err
	}
	req.Header.Set("User-Agent", BrowserUA)
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
