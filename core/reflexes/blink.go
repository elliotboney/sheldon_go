// Package reflexes holds shelldon's resident reflex-tier behaviors (AD-13): they
// run in-core with no worker and no LLM, read personality-state, and push face
// snapshots through the compositor. They are the pet's offline aliveness — it
// keeps moving with the network down (NFR13).
//
// Each reflex runs its own supervised Serve(ctx) loop for now; the reflex-tier
// scheduler (Story 2.5) will later own these cadences as registered jobs with no
// rewrite of the loop shape.
package reflexes

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/compositor"
	"github.com/elliotboney/shelldon_go/core/state"
)

// Tunable cadences (story-time config, not invariants — architecture Deferred).
const (
	// blinkIdleThreshold is how long without interaction before the pet starts
	// ambient blinking — during an active exchange it stays attentive, not blinking.
	blinkIdleThreshold = 5 * time.Second
	// blinkBaseInterval + up to blinkJitter is the gap between blinks while idle.
	blinkBaseInterval = 4 * time.Second
	blinkJitter       = 3 * time.Second
	// blinkDuration is how long the eyes stay closed for one blink.
	blinkDuration = 200 * time.Millisecond
)

// Blink is the idle-gated blink reflex: while the pet is idle it blinks at
// jittered intervals, pushing eyes-closed then eyes-open face frames.
type Blink struct {
	comp  *compositor.Compositor
	store *state.Store
	rng   *rand.Rand // injected so jitter is deterministic in tests
}

// NewBlink returns a Blink reflex. rng is the jitter source (seeded in main,
// fixed in tests).
func NewBlink(comp *compositor.Compositor, store *state.Store, rng *rand.Rand) *Blink {
	return &Blink{comp: comp, store: store, rng: rng}
}

// nextDelay is the jittered gap until the next blink: [base, base+jitter).
func (b *Blink) nextDelay() time.Duration {
	return blinkBaseInterval + time.Duration(b.rng.Int64N(int64(blinkJitter)))
}

// idle reports whether the pet has had no interaction for the idle threshold.
func (b *Blink) idle() bool {
	return time.Since(b.store.Snapshot().LastInteraction) >= blinkIdleThreshold
}

// Serve runs the blink loop until ctx is cancelled. It waits a jittered delay,
// then blinks if idle. It is wrapped by supervisor.Guard (AD-5) and is shaped so
// the reflex-tier scheduler (Story 2.5) can own its cadence with no rewrite.
func (b *Blink) Serve(ctx context.Context) error {
	timer := time.NewTimer(b.nextDelay())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if b.idle() {
				b.blinkOnce(ctx)
			}
			timer.Reset(b.nextDelay())
		}
	}
}

// blinkOnce pushes an eyes-closed frame, holds it for blinkDuration, then reopens
// the eyes. Expression is neutral at M1; Story 2.4 makes the blink read the
// mood-derived expression (the local face var is the seam). A push error is
// logged, not fatal — the reflex keeps running.
func (b *Blink) blinkOnce(ctx context.Context) {
	closed := contracts.Face{Expression: contracts.ExpressionNeutral, EyesOpen: false}
	if err := b.comp.PushFace(closed); err != nil {
		slog.Error("blink push (eyes closed) failed", "err", err)
		return
	}

	timer := time.NewTimer(blinkDuration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}

	// Always reopen the eyes — even on cancellation — so the face is never left
	// frozen mid-blink (eyes closed) on shutdown or a supervisor restart. The
	// reflex edge drains before the renderer, so this push still reaches it.
	open := contracts.Face{Expression: contracts.ExpressionNeutral, EyesOpen: true}
	if err := b.comp.PushFace(open); err != nil {
		slog.Error("blink push (eyes open) failed", "err", err)
	}
}
