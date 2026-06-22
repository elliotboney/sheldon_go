package reflexes

import (
	"context"
	"math/rand/v2"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/bus"
	"github.com/elliotboney/shelldon_go/core/compositor"
	"github.com/elliotboney/shelldon_go/core/state"
)

func seededRNG() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

// TestNextDelay_Jittered is AC2: successive blink delays vary and stay within
// [base, base+jitter).
func TestNextDelay_Jittered(t *testing.T) {
	b := NewBlink(nil, nil, seededRNG())

	seen := make(map[time.Duration]int)
	for i := 0; i < 16; i++ {
		d := b.nextDelay()
		if d < blinkBaseInterval || d >= blinkBaseInterval+blinkJitter {
			t.Fatalf("delay %v out of range [%v, %v)", d, blinkBaseInterval, blinkBaseInterval+blinkJitter)
		}
		seen[d]++
	}
	if len(seen) == 1 {
		t.Fatalf("delays not jittered: every value was identical (%v)", seen)
	}
}

// TestIdle_GatedByThreshold proves the idle gate: the reflex is not idle right
// after an interaction and becomes idle once the threshold elapses.
func TestIdle_GatedByThreshold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := state.New(state.Personality{LastInteraction: time.Now()}, filepath.Join(t.TempDir(), "state.json"))
		b := NewBlink(nil, store, seededRNG())

		if b.idle() {
			t.Fatal("must not be idle immediately after interaction")
		}
		time.Sleep(blinkIdleThreshold + time.Second)
		if !b.idle() {
			t.Fatal("must be idle once the idle threshold has elapsed")
		}
	})
}

// TestBlinkOnce_ReopensEyesEvenWhenCancelled guards the review fix: a blink
// interrupted by ctx cancellation must still push an eyes-open frame, never leave
// the face frozen with eyes closed.
func TestBlinkOnce_ReopensEyesEvenWhenCancelled(t *testing.T) {
	hub := bus.New()
	ch := make(chan contracts.Envelope, 4)
	if err := hub.Register(contracts.KindFaceSnapshot, ch); err != nil {
		t.Fatalf("register: %v", err)
	}
	store := state.New(state.Default(), filepath.Join(t.TempDir(), "state.json"))
	b := NewBlink(compositor.New(hub), store, seededRNG())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the blink completes
	b.blinkOnce(ctx)

	var lastEyesOpen, sawClosed bool
	draining := true
	for draining {
		select {
		case env := <-ch:
			snap := env.Payload.(contracts.RegionSnapshot)
			lastEyesOpen = snap.Face.EyesOpen
			if !snap.Face.EyesOpen {
				sawClosed = true
			}
		default:
			draining = false
		}
	}
	if !sawClosed {
		t.Fatal("blink never pushed an eyes-closed frame")
	}
	if !lastEyesOpen {
		t.Fatal("eyes left closed after a cancelled blink — face frozen mid-blink")
	}
}

// TestServe_BlinksWhenIdle is AC1: with no interaction past the idle threshold,
// the reflex pushes a blink (eyes-closed) frame and reopens the eyes.
func TestServe_BlinksWhenIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := bus.New()
		ch := make(chan contracts.Envelope, 64)
		if err := hub.Register(contracts.KindFaceSnapshot, ch); err != nil {
			t.Fatalf("register: %v", err)
		}
		store := state.New(state.Personality{LastInteraction: time.Now()}, filepath.Join(t.TempDir(), "state.json"))
		b := NewBlink(compositor.New(hub), store, seededRNG())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- b.Serve(ctx) }()

		// Past the idle threshold plus several jittered blink intervals.
		time.Sleep(blinkIdleThreshold + 4*(blinkBaseInterval+blinkJitter))
		synctest.Wait()
		cancel()
		<-done

		var closed, open bool
		draining := true
		for draining {
			select {
			case env := <-ch:
				snap := env.Payload.(contracts.RegionSnapshot)
				if snap.Face.EyesOpen {
					open = true
				} else {
					closed = true
				}
			default:
				draining = false
			}
		}
		if !closed {
			t.Fatal("no blink frame (eyes closed) was rendered while idle")
		}
		if !open {
			t.Fatal("eyes never reopened after a blink")
		}
	})
}
