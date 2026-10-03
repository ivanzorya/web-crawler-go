package crawler

import (
	"reflect"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	html := `
	<html><body>
	  <a href="/abs">one</a>
	  <a href="rel">two</a>
	  <a href="https://example.com/abs">dup of one</a>
	  <a href="/abs#frag">dup with fragment</a>
	  <a href="mailto:a@b.c">dropped</a>
	  <a href="javascript:void(0)">dropped</a>
	  <a href="">dropped</a>
	  <a>no href</a>
	  <A HREF="/upper">case-insensitive</A>
	</body></html>`
	want := []string{
		"https://example.com/abs",
		"https://example.com/dir/rel",
		"https://example.com/upper",
	}
	if got := ExtractLinks(html, "https://example.com/dir/page"); !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractLinks() = %v, want %v", got, want)
	}
}

func TestExtractLinksKeepsOffHostForReporting(t *testing.T) {
	// The crawler filters by host when *following*; extraction keeps
	// everything so the per-page report is complete.
	got := ExtractLinks(`<a href="https://other.com/x">ext</a>`, "https://example.com/")
	want := []string{"https://other.com/x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractLinks() = %v, want %v", got, want)
	}
}

func TestExtractLinksEmpty(t *testing.T) {
	if got := ExtractLinks("", "https://example.com/"); len(got) != 0 {
		t.Errorf("expected no links, got %v", got)
	}
	if got := ExtractLinks("<p>no links</p>", "https://example.com/"); len(got) != 0 {
		t.Errorf("expected no links, got %v", got)
	}
}
