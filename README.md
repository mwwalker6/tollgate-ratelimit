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

## Status

Early skeleton. The token bucket is complete and tested. Not yet covered:
per-key limiters (e.g. one bucket per API client), a sliding-window
alternative for callers who don't want burst tolerance, and serialization
helpers for persisting bucket state between process restarts.

## License

MIT, see [LICENSE](LICENSE).
