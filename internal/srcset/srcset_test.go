package srcset

import (
	"reflect"
	"testing"
)

func TestParseBasic(t *testing.T) {
	got := Parse("a.png 300w, b.png 2x, c.png")
	want := []Candidate{
		{URL: "a.png", Desc: "300w"},
		{URL: "b.png", Desc: "2x"},
		{URL: "c.png"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// URLs containing commas (image CDNs like Substack) must stay intact —
// commas are candidate separators only when adjacent to whitespace.
func TestParseCommasInsideURL(t *testing.T) {
	in := "https://cdn.substack.com/image/fetch/w_424,c_limit,f_webp,q_auto:good/https%3A%2F%2Fexample.com%2Fa.png 424w," +
		" https://cdn.substack.com/image/fetch/w_848,c_limit,f_webp/https%3A%2F%2Fexample.com%2Fa.png 848w"
	got := Parse(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(got), got)
	}
	want0 := "https://cdn.substack.com/image/fetch/w_424,c_limit,f_webp,q_auto:good/https%3A%2F%2Fexample.com%2Fa.png"
	if got[0].URL != want0 || got[0].Desc != "424w" {
		t.Errorf("candidate 0 = %+v, want URL %q desc 424w", got[0], want0)
	}
	want1 := "https://cdn.substack.com/image/fetch/w_848,c_limit,f_webp/https%3A%2F%2Fexample.com%2Fa.png"
	if got[1].URL != want1 || got[1].Desc != "848w" {
		t.Errorf("candidate 1 = %+v, want URL %q desc 848w", got[1], want1)
	}
}

func TestParseTrailingCommaMeansNoDescriptor(t *testing.T) {
	got := Parse("a.png, b.png ,c.png 2x")
	want := []Candidate{
		{URL: "a.png"},
		{URL: "b.png"},
		{URL: "c.png", Desc: "2x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseWhitespaceVariants(t *testing.T) {
	got := Parse("  a.png\t2x\n,\rb.png \f c.png\v3x  ")
	want := []Candidate{
		{URL: "a.png", Desc: "2x"},
		{URL: "b.png"},
		{URL: "c.png", Desc: "3x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseEmptyAndGarbage(t *testing.T) {
	if got := Parse(""); got != nil {
		t.Errorf("empty input should give nil, got %v", got)
	}
	if got := Parse(",,, ,,"); got != nil {
		t.Errorf("comma-only input should give nil, got %v", got)
	}
	// A lone comma glued to a URL is stripped; the URL survives.
	got := Parse(",a.png")
	if len(got) != 1 || got[0].URL != "a.png" {
		t.Errorf("got %v, want single a.png", got)
	}
}

func TestParseDataURIKept(t *testing.T) {
	// Parse is scheme-agnostic; callers decide which schemes to rewrite.
	got := Parse("data:image/png;base64,iVBORwKGgo= 1x")
	if len(got) != 1 || got[0].Desc != "1x" {
		t.Errorf("got %v, want data URI with 1x", got)
	}
}
