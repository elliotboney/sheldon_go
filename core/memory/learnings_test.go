package memory_test

import (
	"context"
	"sync"
	"testing"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/memory"
)

// TestApplyLearning_InsertThenDedup is the FR11 dedup core: a first keyed apply
// inserts a pending row at count 1; a second apply with the same pattern key does
// not insert a new row but increments the count and overwrites the observation
// with the latest text, keeping status pending.
func TestApplyLearning_InsertThenDedup(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if err := s.ApplyLearning(ctx, "obs A", "pk1"); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	got, ok, err := s.LearningByPatternKey(ctx, "pk1")
	if err != nil {
		t.Fatalf("lookup pk1: %v", err)
	}
	if !ok {
		t.Fatalf("learning pk1 not found after insert")
	}
	if got.RecurrenceCount != 1 {
		t.Errorf("recurrence_count = %d, want 1 after first apply", got.RecurrenceCount)
	}
	if got.Status != memory.LearningStatusPending {
		t.Errorf("status = %q, want %q", got.Status, memory.LearningStatusPending)
	}
	if got.Observation != "obs A" {
		t.Errorf("observation = %q, want %q", got.Observation, "obs A")
	}

	if err := s.ApplyLearning(ctx, "obs B", "pk1"); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	got, ok, err = s.LearningByPatternKey(ctx, "pk1")
	if err != nil {
		t.Fatalf("lookup pk1 again: %v", err)
	}
	if !ok {
		t.Fatalf("learning pk1 not found after dedup")
	}
	if got.RecurrenceCount != 2 {
		t.Errorf("recurrence_count = %d, want 2 after dedup", got.RecurrenceCount)
	}
	if got.Observation != "obs B" {
		t.Errorf("observation = %q, want %q (latest wins)", got.Observation, "obs B")
	}
	if got.Status != memory.LearningStatusPending {
		t.Errorf("status = %q, want %q after dedup", got.Status, memory.LearningStatusPending)
	}
}

// TestApplyLearning_ConcurrentNoLostIncrements is the headline race test: 50
// goroutines apply the same pattern key at once; because core is the single writer
// over one pinned connection and the increment is one atomic UPSERT, the final
// recurrence_count must be exactly 50 with no lost updates. Must pass under -race.
func TestApplyLearning_ConcurrentNoLostIncrements(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if err := s.ApplyLearning(ctx, "x", "pkRace"); err != nil {
				t.Errorf("concurrent apply: %v", err)
			}
		}()
	}
	wg.Wait()

	got, ok, err := s.LearningByPatternKey(ctx, "pkRace")
	if err != nil {
		t.Fatalf("lookup pkRace: %v", err)
	}
	if !ok {
		t.Fatalf("learning pkRace not found")
	}
	if got.RecurrenceCount != n {
		t.Errorf("recurrence_count = %d, want %d (no lost increments)", got.RecurrenceCount, n)
	}
}

