package dispatch_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/arbiter"
	"github.com/elliotboney/shelldon_go/core/bus"
	"github.com/elliotboney/shelldon_go/core/dispatch"
	"github.com/elliotboney/shelldon_go/core/state"
	"github.com/elliotboney/shelldon_go/worker"
)

// TestServe_TouchesStateOnInbound verifies an inbound message stamps
// LastInteraction (the idle reset the blink reflex depends on, Story 2.3).
func TestServe_TouchesStateOnInbound(t *testing.T) {
	hub := bus.New()
	inbound := make(chan contracts.Envelope, 1)
	outbound := make(chan contracts.Envelope, 1)
	if err := hub.Register(contracts.KindOutboundMessage, outbound); err != nil {
		t.Fatalf("register outbound: %v", err)
	}

	old := time.Now().Add(-time.Hour)
	store := state.New(state.Personality{LastInteraction: old}, filepath.Join(t.TempDir(), "state.json"))
	d := dispatch.New(hub, arbiter.New(worker.Stub{}), inbound, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Serve(ctx) }()

	inbound <- contracts.Envelope{
		Header:  contracts.Header{Kind: contracts.KindInboundMessage, Src: "cli", Dst: "core"},
		Payload: contracts.InboundMessage{ConvoID: "c1", Text: "hi"},
	}

	// The reply proves the turn was processed; Touch ran before the submit.
	<-outbound
	if !store.Snapshot().LastInteraction.After(old) {
		t.Fatal("LastInteraction was not stamped on inbound message")
	}
}
