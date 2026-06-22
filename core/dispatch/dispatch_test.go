package dispatch_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/arbiter"
	"github.com/elliotboney/shelldon_go/core/bus"
	"github.com/elliotboney/shelldon_go/core/dispatch"
	"github.com/elliotboney/shelldon_go/core/state"
	"github.com/elliotboney/shelldon_go/worker"
)

// wantAck is the canned no-LLM acknowledgement published when the brain cannot
// answer (AC1), linked to the real const so the value can't drift (export_test.go).
var wantAck = dispatch.ReflexAckForTest

// errWorker models an absent brain: every turn fails immediately (the shape of
// Epic 3's provider-chain exhaustion).
type errWorker struct{}

func (errWorker) AssembleAndPropose(_ context.Context, _ contracts.Job) (contracts.Result, error) {
	return contracts.Result{}, errors.New("no brain available")
}

// hangingWorker models a brain that cannot complete: it blocks until its context
// is cancelled, so the arbiter timeout must abandon the turn.
type hangingWorker struct{}

func (hangingWorker) AssembleAndPropose(ctx context.Context, _ contracts.Job) (contracts.Result, error) {
	<-ctx.Done()
	return contracts.Result{}, ctx.Err()
}

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
	d := dispatch.New(hub, arbiter.New(worker.Stub{}, time.Minute), inbound, store)

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

// TestServe_AcksWhenBrainAbsent is AC1: when the worker cannot answer (it errors),
// the message is acknowledged with the canned reflex ack, not dropped.
func TestServe_AcksWhenBrainAbsent(t *testing.T) {
	hub := bus.New()
	inbound := make(chan contracts.Envelope, 1)
	outbound := make(chan contracts.Envelope, 1)
	if err := hub.Register(contracts.KindOutboundMessage, outbound); err != nil {
		t.Fatalf("register outbound: %v", err)
	}
	store := state.New(state.Default(), filepath.Join(t.TempDir(), "state.json"))
	d := dispatch.New(hub, arbiter.New(errWorker{}, time.Minute), inbound, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Serve(ctx) }()

	inbound <- contracts.Envelope{
		Header:  contracts.Header{Kind: contracts.KindInboundMessage, Src: "cli", Dst: "core"},
		Payload: contracts.InboundMessage{ConvoID: "c1", Text: "hi"},
	}

	env := <-outbound
	msg := env.Payload.(contracts.OutboundMessage)
	if msg.Text != wantAck {
		t.Fatalf("brain-absent reply = %q, want the reflex ack %q", msg.Text, wantAck)
	}
}

// TestServe_NeverBlocksUnderHungBrain is AC1's never-block property: with a brain
// that hangs every turn, the dispatch loop still drains the queue — both inbound
// messages are acknowledged (each via the arbiter timeout), so one stuck turn
// never wedges the loop. Deterministic under the synctest fake clock (AD-10).
func TestServe_NeverBlocksUnderHungBrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const timeout = 5 * time.Second
		hub := bus.New()
		inbound := make(chan contracts.Envelope, 2)
		outbound := make(chan contracts.Envelope, 2)
		if err := hub.Register(contracts.KindOutboundMessage, outbound); err != nil {
			t.Fatalf("register outbound: %v", err)
		}
		store := state.New(state.Default(), filepath.Join(t.TempDir(), "state.json"))
		d := dispatch.New(hub, arbiter.New(hangingWorker{}, timeout), inbound, store)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = d.Serve(ctx) }()

		for _, id := range []string{"c1", "c2"} {
			inbound <- contracts.Envelope{
				Header:  contracts.Header{Kind: contracts.KindInboundMessage, Src: "cli", Dst: "core"},
				Payload: contracts.InboundMessage{ConvoID: id, Text: "hi"},
			}
		}

		// Turns run sequentially (≤1 in flight); advance past both deadlines.
		time.Sleep(2*timeout + time.Second)
		synctest.Wait()

		got := map[string]string{}
		for i := 0; i < 2; i++ {
			env := <-outbound
			msg := env.Payload.(contracts.OutboundMessage)
			got[msg.ConvoID] = msg.Text
		}
		for _, id := range []string{"c1", "c2"} {
			if got[id] != wantAck {
				t.Errorf("convo %s reply = %q, want the reflex ack %q (loop wedged or message dropped)", id, got[id], wantAck)
			}
		}
	})
}