// TestApplyLearning_UnkeyedAlwaysNewRow proves an empty pattern key never dedups:
// SQLite treats NULLs as distinct under the UNIQUE index, so each unkeyed apply
// inserts a fresh row.
func TestApplyLearning_UnkeyedAlwaysNewRow(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if err := s.ApplyLearning(ctx, "note", ""); err != nil {
		t.Fatalf("first unkeyed apply: %v", err)
	}
	if err := s.ApplyLearning(ctx, "note", ""); err != nil {
		t.Fatalf("second unkeyed apply: %v", err)
	}

	got, err := s.Learnings(ctx, memory.LearningStatusPending, 10)
	if err != nil {
		t.Fatalf("list learnings: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("unkeyed learnings = %d rows, want 2 distinct (no dedup)", len(got))
	}
}

// TestApplyMemoryOps_CaptureLearning applies the worker's proposal contract: a
// capture_learning op records a learning, and an unknown op kind is skipped with
// no error and writes nothing (forward-compatible vocabulary, AD-6).
func TestApplyMemoryOps_CaptureLearning(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	ops := []contracts.MemoryOp{{
		Kind:        contracts.MemoryOpCaptureLearning,
		Observation: "from worker",
		PatternKey:  "pk2",
	}}
	if err := s.ApplyMemoryOps(ctx, ops); err != nil {
		t.Fatalf("apply ops: %v", err)
	}
	got, ok, err := s.LearningByPatternKey(ctx, "pk2")
	if err != nil {
		t.Fatalf("lookup pk2: %v", err)
	}
	if !ok {
		t.Fatalf("learning pk2 not found after capture_learning op")
	}
	if got.RecurrenceCount != 1 {
		t.Errorf("recurrence_count = %d, want 1", got.RecurrenceCount)
	}
	if got.Observation != "from worker" {
		t.Errorf("observation = %q, want %q", got.Observation, "from worker")
	}

	// An unknown op kind must be skipped silently, creating no learning.
	if err := s.ApplyMemoryOps(ctx, []contracts.MemoryOp{{Kind: "remember", Observation: "ignored", PatternKey: "pk3"}}); err != nil {
		t.Fatalf("unknown op kind errored, want skip: %v", err)
	}
	if _, ok, err := s.LearningByPatternKey(ctx, "pk3"); err != nil {
		t.Fatalf("lookup pk3: %v", err)
	} else if ok {
		t.Fatalf("unknown op kind created a learning, want none")
	}
}

// TestLearningByPatternKey_NotFound returns (zero, false, nil) for an absent key.
func TestLearningByPatternKey_NotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	got, ok, err := s.LearningByPatternKey(ctx, "missing")
	if err != nil {
		t.Fatalf("lookup missing errored: %v", err)
	}
	if ok {
		t.Fatalf("lookup missing = found, want not found")
	}
	if got.ID != 0 {
		t.Fatalf("lookup missing = %+v, want zero Learning", got)
	}
}

// TestLearningsAndMessagesIndependent: the learnings and messages tables are
// independent — appending a conversation message creates no learning, and applying
// a learning creates no message.
func TestLearningsAndMessagesIndependent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.Append(ctx, "c1", "owner", "just chatting"); err != nil {
		t.Fatalf("append: %v", err)
	}
	learnings, err := s.Learnings(ctx, memory.LearningStatusPending, 10)
	if err != nil {
		t.Fatalf("list learnings: %v", err)
	}
	if len(learnings) != 0 {
		t.Fatalf("append created %d learnings, want 0", len(learnings))
	}

	if err := s.ApplyLearning(ctx, "a learning", "pkX"); err != nil {
		t.Fatalf("apply learning: %v", err)
	}
	msgs, err := s.Recent(ctx, "c1", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "just chatting" {
		t.Fatalf("recent = %+v, want only the one chat message (apply learning added none)", msgs)
	}
}

// TestListQueries_NonPositiveLimitReturnsEmpty guards the SQLite "LIMIT < 0 means
// no limit" footgun: a non-positive n must return an empty slice, never the whole
// table. Covers the three list queries in core/memory (the dream cycle, Story 4.4,
// will pass a computed n to Learnings).
func TestListQueries_NonPositiveLimitReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	for _, text := range []string{"one", "two", "three"} {
		if _, err := s.Append(ctx, "c1", "owner", text); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := s.ApplyLearning(ctx, "a learning", "pk1"); err != nil {
		t.Fatalf("apply learning: %v", err)
	}

	for _, n := range []int{0, -1, -100} {
		if got, err := s.Recent(ctx, "c1", n); err != nil || len(got) != 0 {
			t.Errorf("Recent(n=%d) = %d msgs, %v; want 0, nil", n, len(got), err)
		}
		if got, err := s.Search(ctx, "c1", "one", n); err != nil || len(got) != 0 {
			t.Errorf("Search(n=%d) = %d msgs, %v; want 0, nil", n, len(got), err)
		}
		if got, err := s.Learnings(ctx, memory.LearningStatusPending, n); err != nil || len(got) != 0 {
			t.Errorf("Learnings(n=%d) = %d rows, %v; want 0, nil", n, len(got), err)
		}
	}
}
