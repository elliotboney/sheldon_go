// Package terminal is the M1 render target for the display compositor (AD-6): a
// supervised bus-client edge that consumes face-snapshot envelopes and paints the
// face as ANSI. It renders latest-wins, dropping any frame whose seq is not newer
// than the last rendered (NFR12).
//
// The Waveshare E-Ink renderer (Story 6.1) implements the SAME compositor
// contract from contracts/, so the render target swaps with no core change. All
// terminal/ANSI specifics live here; no terminal type ever crosses into core.
package terminal

import (
	"context"
	"fmt"
	"io"

	"github.com/elliotboney/shelldon_go/contracts"
)

// clearScreen homes the cursor and clears the terminal so each face paints over
// the previous one (latest-wins on a live terminal).
const clearScreen = "\033[H\033[2J"

// Renderer paints face snapshots to out, dropping stale frames by seq.
type Renderer struct {
	snapshots <-chan contracts.Envelope
	out       io.Writer
	lastSeq   uint64
}

// New returns a terminal renderer reading snapshots from the given channel and
// painting to out (injected so tests wire a buffer).
func New(snapshots <-chan contracts.Envelope, out io.Writer) *Renderer {
	return &Renderer{snapshots: snapshots, out: out}
}

// Serve renders snapshots until ctx is cancelled. It is wrapped by
// supervisor.Guard under the suture root (AD-5).
func (r *Renderer) Serve(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env := <-r.snapshots:
			r.handle(env)
		}
	}
}

// handle paints the snapshot if it is newer than the last rendered, else drops it
// (latest-wins, NFR12). Non-snapshot payloads are ignored defensively.
func (r *Renderer) handle(env contracts.Envelope) {
	snap, ok := env.Payload.(contracts.RegionSnapshot)
	if !ok {
		return
	}
	if snap.Seq <= r.lastSeq {
		return // stale frame
	}
	r.lastSeq = snap.Seq
	r.paint(snap.Face)
}

// paint maps the render-agnostic Face to a minimal ANSI face and writes it.
func (r *Renderer) paint(face contracts.Face) {
	_, _ = fmt.Fprint(r.out, clearScreen, eyes(face.EyesOpen), "\n", mouth(face.Expression), "\n")
}

// eyes renders the eye line: open or mid-blink.
func eyes(open bool) string {
	if open {
		return "( o   o )"
	}
	return "( -   - )"
}

// mouth renders the mouth line for an expression.
func mouth(e contracts.Expression) string {
	switch e {
	case contracts.ExpressionHappy:
		return `  \___/  `
	case contracts.ExpressionSad:
		return `  /^^^\  `
	default: // ExpressionNeutral and any future value fall back to neutral
		return "  -----  "
	}
}
