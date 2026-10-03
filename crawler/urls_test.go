package crawler

import "testing"

func TestCanonicalize(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		base string
		want string
	}{
		{"lowercases scheme/host, drops default port, strips fragment",
			"HTTP://Example.COM:80/a?x=1#frag", "", "http://example.com/a?x=1"},
		{"keeps non-default port",
			"https://example.com:8443/", "", "https://example.com:8443/"},
		{"empty path becomes slash",
			"https://example.com", "", "https://example.com/"},
		{"resolves relative ref",
			"rel/page", "https://example.com/dir/", "https://example.com/dir/rel/page"},
		{"resolves dot segments of relative refs",
			"../up", "https://example.com/dir/", "https://example.com/up"},
		{"resolves protocol-relative URL",
			"//other.com/x", "https://example.com/", "https://other.com/x"},
		{"does not resolve dot segments of absolute URLs",
			"http://example.com/a/../b", "", "http://example.com/a/../b"},
		{"rejects mailto", "mailto:foo@bar.com", "", ""},
		{"rejects javascript", "javascript:void(0)", "", ""},
		{"rejects ftp", "ftp://example.com/x", "", ""},
		{"rejects garbage", "not a url", "", ""},
		{"rejects empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Canonicalize(tt.raw, tt.base); got != tt.want {
				t.Errorf("Canonicalize(%q, %q) = %q, want %q", tt.raw, tt.base, got, tt.want)
			}
		})
	}
}

func TestSameHost(t *testing.T) {
	if !SameHost("https://a.example.com/x", "https://a.example.com/y") {
		t.Error("same host with different paths should match")
	}
	for _, pair := range [][2]string{
		{"https://a.example.com/", "https://b.example.com/"},
		{"https://example.com/", "https://a.example.com/"},
	} {
		if SameHost(pair[0], pair[1]) {
			t.Errorf("SameHost(%q, %q) = true, want false", pair[0], pair[1])
		}
	}
	// Ports don't define subdomains: same host on another port is in scope.
	if !SameHost("http://a.example.com:80/", "http://a.example.com:8080/") {
		t.Error("same host on different ports should match")
	}
}
