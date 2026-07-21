package sources

import (
	"context"
	"os"
	"time"
)

// requeryLoop serializes calls to a re-query function: at most one call is
// ever in flight, and any Trigger() calls that arrive while a call is
// already running are coalesced into exactly one follow-up call once the
// in-flight one returns. This is design.md's "thundering herd" prevention —
// a burst of fs events, a poll tick landing mid-query, and a retry timer
// firing at the same moment never fan out into concurrent bd/openspec
// shellouts; they collapse into at most one extra run.
type requeryLoop struct {
	trigger chan struct{}
}

func newRequeryLoop() *requeryLoop {
	return &requeryLoop{trigger: make(chan struct{}, 1)}
}

// Trigger requests a re-query. Non-blocking: if one is already queued
// (whether or not a query is currently running), this call is a no-op —
// the queued request already covers it.
func (r *requeryLoop) Trigger() {
	select {
	case r.trigger <- struct{}{}:
	default:
	}
}

// Run executes fn once per coalesced Trigger, serialized, until ctx is
// done. Run is the loop's only goroutine — callers start it with
// `go loop.Run(ctx, fn)` from a context-carrying caller, satisfying task
// 2.4's "no goroutine without a cancellable context" audit.
func (r *requeryLoop) Run(ctx context.Context, fn func(context.Context)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.trigger:
			fn(ctx)
		}
	}
}

// debounce reads raw signals from in and, after d has elapsed with no new
// signal (trailing-edge), calls fire exactly once. It runs until ctx is
// done.
//
// Each restart allocates a fresh timer rather than resetting a shared one.
// time.Timer.Reset has a well-known race when the timer may have already
// fired and its channel not yet drained; abandoning the old timer and
// swapping in a new one sidesteps that race entirely (the old timer's send,
// if any, lands on a channel nothing selects on again and is simply
// garbage-collected) — the extra allocation is irrelevant at fs-event
// rates.
func debounce(ctx context.Context, in <-chan struct{}, d time.Duration, fire func()) {
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-in:
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(d)
			timerC = timer.C
		case <-timerC:
			timerC = nil
			fire()
		}
	}
}

// dirExists reports whether path exists and is a directory. Any stat error
// (including "does not exist") is treated as false rather than propagated
// — consistent with the tolerant/degrade-not-fail convention used
// throughout wavetui's sources.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// backoffDelay returns the retry delay for the failCount'th consecutive
// failure (failCount starts at 1), doubling from 1s and capped at cap. Used
// by BeadsSource per task 2.1's "retry backoff" requirement.
func backoffDelay(failCount int, cap time.Duration) time.Duration {
	if failCount <= 0 {
		return 0
	}
	base := time.Second
	// Guard against overflow for a pathologically long failure streak —
	// once the shift would exceed cap there's no point computing further.
	if failCount > 32 {
		return cap
	}
	d := base << uint(failCount-1)
	if d <= 0 || d > cap {
		return cap
	}
	return d
}
