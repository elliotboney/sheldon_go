// Package compositor is the core-side owner of the face region (AD-6). It assigns
// the monotonic per-region seq and publishes face snapshots to the display edge
// through the bus — core is the sole writer of display state; the display never
// reads shared memory, it only renders pushed snapshots latest-wins.
//
// It imports only contracts + core/bus; it never imports a renderer (display is
// an edge). The reflex stories call PushFace to animate the face: blink (2.3)
// toggles EyesOpen, mood-drift (2.4) sets Expression.
package compositor

import (
	"sync"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/bus"
)

// Compositor owns the face region's monotonic seq and publishes snapshots.
type Compositor struct {
	hub *bus.Hub
	mu  sync.Mutex
	seq uint64
}

// New returns a Compositor publishing through hub.
func New(hub *bus.Hub) *Compositor {
	return &Compositor{hub: hub}
}

// PushFace publishes face as a RegionSnapshot for the face region with the next
// monotonic seq. The seq lets the renderer drop stale frames (AD-4/NFR12).
func (c *Compositor) PushFace(face contracts.Face) error {
	c.mu.Lock()
	c.seq++
	seq := c.seq
	c.mu.Unlock()

	return c.hub.Publish(contracts.Envelope{
		Header:  contracts.Header{Kind: contracts.KindFaceSnapshot, Src: "core", Dst: "display"},
		Payload: contracts.RegionSnapshot{Region: contracts.RegionFace, Seq: seq, Face: face},
	})
}
