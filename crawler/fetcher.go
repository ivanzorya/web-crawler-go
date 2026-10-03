package crawler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxBodyBytes caps how much of a response is buffered. A crawler that
// happily downloads a 2 GB ISO into RAM is a crawler with an incident
// waiting to happen.
const MaxBodyBytes = 2 * 1024 * 1024

// FetchResult describes one HTTP fetch. Failures are returned as data --
// never raised -- so one bad page can't take down the whole crawl.
type FetchResult struct {
	URL     string // final URL after redirects
	Status  int
	HTML    string // empty when the page wasn't fetchable or isn't HTML
	Err     error
	Skipped bool // set for deliberate non-parses (non-HTML, oversized body)
}

// Fetcher performs HTTP GETs with timeouts. Create with NewFetcher.
type Fetcher struct {
	client  *http.Client
	timeout time.Duration
}

func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			// Timeout covers the whole exchange: connect, redirects, body.
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		timeout: timeout,
	}
}

// Fetch GETs url and returns its HTML, or a result describing the failure.
func (f *Fetcher) Fetch(ctx context.Context, url string) FetchResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return FetchResult{URL: url, Err: err}
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return FetchResult{URL: url, Err: err}
	}
	defer resp.Body.Close()
	final := resp.Request.URL.String()
	if resp.StatusCode >= 400 {
		return FetchResult{URL: final, Status: resp.StatusCode,
			Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "text/html") {
		return FetchResult{URL: final, Status: resp.StatusCode, Skipped: true,
			Err: fmt.Errorf("non-HTML content (%s)", ct)}
	}
	// LimitReader with MaxBodyBytes+1: reading one byte past the cap tells
	// us the body is oversized without buffering the whole thing.
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if err != nil {
		return FetchResult{URL: final, Status: resp.StatusCode, Err: err}
	}
	if len(body) > MaxBodyBytes {
		return FetchResult{URL: final, Status: resp.StatusCode, Skipped: true,
			Err: fmt.Errorf("body exceeded %d MiB cap", MaxBodyBytes/1024/1024)}
	}
	return FetchResult{URL: final, Status: resp.StatusCode, HTML: string(body)}
}
