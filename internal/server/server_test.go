package server

import (
	"strings"
	"testing"
)

func TestProxyImgsSanitizesXSS(t *testing.T) {
	input := `<img src="x" onerror="alert(1)">` +
		`<img src="ok" onload="alert(2)">` +
		`<script>alert(3)</script>` +
		`<iframe src="javascript:alert(4)"></iframe>` +
		`<a href="javascript:alert(5)">click5</a>` +
		`<a href="JavaScript:alert(6)">click6</a>` +
		`<img src="https://cdn.example.com/a.png" srcset="https://cdn.example.com/a.png 2x, https://cdn.example.com/b.png 1x">`

	out := string(proxyImgs(input))
	lower := strings.ToLower(out)

	if strings.Contains(lower, "onerror") {
		t.Errorf("output still contains onerror: %q", out)
	}
	if strings.Contains(lower, "onload") {
		t.Errorf("output still contains onload: %q", out)
	}
	if strings.Contains(lower, "<script") {
		t.Errorf("output still contains <script: %q", out)
	}
	if strings.Contains(lower, "</script") {
		t.Errorf("output still contains </script: %q", out)
	}
	if strings.Contains(lower, "<iframe") {
		t.Errorf("output still contains <iframe: %q", out)
	}
	if strings.Contains(lower, "</iframe") {
		t.Errorf("output still contains </iframe: %q", out)
	}
	if strings.Contains(lower, "javascript:") {
		t.Errorf("output still contains javascript: %q", out)
	}

	if !strings.Contains(out, "/img?url=") {
		t.Errorf("safe image src was not proxied (no /img?url=): %q", out)
	}
	if !strings.Contains(out, "cdn.example.com") {
		t.Errorf("host substring cdn.example.com lost: %q", out)
	}
	if !strings.Contains(out, "srcset") {
		t.Errorf("srcset attribute dropped: %q", out)
	}
	if strings.Count(out, "/img?url=") < 2 {
		t.Errorf("expected at least 2 /img?url= occurrences (src + srcset), got %d: %q", strings.Count(out, "/img?url="), out)
	}
}

// TestProxyImgsSrcsetWithCommasInURL locks the fix for image-CDN URLs that
// contain commas (e.g. Substack's /image/fetch/w_424,c_limit,f_webp,.../...).
// The browser must receive one /img?url= per candidate carrying the FULL url
// (commas percent-encoded), not a URL truncated at the first comma.
func TestProxyImgsSrcsetWithCommasInURL(t *testing.T) {
	// Two candidates; the URLs contain raw commas, the candidate separator is ", ".
	srcset := `https://substackcdn.com/image/fetch/w_424,c_limit,f_webp,q_auto:good/https%3A%2F%2Fbucket.example.com%2Fa.jpeg 424w, ` +
		`https://substackcdn.com/image/fetch/w_848,c_limit,f_webp,q_auto:good/https%3A%2F%2Fbucket.example.com%2Fa.jpeg 848w`
	input := `<img src="https://substackcdn.com/image/fetch/w_1456,c_limit/https%3A%2F%2Fbucket.example.com%2Fa.jpeg" srcset="` + srcset + `">`

	out := string(proxyImgs(input))

	// Each candidate URL must be proxied in full: the comma-bearing CDN path
	// "w_424,c_limit,f_webp" must survive (commas encoded as %2C), not be split.
	if strings.Count(out, "/img?url=") < 3 {
		t.Errorf("expected >=3 /img?url= (1 src + 2 srcset), got %d: %q", strings.Count(out, "/img?url="), out)
	}
	// The descriptor tokens must survive, attached to their candidate.
	if !strings.Contains(out, "424w") || !strings.Contains(out, "848w") {
		t.Errorf("srcset descriptors lost: %q", out)
	}
	// The full CDN path with commas must appear (commas encoded), proving the URL
	// was not truncated at the first comma.
	if !strings.Contains(out, "w_424") || !strings.Contains(out, "c_limit") || !strings.Contains(out, "f_webp") {
		t.Errorf("comma-bearing URL was truncated (lost w_424/c_limit/f_webp): %q", out)
	}
	// Commas in the proxied URL must be encoded as %2C, never raw, so the browser
	// doesn't split the candidate at an in-URL comma.
	for _, frag := range []string{"/img?url=https%3A%2F%2Fsubstackcdn.com"} {
		if !strings.Contains(out, frag) {
			t.Errorf("expected proxied URL prefix %q missing: %q", frag, out)
		}
	}
	// No raw ", w_" (comma-space-w) leakage that would indicate a split candidate.
	if strings.Contains(out, ", w_") {
		t.Errorf("raw \", w_\" in output indicates srcset was split on an in-URL comma: %q", out)
	}
}

func TestProxyImgsPreservesSafeStructure(t *testing.T) {
	input := `<p>hello</p><h2>title</h2><a href="https://example.com">link</a>`
	out := string(proxyImgs(input))

	if !strings.Contains(out, "<p>hello</p>") {
		t.Errorf("expected <p>hello</p> preserved: %q", out)
	}
	if !strings.Contains(out, "<h2>title</h2>") {
		t.Errorf("expected <h2>title</h2> preserved: %q", out)
	}
	if !strings.Contains(out, `href="https://example.com"`) {
		t.Errorf("expected href=\"https://example.com\" preserved: %q", out)
	}
	if strings.Contains(strings.ToLower(out), "onerror") {
		t.Errorf("unexpected onerror in output: %q", out)
	}
}
