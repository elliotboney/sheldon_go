// Command shelldon is the single supervised process: it wires the bus, arbiter,
// worker stub, personality-state checkpoint, core dispatch loop, CLI transport
// adapter, terminal face renderer, and blink reflex, then runs them as supervised
// edges under the core suture root until a shutdown signal arrives, draining edges
// in reverse start order (AD-5).
package main

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/arbiter"
	"github.com/elliotboney/shelldon_go/core/bus"
	"github.com/elliotboney/shelldon_go/core/compositor"
	"github.com/elliotboney/shelldon_go/core/dispatch"
	"github.com/elliotboney/shelldon_go/core/reflexes"
	"github.com/elliotboney/shelldon_go/core/state"
	"github.com/elliotboney/shelldon_go/core/supervisor"
	"github.com/elliotboney/shelldon_go/display/terminal"
	"github.com/elliotboney/shelldon_go/transport/cli"
	"github.com/elliotboney/shelldon_go/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := bus.New()
	arb := arbiter.New(worker.Stub{})

	// Personality-state: restore from the RAM checkpoint, or defaults on first
	// boot (AD-16). The checkpoint lives beside, not inside, the Epic 4 durable
	// memory layers (~/.shelldon/memory, ~/.shelldon/history.db).
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("resolve home dir", "err", err)
		os.Exit(1)
	}
	shelldonDir := filepath.Join(home, ".shelldon")
	if err := os.MkdirAll(shelldonDir, 0o755); err != nil {
		slog.Error("create ~/.shelldon", "err", err)
		os.Exit(1)
	}
	statePath := filepath.Join(shelldonDir, "state.json")
	store := state.New(state.Load(statePath), statePath)

	inbound := make(chan contracts.Envelope, 16)
	outbound := make(chan contracts.Envelope, 16)
	if err := hub.Register(contracts.KindInboundMessage, inbound); err != nil {
		slog.Error("register inbound route", "err", err)
		os.Exit(1)
	}
	if err := hub.Register(contracts.KindOutboundMessage, outbound); err != nil {
		slog.Error("register outbound route", "err", err)
		os.Exit(1)
	}
	display := make(chan contracts.Envelope, 16)
	if err := hub.Register(contracts.KindFaceSnapshot, display); err != nil {
		slog.Error("register face-snapshot route", "err", err)
		os.Exit(1)
	}

	disp := dispatch.New(hub, arb, inbound, store)
	adapter := cli.New(hub, outbound, os.Stdin, os.Stdout, "cli")
	comp := compositor.New(hub)
	renderer := terminal.New(display, os.Stdout)
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
	blink := reflexes.NewBlink(comp, store, rng)

	root := supervisor.New("shelldon")
	// Start order: state-checkpoint first, then dispatch, then CLI → reverse drain
	// stops CLI, then dispatch, then state-checkpoint last so its shutdown flush
	// captures the final state after the other edges have stopped.
	root.Add(supervisor.Guard("state-checkpoint", store.RunCheckpointLoop))
	root.Add(supervisor.Guard("core-dispatch", disp.Serve))
	root.Add(supervisor.Guard("cli-transport", adapter.Serve))
	root.Add(supervisor.Guard("display-terminal", renderer.Serve))
	root.Add(supervisor.Guard("reflex-blink", blink.Serve))

	// Show an initial face on boot. The mood-driven expression is Story 2.4; the
	// buffered display channel absorbs this push until the renderer starts.
	if err := comp.PushFace(contracts.Face{Expression: contracts.ExpressionNeutral, EyesOpen: true}); err != nil {
		slog.Error("push initial face", "err", err)
	}

	if err := root.Serve(ctx); err != nil {
		slog.Error("supervisor exited with error", "err", err)
		os.Exit(1)
	}
}
