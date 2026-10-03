package crawler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UserAgent sent on every request, including the robots.txt fetch.
const UserAgent = "web-crawler-go/1.0"

// productToken is the token robots.txt rules match against ("name" in
// "name/version", compared case-insensitively per the de-facto standard).
const productToken = "web-crawler-go"

// Robots holds parsed robots.txt rules for one host. It implements the
// common subset of the spec: user-agent groups with allow/disallow lines,
// longest-prefix match, allow winning ties. Deliberately minimal: no
// wildcards, no crawl-delay, no sitemaps. Fail-open: a nil *Robots allows
// everything (used when robots.txt is unreachable).
type Robots struct {
	groups []robotsGroup
}

type robotsGroup struct {
	agents    []string
	allows    []string
	disallows []string
}

// ParseRobots parses robots.txt body text.
func ParseRobots(body string) *Robots {
	r := &Robots{}
	var cur *robotsGroup
	flush := func() {
		if cur != nil && (len(cur.allows) > 0 || len(cur.disallows) > 0 || len(cur.agents) > 0) {
			r.groups = append(r.groups, *cur)
		}
		cur = nil
	}
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		field = strings.ToLower(strings.TrimSpace(field))
		value = strings.TrimSpace(value)
		switch field {
		case "user-agent":
			// A new user-agent line after rules starts a new group; agent
			// lines before any rule extend the current group.
			if cur != nil && (len(cur.allows) > 0 || len(cur.disallows) > 0) {
				flush()
			}
			if cur == nil {
				cur = &robotsGroup{}
			}
			cur.agents = append(cur.agents, strings.ToLower(value))
		case "allow":
			if cur == nil {
				cur = &robotsGroup{}
			}
			cur.allows = append(cur.allows, value)
		case "disallow":
			if cur == nil {
				cur = &robotsGroup{}
			}
			cur.disallows = append(cur.disallows, value)
		}
	}
	flush()
	return r
}

// Allowed reports whether rawURL may be fetched.
func (r *Robots) Allowed(rawURL string) bool {
	if r == nil {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	path := u.EscapedPath()
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	var g *robotsGroup
	for i := range r.groups {
		for _, a := range r.groups[i].agents {
			if a == productToken {
				g = &r.groups[i] // exact product match wins
				break
			}
		}
		if g != nil {
			break
		}
	}
	if g == nil {
		for i := range r.groups {
			for _, a := range r.groups[i].agents {
				if a == "*" {
					g = &r.groups[i]
					break
				}
			}
			if g != nil {
				break
			}
		}
	}
	if g == nil {
		return true // no matching group: allowed
	}
	bestAllow, bestDisallow := -1, -1
	for _, a := range g.allows {
		if strings.HasPrefix(path, a) && len(a) > bestAllow {
			bestAllow = len(a)
		}
	}
	for _, d := range g.disallows {
		if d == "" {
			continue // "Disallow:" with empty value means allow all
		}
		if strings.HasPrefix(path, d) && len(d) > bestDisallow {
			bestDisallow = len(d)
		}
	}
	if bestDisallow < 0 {
		return true
	}
	return bestAllow >= bestDisallow // allow wins ties
}

// LoadRobots fetches and parses the seed host's robots.txt. It returns nil
// on any failure -- an unreachable robots.txt must not silently disable the
// crawler (fail-open, the standard behaviour).
func LoadRobots(seedURL string, timeout time.Duration) *Robots {
	u, err := url.Parse(seedURL)
	if err != nil {
		return nil
	}
	robotsURL := fmt.Sprintf("%s://%s/robots.txt", u.Scheme, u.Host)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode >= 400 {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	return ParseRobots(string(body))
}
