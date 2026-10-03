package crawler

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Stats is a snapshot of crawl progress.
type Stats struct {
	PagesVisited int64
	LinksFound   int64
	Errors       int64
	Elapsed      time.Duration
}

// Config tunes a Crawler. Zero values select sane defaults.
type Config struct {
	Workers         int           // concurrent fetchers (default 10)
	MaxPages        int           // stop after this many pages (default 1000)
	MaxDepth        int           // max link distance from seed; 0 = unlimited (default)
	RequestTimeout  time.Duration // per-request timeout (default 10s)
	PolitenessDelay time.Duration // pause between fetches per worker (default 0)
	RespectRobots   bool          // honour robots.txt (default true)
	Stdout          io.Writer     // crawl output (default os.Stdout)
	Stderr          io.Writer     // diagnostics (default os.Stderr)
}

func (c *Config) withDefaults() Config {
	out := *c
	if out.Workers <= 0 {
		out.Workers = 10
	}
	if out.MaxPages <= 0 {
		out.MaxPages = 1000
	}
	if out.RequestTimeout <= 0 {
		out.RequestTimeout = 10 * time.Second
	}
	if out.Stdout == nil {
		out.Stdout = os.Stdout
	}
	if out.Stderr == nil {
		out.Stderr = os.Stderr
	}
	return out
}

type job struct {
	url   string
	depth int
}

// Crawler is a breadth-first, single-host web crawler.
//
// Concurrency model: a fixed pool of worker goroutines pulls jobs off a
// channel frontier; each visit may enqueue newly discovered same-host URLs.
// A WaitGroup counts in-flight jobs; when it drains, the channel is closed
// and workers exit. The crawl ends when every worker has returned.
//
// Why this shape:
//   - goroutines, not threads/processes: crawling is I/O-bound, so
//     lightweight goroutines multiplexed over OS threads are the natural
//     fit; there is no CPU-bound work to parallelise.
//   - fixed worker pool: bounds concurrent connections, the politeness knob
//     that matters. Spawning a goroutine per discovered link would open
//     thousands of simultaneous connections the moment a hub page is
//     parsed.
//   - BFS: discovers the site level by level, pairing naturally with
//     MaxDepth.
type Crawler struct {
	seed   string
	cfg    Config
	fetch  *Fetcher
	robots *Robots

	mu   sync.Mutex
	seen map[string]struct{} // canonical URLs already fetched or queued

	printMu sync.Mutex // keeps one page's output lines together

	pagesVisited atomic.Int64
	linksFound   atomic.Int64
	errors       atomic.Int64
}

// NewCrawler validates the seed URL and returns a configured Crawler.
func NewCrawler(seedURL string, cfg Config) (*Crawler, error) {
	seed := Canonicalize(seedURL, "")
	if seed == "" {
		return nil, fmt.Errorf("not a valid http(s) URL: %q", seedURL)
	}
	cfg = cfg.withDefaults()
	return &Crawler{
		seed:  seed,
		cfg:   cfg,
		fetch: NewFetcher(cfg.RequestTimeout),
		seen:  make(map[string]struct{}),
	}, nil
}

// Run crawls until the frontier is exhausted or ctx is cancelled, then
// returns final stats. It prints each visited URL and its links to Stdout.
func (c *Crawler) Run(ctx context.Context) Stats {
	start := time.Now()
	if c.cfg.RespectRobots {
		c.robots = LoadRobots(c.seed, c.cfg.RequestTimeout)
	}

	// Channel capacity >= MaxPages >= any possible number of queued jobs, so
	// a worker's enqueue send can never block forever behind other workers
	// that are themselves trying to send (which would deadlock).
	jobs := make(chan job, c.cfg.MaxPages)
	var inFlight sync.WaitGroup
	var workers sync.WaitGroup

	// enqueue is called from workers; the mutex makes check-and-add atomic.
	enqueue := func(url string, depth int) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, ok := c.seen[url]; ok {
			return false
		}
		if len(c.seen) >= c.cfg.MaxPages {
			return false
		}
		c.seen[url] = struct{}{}
		inFlight.Add(1)
		jobs <- job{url: url, depth: depth}
		return true
	}

	worker := func() {
		defer workers.Done()
		for j := range jobs {
			c.visit(ctx, j, enqueue)
			inFlight.Done()
		}
	}

	enqueue(c.seed, 0)
	for i := 0; i < c.cfg.Workers; i++ {
		workers.Add(1)
		go worker()
	}
	go func() {
		inFlight.Wait()
		close(jobs)
	}()
	workers.Wait()

	stats := Stats{
		PagesVisited: c.pagesVisited.Load(),
		LinksFound:   c.linksFound.Load(),
		Errors:       c.errors.Load(),
		Elapsed:      time.Since(start),
	}
	fmt.Fprintf(c.cfg.Stderr, "\ndone: %d pages visited, %d links found, %d errors, %s elapsed\n",
		stats.PagesVisited, stats.LinksFound, stats.Errors, stats.Elapsed.Round(time.Millisecond))
	return stats
}

func (c *Crawler) printf(format string, args ...any) {
	c.printMu.Lock()
	defer c.printMu.Unlock()
	fmt.Fprintf(c.cfg.Stdout, format, args...)
}

func (c *Crawler) printfErr(format string, args ...any) {
	c.printMu.Lock()
	defer c.printMu.Unlock()
	fmt.Fprintf(c.cfg.Stderr, format, args...)
}

func (c *Crawler) visit(ctx context.Context, j job, enqueue func(string, int) bool) {
	if c.robots != nil && !c.robots.Allowed(j.url) {
		c.printfErr("SKIPPED (robots.txt): %s\n", j.url)
		return
	}

	res := c.fetch.Fetch(ctx, j.url)
	c.pagesVisited.Add(1)

	if res.Err != nil {
		if !res.Skipped {
			c.errors.Add(1)
		}
		label := "error"
		if res.Skipped {
			label = "skipped"
		}
		c.printf("VISITED %s  [%s: %v]\n", j.url, label, res.Err)
		return
	}

	if !SameHost(res.URL, c.seed) {
		// The seed redirected off-host: record the visit, but the spec
		// says stay on one subdomain, so don't extract or follow links.
		c.printf("VISITED %s  [redirected off-host to %s, not followed]\n", j.url, res.URL)
		return
	}

	// Remember the post-redirect URL so a later link to it doesn't refetch.
	if final := Canonicalize(res.URL, ""); final != "" {
		c.mu.Lock()
		c.seen[final] = struct{}{}
		c.mu.Unlock()
	}

	pageLinks := ExtractLinks(res.HTML, res.URL)
	c.linksFound.Add(int64(len(pageLinks)))

	c.printf("VISITED %s\n", j.url)
	for _, l := range pageLinks {
		c.printf("  -> %s\n", l)
	}

	if c.cfg.MaxDepth <= 0 || j.depth < c.cfg.MaxDepth {
		for _, l := range pageLinks {
			if SameHost(l, c.seed) {
				enqueue(l, j.depth+1)
			}
		}
	}

	if c.cfg.PolitenessDelay > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(c.cfg.PolitenessDelay):
		}
	}
}
