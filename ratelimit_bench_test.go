package ratelimit

import (
	"testing"
	"time"
)

// BenchmarkAllowN establishes a baseline cost for the pure function on its
// own: no lock, no goroutines, nothing to contend over. Compare this against
// BenchmarkKeyedLimiterAllowNParallelSameKey to see what the mutex in
// KeyedLimiter costs under contention.
func BenchmarkAllowN(b *testing.B) {
	now := time.Now()
	bucket := NewTokenBucket(1e12, 1e6, now)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bucket, _ = AllowN(bucket, now, 1)
	}
}
