package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.URL.Query().Get("format") != "json" || r.URL.Query().Get("q") != "go news" {
			t.Errorf("unexpected request %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"A","url":"https://a.example","content":"x","publishedDate":"2026-10-01"},{"title":"no url"}]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL).Search(context.Background(), "go news")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "https://a.example" || got[0].Published != "2026-10-01" {
		t.Fatalf("results = %+v", got)
	}
}

func TestSearchErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	if _, err := New(srv.URL).Search(context.Background(), "q"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("want HTTP 403 error, got %v", err)
	}
	if _, err := New(srv.URL).Search(context.Background(), "  "); err == nil {
		t.Fatal("want error for an empty query")
	}
}

func TestFetchPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><title>Hi &amp; bye</title><script>var x=1</script></head><body><p>Hello</p><style>p{}</style> <b>world</b></body></html>`))
		case "/bin":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("x"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := newClient("", true)

	got, err := c.FetchPage(context.Background(), srv.URL+"/ok")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Title: Hi & bye\n\nHello world"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	for _, path := range []string{"/bin", "/missing"} {
		if _, err := c.FetchPage(context.Background(), srv.URL+path); err == nil {
			t.Errorf("%s: want an error", path)
		}
	}
	for _, bad := range []string{"ftp://x.example/a", "/relative", "not a url"} {
		if _, err := c.FetchPage(context.Background(), bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestFetchPageRefusesPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("private address was reached") }))
	defer srv.Close()
	if _, err := New("").FetchPage(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("want a non-public address error, got %v", err)
	}
}

func TestClip(t *testing.T) {
	if got := clip("héllo", 2); got != "hé…" {
		t.Fatalf("clip = %q", got)
	}
}
