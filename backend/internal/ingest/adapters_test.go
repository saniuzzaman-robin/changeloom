package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func fixtureServer(t *testing.T, file, contentType string, gotQuery *string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile("testdata/" + file) //nolint:gosec // fixed fixture names from this file
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotQuery != nil {
			*gotQuery = r.URL.RawQuery
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func cfg(t *testing.T, v map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRSSFetch(t *testing.T) {
	srv := fixtureServer(t, "feed.atom", "application/atom+xml", nil)
	src := Source{Kind: KindRSS, Config: cfg(t, map[string]any{"url": srv.URL})}

	res, err := RSS{Client: srv.Client()}.Fetch(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("got %d items, want 2 (entry without link is dropped): %+v", len(res.Items), res.Items)
	}
	first := res.Items[0]
	if first.Title != "Release 2.0" || first.Content != "Short excerpt." || first.PublishedAt == nil {
		t.Errorf("unexpected first item: %+v", first)
	}
	if res.ETag != `"v1"` {
		t.Errorf("etag = %q, want %q", res.ETag, `"v1"`)
	}

	src.ETag = res.ETag
	res, err = RSS{Client: srv.Client()}.Fetch(context.Background(), src)
	if err != nil || !res.NotModified {
		t.Fatalf("conditional fetch: notModified=%v err=%v", res.NotModified, err)
	}
}

func TestGHAdvisoryFetch(t *testing.T) {
	var query string
	srv := fixtureServer(t, "advisories.json", "application/json", &query)
	src := Source{Kind: KindGHAdvisory, Config: cfg(t, map[string]any{"url": srv.URL, "severity": "critical", "ecosystem": "go"})}

	res, err := GHAdvisory{Client: srv.Client()}.Fetch(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"severity=critical", "ecosystem=go", "type=reviewed"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q missing %q", query, want)
		}
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(res.Items))
	}
	it := res.Items[0]
	if it.ExternalID != "GHSA-aaaa-bbbb-cccc" || it.Title != "Remote code execution in example/lib" {
		t.Errorf("unexpected item: %+v", it)
	}
	for _, want := range []string{"CVE-2026-0001", "go/example/lib < 1.2.3", "patched in: 1.2.3", "arbitrary code"} {
		if !strings.Contains(it.Content, want) {
			t.Errorf("content %q missing %q", it.Content, want)
		}
	}
}

func TestKEVFetch(t *testing.T) {
	srv := fixtureServer(t, "kev.json", "application/json", nil)
	src := Source{Kind: KindKEV, Config: cfg(t, map[string]any{"url": srv.URL})}

	res, err := KEV{Client: srv.Client()}.Fetch(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(res.Items))
	}
	it := res.Items[0]
	if it.URL != "https://nvd.nist.gov/vuln/detail/CVE-2026-1111" || it.PublishedAt == nil || it.PublishedAt.Day() != 27 {
		t.Errorf("unexpected item: %+v", it)
	}
	if !strings.Contains(it.Content, "Apply updates per vendor instructions.") {
		t.Errorf("content missing required action: %q", it.Content)
	}
}

func TestHNFetch(t *testing.T) {
	var query string
	srv := fixtureServer(t, "hn.json", "application/json", &query)
	src := Source{Kind: KindHN, Config: cfg(t, map[string]any{"url": srv.URL, "min_points": 150, "query": "release"})}

	res, err := HN{Client: srv.Client()}.Fetch(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "numericFilters=points%3E%3D150") || !strings.Contains(query, "query=release") {
		t.Errorf("unexpected query %q", query)
	}
	if len(res.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(res.Items))
	}
	if res.Items[1].URL != "https://news.ycombinator.com/item?id=222" || res.Items[1].Content != "What do you use?" {
		t.Errorf("story without url should link to the HN item: %+v", res.Items[1])
	}
}

func TestFetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	src := Source{Kind: KindKEV, Config: cfg(t, map[string]any{"url": srv.URL})}
	if _, err := (KEV{Client: srv.Client()}).Fetch(context.Background(), src); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want error mentioning status 500, got %v", err)
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name string
		kind string
		cfg  string
		ok   bool
	}{
		{"rss ok", KindRSS, `{"url":"https://x.test/feed"}`, true},
		{"rss missing url", KindRSS, `{}`, false},
		{"rss unknown key", KindRSS, `{"url":"https://x.test","urll":"x"}`, false},
		{"kev defaults", KindKEV, `{}`, true},
		{"hn needs min_points", KindHN, `{"query":"go"}`, false},
		{"hn ok", KindHN, `{"min_points":100}`, true},
		{"gh limit too high", KindGHAdvisory, `{"limit":500}`, false},
		{"html unsupported", "html", `{}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.kind, json.RawMessage(tt.cfg))
			if (err == nil) != tt.ok {
				t.Errorf("ValidateConfig err = %v, want ok=%t", err, tt.ok)
			}
		})
	}
}
