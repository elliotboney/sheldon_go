package state

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/elliotboney/shelldon_go/core/memory"
)

// checkpointInterval is how often the loop flushes RAM state to disk — a tunable
// that bounds checkpoint write frequency (SD-card wear, NFR11), not an invariant.
const checkpointInterval = 60 * time.Second

// checkpointPerm is the mode of the checkpoint file.
const checkpointPerm = 0o644

// Load reads a checkpoint file and returns the restored Personality. Any read or
// parse failure degrades gracefully to Default() with a warning log — a bad
// checkpoint must never kill the soul (NFR10). Missing file (first boot) returns
// Default() silently.
func Load(path string) Personality {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default()
	}
	if err != nil {
		slog.Warn("could not read personality checkpoint; starting from defaults", "path", path, "err", err)
		return Default()
	}
	var p Personality
	if err := json.Unmarshal(data, &p); err != nil {
		slog.Warn("corrupt personality checkpoint; starting from defaults", "path", path, "err", err)
		return Default()
	}
	if p.LastInteraction.IsZero() {
		p.LastInteraction = time.Now()
	}
	return p
}

// Checkpoint writes the current state to the Store's path atomically, reusing the
// Story 1.6 memory.WriteAtomic helper (temp → fsync → rename): a reader sees
// either the prior checkpoint or the fully-written new one, never a torn file.
func (s *Store) Checkpoint() error {
	data, err := json.MarshalIndent(s.Snapshot(), "", "  ")
	if err != nil {
		return err
	}
	return memory.WriteAtomic(s.path, data, checkpointPerm)
}

// RunCheckpointLoop checkpoints every checkpointInterval until ctx is cancelled,
// then performs one final checkpoint to flush the latest state on shutdown. It is
// wrapped by supervisor.Guard at main (AD-5). A write error is logged and the
// loop continues — a transient disk error must not stop checkpointing.
func (s *Store) RunCheckpointLoop(ctx context.Context) error {
	ticker := time.NewTicker(checkpointInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := s.Checkpoint(); err != nil {
				slog.Error("personality checkpoint failed", "path", s.path, "err", err)
			}
		case <-ctx.Done():
			if err := s.Checkpoint(); err != nil {
				slog.Error("final personality checkpoint failed", "path", s.path, "err", err)
			}
			return ctx.Err()
		}
	}
}
