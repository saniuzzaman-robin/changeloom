package httpapi

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// refillWindow is the period a bucket's whole capacity refills over.
const refillWindow = time.Minute

// rateLimiter keeps an in-memory token bucket per key (a client IP or a user). Each bucket holds
// perMinute tokens and refills at perMinute per minute. Buckets idle for a whole refillWindow are
// full again, so they are dropped, which bounds memory by the keys seen in the last minute. Limits
// are per instance: with several Cloud Run instances a client gets up to that many times the limit.
type rateLimiter struct {
	limit rate.Limit
	burst int
	now   func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

func newRateLimiter(perMinute int, now func() time.Time) *rateLimiter {
	return &rateLimiter{
		limit:     rate.Limit(float64(perMinute) / refillWindow.Seconds()),
		burst:     perMinute,
		now:       now,
		buckets:   make(map[string]*bucket),
		lastSweep: now(),
	}
}

// allow takes a token from key's bucket. When the bucket is empty it reports how long until the
// next token instead.
func (l *rateLimiter) allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) >= refillWindow {
		for k, b := range l.buckets {
			if now.Sub(b.seen) >= refillWindow {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	res := b.lim.ReserveN(now, 1)
	if wait := res.DelayFrom(now); wait > 0 {
		res.CancelAt(now)
		return false, wait
	}
	return true, 0
}

// limitIP rejects requests from a client IP over its limit. Health checks are exempt so probes
// never fail on it.
func limitIP(l *rateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health", "/healthz", "/ready":
		default:
			if ok, wait := l.allow(clientIP(r)); !ok {
				tooManyRequests(w, wait)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the last X-Forwarded-For entry, which Cloud Run's proxy appends and the client
// cannot forge (earlier entries are client-supplied), or the peer address without the header.
func clientIP(r *http.Request) string {
	if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
		last := values[len(values)-1]
		if i := strings.LastIndexByte(last, ','); i >= 0 {
			last = last[i+1:]
		}
		if ip := strings.TrimSpace(last); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func tooManyRequests(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests; retry after the Retry-After delay")
}
