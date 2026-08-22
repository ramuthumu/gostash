// Package srcset implements the HTML-standard tokenization of srcset
// attribute values, shared by the server-side image proxy (/img rewriting)
// and the archiver's media downloader.
//
// Per the HTML spec (https://html.spec.whatwg.org/multipage/images.html#parsing-images-in-source-sets),
// a candidate URL is a maximal run of non-whitespace code points — commas are
// NOT delimiters for the URL itself. This matters for image CDNs whose URLs
// contain commas (e.g. Substack's /image/fetch/w_424,c_limit,f_webp,...);
// splitting on every comma shreds such URLs.
package srcset

import "strings"

// Candidate is one entry of a srcset attribute: a URL and its optional
// descriptor (e.g. "424w", "2x"). An empty descriptor means 1x.
type Candidate struct {
	URL  string
	Desc string
}

func isWS(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

func isDescriptor(s string) bool {
	if len(s) < 2 {
		return false
	}
	last := s[len(s)-1]
	if last != 'w' && last != 'x' && last != 'h' {
		return false
	}
	for _, ch := range s[:len(s)-1] {
		if (ch < '0' || ch > '9') && ch != '.' {
			return false
		}
	}
	return true
}

// Parse tokenizes a srcset value into candidates. It never fails: malformed
// input yields whatever candidates can be salvaged, matching browser
// leniency. Whitespace is ' ', \t, \n, \r, \f, \v per the spec.
func Parse(val string) []Candidate {
	var out []Candidate
	i, n := 0, len(val)
	for i < n {
		// Skip whitespace and commas separating candidates.
		for i < n && (isWS(val[i]) || val[i] == ',') {
			i++
		}
		if i >= n {
			break
		}
		// URL: run of non-whitespace (commas included, per spec).
		urlStart := i
		for i < n && !isWS(val[i]) {
			i++
		}
		u := val[urlStart:i]
		// Trailing commas on the URL are candidate separators (a parse error
		// per spec); strip them. This candidate then has no descriptor.
		desc := ""
		if strings.HasSuffix(u, ",") {
			u = strings.TrimRight(u, ",")
		} else if i < n {
			// Check if next token is a descriptor
			peek := i
			for peek < n && isWS(val[peek]) {
				peek++
			}
			if peek < n && val[peek] != ',' {
				tokenStart := peek
				for peek < n && !isWS(val[peek]) && val[peek] != ',' {
					peek++
				}
				candidateDesc := val[tokenStart:peek]
				if isDescriptor(candidateDesc) {
					desc = candidateDesc
					i = peek
				}
			}
		}
		if u != "" {
			out = append(out, Candidate{URL: u, Desc: desc})
		}
	}
	return out
}
