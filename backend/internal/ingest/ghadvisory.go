package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGHAdvisoryURL   = "https://api.github.com/advisories"
	defaultGHAdvisoryLimit = 50
	maxGHAdvisoryLimit     = 100
)

type ghAdvisoryConfig struct {
	URL       string `json:"url"`
	Ecosystem string `json:"ecosystem"`
	Severity  string `json:"severity"`
	Limit     int    `json:"limit"`
}

func (c ghAdvisoryConfig) validate() error {
	if c.URL != "" {
		if err := requireHTTPURL("url", c.URL); err != nil {
			return err
		}
	}
	if c.Limit < 0 || c.Limit > maxGHAdvisoryLimit {
		return fmt.Errorf("limit must be between 1 and %d, got %d", maxGHAdvisoryLimit, c.Limit)
	}
	return nil
}

// GHAdvisory reads reviewed advisories from the GitHub Advisory Database API.
// Optional config: ecosystem (e.g. "go"), severity (e.g. "critical"), limit.
type GHAdvisory struct{ Client *http.Client }

type ghAdvisory struct {
	GHSAID          string     `json:"ghsa_id"`
	CVEID           string     `json:"cve_id"`
	HTMLURL         string     `json:"html_url"`
	Summary         string     `json:"summary"`
	Description     string     `json:"description"`
	Severity        string     `json:"severity"`
	PublishedAt     *time.Time `json:"published_at"`
	Vulnerabilities []struct {
		Package struct {
			Ecosystem string `json:"ecosystem"`
			Name      string `json:"name"`
		} `json:"package"`
		VulnerableVersionRange string `json:"vulnerable_version_range"`
		FirstPatchedVersion    string `json:"first_patched_version"`
	} `json:"vulnerabilities"`
}

// Fetch implements Adapter.
func (a GHAdvisory) Fetch(ctx context.Context, src Source) (Result, error) {
	cfg, err := parseConfig[ghAdvisoryConfig](src.Config)
	if err != nil {
		return Result{}, err
	}
	base := cmpOr(cfg.URL, defaultGHAdvisoryURL)
	limit := cfg.Limit
	if limit == 0 {
		limit = defaultGHAdvisoryLimit
	}
	q := url.Values{"type": {"reviewed"}, "sort": {"published"}, "direction": {"desc"}, "per_page": {strconv.Itoa(limit)}}
	if cfg.Ecosystem != "" {
		q.Set("ecosystem", cfg.Ecosystem)
	}
	if cfg.Severity != "" {
		q.Set("severity", cfg.Severity)
	}
	header := http.Header{
		"Accept":               {"application/vnd.github+json"},
		"X-Github-Api-Version": {"2022-11-28"},
	}
	resp, err := get(ctx, a.Client, base+"?"+q.Encode(), &src, header)
	if err != nil {
		return Result{}, err
	}
	if resp.notModified {
		return Result{NotModified: true}, nil
	}

	var advisories []ghAdvisory
	if err := json.Unmarshal(resp.body, &advisories); err != nil {
		return Result{}, fmt.Errorf("decode github advisories: %w", err)
	}
	items := make([]Item, 0, len(advisories))
	for _, adv := range advisories {
		if adv.HTMLURL == "" || adv.Summary == "" {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s (severity: %s)\n", adv.GHSAID, adv.Severity)
		if adv.CVEID != "" {
			fmt.Fprintf(&b, "CVE: %s\n", adv.CVEID)
		}
		for _, v := range adv.Vulnerabilities {
			fmt.Fprintf(&b, "Affected: %s/%s %s (patched in: %s)\n",
				v.Package.Ecosystem, v.Package.Name, v.VulnerableVersionRange, cmpOr(v.FirstPatchedVersion, "none"))
		}
		b.WriteString("\n" + adv.Description)
		items = append(items, Item{
			ExternalID:  adv.GHSAID,
			URL:         adv.HTMLURL,
			Title:       adv.Summary,
			Content:     b.String(),
			PublishedAt: utc(adv.PublishedAt),
		})
	}
	return Result{Items: items, ETag: resp.etag, LastModified: resp.lastModified}, nil
}

func cmpOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
