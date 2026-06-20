package worker

import (
	"context"

	"github.com/elliotboney/shelldon_go/contracts"
)

// Stub is the M0 worker behind the seam. It reads nothing — no history, markdown,
// or vault (prompt assembly is deferred to Story 3.3) — and proposes a trivial
// well-formed Result that echoes the turn input, so later stories have something
// to round-trip and render.
type Stub struct{}

var _ Worker = Stub{}

func (Stub) AssembleAndPropose(_ context.Context, turn contracts.Job) (contracts.Result, error) {
	return contracts.Result{Reply: turn.Input}, nil
}
