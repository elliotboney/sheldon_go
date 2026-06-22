package cli_test

import (
	"bufio"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/arbiter"
	"github.com/elliotboney/shelldon_go/core/bus"
	"github.com/elliotboney/shelldon_go/core/dispatch"
	"github.com/elliotboney/shelldon_go/core/state"
	"github.com/elliotboney/shelldon_go/transport/cli"
	"github.com/elliotboney/shelldon_go/worker"
)

// TestEndToEndRoundTrip is the AC1 spine proof: a CLI line round-trips
// inbound → core → worker seam → stub → outbound → CLI, wiring the REAL bus,
// arbiter, and stub (no mocks). The stub echoes the input, so the reply
// rendered back at the CLI equals the line typed.
func TestEndToEndRoundTrip(t *testing.T) {
	hub := bus.New()
	inbound := make(chan contracts.Envelope, 1)
	outbound := make(chan contracts.Envelope, 1)
	if err := hub.Register(contracts.KindInboundMessage, inbound); err != nil {
		t.Fatalf("register inbound: %v", err)
	}
	if err := hub.Register(contracts.KindOutboundMessage, outbound); err != nil {
		t.Fatalf("register outbound: %v", err)
	}

	arb := arbiter.New(worker.Stub{}, time.Minute)
	store := state.New(state.Default(), filepath.Join(t.TempDir(), "state.json"))
	disp := dispatch.New(hub, arb, inbound, store)

	pr, pw := io.Pipe()
	adapter := cli.New(hub, outbound, strings.NewReader("hello\n"), pw, "cli")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = disp.Serve(ctx) }()
	go func() { _ = adapter.Serve(ctx) }()

	// Reading the reply from the pipe is the happens-before edge — it blocks
	// until the CLI adapter writes. Read in a goroutine guarded by a timeout so a
	// failure to deliver surfaces as a test failure, not a 10-minute CI hang.
	type readResult struct {
		line string
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		line, err := bufio.NewReader(pr).ReadString('\n')
		done <- readResult{line, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("read reply: %v", r.err)
		}
		if got := strings.TrimSpace(r.line); got != "hello" {
			t.Fatalf("reply = %q, want %q (the stub echo did not round-trip)", got, "hello")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the round-trip reply — the spine did not deliver")
	}
}
