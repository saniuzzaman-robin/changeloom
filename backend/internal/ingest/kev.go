package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultKEVURL = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"

type kevConfig struct {
	URL string `json:"url"`
}

func (c kevConfig) validate() error {
	if c.URL == "" {
		return nil
	}
	return requireHTTPURL("url", c.URL)
}

// KEV reads the CISA Known Exploited Vulnerabilities catalog.
type KEV struct{ Client *http.Client }

type kevCatalog struct {
	Vulnerabilities []struct {
		CVEID             string `json:"cveID"`
		VendorProject     string `json:"vendorProject"`
		Product           string `json:"product"`
		VulnerabilityName string `json:"vulnerabilityName"`
		DateAdded         string `json:"dateAdded"`
		ShortDescription  string `json:"shortDescription"`
		RequiredAction    string `json:"requiredAction"`
		DueDate           string `json:"dueDate"`
		KnownRansomware   string `json:"knownRansomwareCampaignUse"`
	} `json:"vulnerabilities"`
}

// Fetch implements Adapter.
func (a KEV) Fetch(ctx context.Context, src Source) (Result, error) {
	cfg, err := parseConfig[kevConfig](src.Config)
	if err != nil {
		return Result{}, err
	}
	resp, err := get(ctx, a.Client, cmpOr(cfg.URL, defaultKEVURL), &src, nil)
	if err != nil {
		return Result{}, err
	}
	if resp.notModified {
		return Result{NotModified: true}, nil
	}

	var catalog kevCatalog
	if err := json.Unmarshal(resp.body, &catalog); err != nil {
		return Result{}, fmt.Errorf("decode CISA KEV catalog: %w", err)
	}
	items := make([]Item, 0, len(catalog.Vulnerabilities))
	for _, v := range catalog.Vulnerabilities {
		if v.CVEID == "" {
			continue
		}
		var published *time.Time
		if t, err := time.Parse(time.DateOnly, v.DateAdded); err == nil {
			published = &t
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s: %s\nVendor/product: %s %s\n%s\n\nRequired action: %s\nDue date: %s\nKnown ransomware campaign use: %s",
			v.CVEID, v.VulnerabilityName, v.VendorProject, v.Product, v.ShortDescription, v.RequiredAction, v.DueDate, v.KnownRansomware)
		items = append(items, Item{
			ExternalID:  v.CVEID,
			URL:         "https://nvd.nist.gov/vuln/detail/" + v.CVEID,
			Title:       fmt.Sprintf("%s: %s (%s %s)", v.CVEID, v.VulnerabilityName, v.VendorProject, v.Product),
			Content:     b.String(),
			PublishedAt: published,
		})
	}
	return Result{Items: items, ETag: resp.etag, LastModified: resp.lastModified}, nil
}
