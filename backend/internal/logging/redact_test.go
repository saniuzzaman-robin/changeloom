package logging

import (
	"net/http"
	"testing"
)

func TestRedactHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer abc")
	h.Set("Cookie", "sid=1")
	h.Set("X-Forwarded-For", "203.0.113.7")
	h.Set("User-Agent", "test")
	got := RedactHeaders(h)
	for _, name := range []string{"Authorization", "Cookie", "X-Forwarded-For"} {
		if got[name] != redacted {
			t.Errorf("%s = %q, want redacted", name, got[name])
		}
	}
	if got["User-Agent"] != "test" {
		t.Errorf("User-Agent = %q, want test", got["User-Agent"])
	}
}

func TestRedactQuery(t *testing.T) {
	if got := RedactQuery(""); got != "" {
		t.Errorf("empty query = %q", got)
	}
	got := RedactQuery("limit=20&token=abc&email=a%40b.c&q=secret-term")
	want := "email=%5Bredacted%5D&limit=20&q=%5Bredacted%5D&token=%5Bredacted%5D"
	if got != want {
		t.Errorf("RedactQuery = %q, want %q", got, want)
	}
}
