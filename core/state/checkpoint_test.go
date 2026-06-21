package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

// assertOnlyFile asserts dir contains exactly one entry named name — catching an
// orphaned temp file (atomic-write leftover) as well as the target. Mirrors the
// helper in core/memory/atomic_test.go.
func assertOnlyFile(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != name {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("dir entries = %v, want exactly [%q] (orphaned temp or missing file)", names, name)
	}
}

// sample is a fixed non-default state for deterministic round-trip assertions.
func sample() Personality {
	return Personality{
		Mood:            0.5,
		Energy:          0.3,
		LastInteraction: time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
	}
}

func assertPersonalityEqual(t *testing.T, got, want Personality) {
	t.Helper()
	if got.Mood != want.Mood || got.Energy != want.Energy {
		t.Fatalf("state = %+v, want %+v", got, want)
	}
	if !got.LastInteraction.Equal(want.LastInteraction) {
		t.Fatalf("LastInteraction = %v, want %v", got.LastInteraction, want.LastInteraction)
	}
}

func assertIsDefault(t *testing.T, got Personality) {
	t.Helper()
	d := Default()
	if got.Mood != d.Mood || got.Energy != d.Energy {
		t.Fatalf("state = %+v, want defaults %+v", got, d)
	}
	if got.LastInteraction.IsZero() {
		t.Fatalf("default LastInteraction should be stamped to now, got zero")
	}
}

// TestRunCheckpointLoop_WritesOneFileOnCadence is the AC1 cadence test: under
// testing/synctest's fake clock, after one interval elapses the loop has written
// exactly one checkpoint file whose contents round-trip to the live state.
func TestRunCheckpointLoop_WritesOneFileOnCadence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "state.json")
		store := New(sample(), path)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- store.RunCheckpointLoop(ctx) }()

		// Fake-advance past one interval; the ticker fires once.
		time.Sleep(checkpointInterval + time.Second)
		synctest.Wait() // let the ticked checkpoint write complete

		assertOnlyFile(t, dir, "state.json")
		assertPersonalityEqual(t, Load(path), sample())

		cancel() // triggers the final shutdown checkpoint, then the loop exits
		<-done
	})
}

// TestLoad_RestoresFromCheckpoint is AC2: a checkpoint on disk restores the
// prior state on restart, not the defaults.
func TestLoad_RestoresFromCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store := New(sample(), path)
	if err := store.Checkpoint(); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	got := Load(path)
	assertPersonalityEqual(t, got, sample())
	if got.Energy == Default().Energy {
		t.Fatalf("restored Energy = %v equals default — checkpoint not actually restored", got.Energy)
	}
}

// TestLoad_MissingFileReturnsDefaults: first boot (no checkpoint) yields defaults.
func TestLoad_MissingFileReturnsDefaults(t *testing.T) {
	assertIsDefault(t, Load(filepath.Join(t.TempDir(), "does-not-exist.json")))
}

// TestLoad_CorruptFileReturnsDefaults: a bad checkpoint degrades gracefully to
// defaults (NFR10) rather than crashing the soul.
func TestLoad_CorruptFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	assertIsDefault(t, Load(path))
}

// TestLoad_IOErrorReturnsDefaults: an unreadable file (permissions) degrades
// gracefully to defaults (NFR10) rather than propagating the error (D1 resolution).
func TestLoad_IOErrorReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"mood":0.5,"energy":0.3,"last_interaction":"2026-06-21T12:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	assertIsDefault(t, Load(path))
}

// TestLoad_ZeroTimeLastInteractionIsRemediated: a checkpoint with a zero
// LastInteraction gets stamped to now rather than treating the pet as idle-since-epoch.
func TestLoad_ZeroTimeLastInteractionIsRemediated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"mood":0.5,"energy":0.3,"last_interaction":"0001-01-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	got := Load(path)
	if got.LastInteraction.IsZero() {
		t.Fatalf("zero LastInteraction must be remediated to now, got zero")
	}
	if got.Mood != 0.5 || got.Energy != 0.3 {
		t.Fatalf("mood/energy must be preserved from checkpoint, got %+v", got)
	}
}
