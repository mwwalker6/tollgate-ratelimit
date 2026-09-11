package ratelimit

import (
	"testing"
	"time"
)

func TestNewSlidingWindowStartsEmpty(t *testing.T) {
	now := time.Now()
	w := NewSlidingWindow(3, time.Second, now)

	for i := 0; i < 3; i++ {
		var ok bool
		w, ok = w.Allow(now)
		if !ok {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	if _, ok := w.Allow(now); ok {
		t.Fatal("4th request within the limit should be denied")
	}
}

func TestSlidingWindowAllowNConsumesVariableCost(t *testing.T) {
	now := time.Now()
	w := NewSlidingWindow(10, time.Second, now)

	w, ok := w.AllowN(now, 4)
	if !ok {
		t.Fatal("request for 4 should be allowed")
	}

	_, ok = w.AllowN(now, 7)
	if ok {
		t.Fatal("request for 7 should be denied when only 6 remain")
	}
}

func TestSlidingWindowAllowNZeroCostAlwaysAllowed(t *testing.T) {
	now := time.Now()
	w := NewSlidingWindow(1, time.Second, now)
	w, _ = w.Allow(now)

	if _, ok := w.AllowN(now, 0); !ok {
		t.Fatal("zero-cost request should always be allowed")
	}
}

func TestSlidingWindowSmoothsAcrossRollover(t *testing.T) {
	now := time.Now()
	w := NewSlidingWindow(10, time.Second, now)

	w, ok := w.AllowN(now, 10)
	if !ok {
		t.Fatal("first 10 requests should fill the window exactly")
	}

	// Halfway into the next window, half of the previous window's count
	// should still be weighing on the estimate: 10*0.5 = 5, leaving 5 of
	// room.
	later := now.Add(1500 * time.Millisecond)
	if got := w.Count(later); got != 5 {
		t.Fatalf("Count = %v, want 5", got)
	}

	if _, ok := w.AllowN(later, 6); ok {
		t.Fatal("6 requests should be denied when only 5 are estimated free")
	}
	if _, ok := w.AllowN(later, 5); !ok {
		t.Fatal("5 requests should be allowed when 5 are estimated free")
	}
}

func TestSlidingWindowIgnoresBackwardClock(t *testing.T) {
	now := time.Now()
	w := NewSlidingWindow(10, time.Second, now)
	w.CurrCount = 4

	earlier := now.Add(-time.Second)
	got := w.advance(earlier)

	if got != w {
		t.Fatalf("advance with earlier time should be a no-op, got %+v", got)
	}
}

func TestSlidingWindowRetryAfterWhenRoomAvailable(t *testing.T) {
	now := time.Now()
	w := NewSlidingWindow(10, time.Second, now)

	if d := w.RetryAfter(now, 1); d != 0 {
		t.Fatalf("RetryAfter = %v, want 0", d)
	}
}

// durationClose reports whether d is within tolerance of want. RetryAfter's
// two-phase math goes through a few float64 divisions, so exact equality
// against a hand-derived expectation risks failing on a rounding ULP that
// has nothing to do with correctness.
func durationClose(d, want, tolerance time.Duration) bool {
	diff := d - want
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

func TestSlidingWindowRetryAfterPhaseOne(t *testing.T) {
	now := time.Now()
	w := NewSlidingWindow(10, time.Second, now)

	w, _ = w.AllowN(now, 8)

	// 1.2s later: one rollover has happened (PrevCount=8, CurrCount=0),
	// 0.2s into the new window, then 3 more requests are counted.
	mid := now.Add(1200 * time.Millisecond)
	w, ok := w.AllowN(mid, 3)
	if !ok {
		t.Fatal("request for 3 should be allowed (estimate is 6.4)")
	}

	d := w.RetryAfter(mid, 5)
	want := 550 * time.Millisecond
	if !durationClose(d, want, 10*time.Microsecond) {
		t.Fatalf("RetryAfter = %v, want ~%v", d, want)
	}

	after := mid.Add(d + time.Millisecond)
	if _, ok := w.AllowN(after, 5); !ok {
		t.Fatal("request for 5 should be allowed shortly after RetryAfter has elapsed")
	}
}

func TestSlidingWindowRetryAfterPhaseTwo(t *testing.T) {
	now := time.Now()
	w := NewSlidingWindow(10, time.Second, now)

	w, _ = w.AllowN(now, 10)

	d := w.RetryAfter(now, 1)
	want := 1100 * time.Millisecond
	if !durationClose(d, want, 10*time.Microsecond) {
		t.Fatalf("RetryAfter = %v, want ~%v", d, want)
	}

	after := now.Add(d + time.Millisecond)
	if _, ok := w.AllowN(after, 1); !ok {
		t.Fatal("request for 1 should be allowed shortly after RetryAfter has elapsed")
	}
}
