// Command crawler crawls all pages on a single host, starting from a URL.
//
// Usage:
//
//	crawler [flags] <url>
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/ivanzorya/web-crawler-go/crawler"
)

func main() {
	workers := flag.Int("workers", 10, "concurrent fetch workers")
	maxPages := flag.Int("max-pages", 1000, "stop after this many pages")
	maxDepth := flag.Int("max-depth", 0, "max link distance from the seed (0 = unlimited)")
	delay := flag.Duration("delay", 0, "pause between fetches per worker (e.g. 500ms)")
	noRobots := flag.Bool("no-robots", false, "ignore robots.txt")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: crawler [flags] <url>")
		flag.PrintDefaults()
		os.Exit(2)
	}

	c, err := crawler.NewCrawler(flag.Arg(0), crawler.Config{
		Workers:         *workers,
		MaxPages:        *maxPages,
		MaxDepth:        *maxDepth,
		PolitenessDelay: *delay,
		RespectRobots:   !*noRobots,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	// Ctrl-C cancels the context: in-flight fetches abort and the crawl
	// winds down, still printing a partial summary.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c.Run(ctx)
	if ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, "interrupted -- partial results above")
		os.Exit(130)
	}
}
