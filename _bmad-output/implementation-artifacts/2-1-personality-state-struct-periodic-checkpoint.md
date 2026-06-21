---
baseline_commit: da39803
---

# Story 2.1: Personality-state struct + periodic checkpoint

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As Elliot (the owner),
I want the pet's mood/energy/last-interaction state to live in RAM and checkpoint to one small file on a cadence,
so that the pet has continuity across restarts without wearing the SD card (FR2, AD-16, NFR11).

## Context

**First story of Epic 2 (M1 — "The Soul").** Epic 1 built the spine: contracts, the bus hub, the worker seam + arbiter, the suture supervisor root, the CLI transport, and the on-Pi proof. Epic 2 makes the pet *feel alive offline, with zero LLM credit* — reflexes (blink, idle, mood-drift), the reflex-tier scheduler, the terminal face, and offline acknowledgement.

This story lays the **foundation every other Epic 2 reflex stands on**: the in-RAM personality-state struct that core owns and mutates, plus its periodic checkpoint to one small file so the pet's mood/energy survive a restart. Story 2.3 (idle reflex) reads `LastInteraction`; Story 2.4 (mood-drift) mutates `Mood` and re-checkpoints; Story 2.5 (reflex scheduler) will likely absorb the checkpoint cadence as a registered reflex job. Build the struct + Store + checkpoint/restore + a periodic checkpoint loop — nothing more.

This story has **no LLM, no worker, no bus traffic**. It is pure in-core state ownership (AD-6) + RAM-checkpoint durability (AD-16) + SD-wear shaping (NFR11). It is also the **first runtime caller of `core/memory.WriteAtomic`** (built in Story 1.6 as the Epic 4 seed) — reuse it for the atomic checkpoint write; do not reinvent atomic-write logic.

**Scope guardrails — this story does NOT:**
- build any reflex behavior (blink/idle/mood-drift — Stories 2.3/2.4) — only the struct they read/write
- build the scheduler (Story 2.5) — the checkpoint runs on its own minimal ticker loop now; 2.5 may later register it as a reflex job with no struct change
- build the curated markdown tree or sqlite store (Epic 4) — the checkpoint file is **RAM-state persistence, NOT a durable memory layer**, and lives separately
- add the state-patch-over-dotted-paths bus protocol (AD-6) — that is for *worker-proposed* changes (Epic 3+); 2.1's writer is core itself
- add a parent-directory fsync durability step beyond `renameio/v2` (AD-7 defer — atomicity is enough here)

## Acceptance Criteria

1. **Checkpoint on cadence to exactly one file.**
   **Given** the personality-state struct (mood/energy/last-interaction) held in RAM
   **When** the checkpoint cadence elapses (verifiable under `testing/synctest` with the fake clock)
   **Then** the state is written to exactly one checkpoint file (AD-16/NFR11).

2. **Restore from checkpoint on restart.**
   **Given** a checkpoint file on disk
   **When** the process restarts
   **Then** personality-state is restored from the checkpoint, not reset to defaults.

3. **RAM is the working copy; durable layers are never sourced from RAM.**
   **Given** RAM is the working copy
   **When** any durable layer (markdown/sqlite) is read
   **Then** RAM state is never treated as the source of truth for those layers (AD-16). *(In 2.1 the durable layers do not exist yet — this is satisfied by construction: the checkpoint file is RAM-state persistence, kept separate from the Epic 4 memory layers. Encode the separation in package docs and path placement; no test needed.)*

## Tasks / Subtasks

- [x] **Task 1 — `core/state` package: the personality-state struct + Store** (AC: 1, 3)
  - [x] Create `core/state/state.go`. Package doc states core-owned volatile personality-state (AD-16) — RAM, checkpointed to one small file; durable layers (markdown + sqlite, Epic 4) separate, RAM never their source of truth.
  - [x] `type Personality struct` with JSON-tagged `Mood float64` (valence, neutral 0.0), `Energy float64`, `LastInteraction time.Time`. Kept minimal (no `Version` field — deferred as an optional non-breaking add).
  - [x] `type Store struct` holds `Personality` guarded by `sync.RWMutex`; `New(p Personality, path string) *Store` (path injected at construction, matching the dispatch/arbiter pattern), `Snapshot() Personality` (RLock copy), plus `SetMood` / `Touch` mutators for Stories 2.3/2.4.
  - [x] `Default() Personality` → Mood 0.0, Energy 1.0, **`LastInteraction = time.Now()`** so a fresh pet is not idle-since-epoch. Numbers commented as tunable story-time config.

