package server

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type outlineEntry struct {
	ID, Title, Level string
}

var headingPunctuation = regexp.MustCompile(`[^\w\s-]`)
var headingWhitespace = regexp.MustCompile(`\s+`)

// Build both the article and its outline on the server. Sanitize before adding
// our own anchors, and use the same HTML tree for the outline and reader body.
func readerContent(raw string) (template.HTML, []outlineEntry) {
	safe := proxyImgs(raw)
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(string(safe)), root)
	if err != nil {
		return safe, nil
	}
	ids := map[string]int{}
	var headings []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for _, a := range n.Attr {
			if a.Key == "id" {
				ids[a.Val]++
			}
		}
		if n.Type == html.ElementNode && (n.Data == "h1" || n.Data == "h2" || n.Data == "h3" || n.Data == "h4") {
			headings = append(headings, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	// Do not let article headings collide with reader controls.
	for _, id := range []string{"article-content", "controls", "toc-menu", "toc-btn", "toc-list", "reading-progress", "reading-progress-fill"} {
		ids[id]++
	}
	var outline []outlineEntry
	for i, n := range headings {
		var text strings.Builder
		var collect func(*html.Node)
		collect = func(node *html.Node) {
			if node.Type == html.TextNode {
				text.WriteString(node.Data)
			}
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				collect(c)
			}
		}
		collect(n)
		title := strings.Join(strings.Fields(text.String()), " ")
		if title == "" {
			title = fmt.Sprintf("Section %d", i+1)
		}
		id := ""
		for _, a := range n.Attr {
			if a.Key == "id" {
				id = a.Val
			}
		}
		if id == "" || ids[id] > 1 {
			slug := headingWhitespace.ReplaceAllString(headingPunctuation.ReplaceAllString(strings.ToLower(title), ""), "-")
			id = fmt.Sprintf("sec-%s-%d", slug, i)
			for ids[id] > 0 {
				id += "-section"
			}
			ids[id]++
		}
		attrs := n.Attr[:0]
		for _, a := range n.Attr {
			if a.Key != "id" {
				attrs = append(attrs, a)
			}
		}
		n.Attr = append(attrs, html.Attribute{Key: "id", Val: id})
		outline = append(outline, outlineEntry{ID: id, Title: title, Level: n.Data})
	}
	var body strings.Builder
	for _, n := range nodes {
		if err := html.Render(&body, n); err != nil {
			return safe, nil
		}
	}
	// Only sanitized HTML plus escaped, generated ID attributes reaches here.
	return template.HTML(body.String()), outline
}
