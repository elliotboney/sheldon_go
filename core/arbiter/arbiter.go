// Package arbiter enforces the ≤1-worker-turn-in-flight invariant (AD-8, NFR4)
// and bounds every turn with a timeout so a brain that cannot complete never
// freezes the pet (AD-8 "a failed call never freezes the pet").
//
// It is the minimal M0 gate: a single in-flight token. A turn submitted while
// another is in flight is rejected with ErrTurnInFlight — it never runs
// concurrently. A turn that does not finish before the timeout is abandoned: the
// worker's context is cancelled, the slot is freed, and Submit returns
// ErrTurnTimeout. The abandoned worker's late Result lands in an unread channel
// and is discarded — at M0 that dropped Result + the context cancellation IS the
// turn_id fence (AD-11); full envelope-id fencing arrives with Epic 3's turn
// lifecycle. Coalescing into a per-class catch-up slot with priority
// (reply > dream > proactive-ping) belongs to the Epic 2–3 scheduler, once those
// turn-classes exist; the reject path is the seam that grows into it.
package arbiter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/worker"
)

// ErrTurnInFlight is returned by Submit when a worker turn is already in flight.
var ErrTurnInFlight = errors.New("arbiter: a worker turn is already in flight")

// ErrTurnTimeout is returned by Submit when the worker turn did not complete
// before the arbiter timeout; the turn is closed and the slot freed (AD-8/AD-11).
var ErrTurnTimeout = errors.New("arbiter: worker turn timed out")

// Arbiter admits at most one worker turn at a time through its constructor-
// injected Worker, bounding each turn with timeout.
type Arbiter struct {
	w       worker.Worker
	token   chan struct{} // capacity 1: the single in-flight slot
	timeout time.Duration
}

// New returns an Arbiter governing turns through w, bounding each turn with
// timeout. The timeout is injected (AD-10): tests pass a short one under
// synctest, main a real one.
func New(w worker.Worker, timeout time.Duration) *Arbiter {
	return &Arbiter{w: w, token: make(chan struct{}, 1), timeout: timeout}
}

// turnResult carries a worker outcome back from the per-turn goroutine.
type turnResult struct {
	res contracts.Result
	err error
}

// Submit runs turn through the worker iff no turn is in flight; otherwise it
// returns ErrTurnInFlight without invoking the worker. The worker runs in its own
// goroutine under a timeout-bounded context, so a hung turn is abandoned instead
// of freezing the caller. On timeout the slot is freed and ErrTurnTimeout
// returned; if the parent ctx was cancelled (shutdown) its error is returned
// instead. The token is released in exactly one branch, so close is idempotent.
func (a *Arbiter) Submit(ctx context.Context, turn contracts.Job) (contracts.Result, error) {
	select {
	case a.token <- struct{}{}:
	default:
		return contracts.Result{}, ErrTurnInFlight
	}

	tctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	done := make(chan turnResult, 1) // buffered: an abandoned worker's late send is dropped, not leaked
	go func() {
		// recover() does not cross goroutines (AD-5): this per-turn goroutine must
		// recover its own panic, or a panicking worker would die without sending to
		// done — the token would never be released and the pet would freeze. On
		// panic the turn degrades to a synthetic error (→ reflex ack downstream).
		defer func() {
			if r := recover(); r != nil {
				slog.Error("worker turn recovered from panic", "panic", fmt.Sprint(r))
				done <- turnResult{err: fmt.Errorf("worker turn panicked: %v", r)}
			}
		}()
		res, err := a.w.AssembleAndPropose(tctx, turn)
		done <- turnResult{res: res, err: err}
	}()

	select {
	case out := <-done:
		<-a.token
		return out.res, out.err
	case <-tctx.Done():
		<-a.token // turn closed at the deadline: no turn remains in flight past the timeout
		if ctx.Err() != nil {
			return contracts.Result{}, ctx.Err() // parent cancelled (shutdown), not a turn timeout
		}
		return contracts.Result{}, ErrTurnTimeout
	}
}
