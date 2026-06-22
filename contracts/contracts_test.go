package contracts

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"
)

// roundTrip encodes v to gob and decodes it back into a fresh value of the same
// type, returning the decoded value.
func roundTrip[T any](t *testing.T, v T) T {
	t.Helper()
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got T
	if err := gob.NewDecoder(&buf).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

// TestEnvelopeRoundTrip is the required M0 contract gob round-trip test. It
// exercises every declared Kind (AC1: every kind, not a representative sample).
// The cases are keyed by Kind and cross-checked against AllKinds, so adding a
// Kind without a case here fails the suite.
func TestEnvelopeRoundTrip(t *testing.T) {
	cases := map[Kind]Envelope{
		KindJob: {
			Header:  Header{ID: "e1", V: 1, Kind: KindJob, Src: "core", Dst: "worker", TurnID: "t1"},
			Payload: Job{Input: "hello", ConvoID: "c1"},
		},
		KindResult: {
			Header:  Header{ID: "e2", V: 1, Kind: KindResult, Src: "worker", Dst: "core", TurnID: "t1"},
			Payload: Result{Reply: "hi", MemoryOps: []MemoryOp{{Kind: "remember"}}},
		},
		KindInboundMessage: {
			Header:  Header{ID: "e3", V: 1, Kind: KindInboundMessage, Src: "cli", Dst: "core", TurnID: "t2"},
			Payload: InboundMessage{ConvoID: "cli", Text: "hello"},
		},
		KindOutboundMessage: {
			Header:  Header{ID: "e4", V: 1, Kind: KindOutboundMessage, Src: "core", Dst: "cli", TurnID: "t2"},
			Payload: OutboundMessage{ConvoID: "cli", Text: "hello"},
		},
		KindFaceSnapshot: {
			Header:  Header{ID: "e5", V: 1, Kind: KindFaceSnapshot, Src: "core", Dst: "display", TurnID: ""},
			Payload: RegionSnapshot{Region: RegionFace, Seq: 7, Face: Face{Expression: ExpressionHappy, EyesOpen: true}},
		},
	}

	for _, k := range AllKinds {
		env, ok := cases[k]
		if !ok {
			t.Errorf("kind %q has no round-trip case; every declared kind must be exercised (AC1)", k)
			continue
		}
		t.Run(string(k), func(t *testing.T) {
			got := roundTrip(t, env)
			if !reflect.DeepEqual(env, got) {
				t.Errorf("round-trip mismatch\n got: %#v\nwant: %#v", got, env)
			}
		})
	}
}

// jobV1 is an older shape of Job: a subset of its fields (it predates ConvoID).
// It exists only to prove additive-only evolution (AC2).
type jobV1 struct {
	Input string
}

// TestAdditiveEvolution proves additive-only field changes are non-breaking in
// both directions (AC2): a decoder built against the older shape still decodes
// newer bytes, and a newer decoder reading older bytes leaves the new field at
// its zero value.
func TestAdditiveEvolution(t *testing.T) {
	t.Run("new->old drops appended field", func(t *testing.T) {
		newJob := Job{Input: "hello", ConvoID: "c1"}
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(newJob); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var old jobV1
		if err := gob.NewDecoder(&buf).Decode(&old); err != nil {
			t.Fatalf("older decoder must read newer bytes without error: %v", err)
		}
		if old.Input != newJob.Input {
			t.Errorf("Input = %q, want %q", old.Input, newJob.Input)
		}
	})

	t.Run("old->new zero-fills appended field", func(t *testing.T) {
		oldJob := jobV1{Input: "world"}
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(oldJob); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var got Job
		if err := gob.NewDecoder(&buf).Decode(&got); err != nil {
			t.Fatalf("newer decoder must read older bytes without error: %v", err)
		}
		if got.Input != oldJob.Input {
			t.Errorf("Input = %q, want %q", got.Input, oldJob.Input)
		}
		if got.ConvoID != "" {
			t.Errorf("appended field ConvoID = %q, want zero value", got.ConvoID)
		}
	})
}

// resultV1 is an older shape of Result: a subset of its fields (it predates
// MemoryOps). It exists only to prove additive-only evolution (AC2) for the
// Result payload, mirroring jobV1 for Job.
type resultV1 struct {
	Reply string
}

// TestResultAdditiveEvolution proves additive-only evolution for Result in both
// directions (AC2): appending the MemoryOps slice is non-breaking against an
// older Reply-only decoder, and older bytes leave MemoryOps at its zero value.
func TestResultAdditiveEvolution(t *testing.T) {
	t.Run("new->old drops appended field", func(t *testing.T) {
		newResult := Result{Reply: "hi", MemoryOps: []MemoryOp{{Kind: "remember"}}}
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(newResult); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var old resultV1
		if err := gob.NewDecoder(&buf).Decode(&old); err != nil {
			t.Fatalf("older decoder must read newer bytes without error: %v", err)
		}
		if old.Reply != newResult.Reply {
			t.Errorf("Reply = %q, want %q", old.Reply, newResult.Reply)
		}
	})

	t.Run("old->new zero-fills appended field", func(t *testing.T) {
		oldResult := resultV1{Reply: "bye"}
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(oldResult); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var got Result
		if err := gob.NewDecoder(&buf).Decode(&got); err != nil {
			t.Fatalf("newer decoder must read older bytes without error: %v", err)
		}
		if got.Reply != oldResult.Reply {
			t.Errorf("Reply = %q, want %q", got.Reply, oldResult.Reply)
		}
		if got.MemoryOps != nil {
			t.Errorf("appended field MemoryOps = %v, want zero value (nil)", got.MemoryOps)
		}
	})
}
