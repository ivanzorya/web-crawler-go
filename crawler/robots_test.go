package crawler

import "testing"

func TestRobotsBasicDisallow(t *testing.T) {
	r := ParseRobots("User-agent: *\nDisallow: /b\n")
	if !r.Allowed("http://x.test/a") {
		t.Error("/a should be allowed")
	}
	if r.Allowed("http://x.test/b") {
		t.Error("/b should be disallowed")
	}
	if r.Allowed("http://x.test/b/c") {
		t.Error("/b/c should be disallowed (prefix match)")
	}
}

func TestRobotsLongestMatchAllowWinsTies(t *testing.T) {
	r := ParseRobots("User-agent: *\nDisallow: /x\nAllow: /x/y\n")
	if r.Allowed("http://x.test/x/other") {
		t.Error("/x/other should be disallowed")
	}
	if !r.Allowed("http://x.test/x/y") {
		t.Error("/x/y should be allowed (longer allow rule)")
	}
	// Tie: allow wins.
	r = ParseRobots("User-agent: *\nDisallow: /t\nAllow: /t\n")
	if !r.Allowed("http://x.test/t") {
		t.Error("on tie, allow should win")
	}
}

func TestRobotsEmptyDisallowAllowsAll(t *testing.T) {
	r := ParseRobots("User-agent: *\nDisallow:\n")
	if !r.Allowed("http://x.test/anything") {
		t.Error("empty Disallow means allow all")
	}
}

func TestRobotsAgentMatching(t *testing.T) {
	body := "User-agent: other-bot\nDisallow: /a\n\nUser-agent: *\nDisallow: /b\n"
	r := ParseRobots(body)
	if r.Allowed("http://x.test/b") {
		t.Error("/b should be disallowed via the * group")
	}
	if !r.Allowed("http://x.test/a") {
		t.Error("/a should be allowed (rule is for another bot)")
	}
	// No matching group at all: allowed.
	r = ParseRobots("User-agent: some-bot\nDisallow: /\n")
	if !r.Allowed("http://x.test/") {
		t.Error("no matching group should allow")
	}
}

func TestRobotsNilAllowsAll(t *testing.T) {
	var r *Robots
	if !r.Allowed("http://x.test/anything") {
		t.Error("nil robots (fetch failed) must fail open")
	}
}

func TestRobotsQueryStringMatched(t *testing.T) {
	r := ParseRobots("User-agent: *\nDisallow: /search\n")
	if r.Allowed("http://x.test/search?q=hi") {
		t.Error("query string should be part of the match")
	}
}
