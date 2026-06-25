package dream

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/arbiter"
	"github.com/elliotboney/shelldon_go/core/memory"
	"github.com/elliotboney/shelldon_go/core/turntier"
)

// dreamWorker is a fake worker.Worker standing in for the real LLM dream so no
// credit burns: it captures the dream input and returns canned promote/prune ops.
type dreamWorker struct {
	ops      []contracts.MemoryOp
	gotInput string
}

func (w *dreamWorker) AssembleAndPropose(_ context.Context, turn contracts.Job) (contracts.Result, error) {
	w.gotInput = turn.Input
	return contracts.Result{MemoryOps: w.ops}, nil
}

// newStores opens a fresh sqlite store + curated tree under t.TempDir(). It returns
// the store, the curated tree, and the curated root path (for vault assertions).
func newStores(t *testing.T) (*memory.Store, *memory.Curated, string) {
	t.Helper()
	store, err := memory.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := t.TempDir()
	curated, err := memory.OpenCurated(root)
	if err != nil {
		t.Fatalf("open curated: %v", err)
	}
	return store, curated, root
}

// applyN records observation under patternKey n times so its recurrence_count
// reaches n (each ApplyLearning bumps the keyed row).
func applyN(t *testing.T, store *memory.Store, observation, patternKey string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := store.ApplyLearning(context.Background(), observation, patternKey); err != nil {
			t.Fatalf("seed learning: %v", err)
		}
	}
}

// TestBuild_FiltersByRecurrence proves build offers only at/above-threshold pending
// learnings as candidates, formats them into the JobDream input, and drops
// low-recurrence noise.
func TestBuild_FiltersByRecurrence(t *testing.T) {
	store, _, _ := newStores(t)
	applyN(t, store, "owner prefers terse replies", "style.terse", promoteThreshold+1) // candidate
	applyN(t, store, "owner mentioned cats once", "topic.cats", 1)                      // below threshold

	job := build(store)
	if job.Kind != contracts.JobDream {
		t.Fatalf("Kind = %q, want JobDream", job.Kind)
	}
	if !strings.Contains(job.Input, "style.terse") {
		t.Errorf("dream input %q missing the recurring candidate", job.Input)
	}
	if strings.Contains(job.Input, "topic.cats") {
		t.Errorf("dream input %q included a below-threshold learning", job.Input)
	}
}

// TestDream_PromoteInfluencesLaterTurn is AC1 end-to-end: a recurring pending
// learning, dreamed over a real arbiter+fake worker that proposes promoting it, is
// marked promoted in sqlite AND written into curated markdown — and a LATER normal
// turn's assembled prompt context includes it (via 4.4's read path), proving the
// promotion influenced a later turn. Low-value learnings are pruned, not promoted.
func TestDream_PromoteInfluencesLaterTurn(t *testing.T) {
	ctx := context.Background()
	store, curated, _ := newStores(t)

	const key = "style.terse"
	const obs = "The owner prefers short, direct replies."
	applyN(t, store, obs, key, 5) // recurring → a promotion candidate

	w := &dreamWorker{ops: []contracts.MemoryOp{
		{Kind: contracts.MemoryOpPromoteLearning, PatternKey: key, Observation: obs},
	}}
	arb := arbiter.New(w, time.Minute)

	// Build → submit through the arbiter → core applies the proposal.
	res, err := arb.Submit(ctx, build(store))
	if err != nil {
		t.Fatalf("arbiter submit: %v", err)
	}
	if !strings.Contains(w.gotInput, key) {
		t.Fatalf("worker never saw the candidate; input=%q", w.gotInput)
	}
	applyResult(ctx, store, curated, res, nil)

	// Marked promoted in sqlite.
	got, ok, err := store.LearningByPatternKey(ctx, key)
	if err != nil || !ok {
		t.Fatalf("read learning back: ok=%v err=%v", ok, err)
	}
	if got.Status != memory.LearningStatusPromoted {
		t.Errorf("status = %q, want %q", got.Status, memory.LearningStatusPromoted)
	}

	// Written into curated markdown.
	facts, err := curated.ReadLearnings()
	if err != nil {
		t.Fatalf("read learnings: %v", err)
	}
	if !strings.Contains(facts, obs) {
		t.Errorf("curated learnings %q missing the promoted observation", facts)
	}

	// Influences a LATER normal turn: the assembled prompt context includes it.
	later := memory.NewContext(store, curated, 10)
	block, err := later.PromptContext(ctx, "c1")
	if err != nil {
		t.Fatalf("prompt context: %v", err)
	}
	if !strings.Contains(block, obs) {
		t.Errorf("later turn's prompt context %q did not include the promoted learning", block)
	}
}

