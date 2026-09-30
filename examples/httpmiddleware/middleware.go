// Package httpmiddleware shows one way to put a ratelimit.KeyedLimiter in
// front of an http.Handler. It is an example, not part of the library's API:
// copy it and change the key function and the response to suit your service.
package httpmiddleware

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	ratelimit "github.com/mwwalker6/tollgate-ratelimit"
)

// KeyFunc picks the rate limit key for a request.
type KeyFunc func(r *http.Request) string

// RemoteIP keys requests by the host part of r.RemoteAddr. It deliberately
// ignores X-Forwarded-For: that header is client-controlled unless a trusted
// proxy overwrites it, so honoring it by default would let anyone pick their
// own bucket. If you run behind a proxy, write a KeyFunc that knows which
// hop to trust.
func RemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// New returns middleware that spends one token from the requester's bucket
// per request and answers 429 when the bucket is empty. now supplies the
// time; pass time.Now in production and a fake clock in tests.
func New(l *ratelimit.KeyedLimiter, key KeyFunc, now func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := key(r)
			t := now()

			if !l.Allow(k, t) {
				w.Header().Set("Retry-After", retryAfterSeconds(l.RetryAfter(k, t, 1)))
				http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// retryAfterSeconds formats d for the Retry-After header, which only carries
// whole seconds. Rounding up (and never reporting 0) keeps a client that
// obeys the header from coming back a moment too early and being rejected
// again.
func retryAfterSeconds(d time.Duration) string {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}
