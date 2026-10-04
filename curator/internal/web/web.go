// Package web gives a model without built-in browsing a search tool (a self-hosted SearXNG) and a
// page fetcher.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	requestTimeout = 30 * time.Second
	maxRedirects   = 5
	// maxBodyBytes bounds what is read from a page.
	maxBodyBytes = 2 << 20
	// MaxPageChars bounds the text returned for one page.
	MaxPageChars = 12000
	// MaxResults bounds the results returned for one search.
	MaxResults  = 8
	userAgent   = "changeloom-curator/1.0"
	snippetLen  = 400
	searchLimit = 1 << 20
)

// SearchResult is one search hit.
type SearchResult struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Snippet   string `json:"snippet"`
	Published string `json:"published,omitempty"`
}

// Client searches and fetches.
type Client struct {
	searxngURL string
	search     *http.Client
	pages      *http.Client
}

// New returns a client that searches the SearXNG instance at searxngURL. Pages are fetched over
// plain HTTP(S) and never from loopback or private addresses.
func New(searxngURL string) *Client {
	return newClient(searxngURL, false)
}

func newClient(searxngURL string, allowPrivate bool) *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		dialer.Control = refusePrivate
	}
	pages := &http.Client{
		Timeout:   requestTimeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	return &Client{searxngURL: searxngURL, search: &http.Client{Timeout: requestTimeout}, pages: pages}
}

// refusePrivate rejects connections to addresses a fetched URL must not reach (checked after DNS
// resolution, so redirects and rebinding are covered).
func refusePrivate(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return fmt.Errorf("refusing to fetch from non-public address %s", host)
	}
	return nil
}

type searxngResponse struct {
	Results []struct {
		Title         string `json:"title"`
		URL           string `json:"url"`
		Content       string `json:"content"`
		PublishedDate string `json:"publishedDate"`
	} `json:"results"`
}

// Search runs query on SearXNG and returns at most MaxResults hits.
func (c *Client) Search(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("search query is empty")
	}
	u, err := url.Parse(c.searxngURL + "/search")
	if err != nil {
		return nil, fmt.Errorf("parse SEARXNG_URL %q: %w", c.searxngURL, err)
	}
	u.RawQuery = url.Values{"q": {query}, "format": {"json"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.search.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search %q (is SearXNG up? make searxng-up): %w", c.searxngURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, searchLimit))
	if err != nil {
		return nil, fmt.Errorf("read search response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned HTTP %d (the SearXNG json format must be enabled): %s", resp.StatusCode, clip(string(body), 200))
	}
	var out searxngResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	results := make([]SearchResult, 0, MaxResults)
	for _, r := range out.Results {
		if r.URL == "" {
			continue
		}
		results = append(results, SearchResult{Title: r.Title, URL: r.URL, Snippet: clip(r.Content, snippetLen), Published: r.PublishedDate})
		if len(results) == MaxResults {
			break
		}
	}
	return results, nil
}

var (
	dropBlocks = regexp.MustCompile(`(?is)<(script|style|noscript|svg|head)\b.*?</(script|style|noscript|svg|head)>`)
	anyTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	spaces     = regexp.MustCompile(`\s+`)
	titleTag   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// FetchPage downloads rawURL and returns its title and readable text, cut to MaxPageChars.
func (c *Client) FetchPage(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("fetch_page needs an absolute http(s) URL, got %q", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return "", fmt.Errorf("build page request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain")
	resp, err := c.pages.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", u.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch %s: HTTP %d", u.Redacted(), resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "text/") && !strings.Contains(ct, "xml") {
		return "", fmt.Errorf("fetch %s: unsupported content type %q", u.Redacted(), ct)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", u.Redacted(), err)
	}
	return pageText(string(body)), nil
}

// pageText reduces HTML to its title and visible text.
func pageText(raw string) string {
	var title string
	if m := titleTag.FindStringSubmatch(raw); m != nil {
		title = squash(m[1])
	}
	text := squash(anyTag.ReplaceAllString(dropBlocks.ReplaceAllString(raw, " "), " "))
	if title != "" {
		text = "Title: " + title + "\n\n" + text
	}
	return clip(text, MaxPageChars)
}

func squash(s string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(s), " "))
}

// clip cuts s to at most n runes.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
