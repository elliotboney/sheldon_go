package arbiter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/elliotboney/shelldon_go/contracts"
)

// blockingWorker records the maximum number of concurrent AssembleAndPropose
// calls and blocks inside the call until released, so a test can hold a turn
// "in flight" while it probes the arbiter gate.
type blockingWorker struct {
	inFlight atomic.Int32
	maxSeen  atomic.Int32
	entered  chan struct{} // signalled once when the first call enters
	release  chan struct{} // closed to let blocked calls return
}

func newBlockingWorker() *blockingWorker {
	return &blockingWorker{entered: make(chan struct{}, 1), release: make(chan struct{})}
}

func (w *blockingWorker) AssembleAndPropose(_ context.Context, turn contracts.Job) (contracts.Result, error) {
	n := w.inFlight.Add(1)
	for {
		old := w.maxSeen.Load()
		if n <= old || w.maxSeen.CompareAndSwap(old, n) {
			break
		}
	}
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	w.inFlight.Add(-1)
	return contracts.Result{Reply: turn.Input}, nil
}

// TestArbiter_AtMostOneInFlight is the required ≤1-worker M0 test (NFR4/AD-8):
// with one turn in flight, every concurrent submission is rejected and the two
// turns never overlap. Run under `go test -race` for AC3.
func TestArbiter_AtMostOneInFlight(t *testing.T) {
	w := newBlockingWorker()
	a := New(w)
	ctx := context.Background()
	job := contracts.Job{Input: "hi", ConvoID: "c1"}

	// First turn: acquires the token and blocks inside the worker.
	var first contracts.Result
	var firstErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		first, firstErr = a.Submit(ctx, job)
	}()
	<-w.entered // happens-before: the first turn now holds the single in-flight slot

	// Every concurrent submission while the slot is held must be rejected.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.Submit(ctx, job); !errors.Is(err, ErrTurnInFlight) {
				t.Errorf("concurrent Submit: got %v, want ErrTurnInFlight", err)
			}
		}()
	}
	wg.Wait()

	if got := w.maxSeen.Load(); got != 1 {
		t.Fatalf("max concurrent turns = %d, want 1 (≤1-worker invariant violated)", got)
	}

	close(w.release)
	<-done
	if firstErr != nil {
		t.Fatalf("first turn errored: %v", firstErr)
	}
	if first.Reply != "hi" {
		t.Errorf("first Result.Reply = %q, want %q", first.Reply, "hi")
	}

	// The SAME arbiter's gate must reopen once the in-flight turn completes —
	// i.e. the deferred `<-a.token` drain actually ran. release is already
	// closed, so the worker no longer blocks.
	got, err := a.Submit(ctx, job)
	if err != nil {
		t.Fatalf("same arbiter rejected a new turn after the first completed (token not drained): %v", err)
	}
	if got.Reply != "hi" {
		t.Errorf("reopened gate Result.Reply = %q, want %q", got.Reply, "hi")
	}
}
