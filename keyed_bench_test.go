package ratelimit

import (
	"strconv"
	"testing"
	"time"
)

// BenchmarkKeyedLimiterAllowN measures a single goroutine hitting one key,
// so there's no contention on the limiter's mutex to speak of.
func BenchmarkKeyedLimiterAllowN(b *testing.B) {
	l := NewKeyedLimiter(1e12, 1e6)
	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.AllowN("tenant", now, 1)
	}
}

// BenchmarkKeyedLimiterAllowNParallelSameKey measures many goroutines
// hitting the same key, so every call serializes on the limiter's single
// mutex - the worst case for contention, since AllowN never releases the
// lock early regardless of which key it's given.
func BenchmarkKeyedLimiterAllowNParallelSameKey(b *testing.B) {
	l := NewKeyedLimiter(1e12, 1e6)
	now := time.Now()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.AllowN("tenant", now, 1)
		}
	})
}

// BenchmarkKeyedLimiterAllowNParallelManyKeys measures the same number of
// concurrent goroutines spread across a large key space instead of one key.
// The mutex is still global - AllowN has no per-key locking - so this
// isolates how much of the contention cost comes from goroutine count alone
// versus from every call fighting over the same map entry.
func BenchmarkKeyedLimiterAllowNParallelManyKeys(b *testing.B) {
	l := NewKeyedLimiter(1e12, 1e6)
	now := time.Now()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := "tenant-" + strconv.Itoa(i%1024)
			l.AllowN(key, now, 1)
			i++
		}
	})
}
