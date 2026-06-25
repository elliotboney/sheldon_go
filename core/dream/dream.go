// Package dream is the AD-15 dream cycle: a scheduled introspective worker turn
// that reviews pending learnings, promotes the durable/recurring ones into the
// curated markdown tree, and prunes the rest — so a learning the pet keeps
// re-observing becomes durable knowledge that grounds later replies (FR11).
//
// It reuses the machinery; it is not a new subsystem (AD-15). The dream is a
// turn-tier Job (core/turntier) submitted through the arbiter (≤1 in flight, AD-8);
// the worker dreams (one LLM call) and PROPOSES promote/prune memory-ops; core
// (this package's OnResult) APPLIES them — marking the learning promoted/pruned in
// sqlite and appending the promoted observation into curated markdown. The worker
// holds no store handle and never writes (AD-6); it receives the candidate
// learnings via the dream Job.Input, so no new worker→memory seam is needed.
//
// The sensitive-classification lane is OFF until Epic 5 (NFR6/AD-3): while
// sensitiveLaneEnabled is false the dream never classifies or routes anything
// sensitive — promotion always targets facts/about, never vault/ (which does not
// exist yet, and curated.WriteFile rejects regardless). The scheduler loop is
// unchanged: the dream registers as a plain scheduler.Job (Yui's condition, AD-13).
package dream

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/elliotboney/shelldon_go/contracts"
	"github.com/elliotboney/shelldon_go/core/memory"
	"github.com/elliotboney/shelldon_go/core/scheduler"
	"github.com/elliotboney/shelldon_go/core/turntier"
)

// sensitiveLaneEnabled gates routing learnings to the vault/ sensitive lane. It is
// held OFF until Epic 5, when the worker is uid-separated across the process wall
// (NFR6/AD-3). While false, the dream runs no sensitive classification and routes
// nothing to a vault — promotion always targets the curated facts/about tree, and
// curated.WriteFile rejects vault/ regardless. Epic 5 (Story 5-3) turns it on.
// AC2 asserts this stays false.
const sensitiveLaneEnabled = false

// promoteThreshold is the minimum recurrence_count for a pending learning to be
// offered to the dream as a promotion candidate. The model then decides
// promote-vs-prune among the candidates — keeping the dream cheap and the scope
// LIGHT (AD-15): no elaborate taxonomy, promotion by impact + recurrence.
const promoteThreshold = 2

// maxCandidates caps how many pending learnings one dream considers, bounding the
// turn's prompt size and cost (NFR14).
const maxCandidates = 20

// NewJob builds the dream-cycle turn job as a scheduler.Job, ready to register
// alongside the reflex + proactive jobs (no scheduler-loop change). Build reads the
// recurring pending learnings and formats them into a JobDream turn; the arbiter
// submits it to the worker (≤1 in flight); OnResult applies the worker's proposed
// promote/prune ops as the sole writer (AD-6). cadence is how often to consider
// firing; cooldown is the minimum interval between dreams; budget/power are the
// shared turn-tier gates. A nil power defaults to AC power inside turntier.
func NewJob(arb turntier.Submitter, store *memory.Store, curated *memory.Curated, budget *turntier.Budget, power turntier.Power, cadence func() time.Duration, cooldown time.Duration) scheduler.Job {
	return turntier.NewJob(turntier.Config{
		Name:     "dream",
		Cadence:  cadence,
		Cooldown: cooldown,
		Build:    func() contracts.Job { return build(store) },
		Arbiter:  arb,
		Budget:   budget,
		Power:    power,
		OnResult: func(ctx context.Context, res contracts.Result, err error) {
			applyResult(ctx, store, curated, res, err)
		},
	}).Scheduler()
}

// build reads up to maxCandidates pending learnings and formats those at or above
// promoteThreshold into the dream Job.Input, one per line as
// "pattern_key | recurrence_count | observation". The worker reads no store, so the
// candidates ride in via Input. With no candidates the Input is empty — a harmless
// no-op dream (the worker proposes nothing; OnResult applies nothing). A read error
// degrades to an empty dream rather than failing the scheduler (AD-17).
func build(store *memory.Store) contracts.Job {
	learnings, err := store.Learnings(context.Background(), memory.LearningStatusPending, maxCandidates)
	if err != nil {
		slog.Warn("dream: read pending learnings failed; dreaming over nothing this cycle", "err", err)
		return contracts.Job{Kind: contracts.JobDream}
	}
	var b strings.Builder
	for _, l := range learnings {
		if l.RecurrenceCount < promoteThreshold {
			continue
		}
		fmt.Fprintf(&b, "%s | %d | %s\n", l.PatternKey, l.RecurrenceCount, l.Observation)
	}
	return contracts.Job{Kind: contracts.JobDream, Input: b.String()}
}

// applyResult is core's single-writer apply for the worker's proposed dream ops
// (AD-6). A failed dream changes nothing (AD-8). Each promote marks the learning
// promoted in sqlite AND appends its observation into the curated facts tree; each
// prune marks the learning pruned. Per-op errors are logged and skipped so one bad
// op never aborts the rest. Promotion always targets facts/about — the sensitive
// vault lane is unreachable while sensitiveLaneEnabled is false (Epic 5 turns it
// on), and curated.AppendFact rejects vault/ regardless (NFR6/AD-3).
func applyResult(ctx context.Context, store *memory.Store, curated *memory.Curated, res contracts.Result, err error) {
	if err != nil {
		slog.Warn("dream: turn failed; no memory changes this cycle (AD-8)", "err", err)
		return
	}
	for _, op := range res.MemoryOps {
		switch op.Kind {
		case contracts.MemoryOpPromoteLearning:
			if e := store.PromoteLearning(ctx, op.PatternKey); e != nil {
				slog.Warn("dream: promote learning failed", "pattern_key", op.PatternKey, "err", e)
				continue
			}
			// Sensitive lane OFF: the promoted observation always goes to the curated
			// facts tree, never vault/ (Epic 5, NFR6/AD-3). curated.AppendFact rejects
			// vault/ regardless, so this is doubly safe.
			//
			// DB promotion and the curated fact must agree (a learning is "promoted"
			// iff its fact is durable in the tree). If the append fails, compensate by
			// reverting the row to pending so the next dream retries cleanly — the
			// markdown was not written, so the retry appends no duplicate. This avoids a
			// promoted row with no backing fact (Epic-4 data-integrity).
			if e := curated.AppendFact(memory.FactsLearningsPath, op.Observation); e != nil {
				slog.Warn("dream: append promoted fact failed; reverting promotion to pending", "pattern_key", op.PatternKey, "err", e)
				if re := store.RevertLearningToPending(ctx, op.PatternKey); re != nil {
					slog.Error("dream: revert promotion failed; learning is promoted in db but its fact was not written", "pattern_key", op.PatternKey, "err", re)
				}
			}
		case contracts.MemoryOpPruneLearning:
			if e := store.PruneLearning(ctx, op.PatternKey); e != nil {
				slog.Warn("dream: prune learning failed", "pattern_key", op.PatternKey, "err", e)
			}
		default:
			// Unknown op kind: skip silently — forward-compatible vocabulary (AD-6),
			// matching Store.ApplyMemoryOps.
		}
	}
}
