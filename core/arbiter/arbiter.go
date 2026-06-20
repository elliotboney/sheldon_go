// Package arbiter enforces the ≤1-worker-turn-in-flight invariant (AD-8, NFR4).
//
// It is the minimal M0 gate: a single in-flight token. A turn submitted while
// another is in flight is rejected with ErrTurnInFlight — it never runs
// concurrently. Coalescing into a per-class catch-up slot with priority
// (reply > dream > proactive-ping) belongs to the Epic 2–3 scheduler, once those
// turn-classes exist; the reject path is the seam that grows into it.
package arbiter

import (
	"context"
	"errors"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/worker"
)

// ErrTurnInFlight is returned by Submit when a worker turn is already in flight.
var ErrTurnInFlight = errors.New("arbiter: a worker turn is already in flight")

// Arbiter admits at most one worker turn at a time through its constructor-
// injected Worker.
type Arbiter struct {
	w     worker.Worker
	token chan struct{} // capacity 1: the single in-flight slot
}

// New returns an Arbiter governing turns through w.
func New(w worker.Worker) *Arbiter {
	return &Arbiter{w: w, token: make(chan struct{}, 1)}
}

// Submit runs turn through the worker iff no turn is in flight; otherwise it
// returns ErrTurnInFlight without invoking the worker. ctx is threaded to the
// worker so cancellation propagates to the in-flight turn.
func (a *Arbiter) Submit(ctx context.Context, turn contracts.Job) (contracts.Result, error) {
	select {
	case a.token <- struct{}{}:
		defer func() { <-a.token }()
		return a.w.AssembleAndPropose(ctx, turn)
	default:
		return contracts.Result{}, ErrTurnInFlight
	}
}
