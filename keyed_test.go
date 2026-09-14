package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestKeyedLimiterCreatesBucketOnFirstUse(t *testing.T) {
	now := time.Now()
	l := NewKeyedLimiter(1, 1)

	if !l.Allow("tenant-a", now) {
		t.Fatal("first request for a new key should be allowed")
	}
	if l.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", l.Len())
	}
}

func TestKeyedLimiterTracksKeysIndependently(t *testing.T) {
	now := time.Now()
	l := NewKeyedLimiter(1, 1)

	if !l.Allow("tenant-a", now) {
		t.Fatal("first request for tenant-a should be allowed")
	}
	if l.Allow("tenant-a", now) {
		t.Fatal("second immediate request for tenant-a should be denied")
	}
	if !l.Allow("tenant-b", now) {
		t.Fatal("first request for tenant-b should be allowed, independent of tenant-a")
	}
}

func TestKeyedLimiterAllowNConsumesVariableCost(t *testing.T) {
	now := time.Now()
	l := NewKeyedLimiter(10, 1)

	if !l.AllowN("tenant-a", now, 4) {
		t.Fatal("request for 4 tokens should be allowed")
	}
	if !l.AllowN("tenant-a", now, 6) {
		t.Fatal("request for remaining 6 tokens should be allowed")
	}
	if l.AllowN("tenant-a", now, 1) {
		t.Fatal("request after bucket is drained should be denied")
	}
}

func TestKeyedLimiterRetryAfterUnknownKey(t *testing.T) {
	now := time.Now()
	l := NewKeyedLimiter(5, 1)

	if d := l.RetryAfter("tenant-a", now, 1); d != 0 {
		t.Fatalf("RetryAfter for a fresh key = %v, want 0", d)
	}
}

func TestKeyedLimiterRetryAfterWhenDrained(t *testing.T) {
	now := time.Now()
	l := NewKeyedLimiter(5, 1) // 1 token/sec

	l.AllowN("tenant-a", now, 5)

	d := l.RetryAfter("tenant-a", now, 1)
	want := time.Second
	if d != want {
		t.Fatalf("RetryAfter = %v, want %v", d, want)
	}
}

func TestKeyedLimiterRemove(t *testing.T) {
	now := time.Now()
	l := NewKeyedLimiter(1, 1)

	l.Allow("tenant-a", now)
	if l.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", l.Len())
	}

	l.Remove("tenant-a")
	if l.Len() != 0 {
		t.Fatalf("Len() after Remove = %d, want 0", l.Len())
	}

	// Removing resets the bucket, so the next request sees a fresh, full one.
	if !l.Allow("tenant-a", now) {
		t.Fatal("request after Remove should be allowed like a new key")
	}
}

func TestKeyedLimiterConcurrentAccess(t *testing.T) {
	now := time.Now()
	l := NewKeyedLimiter(1000, 1)

	var wg sync.WaitGroup
	keys := []string{"a", "b", "c"}
	for _, key := range keys {
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(key string) {
				defer wg.Done()
				l.Allow(key, now)
			}(key)
		}
	}
	wg.Wait()

	if l.Len() != len(keys) {
		t.Fatalf("Len() = %d, want %d", l.Len(), len(keys))
	}
}

func TestKeyedLimiterSnapshotRestore(t *testing.T) {
	now := time.Now()
	l := NewKeyedLimiter(10, 1)

	l.AllowN("tenant-a", now, 6)
	l.AllowN("tenant-b", now, 3)

	snap := l.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d, want 2", len(snap))
	}

	restored := NewKeyedLimiter(10, 1)
	restored.Restore(snap)

	if restored.Len() != 2 {
		t.Fatalf("Len() after Restore = %d, want 2", restored.Len())
	}
	// tenant-a has 4 tokens left (10 - 6); a 5-token request should be denied.
	if restored.AllowN("tenant-a", now, 5) {
		t.Fatal("tenant-a should not have 5 tokens after restoring a drained snapshot")
	}
	if !restored.AllowN("tenant-a", now, 4) {
		t.Fatal("tenant-a should have exactly 4 tokens left after restoring")
	}
}

func TestKeyedLimiterSnapshotIsIndependentCopy(t *testing.T) {
	now := time.Now()
	l := NewKeyedLimiter(10, 1)
	l.Allow("tenant-a", now)

	snap := l.Snapshot()
	l.AllowN("tenant-a", now, 100) // drive tenant-a's live bucket further down

	if snap["tenant-a"] == l.Snapshot()["tenant-a"] {
		t.Fatal("mutating the limiter after Snapshot should not affect the earlier snapshot")
	}
}

func TestKeyedLimiterRestoreDropsUnlistedKeys(t *testing.T) {
	now := time.Now()
	l := NewKeyedLimiter(10, 1)
	l.Allow("tenant-a", now)
	l.Allow("tenant-b", now)

	l.Restore(map[string]TokenBucket{
		"tenant-a": NewTokenBucket(10, 1, now),
	})

	if l.Len() != 1 {
		t.Fatalf("Len() after Restore = %d, want 1", l.Len())
	}
}