- [x] **Task 2 — Checkpoint write + restore** (AC: 1, 2)
  - [x] Create `core/state/checkpoint.go`. `Personality` marshaled to **JSON** via stdlib `encoding/json` (`MarshalIndent`); no new dependency (NFR2 holds). Written with `memory.WriteAtomic` — first runtime caller of the Story 1.6 helper.
  - [x] `(*Store) Checkpoint() error` — snapshot, marshal, `WriteAtomic` to `s.path`.
  - [x] `Load(path string) (Personality, error)` — missing file (`fs.ErrNotExist`) → `Default()`, no error; corrupt/unparseable → `Default()` + `slog.Warn`, no error (NFR10 graceful degradation).

- [x] **Task 3 — Periodic checkpoint loop (supervised)** (AC: 1)
  - [x] `(*Store) RunCheckpointLoop(ctx context.Context) error` — `time.NewTicker(checkpointInterval)` loop, `Checkpoint()` each tick (log+continue on error), one final `Checkpoint()` on `ctx.Done()` then `return ctx.Err()`. Matches the `Guard`/`Serve(ctx)` shape.
  - [x] `const checkpointInterval = 60 * time.Second`, commented as a tunable. Real `time.Ticker`, no clock interface — `testing/synctest` fakes time for the cadence test.
  - [x] Wired into `cmd/shelldon/main.go`: `os.UserHomeDir()` + `os.MkdirAll(~/.shelldon, 0o755)`, checkpoint path `~/.shelldon/state.json` (outside the Epic 4 durable layers), `Load` at startup, `state.New(loaded, statePath)`, and `supervisor.Guard("state-checkpoint", store.RunCheckpointLoop)` registered **first** in start order (drains last → shutdown flush after other edges stop).

- [x] **Task 4 — Tests (`testing/synctest`, stdlib, no testify)** (AC: 1, 2)
  - [x] `core/state/checkpoint_test.go`. **Cadence (AC1):** inside `synctest.Test(...)`, `go store.RunCheckpointLoop(ctx)`, `time.Sleep(checkpointInterval + 1s)`, `synctest.Wait()`, assert exactly one `state.json` and JSON round-trips to the live state. `assertOnlyFile` mirrors `core/memory/atomic_test.go`.
  - [x] **Restore (AC2):** `Checkpoint` a non-default `Personality`, `Load` it, assert equal + not `Default()`.
  - [x] **Missing file:** `Load` nonexistent → `Default()`, no error.
  - [x] **Corrupt file:** garbage bytes → `Load` → `Default()`, no panic.
  - [x] `go test -race ./core/state/` passes (4 tests); native + arm64 `CGO_ENABLED=0` builds succeed; `golangci-lint run` → 0 issues (`.golangci.yml` unchanged).

### Review Findings

- [x] [Review][Decision→Patch] I/O error path in `Load` — resolved: all read/parse errors now degrade gracefully to `Default()` + `slog.Warn` (NFR10); `Load` signature simplified to `Personality` (no error return). `TestLoad_IOErrorReturnsDefaults` added. [core/state/checkpoint.go]
- [x] [Review][Patch] Zero-time `LastInteraction` not remediated on load — fixed: after successful JSON unmarshal, `if p.LastInteraction.IsZero() { p.LastInteraction = time.Now() }`. `TestLoad_ZeroTimeLastInteractionIsRemediated` added. [core/state/checkpoint.go:38-40]
- [x] [Review][Defer] `Store.path` not validated in `New` — empty-string path fails at first checkpoint rather than at construction [core/state/state.go:51] — deferred, pre-existing
- [x] [Review][Defer] `assertOnlyFile` test helper duplicated from `core/memory/atomic_test.go` [core/state/checkpoint_test.go:15] — deferred, pre-existing
- [x] [Review][Defer] Float64 `!=` comparison in test assertions fragile for future arithmetic on Mood/Energy [core/state/checkpoint_test.go:42] — deferred, pre-existing
- [x] [Review][Defer] No bounds/range enforcement in `SetMood` — NaN/Inf writes null to JSON checkpoint, silently loses state [core/state/state.go:62] — deferred, pre-existing
- [x] [Review][Defer] Double write possible when `ticker.C` and `ctx.Done()` both ready at shutdown (benign extra SD write) [core/state/checkpoint.go:62-71] — deferred, pre-existing
- [x] [Review][Defer] `Store.path` empty-string accepted in `New` [core/state/state.go:51] — deferred, pre-existing

