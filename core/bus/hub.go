// Package bus is the core-owned, in-process message hub. core is the LLM-free
// supervisor root that hosts this hub (AD-1); edges communicate only through it.
//
// The hub routes contracts.Envelope values by Kind to a registered destination
// channel — point-to-point, no broadcast (that arrives with later event-kind
// stories). Envelopes are passed as Go structs with NO serialization (AD-4); the
// channel transport is swappable seed (channel now → UDS+gob at the worker wall
// in M3) and the Register/Publish API is transport-agnostic so that swap reshapes
// no caller. An unknown destination fails safe with ErrNoRoute — the hub never
// panics (AD-4).
package bus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/elliotboney/shelldon_go/contracts"
)

// ErrNoRoute is returned by Publish when no destination is registered for the
// envelope's Kind. It is a routing error, not a panic (AD-4 fail-safe). The
// returned error wraps it with the offending kind for diagnostics.
var ErrNoRoute = errors.New("bus: no route registered for kind")

// ErrDuplicateRoute is returned by Register when a destination is already
// registered for the kind. Routes are not silently clobbered (AD-4 fail-safe).
var ErrDuplicateRoute = errors.New("bus: kind already registered")

// ErrNilDestination is returned by Register when the destination channel is nil.
// A nil channel would silently block every send forever, so it is rejected at
// registration (AD-4 fail-safe).
var ErrNilDestination = errors.New("bus: nil destination channel")

// Hub is the point-to-point routing table from Kind to a destination channel.
// The zero value is not usable; construct with New.
type Hub struct {
	mu     sync.RWMutex
	routes map[contracts.Kind]chan<- contracts.Envelope
}

// New returns a ready-to-use Hub.
func New() *Hub {
	return &Hub{routes: make(map[contracts.Kind]chan<- contracts.Envelope)}
}

// Register binds a destination channel to a Kind. It returns ErrDuplicateRoute if
// the Kind is already registered; the registrant owns the channel and chooses its
// buffering.
func (h *Hub) Register(kind contracts.Kind, dst chan<- contracts.Envelope) error {
	if dst == nil {
		return ErrNilDestination
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.routes[kind]; ok {
		return ErrDuplicateRoute
	}
	h.routes[kind] = dst
	return nil
}

// Publish delivers env to the destination registered for env.Kind. It returns
// ErrNoRoute if no destination is registered. Delivery is a blocking channel send
// (point-to-point backpressure to the single consumer); the routing-table lock is
// released before the send so a slow receiver cannot stall other routing.
func (h *Hub) Publish(env contracts.Envelope) error {
	h.mu.RLock()
	dst, ok := h.routes[env.Kind]
	h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoRoute, env.Kind)
	}
	dst <- env
	return nil
}
