package ratelimit

import (
	"sync"
	"time"
)

// KeyedLimiter manages one TokenBucket per key, for the common multi-tenant
// case where the set of keys (API clients, tenant IDs, source IPs) isn't
// known ahead of time and buckets need to be created on demand.
//
// Unlike the free functions above, KeyedLimiter owns a mutex: the whole
// point of a keyed limiter is serving many keys concurrently, and asking
// every caller to reimplement a map plus a lock would defeat that. Time is
// still passed in explicitly rather than read from the system clock, so
// callers can keep testing with fake timestamps the same way they would
// with a single TokenBucket.
type KeyedLimiter struct {
	capacity   float64
	refillRate float64

	mu      sync.Mutex
	buckets map[string]TokenBucket
}

// NewKeyedLimiter returns a KeyedLimiter where every key shares the same
// capacity and refill rate. A bucket is created the first time its key is
// seen, and starts full.
func NewKeyedLimiter(capacity, refillRate float64) *KeyedLimiter {
	return &KeyedLimiter{
		capacity:   capacity,
		refillRate: refillRate,
		buckets:    make(map[string]TokenBucket),
	}
}

// Allow attempts to consume one token from key's bucket, creating the
// bucket first if key hasn't been seen before.
func (l *KeyedLimiter) Allow(key string, now time.Time) bool {
	return l.AllowN(key, now, 1)
}

// AllowN is like Allow but consumes n tokens instead of one.
func (l *KeyedLimiter) AllowN(key string, now time.Time, n float64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		b = NewTokenBucket(l.capacity, l.refillRate, now)
	}

	b, allowed := AllowN(b, now, n)
	l.buckets[key] = b
	return allowed
}

// RetryAfter reports how long the caller should wait before n tokens will
// be available for key, without consuming any. A key that hasn't been seen
// yet behaves like a fresh, full bucket, so this returns 0 unless n
// exceeds capacity.
func (l *KeyedLimiter) RetryAfter(key string, now time.Time, n float64) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		b = NewTokenBucket(l.capacity, l.refillRate, now)
	}

	return RetryAfter(b, now, n)
}

// Remove deletes key's bucket. Callers that know when a tenant goes away
// (an API key revoked, a connection closed) should call this so the
// underlying map doesn't grow for the life of a long-running process; the
// limiter itself never expires or evicts entries on its own.
func (l *KeyedLimiter) Remove(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

// Len returns the number of keys currently tracked.
func (l *KeyedLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Snapshot returns a copy of the buckets currently tracked, keyed the same
// way Allow and AllowN are. Since TokenBucket marshals with encoding/json,
// the result can be persisted directly and handed to Restore after a
// process restart to pick up where the limiter left off.
func (l *KeyedLimiter) Snapshot() map[string]TokenBucket {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := make(map[string]TokenBucket, len(l.buckets))
	for k, v := range l.buckets {
		out[k] = v
	}
	return out
}

// Restore replaces the limiter's buckets with buckets, typically a snapshot
// produced by an earlier call to Snapshot and decoded after a restart.
// Keys not present in buckets are dropped, matching a limiter that was
// rebuilt from scratch and then loaded.
func (l *KeyedLimiter) Restore(buckets map[string]TokenBucket) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.buckets = make(map[string]TokenBucket, len(buckets))
	for k, v := range buckets {
		l.buckets[k] = v
	}
}
