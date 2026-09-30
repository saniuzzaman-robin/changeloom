package ingest

import "testing"

func TestNormalizeURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://Example.com/a/?utm_source=x&b=2&a=1#frag", "https://example.com/a?a=1&b=2"},
		{"http://example.com:80/post", "http://example.com/post"},
		{"https://example.com:8443/post/", "https://example.com:8443/post"},
		{"https://example.com/", "https://example.com/"},
		{"  https://example.com/x?fbclid=abc  ", "https://example.com/x"},
	}
	for _, tt := range tests {
		got, err := NormalizeURL(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{"", "/relative", "ftp://example.com/x", "not a url"} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Errorf("NormalizeURL(%q) should fail", bad)
		}
	}
}
