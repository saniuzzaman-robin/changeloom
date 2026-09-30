package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	userAgent    = "Changeloom/1.0"
	maxBodyBytes = 10 << 20
	httpTimeout  = 30 * time.Second
)

// NewHTTPClient returns the client used by all adapters and the extractor.
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: httpTimeout}
}

type response struct {
	body         []byte
	notModified  bool
	etag         string
	lastModified string
}

// get issues a conditional GET. A 304 yields notModified; any other non-2xx status is an error.
func get(ctx context.Context, client *http.Client, url string, src *Source, header http.Header) (response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return response{}, fmt.Errorf("build request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range header {
		req.Header[k] = v
	}
	if src != nil {
		if src.ETag != "" {
			req.Header.Set("If-None-Match", src.ETag)
		}
		if src.LastModified != "" {
			req.Header.Set("If-Modified-Since", src.LastModified)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		return response{notModified: true}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return response{}, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return response{}, fmt.Errorf("read %s: %w", url, err)
	}
	if len(body) > maxBodyBytes {
		return response{}, fmt.Errorf("GET %s: response larger than %d bytes", url, maxBodyBytes)
	}
	return response{
		body:         body,
		etag:         resp.Header.Get("ETag"),
		lastModified: resp.Header.Get("Last-Modified"),
	}, nil
}

// parseConfig decodes a source config, rejecting unknown keys so typos surface at sync time.
func parseConfig[T interface{ validate() error }](raw json.RawMessage) (T, error) {
	var cfg T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("invalid source config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("invalid source config: %w", err)
	}
	return cfg, nil
}

func requireHTTPURL(field, v string) error {
	if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
		return fmt.Errorf("%s must be an http(s) URL, got %q", field, v)
	}
	return nil
}
