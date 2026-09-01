// Package ratelimit implements token-bucket rate limiting as a set of pure
// functions over an explicit state value. There is no internal clock, no
// goroutine, and no lock: every function takes the current bucket and the
// current time as arguments and returns the next bucket. Callers decide how
// state is stored and how concurrent access is serialized.
package ratelimit

import "time"

// TokenBucket is the state of a single rate limit. The zero value is not a
// valid bucket; construct one with NewTokenBucket.
type TokenBucket struct {
	Capacity   float64   // maximum number of tokens the bucket can hold
	RefillRate float64   // tokens added per second
	Tokens     float64   // tokens currently available
	UpdatedAt  time.Time // last time the bucket was refilled
}

// NewTokenBucket returns a bucket that starts full, so the first burst of
// requests up to capacity succeeds immediately.
func NewTokenBucket(capacity, refillRate float64, now time.Time) TokenBucket {
	return TokenBucket{
		Capacity:   capacity,
		RefillRate: refillRate,
		Tokens:     capacity,
		UpdatedAt:  now,
	}
}

// Refill returns the bucket with tokens added for the time elapsed since
// UpdatedAt, capped at Capacity. It does not consume any tokens. If now is
// before UpdatedAt (a clock that moved backward), no tokens are added and
// UpdatedAt is left unchanged, so a later call can still account for the
// skipped interval correctly.
func Refill(b TokenBucket, now time.Time) TokenBucket {
	elapsed := now.Sub(b.UpdatedAt).Seconds()
	if elapsed <= 0 {
		return b
	}

	b.Tokens += elapsed * b.RefillRate
	if b.Tokens > b.Capacity {
		b.Tokens = b.Capacity
	}
	b.UpdatedAt = now
	return b
}

// Allow refills the bucket to now and then attempts to consume one token.
// It returns the resulting bucket and whether the request is allowed. The
// returned bucket should replace the caller's stored state regardless of
// the outcome, since a refill may have happened even on denial.
func Allow(b TokenBucket, now time.Time) (TokenBucket, bool) {
	return AllowN(b, now, 1)
}

// AllowN is like Allow but consumes n tokens instead of one, for requests
// that carry a variable cost (batch size, payload weight, and so on). A
// negative or zero n is treated as free and always allowed after refill.
func AllowN(b TokenBucket, now time.Time, n float64) (TokenBucket, bool) {
	b = Refill(b, now)

	if n <= 0 {
		return b, true
	}
	if b.Tokens < n {
		return b, false
	}

	b.Tokens -= n
	return b, true
}

// RetryAfter returns how long the caller should wait before n tokens will
// be available, assuming no further consumption in the meantime. It returns
// zero if n tokens are already available.
func RetryAfter(b TokenBucket, now time.Time, n float64) time.Duration {
	b = Refill(b, now)

	deficit := n - b.Tokens
	if deficit <= 0 || b.RefillRate <= 0 {
		return 0
	}

	seconds := deficit / b.RefillRate
	return time.Duration(seconds * float64(time.Second))
}
