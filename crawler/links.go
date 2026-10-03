package crawler

import (
	"strings"

	"golang.org/x/net/html"
)

// ExtractLinks returns the unique canonical absolute http(s) URLs from
// <a href> tags in body, resolved against baseURL (the page's final URL
// after redirects). Fragments, mailto:, javascript: etc. are dropped by
// Canonicalize. Order of first appearance is preserved.
//
// The only third-party dependency in this project is golang.org/x/net/html:
// the standard library has no HTML parser, and hand-rolling tag scanning
// with regexps is how crawlers get parsing bugs.
func ExtractLinks(body, baseURL string) []string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if strings.EqualFold(attr.Key, "href") && attr.Val != "" {
					if u := Canonicalize(attr.Val, baseURL); u != "" {
						if _, ok := seen[u]; !ok {
							seen[u] = struct{}{}
							out = append(out, u)
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}