// TestDream_PrunesLowValue proves a prune op marks the learning pruned (and writes
// nothing to curated markdown).
func TestDream_PrunesLowValue(t *testing.T) {
	ctx := context.Background()
	store, curated, _ := newStores(t)

	const key = "topic.weather"
	applyN(t, store, "owner said it was raining", key, 2)

	res := contracts.Result{MemoryOps: []contracts.MemoryOp{
		{Kind: contracts.MemoryOpPruneLearning, PatternKey: key},
	}}
	applyResult(ctx, store, curated, res, nil)

	got, ok, err := store.LearningByPatternKey(ctx, key)
	if err != nil || !ok {
		t.Fatalf("read learning back: ok=%v err=%v", ok, err)
	}
	if got.Status != memory.LearningStatusPruned {
		t.Errorf("status = %q, want %q", got.Status, memory.LearningStatusPruned)
	}
	if facts, _ := curated.ReadLearnings(); facts != "" {
		t.Errorf("prune wrote to curated markdown %q, want nothing", facts)
	}
}

// TestApplyResult_FailedTurnNoChange proves a failed dream changes nothing (AD-8).
func TestApplyResult_FailedTurnNoChange(t *testing.T) {
	ctx := context.Background()
	store, curated, _ := newStores(t)

	const key = "style.terse"
	applyN(t, store, "obs", key, 3)

	// A non-nil err with (ignored) ops must apply nothing.
	applyResult(ctx, store, curated, contracts.Result{MemoryOps: []contracts.MemoryOp{
		{Kind: contracts.MemoryOpPromoteLearning, PatternKey: key, Observation: "obs"},
	}}, context.DeadlineExceeded)

	got, _, _ := store.LearningByPatternKey(ctx, key)
	if got.Status != memory.LearningStatusPending {
		t.Errorf("status = %q, want still pending after a failed dream", got.Status)
	}
	if facts, _ := curated.ReadLearnings(); facts != "" {
		t.Errorf("failed dream wrote to curated markdown %q, want nothing", facts)
	}
}

// TestSensitiveLaneOff is AC2: the sensitive-classification lane is held OFF and
// asserted off, and after a full dream run NOTHING is routed to a vault/ — no vault
// path is ever created under the curated root.
func TestSensitiveLaneOff(t *testing.T) {
	if sensitiveLaneEnabled {
		t.Fatal("sensitiveLaneEnabled = true, want false until Epic 5 (NFR6/AD-3)")
	}

	ctx := context.Background()
	store, curated, root := newStores(t)
	const key = "style.terse"
	applyN(t, store, "The owner prefers short replies.", key, 4)

	applyResult(ctx, store, curated, contracts.Result{MemoryOps: []contracts.MemoryOp{
		{Kind: contracts.MemoryOpPromoteLearning, PatternKey: key, Observation: "The owner prefers short replies."},
	}}, nil)

	if _, err := os.Stat(filepath.Join(root, "vault")); !os.IsNotExist(err) {
		t.Fatalf("a vault/ path exists under the curated root after a dream (err=%v); nothing must be routed to a vault", err)
	}
}

// TestNewJob_RegistersAsScheduler proves the dream registers as a plain
// scheduler.Job named "dream" (Yui's condition — no scheduler-loop change).
func TestNewJob_RegistersAsScheduler(t *testing.T) {
	store, curated, _ := newStores(t)
	w := &dreamWorker{}
	arb := arbiter.New(w, time.Minute)
	job := NewJob(arb, store, curated, turntier.NewBudget(2), turntier.ACPower{},
		func() time.Duration { return time.Hour }, time.Hour)
	if job.Name != "dream" {
		t.Errorf("job.Name = %q, want \"dream\"", job.Name)
	}
}

