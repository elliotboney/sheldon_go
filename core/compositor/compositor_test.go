package compositor

import (
	"testing"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/bus"
)

// newWired returns a Compositor and the channel registered to receive its
// face-snapshot envelopes.
func newWired(t *testing.T) (*Compositor, <-chan contracts.Envelope) {
	t.Helper()
	hub := bus.New()
	ch := make(chan contracts.Envelope, 8)
	if err := hub.Register(contracts.KindFaceSnapshot, ch); err != nil {
		t.Fatalf("register face-snapshot route: %v", err)
	}
	return New(hub), ch
}

// TestPushFace_PublishesSnapshot asserts PushFace emits a face-snapshot envelope
// for the face region carrying the given Face (AC1/AC3).
func TestPushFace_PublishesSnapshot(t *testing.T) {
	c, ch := newWired(t)
	face := contracts.Face{Expression: contracts.ExpressionHappy, EyesOpen: true}

	if err := c.PushFace(face); err != nil {
		t.Fatalf("push: %v", err)
	}

	env := <-ch
	if env.Kind != contracts.KindFaceSnapshot {
		t.Fatalf("kind = %q, want %q", env.Kind, contracts.KindFaceSnapshot)
	}
	snap, ok := env.Payload.(contracts.RegionSnapshot)
	if !ok {
		t.Fatalf("payload type = %T, want RegionSnapshot", env.Payload)
	}
	if snap.Region != contracts.RegionFace {
		t.Fatalf("region = %q, want %q", snap.Region, contracts.RegionFace)
	}
	if snap.Face != face {
		t.Fatalf("face = %+v, want %+v", snap.Face, face)
	}
}

// TestPushFace_SeqIsMonotonic asserts the seq strictly increases across pushes,
// so the renderer can drop stale frames (AC1/NFR12).
func TestPushFace_SeqIsMonotonic(t *testing.T) {
	c, ch := newWired(t)

	for i := 0; i < 3; i++ {
		if err := c.PushFace(contracts.Face{Expression: contracts.ExpressionNeutral, EyesOpen: true}); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}

	var last uint64
	for i := 0; i < 3; i++ {
		snap := (<-ch).Payload.(contracts.RegionSnapshot)
		if snap.Seq <= last {
			t.Fatalf("seq %d = %d, want > %d (monotonic)", i, snap.Seq, last)
		}
		last = snap.Seq
	}
}
