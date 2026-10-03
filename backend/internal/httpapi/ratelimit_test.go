package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterRefillsAndEvicts(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := newRateLimiter(60, func() time.Time { return now })

	for i := range 60 {
		if ok, _ := l.allow("a"); !ok {
			t.Fatalf("request %d within the burst was rejected", i)
		}
	}
	ok, wait := l.allow("a")
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("over the burst: allow = (%v, %s), want rejected with a wait up to 1s", ok, wait)
	}
	if ok, _ := l.allow("b"); !ok {
		t.Fatal("another key shares the bucket")
	}

	now = now.Add(time.Second)
	if ok, _ := l.allow("a"); !ok {
		t.Fatal("one token should refill after a second at 60/min")
	}

	now = now.Add(refillWindow)
	l.allow("c")
	if _, ok := l.buckets["a"]; ok || len(l.buckets) != 1 {
		t.Fatalf("buckets after a idle window = %d (a kept: %v), want only c", len(l.buckets), ok)
	}
}

func TestClientIP(t *testing.T) {
	for _, tc := range []struct {
		name string
		xff  []string
		want string
	}{
		{"peer address", nil, "192.0.2.1"},
		{"single entry", []string{"203.0.113.9"}, "203.0.113.9"},
		{"spoofed prefix", []string{"10.0.0.1, 203.0.113.9"}, "203.0.113.9"},
		{"repeated header", []string{"10.0.0.1", "198.51.100.7"}, "198.51.100.7"},
		{"empty header", []string{""}, "192.0.2.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/me", nil)
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientIP(r); got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
