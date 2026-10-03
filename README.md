# Web Crawler (Go)

A simple concurrent web crawler in Go. Given a starting URL, it visits every
page on the same host (one subdomain only), prints each URL visited along
with the links found on that page, and never follows external links.

```bash
go build ./...
./crawler [flags] <url>
```

```
VISITED https://example.com/
  -> https://example.com/about
  -> https://example.com/blog/
VISITED https://example.com/about
  -> https://example.com/
...
done: 42 pages visited, 137 links found, 3 errors, 4.2s elapsed
```

Flags: `-workers` (default 10), `-max-pages` (default 1000), `-max-depth`
(0 = unlimited), `-delay` (e.g. `500ms`), `-no-robots`. Note: Go's `flag`
package stops parsing at the first positional argument, so flags go before
the URL.

Tests (including the race detector -- non-negotiable for concurrent code):

```bash
go test -race ./...
```

Note: `https://crawlme.monzo.com/` no longer resolves (retired by Monzo), so
this was developed and tested against local fixture servers (`httptest`).
Point it at any live site instead.

## Layout

```
cmd/crawler/main.go   CLI: flags -> signal-aware context -> Crawler.Run
crawler/crawler.go    Crawler: channel frontier + worker goroutine pool, BFS
crawler/fetcher.go    HTTP GET with timeouts, body-size cap, content-type gate
crawler/links.go      <a href> extraction via golang.org/x/net/html
crawler/urls.go       canonicalisation + exact-host comparison
crawler/robots.go     minimal robots.txt parser (fail-open)
crawler/*_test.go     unit + httptest end-to-end tests
```

## Design decisions and trade-offs

**1. Goroutines + channels, not threads.** Crawling is I/O-bound: workers
spend ~99% of their time waiting on sockets. Goroutines multiplexed over a
small OS thread pool are the idiomatic fit; there is no CPU-bound work that
would benefit from real parallelism.

**2. Fixed worker pool, not goroutine-per-link.** Spawning a goroutine per
discovered link would open thousands of simultaneous connections the moment
a hub page is parsed -- a self-inflicted DDoS. A fixed pool of N workers
pulling from a channel bounds concurrency to N connections, which is also
the politeness knob (`-workers`, plus `-delay`).

**3. Buffered channel sized to MaxPages -- and why that's load-bearing.**
Workers enqueue newly found URLs by sending on the jobs channel. With an
*unbuffered* (or too-small) channel, all workers could end up blocked on
send while none is receiving -- a classic deadlock. The invariant here:
channel capacity (MaxPages) >= any possible number of queued jobs, because
enqueueing stops once `len(seen) == MaxPages`. So a send can never block
forever. This is the kind of thing `-race` won't catch; it needs the
invariant written down.

**4. Mutex-guarded map for `seen`, not sync.Map.** Dedup needs check-*and*-
add to be atomic. `sync.Map`'s `LoadOrStore` would work, but a plain map
with a mutex makes the critical section explicit and keeps the zero value
usable. Contention is negligible: the critical section is two map ops.

**5. WaitGroup for in-flight jobs; shutdown via close(jobs).** Each queued
job holds one WaitGroup count; a worker calls Done after visiting. A
closer goroutine waits, then closes the channel, which makes every
`for range jobs` loop exit. `Add` is only ever called while the counter is
>= 1 (a worker holds its own job's count while enqueueing children), so
the counter can't hit zero prematurely -- the one case where Add racing
Wait is unsafe. Termination is therefore exact: no page lost, no goroutine
leaked.

**6. Conservative URL canonicalisation.** Same rule as any correct crawler:
merge only provably-identical URLs (lowercase scheme/host, drop default
ports, strip fragments, resolve relative refs). Deliberately *not*
lowercasing paths, stripping trailing slashes, or sorting query params --
those can alias genuinely different pages. A duplicate fetch is a cheaper
mistake than a skipped page.

**7. Failures are data, never panics.** `Fetch` returns a `FetchResult`
describing timeouts, DNS failures, HTTP errors, non-HTML content and
oversized bodies. One bad page must never take down the crawl. Genuine
errors increment the error count; deliberate skips (PDF, 2 MiB+ body)
don't.

**8. Body-size cap via LimitReader(MaxBodyBytes+1).** Reading one byte past
the cap detects oversized bodies without buffering them. `http.Client`
`Timeout` covers the whole exchange including redirects (10 max).

**9. robots.txt respected, fail-open, hand-rolled parser.** A production
crawler that ignores robots.txt gets blocked. The parser implements the
common subset (user-agent groups, allow/disallow, longest-prefix match,
allow wins ties) with zero new dependencies -- ~80 lines I can fully
explain, versus a library I'd have to take on trust. If robots.txt is
unreachable, crawling proceeds: an unreachable robots.txt must not
silently disable the crawler.

**10. One third-party dependency: x/net/html.** The standard library has
no HTML parser, and regex-based tag scanning is how crawlers get parsing
bugs. Everything else is stdlib.

**11. No JavaScript rendering.** Links injected by client-side JS are
invisible. Supporting them means headless Chrome per page -- 10-100x
slower and a different project. Documented limitation.

**12. In-memory frontier and `seen` set.** Fine for thousands of pages;
the simplest thing that works. Doesn't survive restarts -- see scaling.

## Concurrency in more depth

The crawl is producer-consumer with a twist: consumers are also producers.
The WaitGroup is the exact-termination mechanism (see decision 5).
`Ctrl-C` cancels the context: in-flight fetches abort via
`NewRequestWithContext`, workers wind down, and a partial summary still
prints. Stats use `atomic.Int64` (lock-free counters); printing uses a
dedicated mutex so one page's lines never interleave with another's.
`go vet` is clean and `go test -race` passes.

## Scaling: what breaks and what I'd change

- **`seen` map memory.** ~10M URLs is ~1 GB. Fix: Bloom filter (tiny
  false-positive rate = occasionally skipping a page) or disk-backed store.
- **Single-process frontier.** Fix: external queue (Redis), N crawler
  processes, shard by hash(host) so politeness state stays local.
- **Politeness.** Currently global to the run. Multi-host needs per-host
  token buckets and robots crawl-delay.
- **Throughput.** `net/http` handles thousands of concurrent connections;
  next bottlenecks are DNS and HTML parsing (x/net/html is pure Go -- lxml
  isn't an option here; profile first).
- **Robustness.** Retries with exponential backoff for 5xx/timeouts
  (currently one strike and out), persistent frontier for crash recovery,
  structured (JSON lines) output option.

## What I'd do with more time (prioritised)

1. Retry with backoff for transient failures -- biggest correctness win.
2. Per-host politeness (token bucket) -- required before multi-host.
3. JSON-lines output alongside the human-readable print.
4. Persist frontier + seen set for resumable crawls.

## Time log

Built in ~2 hours: core crawler + CLI (~45 min), tests incl. race detector
(~45 min), README/decisions (~30 min).
