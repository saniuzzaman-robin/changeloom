package ingest

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// trackingParams are query parameters that never change which page a URL points to.
var trackingParams = map[string]bool{"fbclid": true, "gclid": true, "mc_cid": true, "mc_eid": true, "ref_src": true}

// NormalizeURL canonicalizes u so the same page always yields the same string:
// lowercase scheme and host, no default port, no fragment, no tracking parameters,
// sorted query and no trailing slash on non-root paths.
func NormalizeURL(u string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		return "", fmt.Errorf("parse url %q: %w", u, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", fmt.Errorf("url %q is not an absolute http(s) URL", u)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	defaultPort := (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443")
	if port != "" && !defaultPort {
		host += ":" + port
	}
	parsed.Host = host
	parsed.Fragment = ""
	parsed.User = nil
	if parsed.Path != "/" {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
		parsed.RawPath = ""
	}

	q := parsed.Query()
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), "utm_") || trackingParams[strings.ToLower(k)] {
			q.Del(k)
		}
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var rq strings.Builder
	for _, k := range keys {
		vals := q[k]
		sort.Strings(vals)
		for _, v := range vals {
			if rq.Len() > 0 {
				rq.WriteByte('&')
			}
			rq.WriteString(url.QueryEscape(k) + "=" + url.QueryEscape(v))
		}
	}
	parsed.RawQuery = rq.String()
	return parsed.String(), nil
}

// URLHash is the dedupe key stored in raw_items.url_hash.
func URLHash(normalized string) []byte {
	sum := sha256.Sum256([]byte(normalized))
	return sum[:]
}
