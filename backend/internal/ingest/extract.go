package ingest

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
)

// Extractor pulls the main article text out of a web page.
type Extractor interface {
	Extract(ctx context.Context, pageURL string) (string, error)
}

// ReadabilityExtractor fetches a page and extracts its readable text.
type ReadabilityExtractor struct{ Client *http.Client }

// Extract implements Extractor.
func (e ReadabilityExtractor) Extract(ctx context.Context, pageURL string) (string, error) {
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return "", fmt.Errorf("parse url %q: %w", pageURL, err)
	}
	resp, err := get(ctx, e.Client, pageURL, nil, http.Header{"Accept": {"text/html,application/xhtml+xml"}})
	if err != nil {
		return "", err
	}
	article, err := readability.FromReader(bytes.NewReader(resp.body), parsedURL)
	if err != nil {
		return "", fmt.Errorf("extract article from %s: %w", pageURL, err)
	}
	var text strings.Builder
	if err := article.RenderText(&text); err != nil {
		return "", fmt.Errorf("render article text from %s: %w", pageURL, err)
	}
	return strings.TrimSpace(text.String()), nil
}
