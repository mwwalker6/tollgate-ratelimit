package ratelimit

import "time"

// SlidingWindow implements the sliding window counter algorithm: an
// alternative to TokenBucket for callers who don't want burst tolerance.
// Where a token bucket lets a caller spend a full Capacity worth of tokens
// in a single instant as long as they've been sitting idle, a sliding
// window smooths that out by blending the previous window's count into the
// current one, weighted by how much of the current window has elapsed.
// The zero value is not valid; construct one with NewSlidingWindow.
//
// This is the counter variant, not the log variant: it tracks two running
// totals instead of a timestamp per request, so its size is fixed
// regardless of how many requests pass through it. The tradeoff is that
// the smoothing is an approximation - it assumes requests are spread evenly
// through the previous window - rather than an exact count of the trailing
// interval.
type SlidingWindow struct {
	Limit  float64       // maximum requests allowed per Window
	Window time.Duration // length of the window

	PrevCount float64   // requests counted in the previous window
	CurrCount float64   // requests counted in the current window so far
	CurrStart time.Time // start of the current window
}

// NewSlidingWindow returns a window with nothing counted yet, so the first
// Limit requests succeed immediately.
func NewSlidingWindow(limit float64, window time.Duration, now time.Time) SlidingWindow {
	return SlidingWindow{
		Limit:     limit,
		Window:    window,
		CurrStart: now,
	}
}

// advance rolls the window forward to now, sliding CurrCount into
// PrevCount if exactly one window has passed, or dropping both counts if
// more than one has (nothing was counted in between, so there's nothing to
// carry). If now is before CurrStart, it's treated the same as no time
// having passed, matching Refill's handling of a backward clock.
func (w SlidingWindow) advance(now time.Time) SlidingWindow {
	elapsed := now.Sub(w.CurrStart)
	if elapsed < w.Window {
		return w
	}

	steps := int64(elapsed / w.Window)
	if steps == 1 {
		w.PrevCount = w.CurrCount
	} else {
		w.PrevCount = 0
	}
	w.CurrCount = 0
	w.CurrStart = w.CurrStart.Add(time.Duration(steps) * w.Window)
	return w
}

// normalize advances w to now and returns the adjusted window along with
// how much of the current window remains and the estimated request count
// over the trailing Window ending at now.
func (w SlidingWindow) normalize(now time.Time) (adj SlidingWindow, remaining time.Duration, estimate float64) {
	adj = w.advance(now)
	remaining = adj.Window - now.Sub(adj.CurrStart)
	weight := remaining.Seconds() / adj.Window.Seconds()
	estimate = adj.PrevCount*weight + adj.CurrCount
	return adj, remaining, estimate
}

// Count returns the estimated number of requests over the trailing Window
// ending at now, without recording a new one. Useful for inspecting
// current usage the way Refill lets a caller inspect a TokenBucket.
func (w SlidingWindow) Count(now time.Time) float64 {
	_, _, estimate := w.normalize(now)
	return estimate
}

// Allow advances the window to now and attempts to count one request
// against it. It returns the resulting window and whether the request is
// allowed; the returned window should replace the caller's stored state
// regardless of the outcome.
func (w SlidingWindow) Allow(now time.Time) (SlidingWindow, bool) {
	return w.AllowN(now, 1)
}

// AllowN is like Allow but counts n requests instead of one. A negative or
// zero n is treated as free and always allowed.
func (w SlidingWindow) AllowN(now time.Time, n float64) (SlidingWindow, bool) {
	w, _, estimate := w.normalize(now)

	if n <= 0 {
		return w, true
	}
	if estimate+n > w.Limit {
		return w, false
	}

	w.CurrCount += n
	return w, true
}

// RetryAfter returns how long the caller should wait before n more
// requests would be allowed, assuming no further requests are counted in
// the meantime. It returns zero if n requests are already allowed now.
//
// The estimate decays in two phases: first PrevCount's contribution fades
// to zero by the end of the current window, then (since we're assuming no
// further requests) CurrCount becomes the next window's PrevCount and
// fades the same way. RetryAfter solves for the first point in whichever
// phase applies.
func (w SlidingWindow) RetryAfter(now time.Time, n float64) time.Duration {
	w, remaining, estimate := w.normalize(now)

	if n <= 0 || w.Limit <= 0 {
		return 0
	}
	if estimate+n <= w.Limit {
		return 0
	}

	// Phase 1: can waiting out the rest of the current window, while
	// PrevCount's weight decays to zero, bring the estimate low enough?
	room := w.Limit - n - w.CurrCount
	if w.PrevCount > 0 && room > 0 {
		targetWeight := room / w.PrevCount
		wait := remaining.Seconds() - targetWeight*w.Window.Seconds()
		return time.Duration(wait * float64(time.Second))
	}

	// Phase 2: no, so wait for the current window to end (CurrCount slides
	// into PrevCount) and let that decay too. If n is so large that even a
	// fully decayed window wouldn't be enough, two windows is the best
	// available estimate without waiting indefinitely.
	room2 := w.Limit - n
	if w.CurrCount <= 0 || room2 <= 0 {
		return remaining + w.Window
	}

	targetWeight2 := room2 / w.CurrCount
	if targetWeight2 > 1 {
		targetWeight2 = 1
	}
	wait := remaining.Seconds() + (1-targetWeight2)*w.Window.Seconds()
	return time.Duration(wait * float64(time.Second))
}
