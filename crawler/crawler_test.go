package crawler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var fixturePages = map[string]struct {
	body        string
	contentType string
}{
	"/": {`<a href="/a">a</a><a href="/b">b</a><a href="/redir">redir</a>` +
		`<a href="/file.pdf">pdf</a><a href="http://example.invalid/">external</a>` +
		`<a href="/a#frag">a again</a><a href="mailto:x@y.z">mail</a>`, "text/html"},
	"/a":    {`<a href="/b">b</a><a href="/">home</a><a href="/deep">deep</a>`, "text/html"},
	"/b":    {`<p>leaf</p>`, "text/html"},
	"/deep": {`<p>deep leaf</p>`, "text/html"},
	"/file.pdf": {`%PDF-1.4 fake`, "application/pdf"},
}

// fixtureServer serves the interlinked site; robotsTxt controls /robots.txt
// (empty string = 404, i.e. no robots file).
func fixtureServer(t *testing.T, robotsTxt string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/robots.txt":
			if robotsTxt == "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, robotsTxt)
		case r.URL.Path == "/redir":
			http.Redirect(w, r, "/a", http.StatusFound)
		default:
			p, ok := fixturePages[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", p.contentType)
			io.WriteString(w, p.body)
		}
	})
	return httptest.NewServer(mux)
}

func crawlQuiet(t *testing.T, seed string, cfg Config) (Stats, string) {
	t.Helper()
	var buf bytes.Buffer
	cfg.Stdout = &buf
	cfg.Stderr = io.Discard
	cfg.Workers = 4
	c, err := NewCrawler(seed, cfg)
	if err != nil {
		t.Fatal(err)
	}
	stats := c.Run(context.Background())
	return stats, buf.String()
}

func visitedURLs(t *testing.T, out string) map[string]bool {
	t.Helper()
	visited := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "VISITED ") {
			u := strings.TrimPrefix(line, "VISITED ")
			if i := strings.Index(u, "  ["); i >= 0 {
				u = u[:i]
			}
			visited[u] = true
		}
	}
	return visited
}

func TestCrawlVisitsOnlySameHostPages(t *testing.T) {
	srv := fixtureServer(t, "")
	defer srv.Close()

	stats, out := crawlQuiet(t, srv.URL+"/", Config{RespectRobots: false})
	visited := visitedURLs(t, out)

	for _, p := range []string{"/", "/a", "/b", "/deep", "/redir", "/file.pdf"} {
		if !visited[srv.URL+p] {
			t.Errorf("expected %s to be visited", srv.URL+p)
		}
	}
	if len(visited) != 6 {
		t.Errorf("expected 6 visited pages, got %d: %v", len(visited), visited)
	}
	for line := range strings.Lines(out) {
		if strings.HasPrefix(line, "VISITED ") && strings.Contains(line, "example.invalid") {
			t.Errorf("external URL must never be visited: %s", line)
		}
	}
	if stats.PagesVisited != 6 {
		t.Errorf("PagesVisited = %d, want 6", stats.PagesVisited)
	}
	if stats.Errors != 0 {
		t.Errorf("Errors = %d, want 0", stats.Errors)
	}
	// /: /a /b /redir /file.pdf example.invalid (5); /a: /b / /deep (3);
	// /redir -> /a's links (3). Fragments/mailto deduped/dropped.
	if stats.LinksFound != 11 {
		t.Errorf("LinksFound = %d, want 11", stats.LinksFound)
	}
}

func TestCrawlRedirectTargetNotFetchedTwice(t *testing.T) {
	srv := fixtureServer(t, "")
	defer srv.Close()

	_, out := crawlQuiet(t, srv.URL+"/", Config{RespectRobots: false})
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if line == "VISITED "+srv.URL+"/a" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("/a visited %d times, want exactly 1", count)
	}
}

func TestCrawlRespectsRobotsTxt(t *testing.T) {
	srv := fixtureServer(t, "User-agent: *\nDisallow: /b\n")
	defer srv.Close()

	_, out := crawlQuiet(t, srv.URL+"/", Config{RespectRobots: true})
	visited := visitedURLs(t, out)
	if visited[srv.URL+"/b"] {
		t.Error("/b must not be visited (robots.txt disallow)")
	}
	if !visited[srv.URL+"/a"] {
		t.Error("/a should still be visited")
	}
}

func TestCrawlMaxDepth(t *testing.T) {
	srv := fixtureServer(t, "")
	defer srv.Close()

	_, out := crawlQuiet(t, srv.URL+"/", Config{RespectRobots: false, MaxDepth: 1})
	visited := visitedURLs(t, out)
	if visited[srv.URL+"/deep"] {
		t.Error("/deep must not be visited (only reachable at depth 2)")
	}
	if !visited[srv.URL+"/a"] {
		t.Error("/a should be visited at depth 1")
	}
}

func TestCrawlMaxPages(t *testing.T) {
	srv := fixtureServer(t, "")
	defer srv.Close()

	stats, _ := crawlQuiet(t, srv.URL+"/", Config{RespectRobots: false, MaxPages: 2})
	if stats.PagesVisited != 2 {
		t.Errorf("PagesVisited = %d, want 2", stats.PagesVisited)
	}
}
