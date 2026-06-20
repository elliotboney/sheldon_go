package worker

import (
	"context"
	"testing"

	"github.com/elliotboney/shelldon_go/contracts"
)

// TestStub_ReturnsWellFormedResult proves the stub behind the Worker seam returns
// a well-formed Result with no error (AC1).
func TestStub_ReturnsWellFormedResult(t *testing.T) {
	var w Worker = Stub{} // compile-time: Stub satisfies the seam

	got, err := w.AssembleAndPropose(context.Background(), contracts.Job{Input: "hi", ConvoID: "c1"})
	if err != nil {
		t.Fatalf("AssembleAndPropose: unexpected error %v", err)
	}
	if got.Reply != "hi" {
		t.Errorf("Reply = %q, want %q (echo stub)", got.Reply, "hi")
	}
}
