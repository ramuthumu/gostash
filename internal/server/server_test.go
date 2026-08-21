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
