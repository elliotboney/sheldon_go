// Package state holds shelldon's core-owned volatile personality-state (AD-16):
// mood/energy/last-interaction live in RAM and are checkpointed periodically to
// one small file, so the pet keeps continuity across restarts without wearing the
// SD card (NFR11).
//
// core is the single writer (AD-6): reflexes mutate the Store in-core; the
// checkpoint loop only reads a Snapshot. The checkpoint file is RAM-state
// persistence — NOT a durable memory layer. The durable layers (the curated
// markdown tree and sqlite history, Epic 4) live separately and RAM is never
// their source of truth.
package state

import (
	"sync"
	"time"
)

// Personality is the pet's volatile mood/energy/last-interaction state (AD-16).
// Fields are JSON-tagged for the checkpoint file (encoding/json).
type Personality struct {
	Mood            float64   `json:"mood"`             // valence; neutral 0.0
	Energy          float64   `json:"energy"`           // 0.0 drained .. 1.0 full
	LastInteraction time.Time `json:"last_interaction"` // last inbound owner contact
}

// Default returns neutral starting state for a fresh pet with no checkpoint yet.
// LastInteraction is stamped to now so a fresh pet is not treated as idle since
// the epoch by the idle reflex (Story 2.3). Exact mood/energy values are tunable
// story-time config, not invariants.
func Default() Personality {
	return Personality{
		Mood:            0.0,
		Energy:          1.0,
		LastInteraction: time.Now(),
	}
}

// Store holds the personality-state in RAM. core is the single writer (AD-6),
// but the checkpoint loop reads concurrently with future reflex writers, so a
// RWMutex keeps that access race-free. The checkpoint path is injected at
// construction (like the other core edges) so tests target a temp dir.
type Store struct {
	mu   sync.RWMutex
	p    Personality
	path string
}

// New returns a Store seeded with p (typically Load's result at startup) that
// checkpoints to path.
func New(p Personality, path string) *Store {
	return &Store{p: p, path: path}
}

// Snapshot returns a copy of the current state under a read lock — used by the
// checkpoint loop and any reader that must not alias the live struct.
func (s *Store) Snapshot() Personality {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.p
}

// SetMood sets the valence under a write lock (the mood-drift reflex, Story 2.4).
func (s *Store) SetMood(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.p.Mood = v
}

// Touch stamps LastInteraction to now when an owner message arrives (read by the
// idle reflex, Story 2.3).
func (s *Store) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.p.LastInteraction = time.Now()
}