// TestDream_FullCycleThroughOnResult drives the dream through the real NewJob
// wiring — job.Run fires the gates, calls Build, submits through the arbiter, and
// invokes OnResult — so a bug in the turntier.Config OnResult/Build wiring would be
// caught (not just applyResult in isolation). It proves BOTH the promote and prune
// paths end-to-end through build(): a recurring candidate is promoted into sqlite +
// curated markdown, and another candidate the worker prunes is marked pruned.
func TestDream_FullCycleThroughOnResult(t *testing.T) {
	ctx := context.Background()
	store, curated, _ := newStores(t)

	const promoteKey, promoteObs = "style.terse", "The owner prefers short replies."
	const pruneKey = "topic.weather"
	applyN(t, store, promoteObs, promoteKey, 4)
	applyN(t, store, "owner said it rained", pruneKey, 3)

	w := &dreamWorker{ops: []contracts.MemoryOp{
		{Kind: contracts.MemoryOpPromoteLearning, PatternKey: promoteKey, Observation: promoteObs},
		{Kind: contracts.MemoryOpPruneLearning, PatternKey: pruneKey},
	}}
	arb := arbiter.New(w, time.Minute)

	// cooldown 0 so the first tick fires immediately; budget 2 covers one dream.
	job := NewJob(arb, store, curated, turntier.NewBudget(2), turntier.ACPower{},
		func() time.Duration { return time.Hour }, 0)
	job.Run(ctx) // gates → Build → arbiter.Submit → OnResult → applyResult

	// Build fed BOTH recurring candidates to the worker.
	if !strings.Contains(w.gotInput, promoteKey) || !strings.Contains(w.gotInput, pruneKey) {
		t.Fatalf("dream input missing a candidate: %q", w.gotInput)
	}
	// OnResult applied the promote (sqlite + curated) and the prune (sqlite).
	if got, _, _ := store.LearningByPatternKey(ctx, promoteKey); got.Status != memory.LearningStatusPromoted {
		t.Errorf("promote: status = %q, want promoted", got.Status)
	}
	if got, _, _ := store.LearningByPatternKey(ctx, pruneKey); got.Status != memory.LearningStatusPruned {
		t.Errorf("prune: status = %q, want pruned", got.Status)
	}
	if facts, _ := curated.ReadLearnings(); !strings.Contains(facts, promoteObs) {
		t.Errorf("curated learnings %q missing the promoted observation", facts)
	}
}

// TestApplyResult_HallucinatedKeyWritesNoFact proves a promote op for a pattern_key
// with no backing row (an LLM hallucination) writes NOTHING to curated markdown —
// PromoteLearning returns ErrLearningNotFound, so applyResult skips the AppendFact.
func TestApplyResult_HallucinatedKeyWritesNoFact(t *testing.T) {
	ctx := context.Background()
	store, curated, _ := newStores(t)

	applyResult(ctx, store, curated, contracts.Result{MemoryOps: []contracts.MemoryOp{
		{Kind: contracts.MemoryOpPromoteLearning, PatternKey: "ghost", Observation: "should not be written"},
	}}, nil)

	if facts, _ := curated.ReadLearnings(); facts != "" {
		t.Errorf("a hallucinated promote wrote curated markdown %q, want nothing", facts)
	}
}

// TestApplyResult_AppendFailRevertsPromotion proves the Epic-4 data-integrity fix:
// when the curated-markdown append fails after the DB row was marked promoted, the
// promotion is compensated back to pending — never left "promoted" with no backing
// fact (so the next dream retries and writes no duplicate).
func TestApplyResult_AppendFailRevertsPromotion(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("cannot force a write-permission failure as root; skipping")
	}
	ctx := context.Background()
	store, curated, root := newStores(t)

	if err := store.ApplyLearning(ctx, "durable obs", "pk"); err != nil {
		t.Fatalf("seed learning: %v", err)
	}
	if err := store.PromoteLearning(ctx, "pk"); err == nil {
		// Reset to pending so applyResult performs the promote itself (mirrors the
		// real flow where the dream proposes the promote on a pending candidate).
		if err := store.RevertLearningToPending(ctx, "pk"); err != nil {
			t.Fatalf("reset to pending: %v", err)
		}
	}

	// Force AppendFact to fail: make the facts dir unwritable so WriteAtomic cannot
	// create its temp file. The vault-style 0500 keeps read/traverse (for the
	// post-assertion read) but denies the write.
	factsDir := filepath.Join(root, "facts")
	if err := os.Chmod(factsDir, 0o500); err != nil {
		t.Fatalf("chmod facts read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(factsDir, 0o755) }) // let t.TempDir cleanup remove it

	applyResult(ctx, store, curated, contracts.Result{MemoryOps: []contracts.MemoryOp{
		{Kind: contracts.MemoryOpPromoteLearning, PatternKey: "pk", Observation: "durable obs"},
	}}, nil)

	// The append failed → the promotion must have been reverted to pending.
	got, ok, err := store.LearningByPatternKey(ctx, "pk")
	if err != nil || !ok {
		t.Fatalf("lookup pk: ok=%v err=%v", ok, err)
	}
	if got.Status != memory.LearningStatusPending {
		t.Errorf("status = %q after failed append, want pending (promotion must be reverted)", got.Status)
	}
	// And no fact was written.
	if facts, _ := curated.ReadLearnings(); facts != "" {
		t.Errorf("a fact was written despite the append failing: %q", facts)
	}
}