## Dev Notes

### Architecture constraints (binding)

- **AD-16 — Volatile state in RAM, checkpointed.** "the personality-state struct and the working window live in **RAM**, checkpointed periodically to **one small file**; the durable layers remain markdown (curated) + sqlite (history) per AD-7. RAM state is **never the source of truth** for either durable layer." This is the core of the story: the checkpoint is RAM-persistence, distinct from the Epic 4 durable layers. [Source: ARCHITECTURE-SPINE.md#AD-16]
- **AD-6 — Core is the sole writer of state and memory.** "only `core` mutates personality-state… Reflexes mutate state in-core." In 2.1 the writer is core itself (the checkpoint loop reads; later reflexes write). The worker-proposed state-patch protocol (sparse patches over fixed dotted paths) is **not** in scope. [Source: ARCHITECTURE-SPINE.md#AD-6]
- **NFR11 — SD-card write wear.** "high-frequency state stays in **RAM checkpointed to one file**… markdown writes are atomic (temp + fsync + rename… via `renameio/v2`)." The checkpoint write reuses `core/memory.WriteAtomic` (atomic + SD-wear-shaped). The cadence (one write per interval, not per state change) is what bounds wear. [Source: ARCHITECTURE-SPINE.md#NFR11, Consistency Conventions/State]
- **AD-10 — Tests from M0; synctest for cadence; clock as a seam, no monkeypatch.** "Use `testing/synctest` for deterministic scheduler-cadence tests; narrow interfaces over every external seam (SPI/GPIO/LLM/clock) wired by constructor injection (no monkeypatch)." For an **in-core** ticker cadence, `testing/synctest` (Go 1.25 stable) is the prescribed tool — it fakes time for the bubble, so no clock interface is needed here (the clock-as-seam applies to external edges like the worker/LLM). [Source: ARCHITECTURE-SPINE.md#AD-10]
- **NFR2 — single static pure-Go binary.** JSON via stdlib `encoding/json` adds no dependency; the arm64 `CGO_ENABLED=0` cross-compile must stay green. No third external dep this story (suture/v4 + renameio/v2 remain the only two). [Source: ARCHITECTURE-SPINE.md#NFR2]
- **Structural Seed — package placement.** `core/ … state/` is a named seed package; runtime memory lives **outside source** at `~/.shelldon` (curated md tree + `history.db`). The checkpoint file is `~/.shelldon/state.json` — a sibling of, not inside, those durable layers. [Source: ARCHITECTURE-SPINE.md#Structural Seed]

### Format decision (JSON) and the WriteAtomic reuse

- **JSON, not gob.** `gob` is reserved for the bus/worker-wall transport (AD-4). The checkpoint is a small, single, human-debuggable file — stdlib `encoding/json` is the simpler fit (debuggable on the Pi, no dep). If you later need a versioned/compact format it is a non-breaking swap behind `Checkpoint`/`Load`.
- **Reuse `core/memory.WriteAtomic` — do not reinvent.** It already wraps `renameio.WriteFile` (temp→fsync→atomic-rename) and is crash-safe-tested (Story 1.6). `core/state` importing `core/memory` is in-core→in-core (LLM-free, NFR3 holds). This makes 2.1 the helper's first runtime caller — the memory package doc currently says "Nothing in M0 calls WriteAtomic yet"; that line may be updated when you wire the caller (optional, surgical).

### Previous story intelligence (Stories 1.1–1.6)

- **Conventions to mirror:** package doc comment on the primary file (`atomic.go`, `arbiter.go`, `supervisor.go` all lead with one); small files per type; table-free **stdlib `testing`**, no `testify`; `t.Helper()` helpers (`assertOnlyFile`); `t.TempDir()` for filesystem tests (auto-cleaned). [Source: core/memory/atomic_test.go, core/supervisor/supervisor_test.go]
- **Supervised-loop shape:** `dispatch.Serve(ctx) error` and the `Guard("name", fn)` pattern in `cmd/shelldon/main.go` are the template for `RunCheckpointLoop` — block on work, return `ctx.Err()` on `ctx.Done()`, get wrapped by `supervisor.Guard` (which supplies the mandatory `defer recover()`, AD-5). [Source: core/supervisor/supervisor.go:84, cmd/shelldon/main.go:44]
- **Start/drain order matters:** `main` adds edges in start order; reverse-drain stops last-added first. Add `state-checkpoint` first so it drains last and its shutdown flush runs after dispatch/CLI have stopped. [Source: cmd/shelldon/main.go:45, core/supervisor/supervisor.go:64]
- **renameio/v2 v2.0.2 is already a dependency** (Story 1.6); `WriteAtomic(path, data, perm)` is the public surface. No new `go get` this story.
- **Go 1.25** (`go.mod`) — `testing/synctest` is stable (no `GOEXPERIMENT` flag needed). No synctest usage exists in the repo yet; this story introduces it. [Source: go.mod]

### testing/synctest usage (Go 1.25)

- `synctest.Test(t, func(t *testing.T){ … })` runs the body in a bubble with a fake clock; time auto-advances when all bubble goroutines are durably blocked. `synctest.Wait()` blocks until all *other* bubble goroutines are durably blocked — use it after a `time.Sleep` to ensure the ticked checkpoint write has completed before asserting on the file.
- Real file I/O is allowed inside the bubble (only time and goroutine scheduling are virtualized). The checkpoint goroutine does a quick synchronous write then returns to blocking on the ticker — `synctest.Wait()` handles the brief running window.

### Project Structure Notes

- New: `core/state/` (`state.go`, `checkpoint.go`, `checkpoint_test.go`). Matches the Structural Seed (`core/ … state/`). Epic 2 reflex stories extend this package.
- Modified: `cmd/shelldon/main.go` (load checkpoint at startup, register the `state-checkpoint` supervised loop). Optionally `core/memory/atomic.go` package-doc line (the "no caller yet" note) — surgical, optional.
- `.golangci.yml` unchanged. No new dependency in `go.mod`/`go.sum`.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.1] — ACs, epic goal, cross-story dependencies (2.3 reads LastInteraction, 2.4 mutates Mood, 2.5 scheduler)
- [Source: ...ARCHITECTURE-SPINE.md#AD-16] — volatile state in RAM, checkpointed to one file; RAM never source of truth for durable layers
- [Source: ...ARCHITECTURE-SPINE.md#AD-6] — core sole writer of state; reflexes mutate in-core; state-patch protocol (out of scope here)
- [Source: ...ARCHITECTURE-SPINE.md#NFR11] — SD write-wear; RAM checkpoint to one file; atomic writes via renameio/v2
- [Source: ...ARCHITECTURE-SPINE.md#AD-10] — testing/synctest for cadence; clock seam injected, no monkeypatch
- [Source: ...ARCHITECTURE-SPINE.md#Structural Seed] — core/state/ package; ~/.shelldon runtime memory layout
- [Source: core/memory/atomic.go] — WriteAtomic, the helper to reuse for the checkpoint write
- [Source: cmd/shelldon/main.go, core/supervisor/supervisor.go] — Guard + Serve(ctx) supervised-loop pattern, start/drain order

## Dev Agent Record

### Agent Model Used

claude-opus-4-8[1m] (Claude Opus 4.8, 1M context) via Claude Code dev-story workflow.

### Debug Log References

- TDD: wrote `core/state/checkpoint_test.go` first (RED — `Personality`/`Store`/`Load` undefined, build failed), then `state.go` + `checkpoint.go` to GREEN. 4 tests pass under `-race`.
- `testing/synctest` (Go 1.25 stable, no GOEXPERIMENT) used for the cadence test — first synctest usage in the repo. Fake clock advances on `time.Sleep(checkpointInterval + 1s)`; `synctest.Wait()` lets the ticked write finish before asserting one file. The loop goroutine is cancelled and joined (`<-done`) before the bubble returns so synctest sees no lingering goroutine.
- `time.Time` round-trip: JSON (RFC3339Nano) preserves wall-clock ns but drops the monotonic reading, so tests compare `LastInteraction` with `.Equal()`, not `==`. Default-state tests assert Mood/Energy + non-zero `LastInteraction` (each `Default()` stamps a fresh `time.Now()`, so the timestamp can't be equality-checked).
- End-to-end manual run (`HOME=$tmp`, piped message, SIGTERM at 2s): CLI round-trip still echoes; the shutdown flush wrote `~/.shelldon/state.json` (defaults, `last_interaction` stamped), no orphaned temp — atomic write clean.

### Completion Notes List

- **AC1 satisfied (checkpoint on cadence → exactly one file).** `RunCheckpointLoop` tickers at `checkpointInterval` (60s, tunable) and writes via `memory.WriteAtomic` (atomic temp→fsync→rename, NFR11/AD-7). `TestRunCheckpointLoop_WritesOneFileOnCadence` proves it under `testing/synctest` — one `state.json`, no temp, contents round-trip.
- **AC2 satisfied (restore on restart).** `Load` reads + unmarshals the checkpoint; `TestLoad_RestoresFromCheckpoint` confirms the restored state equals what was written and differs from `Default()`. `main` calls `Load` at startup before constructing the Store.
- **AC3 satisfied (RAM not source of truth for durable layers).** By construction + documentation: the checkpoint is RAM-state persistence at `~/.shelldon/state.json`, a sibling of (not inside) the Epic 4 durable layers (`~/.shelldon/memory/`, `~/.shelldon/history.db`), which do not exist yet. Encoded in the package doc and path placement; no test (nothing to read).
- **Graceful degradation (NFR10).** Missing checkpoint → `Default()` (first boot); corrupt checkpoint → `Default()` + `slog.Warn`, never a crash. Covered by `TestLoad_MissingFileReturnsDefaults` and `TestLoad_CorruptFileReturnsDefaults`.
- **First runtime caller of `core/memory.WriteAtomic`** (built in Story 1.6 as the Epic 4 seed) — no atomic-write logic reinvented. `core/state` imports only stdlib + `core/memory` (LLM-free, NFR3 holds).
- **No new dependency** — JSON via stdlib `encoding/json`; suture/v4 + renameio/v2 remain the only two external deps. arm64 `CGO_ENABLED=0` cross-compile stays green (NFR2).
- **Scope held:** struct + Store + checkpoint/restore + supervised periodic loop only. No reflex behavior (2.3/2.4), no scheduler (2.5), no markdown/sqlite (Epic 4), no worker-proposed state-patch protocol. `SetMood`/`Touch` are the minimal mutators the next reflex stories will use.
- **Validation:** native + arm64 builds OK; `go test -race -count=1 ./...` → 31 tests, 10 packages, no data race; `golangci-lint run` → 0 issues.

### File List

- `core/state/state.go` (new) — `Personality` struct, `Store` (RWMutex-guarded), `New`/`Snapshot`/`SetMood`/`Touch`/`Default`
- `core/state/checkpoint.go` (new) — `Load`, `(*Store) Checkpoint`, `(*Store) RunCheckpointLoop`, `checkpointInterval`
- `core/state/checkpoint_test.go` (new) — synctest cadence test + restore/missing/corrupt tests (stdlib, no testify)
- `cmd/shelldon/main.go` (modified) — resolve `~/.shelldon`, `Load` checkpoint at startup, register the supervised `state-checkpoint` loop first in start order
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (modified) — story status → in-progress → review

## Change Log

| Date       | Change                                                                 |
|------------|------------------------------------------------------------------------|
| 2026-06-21 | Implemented the `core/state` package: in-RAM `Personality` (mood/energy/last-interaction) held by a RWMutex-guarded `Store`, JSON checkpoint to `~/.shelldon/state.json` via the Story 1.6 `memory.WriteAtomic` helper (first runtime caller), restore-on-restart with graceful defaults on missing/corrupt files, and a supervised periodic checkpoint loop (60s ticker + shutdown flush) wired into `main` as the first-started edge. First `testing/synctest` usage in the repo for the deterministic cadence test. No new dependency; native + arm64 builds green, `-race` suite 31/31 across 10 packages, lint 0 issues (Story 2.1). |
