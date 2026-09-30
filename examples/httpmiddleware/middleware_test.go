package httpmiddleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ratelimit "github.com/mwwalker6/tollgate-ratelimit"
)

func serve(h http.Handler, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMiddleware(t *testing.T) {
	clock := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }

	// Burst of 2, one token back every 2 seconds.
	l := ratelimit.NewKeyedLimiter(2, 0.5)
	h := New(l, RemoteIP, now)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		if rec := serve(h, "192.0.2.1:1000"); rec.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i, rec.Code)
		}
	}

	rec := serve(h, "192.0.2.1:1001")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third request: got %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After = %q, want %q", got, "2")
	}

	// A different host has its own bucket, even with the same port reused.
	if rec := serve(h, "192.0.2.2:1000"); rec.Code != http.StatusOK {
		t.Errorf("other client: got %d, want 200", rec.Code)
	}

	clock = clock.Add(2 * time.Second)
	if rec := serve(h, "192.0.2.1:1002"); rec.Code != http.StatusOK {
		t.Errorf("after refill: got %d, want 200", rec.Code)
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "1"},
		{time.Millisecond, "1"},
		{time.Second, "1"},
		{1500 * time.Millisecond, "2"},
		{30 * time.Second, "30"},
	}
	for _, tt := range tests {
		if got := retryAfterSeconds(tt.d); got != tt.want {
			t.Errorf("retryAfterSeconds(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestRemoteIPWithoutPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.9"
	if got := RemoteIP(req); got != "192.0.2.9" {
		t.Errorf("RemoteIP = %q, want %q", got, "192.0.2.9")
	}
}
