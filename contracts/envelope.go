// Package contracts defines the versioned Envelope/Job/Result types that are
// the uniform contract over shelldon's internal bus. They are passed as Go
// structs in-process and gob-serialized only at the worker wall (M3); this
// package proves the gob path now so that swap is a pure transport change.
//
// Binding invariants (ARCHITECTURE-SPINE.md):
//   - The header is closed: exactly id/v/kind/src/dst/turn_id (AD-11).
//   - Versioning is the v field + additive-only struct fields; never remove or
//     reorder, only append (AD-10).
//   - No credentials ever travel on the bus (AD-9, NFR8).
//   - This package imports no provider/LLM SDK (AD-1, NFR3).
package contracts

// Kind identifies the kind of an Envelope. It is a closed set of typed
// constants; event kinds are added by the stories that introduce them.
type Kind string

const (
	// KindJob is a turn input dispatched from core to the worker.
	KindJob Kind = "job"
	// KindResult is a worker proposal returned to core.
	KindResult Kind = "result"
	// KindInboundMessage is a chat message from a transport adapter to core.
	KindInboundMessage Kind = "inbound-message"
	// KindOutboundMessage is a reply from core to a transport adapter.
	KindOutboundMessage Kind = "outbound-message"
	// KindFaceSnapshot is a face-region snapshot core pushes to the display edge
	// (AD-6); its payload is a RegionSnapshot.
	KindFaceSnapshot Kind = "face-snapshot"
)

// AllKinds is the closed set of declared kinds. The required M0 round-trip test
// is derived from this slice, so adding a Kind without a corresponding test row
// fails the suite (AC1: every kind, not a sample).
var AllKinds = []Kind{KindJob, KindResult, KindInboundMessage, KindOutboundMessage, KindFaceSnapshot}

// Header is the closed envelope header from AD-11. No fields may be added here
// without a contract change.
type Header struct {
	ID     string // unique id of this envelope
	V      int    // contract version carried by the payload
	Kind   Kind   // discriminates the payload kind
	Src    string // source address on the bus
	Dst    string // destination address on the bus
	TurnID string // fencing field for idempotent close (AD-11)
}

// Payload is the marker interface implemented by every concrete payload. The
// unexported method keeps the set closed to this package.
type Payload interface {
	isPayload()
}

// Envelope is the uniform unit on the bus: the closed Header plus a concrete
// Payload. The Payload field is an interface, so each concrete type must be
// registered with gob before encode/decode (see register.go).
type Envelope struct {
	Header
	Payload Payload
}
