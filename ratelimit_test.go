package ratelimit

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewTokenBucketStartsFull(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(10, 1, now)

	if b.Tokens != 10 {
		t.Fatalf("Tokens = %v, want 10", b.Tokens)
	}
	if b.UpdatedAt != now {
		t.Fatalf("UpdatedAt = %v, want %v", b.UpdatedAt, now)
	}
}

func TestAllowConsumesOneToken(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(2, 1, now)

	b, ok := Allow(b, now)
	if !ok {
		t.Fatal("first request should be allowed")
	}
	if b.Tokens != 1 {
		t.Fatalf("Tokens = %v, want 1", b.Tokens)
	}
}

func TestAllowDeniesWhenEmpty(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(1, 1, now)

	b, ok := Allow(b, now)
	if !ok {
		t.Fatal("first request should be allowed")
	}

	// No time has passed, so no tokens have refilled.
	_, ok = Allow(b, now)
	if ok {
		t.Fatal("second request should be denied")
	}
}

func TestRefillIsCappedAtCapacity(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(5, 10, now) // 10 tokens/sec

	later := now.Add(time.Hour) // plenty of time to overfill
	b = Refill(b, later)

	if b.Tokens != 5 {
		t.Fatalf("Tokens = %v, want 5 (capped at capacity)", b.Tokens)
	}
}

func TestRefillAddsProportionalTokens(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(10, 2, now) // 2 tokens/sec
	b.Tokens = 0
	b.UpdatedAt = now

	later := now.Add(1500 * time.Millisecond)
	b = Refill(b, later)

	want := 3.0 // 1.5s * 2/s
	if b.Tokens != want {
		t.Fatalf("Tokens = %v, want %v", b.Tokens, want)
	}
}

func TestRefillIgnoresBackwardClock(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(10, 2, now)
	b.Tokens = 4
	b.UpdatedAt = now

	earlier := now.Add(-time.Second)
	got := Refill(b, earlier)

	if got != b {
		t.Fatalf("Refill with earlier time should be a no-op, got %+v", got)
	}
}

func TestAllowNConsumesVariableCost(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(10, 1, now)

	b, ok := AllowN(b, now, 4)
	if !ok {
		t.Fatal("request for 4 tokens should be allowed")
	}
	if b.Tokens != 6 {
		t.Fatalf("Tokens = %v, want 6", b.Tokens)
	}

	_, ok = AllowN(b, now, 7)
	if ok {
		t.Fatal("request for 7 tokens should be denied when only 6 remain")
	}
}

func TestAllowNZeroCostAlwaysAllowed(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(1, 1, now)
	b.Tokens = 0
	b.UpdatedAt = now

	_, ok := AllowN(b, now, 0)
	if !ok {
		t.Fatal("zero-cost request should always be allowed")
	}
}

func TestRetryAfterWhenTokensAvailable(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(10, 1, now)

	if d := RetryAfter(b, now, 1); d != 0 {
		t.Fatalf("RetryAfter = %v, want 0", d)
	}
}

func TestRetryAfterWhenTokensMissing(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(10, 2, now) // 2 tokens/sec
	b.Tokens = 0
	b.UpdatedAt = now

	d := RetryAfter(b, now, 5)
	want := 2500 * time.Millisecond // need 5 tokens at 2/sec
	if d != want {
		t.Fatalf("RetryAfter = %v, want %v", d, want)
	}
}

func TestTokenBucketJSONRoundTrip(t *testing.T) {
	now := time.Now()
	b := NewTokenBucket(20, 5, now)
	b, _ = AllowN(b, now, 7)

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got TokenBucket
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.UpdatedAt.Equal(b.UpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want %v", got.UpdatedAt, b.UpdatedAt)
	}
	got.UpdatedAt = b.UpdatedAt // time.Time round-trips to an equal instant, not an identical value
	if got != b {
		t.Fatalf("round-tripped bucket = %+v, want %+v", got, b)
	}
}

func TestTokenBucketJSONFieldNames(t *testing.T) {
	b := NewTokenBucket(20, 5, time.Unix(0, 0).UTC())

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	for _, field := range []string{"capacity", "refillRate", "tokens", "updatedAt"} {
		if !json.Valid(data) {
			t.Fatalf("Marshal produced invalid JSON: %s", data)
		}
		if !containsKey(data, field) {
			t.Fatalf("Marshal output missing field %q: %s", field, data)
		}
	}
}

func containsKey(data []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
