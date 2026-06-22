package arbiter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

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

// hangingWorker blocks until its context is cancelled, recording that it observed
// the cancellation. It models a brain that cannot complete a turn, so the arbiter
// timeout must abandon it.
type hangingWorker struct {
	sawCancel atomic.Bool
}

func (w *hangingWorker) AssembleAndPropose(ctx context.Context, _ contracts.Job) (contracts.Result, error) {
	<-ctx.Done()
	w.sawCancel.Store(true)
	return contracts.Result{}, ctx.Err()
}

// TestArbiter_TimeoutClosesTurn is AC2 (AD-8/AD-11): a turn the brain cannot
// complete is closed when the arbiter timeout elapses — Submit returns
// ErrTurnTimeout, the worker's context is cancelled (the late-Result fence), and
// the slot is freed so a subsequent turn is admitted (not rejected as in-flight).
// Deterministic under the synctest fake clock (AD-10).
func TestArbiter_TimeoutClosesTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const timeout = 5 * time.Second
		w := &hangingWorker{}
		a := New(w, timeout)
		job := contracts.Job{Input: "hi", ConvoID: "c1"}

		submit := func() (contracts.Result, error) {
			var res contracts.Result
			var err error
			done := make(chan struct{})
			go func() {
				defer close(done)
				res, err = a.Submit(context.Background(), job)
			}()
			time.Sleep(timeout + time.Second) // fake-advance past the deadline
			synctest.Wait()
			<-done
			return res, err
		}

		res, err := submit()
		if !errors.Is(err, ErrTurnTimeout) {
			t.Fatalf("Submit returned %v, want ErrTurnTimeout", err)
		}
		if res.Reply != "" {
			t.Errorf("timed-out turn returned a reply %q, want empty", res.Reply)
		}
		if !w.sawCancel.Load() {
			t.Fatal("worker context was not cancelled on timeout — AD-11 fence missing")
		}

		// The slot must have reopened: a second turn is admitted (it times out
		// again) rather than rejected with ErrTurnInFlight — no turn remains in
		// flight past the timeout.
		if _, err := submit(); !errors.Is(err, ErrTurnTimeout) {
			if errors.Is(err, ErrTurnInFlight) {
				t.Fatal("slot not freed after timeout: second Submit rejected as in-flight")
			}
			t.Fatalf("second Submit returned %v, want ErrTurnTimeout (admitted then timed out)", err)
		}
	})
}

// panicWorker panics on every turn — models a buggy/injected brain.
type panicWorker struct{}

func (panicWorker) AssembleAndPropose(_ context.Context, _ contracts.Job) (contracts.Result, error) {
	panic("worker blew up")
}

// TestArbiter_RecoversWorkerPanic proves the per-turn goroutine recovers its own
// panic (AD-5: recover does not cross goroutines): a panicking worker returns an
// error rather than leaking the in-flight token, and the slot reopens so the next
// turn is admitted — the pet never freezes.
func TestArbiter_RecoversWorkerPanic(t *testing.T) {
	a := New(panicWorker{}, time.Minute)
	job := contracts.Job{Input: "hi", ConvoID: "c1"}

	if _, err := a.Submit(context.Background(), job); err == nil {
		t.Fatal("Submit returned nil error for a panicking worker, want a turn error")
	}
	// The token must have been released: a second turn is admitted, not rejected.
	if _, err := a.Submit(context.Background(), job); errors.Is(err, ErrTurnInFlight) {
		t.Fatal("token leaked after worker panic: second Submit rejected as in-flight (frozen pet)")
	}
}

// TestArbiter_AtMostOneInFlight is the required ≤1-worker M0 test (NFR4/AD-8):
// with one turn in flight, every concurrent submission is rejected and the two
// turns never overlap. Run under `go test -race` for AC3.
func TestArbiter_AtMostOneInFlight(t *testing.T) {
	w := newBlockingWorker()
	a := New(w, time.Minute) // generous timeout: the blocking worker must not be killed mid-test
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
