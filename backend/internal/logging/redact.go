package logging

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	redacted       = "[redacted]"
	maxValueLength = 100
)

// redactedHeaders hold credentials or client addresses and are never logged (lower case).
var redactedHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
	"x-firebase-appcheck": true,
	"x-forwarded-for":     true,
	"x-real-ip":           true,
	"forwarded":           true,
}

// sensitiveQueryKey matches query keys whose values are credentials or personal data, including search terms.
var sensitiveQueryKey = regexp.MustCompile(`(?i)^(q|query|search)$|token|secret|code|pass|key|sig|email|auth`)

func clip(s string) string {
	if len(s) <= maxValueLength {
		return s
	}
	return s[:maxValueLength] + "…"
}

// RedactHeaders returns the request headers for logging, with credentials and client addresses hidden.
func RedactHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for name, values := range h {
		if redactedHeaders[strings.ToLower(name)] {
			out[name] = redacted
			continue
		}
		out[name] = clip(strings.Join(values, ", "))
	}
	return out
}

// RedactQuery returns the query string for logging, with sensitive values hidden and long ones
// clipped. It is empty when there are no parameters.
func RedactQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	params, err := url.ParseQuery(rawQuery)
	if err != nil {
		return redacted
	}
	for key, values := range params {
		for i, v := range values {
			if sensitiveQueryKey.MatchString(key) {
				values[i] = redacted
			} else {
				values[i] = clip(v)
			}
		}
	}
	return params.Encode()
}
