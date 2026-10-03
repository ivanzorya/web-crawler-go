// Package crawler implements a simple concurrent web crawler limited to a
// single host.
//
// URL canonicalisation exists for one reason: deciding whether two URLs
// point at the same page, so the crawler doesn't visit it twice. The rules
// here are deliberately *conservative* -- they only merge URLs that are
// provably the same resource. Merging aggressively (e.g. stripping trailing
// slashes or sorting query params) risks skipping genuinely different
// pages; the cost of being conservative is at worst a few duplicate
// fetches. See README.
package crawler

import (
	"net/url"
	"strings"
)

// Canonicalize resolves raw (possibly relative) against base and returns a
// canonical absolute URL, or "" for non-HTTP(S) or unparseable input.
//
// Normalisation applied: relative references resolved, scheme and host
// lowercased, default ports (:80, :443) removed, fragments stripped, empty
// path normalised to "/". Deliberately NOT applied: path lowercasing,
// trailing-slash stripping, query-param sorting, dot-segment resolution of
// absolute URLs (only relative resolution touches those).
func Canonicalize(raw, base string) string {
	var u *url.URL
	var err error
	if base != "" {
		b, err := url.Parse(base)
		if err != nil {
			return ""
		}
		// ResolveReference handles relative refs, protocol-relative URLs
		// and dot segments.
		u, err = b.Parse(raw)
		if err != nil {
			return ""
		}
	} else {
		u, err = url.Parse(raw)
		if err != nil {
			return ""
		}
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	if port := u.Port(); port != "" {
		if (u.Scheme == "http" && port != "80") || (u.Scheme == "https" && port != "443") {
			host += ":" + port
		}
	}
	u.User = nil // never let credentials leak into dedup keys or logs
	u.Host = host
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

// SameHost reports whether a and b are on exactly the same host -- the
// "one subdomain" rule. Ports are ignored: they don't define subdomains.
func SameHost(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	return strings.EqualFold(ua.Hostname(), ub.Hostname())
}
