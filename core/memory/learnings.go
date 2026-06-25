package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/elliotboney/shelldon_go/contracts"
)

// Learning is one recorded observation in the FR11 hot-path capture_learning
// store. A learning is proposed by the worker and applied by core as the sole
// writer (AD-6). PatternKey, when set, dedups repeated observations and bumps
// RecurrenceCount; an empty PatternKey maps to SQL NULL, so unkeyed notes never
// collide. Status stays LearningStatusPending until the dream cycle (Story 4.4)
// promotes or prunes it.
type Learning struct {
	ID              int64
	PatternKey      string
	Observation     string
	RecurrenceCount int
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// LearningStatusPending is the status a freshly captured learning carries until
// the dream cycle reviews it.
const LearningStatusPending = "pending"

// LearningStatusPromoted is the status a learning receives when the dream cycle
// promotes it as a durable fact.
const LearningStatusPromoted = "promoted"

// LearningStatusPruned is the status a learning receives when the dream cycle
// decides it is noise and prunes it.
const LearningStatusPruned = "pruned"

// ApplyLearning records observation under patternKey as the single-writer dedup
// apply (AD-6). A new keyed observation inserts a pending row at count 1; a repeat
// of the same patternKey increments RecurrenceCount and overwrites the observation
// with the latest text via one atomic UPSERT, so concurrent applies can't lose an
// increment. An empty patternKey maps to SQL NULL — which never conflicts under the
// UNIQUE index — so every unkeyed apply inserts a fresh row.
//
// The conflict update deliberately leaves status untouched: a learning the dream
// cycle already promoted or pruned keeps that decision when re-observed on the hot
// path. Resetting it to pending would re-queue a promoted fact for duplicate
// promotion (or revive pruned noise) — the Epic-4 data-integrity bug this avoids.
func (s *Store) ApplyLearning(ctx context.Context, observation, patternKey string) error {
	key := sql.NullString{String: patternKey, Valid: patternKey != ""}
	now := time.Now().UnixNano()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO learnings (pattern_key, observation, recurrence_count, status, created_at, updated_at)
		      VALUES (?, ?, 1, 'pending', ?, ?)
		 ON CONFLICT(pattern_key) DO UPDATE SET
		      recurrence_count = recurrence_count + 1,
		      observation      = excluded.observation,
		      updated_at       = excluded.updated_at`,
		key, observation, now, now)
	if err != nil {
		return fmt.Errorf("memory: apply learning: %w", err)
	}
	return nil
}

// ApplyMemoryOps is core's entry point for applying the worker's proposed memory
// mutations (AD-6). It applies each op in order and returns the first error. An op
// kind it does not recognize is skipped with no error, so the worker's vocabulary
// can grow ahead of core without breaking applies.
func (s *Store) ApplyMemoryOps(ctx context.Context, ops []contracts.MemoryOp) error {
	for _, op := range ops {
		switch op.Kind {
		case contracts.MemoryOpCaptureLearning:
			if err := s.ApplyLearning(ctx, op.Observation, op.PatternKey); err != nil {
				return err
			}
		default:
			// Unknown op kind: skip silently — forward-compatible vocabulary.
		}
	}
	return nil
}

// LearningByPatternKey returns the learning recorded under patternKey. The bool is
// false (with a zero Learning and nil error) when no such key exists.
func (s *Store) LearningByPatternKey(ctx context.Context, patternKey string) (Learning, bool, error) {
	var (
		l                  Learning
		key                sql.NullString
		createdAt, updated int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, pattern_key, observation, recurrence_count, status, created_at, updated_at
		   FROM learnings
		  WHERE pattern_key = ?`, patternKey).
		Scan(&l.ID, &key, &l.Observation, &l.RecurrenceCount, &l.Status, &createdAt, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Learning{}, false, nil
	}
	if err != nil {
		return Learning{}, false, fmt.Errorf("memory: learning by pattern key: %w", err)
	}
	l.PatternKey = key.String
	l.CreatedAt = time.Unix(0, createdAt)
	l.UpdatedAt = time.Unix(0, updated)
	return l, true, nil
}

// ErrLearningNotFound is returned by PromoteLearning/PruneLearning when no learning
// matches the given pattern_key. The dream worker proposes ops from an LLM, which
// can hallucinate a pattern_key no row backs; surfacing this lets core (the dream's
// OnResult) skip the curated write instead of recording a fact with no backing DB
// promotion (AD-6).
var ErrLearningNotFound = errors.New("memory: no learning for pattern key")

// PromoteLearning sets the status of the learning with patternKey to "promoted",
// returning ErrLearningNotFound if no row matches.
func (s *Store) PromoteLearning(ctx context.Context, patternKey string) error {
	return s.setLearningStatus(ctx, patternKey, LearningStatusPromoted)
}

// PruneLearning sets the status of the learning with patternKey to "pruned",
// returning ErrLearningNotFound if no row matches.
func (s *Store) PruneLearning(ctx context.Context, patternKey string) error {
	return s.setLearningStatus(ctx, patternKey, LearningStatusPruned)
}

// RevertLearningToPending sets a learning's status back to "pending", returning
// ErrLearningNotFound if no row matches. It is the compensating action for a
// half-applied promotion: if the curated-markdown fact write fails after the DB
// row was marked promoted, the dream reverts the row so the next cycle retries —
// never leaving a "promoted" row with no backing fact (Epic-4 data-integrity).
func (s *Store) RevertLearningToPending(ctx context.Context, patternKey string) error {
	return s.setLearningStatus(ctx, patternKey, LearningStatusPending)
}

// setLearningStatus updates a learning's status (+ updated_at) by pattern_key. It
// returns ErrLearningNotFound when the UPDATE matched no row, so a hallucinated key
// is rejected rather than silently no-op'd.
func (s *Store) setLearningStatus(ctx context.Context, patternKey, status string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE learnings SET status = ?, updated_at = ? WHERE pattern_key = ?`,
		status, time.Now().UnixNano(), patternKey)
	if err != nil {
		return fmt.Errorf("memory: set learning status %q: %w", status, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("memory: set learning status %q rows affected: %w", status, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %q", ErrLearningNotFound, patternKey)
	}
	return nil
}

// Learnings returns up to n learnings with the given status, most-recently-updated
// first. No matches yields an empty slice, not an error.
func (s *Store) Learnings(ctx context.Context, status string, n int) ([]Learning, error) {
	if n <= 0 {
		return []Learning{}, nil // SQLite reads LIMIT < 0 as "no limit"; a non-positive cap returns nothing
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, pattern_key, observation, recurrence_count, status, created_at, updated_at
		   FROM learnings
		  WHERE status = ?
		  ORDER BY updated_at DESC
		  LIMIT ?`, status, n)
	if err != nil {
		return nil, fmt.Errorf("memory: learnings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	learnings := []Learning{}
	for rows.Next() {
		var (
			l                  Learning
			key                sql.NullString
			createdAt, updated int64
		)
		if err := rows.Scan(&l.ID, &key, &l.Observation, &l.RecurrenceCount, &l.Status, &createdAt, &updated); err != nil {
			return nil, fmt.Errorf("memory: scan learning: %w", err)
		}
		l.PatternKey = key.String
		l.CreatedAt = time.Unix(0, createdAt)
		l.UpdatedAt = time.Unix(0, updated)
		learnings = append(learnings, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: learnings rows: %w", err)
	}
	return learnings, nil
}
