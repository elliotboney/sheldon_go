package contracts

import "encoding/gob"

// Register makes the concrete payload types known to gob. gob requires every
// concrete type stored behind an interface field (Envelope.Payload) to be
// registered before encode/decode, or it returns "gob: type not registered".
//
// It is called automatically by init; it is also exported so callers that build
// their own encoders/decoders can ensure registration explicitly. Register is
// safe to call more than once.
func Register() {
	gob.Register(Job{})
	gob.Register(Result{})
	gob.Register(InboundMessage{})
	gob.Register(OutboundMessage{})
	gob.Register(RegionSnapshot{})
}

func init() {
	Register()
}
