package terminal

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/elliotboney/shelldon_go/contracts"
)

func snapshot(seq uint64, face contracts.Face) contracts.Envelope {
	return contracts.Envelope{
		Header:  contracts.Header{Kind: contracts.KindFaceSnapshot, Src: "core", Dst: "display"},
		Payload: contracts.RegionSnapshot{Region: contracts.RegionFace, Seq: seq, Face: face},
	}
}

// TestHandle_RendersLatestDropsStale is the AC1 test: newer seqs paint, and any
// frame whose seq is not newer than the last rendered is dropped.
func TestHandle_RendersLatestDropsStale(t *testing.T) {
	var buf bytes.Buffer
	r := New(nil, &buf)

	r.handle(snapshot(1, contracts.Face{Expression: contracts.ExpressionNeutral, EyesOpen: true}))
	r.handle(snapshot(2, contracts.Face{Expression: contracts.ExpressionHappy, EyesOpen: true}))
	afterLatest := buf.Len()

	// Stale frames (older seq, and a repeat of the current seq) must not paint.
	r.handle(snapshot(1, contracts.Face{Expression: contracts.ExpressionSad, EyesOpen: false}))
	r.handle(snapshot(2, contracts.Face{Expression: contracts.ExpressionSad, EyesOpen: false}))
	if buf.Len() != afterLatest {
		t.Fatalf("stale frame painted: buffer grew from %d to %d", afterLatest, buf.Len())
	}

	out := buf.String()
	// The latest rendered face is the happy one; the dropped sad face must not show.
	if !strings.Contains(out, mouth(contracts.ExpressionHappy)) {
		t.Fatalf("latest (happy) mouth not rendered:\n%s", out)
	}
	if strings.Contains(out, mouth(contracts.ExpressionSad)) {
		t.Fatalf("stale (sad) frame was rendered:\n%s", out)
	}
}

// TestHandle_BlinkTogglesEyes asserts EyesOpen drives the eye glyphs (the seam
// Story 2.3 uses).
func TestHandle_BlinkTogglesEyes(t *testing.T) {
	var buf bytes.Buffer
	r := New(nil, &buf)

	r.handle(snapshot(1, contracts.Face{Expression: contracts.ExpressionNeutral, EyesOpen: true}))
	r.handle(snapshot(2, contracts.Face{Expression: contracts.ExpressionNeutral, EyesOpen: false}))

	out := buf.String()
	if !strings.Contains(out, eyes(true)) || !strings.Contains(out, eyes(false)) {
		t.Fatalf("expected both open and blink eye frames:\n%s", out)
	}
}

// TestHandle_IgnoresNonSnapshotPayload is defensive: a wrong payload kind is a
// no-op, never a panic.
func TestHandle_IgnoresNonSnapshotPayload(t *testing.T) {
	var buf bytes.Buffer
	r := New(nil, &buf)

	r.handle(contracts.Envelope{
		Header:  contracts.Header{Kind: contracts.KindFaceSnapshot},
		Payload: contracts.OutboundMessage{Text: "not a snapshot"},
	})
	if buf.Len() != 0 {
		t.Fatalf("non-snapshot payload produced output: %q", buf.String())
	}
}

// TestServe_ExitsOnContextCancel covers the supervised-edge shutdown path.
func TestServe_ExitsOnContextCancel(t *testing.T) {
	ch := make(chan contracts.Envelope)
	r := New(ch, &bytes.Buffer{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Serve(ctx) }()

	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("Serve returned %v, want context.Canceled", err)
	}
}
