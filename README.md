# tollgate-ratelimit

A token-bucket rate limiter for Go, built around one idea: every function
that matters is pure. No goroutine ticks the bucket in the background, no
`time.Now()` is called for you, and no lock lives inside the library. You
pass in the current state and the current time, you get back the next
state and a decision. That makes the whole thing testable with plain table
tests and fake timestamps - no `time.Sleep`, no flaky CI runs.

## The problem

Rate limiting libraries often hide a mutex and a background goroutine
inside a `Limiter` struct, which is convenient until you need to test the
edge cases: what happens right at the capacity boundary, what happens if
two refills land in the same millisecond, what happens if the system clock
jumps backward. Those cases are hard to hit reliably against a limiter that
owns its own clock.

Here the state is just a struct, and advancing it is just a function call.
Tests can construct any point in time they want and assert on the exact
result.

## Usage

```go
package main

import (
	"fmt"
	"time"

	"github.com/mwwalker6/tollgate-ratelimit"
)

func main() {
	// 5 requests/sec sustained, bursts up to 20.
	bucket := ratelimit.NewTokenBucket(20, 5, time.Now())

	for i := 0; i < 25; i++ {
		var ok bool
		bucket, ok = ratelimit.Allow(bucket, time.Now())
		if !ok {
			wait := ratelimit.RetryAfter(bucket, time.Now(), 1)
			fmt.Printf("request %d denied, retry in %v\n", i, wait)
			continue
		}
		fmt.Printf("request %d allowed\n", i)
	}
}
```

Because the library never mutates shared state on its own, using it from
multiple goroutines just means guarding the bucket the same way you'd guard
any other shared value:

```go
type limiter struct {
	mu     sync.Mutex
	bucket ratelimit.TokenBucket
}

func (l *limiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	var ok bool
	l.bucket, ok = ratelimit.Allow(l.bucket, time.Now())
	return ok
}
```

## API

- `NewTokenBucket(capacity, refillRate float64, now time.Time) TokenBucket`
  builds a bucket that starts full.
- `Allow(b TokenBucket, now time.Time) (TokenBucket, bool)` refills and
  attempts to take one token.
- `AllowN(b TokenBucket, now time.Time, n float64) (TokenBucket, bool)` is
  `Allow` for a request with a variable cost.
- `Refill(b TokenBucket, now time.Time) TokenBucket` advances the bucket's
  clock without consuming anything, useful for inspecting current capacity.
- `RetryAfter(b TokenBucket, now time.Time, n float64) time.Duration`
  reports how long until `n` tokens would be available.

All four take the bucket by value and return a new one; none of them
mutate anything you pass in.

## Multi-tenant use

For the common case of one limit per API client, tenant, or source IP,
`KeyedLimiter` manages a bucket per key so you don't have to build your own
map and mutex around single-bucket `Allow`:

```go
limiter := ratelimit.NewKeyedLimiter(20, 5) // per key: burst 20, 5/sec

func handle(clientID string) {
	if !limiter.Allow(clientID, time.Now()) {
		// reject the request
	}
}
```

Buckets are created lazily on first use and never expire on their own; call
`limiter.Remove(key)` when a tenant goes away (its key is revoked, its
session ends) to keep the map from growing without bound.

## Sliding window

`TokenBucket` lets a caller spend a full burst of `Capacity` requests in a
single instant, as long as it's been idle long enough to refill. If that
burst tolerance isn't what you want, `SlidingWindow` counts requests
against a rolling window instead, blending the previous window's count
into the current one so the limit is enforced smoothly rather than resetting
in steps:

```go
w := ratelimit.NewSlidingWindow(100, time.Minute, time.Now()) // 100/min

func handle() {
	var ok bool
	w, ok = w.Allow(time.Now())
	if !ok {
		// reject the request
	}
}
```

It's the counter variant of the algorithm, not the log variant: it tracks
two running totals (previous window, current window) rather than a
timestamp per request, so its size is fixed no matter how much traffic
passes through it. The cost is that the smoothing is an approximation - it
assumes requests were spread evenly through the previous window - rather
than an exact count of the trailing interval.

## Persisting state across restarts

`TokenBucket` and `SlidingWindow` are plain structs with exported, json-tagged
fields, so `encoding/json` round-trips them without any glue code:

```go
data, err := json.Marshal(bucket)
// ... write data somewhere durable ...

var restored ratelimit.TokenBucket
err = json.Unmarshal(data, &restored)
// restored is ready to pass to Allow or Refill as-is
```

For `KeyedLimiter`, `Snapshot` returns a copy of every tracked bucket keyed
by tenant, and `Restore` loads a decoded snapshot back in:

```go
data, _ := json.Marshal(limiter.Snapshot())
// ... write data somewhere durable ...

var buckets map[string]ratelimit.TokenBucket
json.Unmarshal(data, &buckets)
limiter.Restore(buckets)
```

## Status

The token bucket, the sliding window, the keyed multi-tenant limiter, and
JSON persistence for all three are complete and tested. `go test -bench .`
covers `AllowN` both bare and behind `KeyedLimiter`'s mutex, alone and under
concurrent load, so a change to the locking strategy has something to be
measured against.

## License

MIT, see [LICENSE](LICENSE).
