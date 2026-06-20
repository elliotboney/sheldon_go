// Package worker defines the isolation seam between core and the turn executor.
//
// Worker is a Go interface (AD-2): the same seam hosts a Monolith+ goroutine
// implementation for M0–M2 and, at M3, a uid-separated Privsep-lite subprocess —
// callers never reshape across that swap. The worker only *proposes* a Result; it
// never writes state or memory (AD-6), so core stays the single writer.
package worker

import (
	"context"

	"github.com/elliotboney/shelldon_go/contracts"
)

// Worker assembles a turn and proposes a Result. The bus transport beneath this
// seam (in-process channels now, UDS+gob at M3) swaps without changing this
// interface.
type Worker interface {
	AssembleAndPropose(ctx context.Context, turn contracts.Job) (contracts.Result, error)
}
