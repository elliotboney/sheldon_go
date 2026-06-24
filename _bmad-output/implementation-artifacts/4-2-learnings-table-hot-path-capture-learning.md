---
baseline_commit: c44041dee26a0487161366843dd5206f6ca9ae85
---

# Story 4.2: learnings table + hot-path capture_learning

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As the system,
I want the worker to propose `capture_learning` on the hot path and core to apply it serially as single writer,
so that learnings dedup on `pattern_key` with no row race (FR11, AD-6, AD-7).

## Context

**Second story of Epic 4 (M2 — "Memory & Dreams").** Story 4.1 built the sqlite conversation store (messages, recency + FTS5). This story adds the **second table in that same db — `learnings`** — and the **proposal→apply path** that makes the pet lightly self-improving (FR11): on a turn the worker may propose `capture_learning(observation, pattern_key?)`, and **core applies it serially as the single writer** (AD-6), deduping on `pattern_key` by incrementing a `recurrence_count`. This is the write-side counterpart to AD-6's "the worker proposes; core decides and writes."

**The proposal channel finally gets a real op.** `contracts.MemoryOp` is currently a `Kind`-only placeholder ("vocabulary defined later"). This story gives it `capture_learning`'s fixed arg schema (`Observation`, `PatternKey`) and the `Kind` constant — the first concrete entry in AD-6's memory-op vocabulary (`remember`/`rewrite_about`/`log_episode`/`capture_learning`); the others come later. The extension is **additive** to the gob-versioned contract (NFR9/AD-10).

**"No row race" = single writer + atomic upsert (the reviewer-gate dedup fix).** Core is the sole writer (AD-6); the store already pins one connection (`SetMaxOpenConns(1)`, Story 4.1), so applies serialize. On top of that, the dedup is a **single atomic `INSERT … ON CONFLICT(pattern_key) DO UPDATE SET recurrence_count = recurrence_count + 1 …`** — so even concurrent proposals (e.g. a reflection turn and a dream turn, later) can't lose an increment. A null/absent `pattern_key` is always a fresh row (SQLite treats NULLs as distinct under UNIQUE), so unkeyed learnings never collide.

**Built in isolation, wired when the worker emits ops — the 4.1 pattern.** The real worker (`monolith`, Story 3.3) returns `Result{Reply}` with **empty `MemoryOps`** — it does not yet emit `capture_learning` (that needs LLM structured-output parsing, a later concern). So this story delivers the **machinery and apply path** (contract op + `learnings` table + `ApplyLearning` dedup + a core `ApplyMemoryOps` applier), proven with a worker/Result that *does* propose, in isolation. Recording into the live dispatch turn path waits for the worker to actually emit ops (a later Epic 4 wiring step bundled with prompt-assembly/structured output). No `dispatch`/`main` change here.

**This story does NOT:**
- make the `monolith` worker emit `capture_learning` from live LLM output — that's structured-output parsing, a later step; here the proposal is exercised by a worker/Result that proposes in tests
- wire the apply path into `dispatch`/`main` — deferred to when the worker emits ops (matches 4.1's "built in isolation, wired later")
- implement the other memory-ops (`remember`/`rewrite_about`/`log_episode`) — only `capture_learning`; the rest are later vocabulary
- promote/prune learnings (the `status` transitions pending→promoted/pruned are the **dream cycle**, Story 4.4) — 4.2 only ever writes `status='pending'`
- touch the curated markdown tree / `DIRECTIVE.md` (Story 4.3), the conversation `messages` table (4.1, unchanged), the broker, the arbiter, the transports, or the scheduler

## Acceptance Criteria

1. **Worker proposes `capture_learning`; core applies serially; `pattern_key` dedups with no race.**
   **Given** the worker proposing `capture_learning(observation, pattern_key?)` in `Result.MemoryOps`
   **When** core applies the proposals serially as the single writer
   **Then** a new `pattern_key` inserts a learning at `recurrence_count = 1, status = 'pending'`, and a **repeated `pattern_key` increments `recurrence_count`** (refreshing `observation` to the latest, status reset to `'pending'`) **with no lost updates under concurrency** (the reviewer-gate dedup fix, AD-6/AD-7) — an unkeyed proposal is always a new row.

## Tasks / Subtasks

- [x] **Task 1 — Give `capture_learning` a real arg schema (`contracts/result.go`)** (AC: 1)
  - [x] Extend `MemoryOp` (currently `{ Kind string }`) with the `capture_learning` args — **additive only** (NFR9/AD-10): add `Observation string` and `PatternKey string` fields. Keep `Kind`. Document that each op kind uses the subset of fields its schema defines (capture_learning uses Observation + optional PatternKey; other kinds add their own fields later).
  - [x] Add `const MemoryOpCaptureLearning = "capture_learning"` (the first concrete entry in AD-6's memory-op vocabulary). Update the `MemoryOp`/`Result` doc comments: the proposal channel now carries real ops; the worker proposes, core applies (AD-6).
  - [x] Confirm the gob round-trip contract test (Story 1.1, `contracts/contracts_test.go`) still passes — the additive fields must not break envelope round-trip.

- [x] **Task 2 — The learnings table + dedup apply (`core/memory/learnings.go`)** (AC: 1)
  - [x] Add the `learnings` table to the `Open` migration (same db as `messages`; extend the existing migration string in `store.go` or run an additional idempotent `CREATE`): `learnings(id INTEGER PRIMARY KEY AUTOINCREMENT, pattern_key TEXT, observation TEXT NOT NULL, recurrence_count INTEGER NOT NULL DEFAULT 1, status TEXT NOT NULL DEFAULT 'pending', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)` + `CREATE UNIQUE INDEX IF NOT EXISTS idx_learnings_pattern_key ON learnings(pattern_key)` (NULLs are distinct, so unkeyed rows never conflict).
  - [x] `type Learning struct { ID int64; PatternKey string; Observation string; RecurrenceCount int; Status string; CreatedAt, UpdatedAt time.Time }`. Define `const LearningStatusPending = "pending"` (promoted/pruned are Story 4.4).
  - [x] `func (s *Store) ApplyLearning(ctx context.Context, observation, patternKey string) error` — the single-writer dedup apply. When `patternKey == ""`, store SQL `NULL` (a `sql.NullString{Valid:false}`) so it's always a fresh row; otherwise an **atomic upsert**: `INSERT INTO learnings(pattern_key, observation, recurrence_count, status, created_at, updated_at) VALUES (?, ?, 1, 'pending', ?, ?) ON CONFLICT(pattern_key) DO UPDATE SET recurrence_count = recurrence_count + 1, observation = excluded.observation, status = 'pending', updated_at = excluded.updated_at`. One statement → no read-modify-write race even before the single-conn serialization (the reviewer-gate fix).
  - [x] `func (s *Store) ApplyMemoryOps(ctx context.Context, ops []contracts.MemoryOp) error` — core's apply entry point: iterate ops, switch on `op.Kind`; for `MemoryOpCaptureLearning` call `ApplyLearning(ctx, op.Observation, op.PatternKey)`. Unknown kinds are skipped (forward-compatible) — or returned as an error; pick skip-with-no-error for M1 so a newer worker proposing a not-yet-implemented op doesn't fail the turn. (Note: this makes `core/memory` import `contracts` — no cycle, contracts imports nothing internal.)
  - [x] A read accessor for tests + the future dream cycle: `func (s *Store) LearningByPatternKey(ctx context.Context, patternKey string) (Learning, bool, error)` (and/or `Learnings(ctx, status string, n int) ([]Learning, error)`). Returns the row + found flag.
  - [x] Doc: this is FR11's hot-path `capture_learning` — proposed by the worker, applied by core as sole writer (AD-6); `status` stays `pending` until the dream cycle (4.4) promotes/prunes.

- [x] **Task 3 — Tests (stdlib `testing`, no testify)** (AC: 1)
  - [x] `core/memory/learnings_test.go`, using the same `t.TempDir()`/`Open` helper as `store_test.go`.
  - [x] **AC1 insert + dedup:** `ApplyLearning(ctx, "obs A", "pk1")` → `LearningByPatternKey("pk1")` has `recurrence_count == 1, status == "pending"`. A second `ApplyLearning(ctx, "obs B", "pk1")` → `recurrence_count == 2`, `observation == "obs B"` (latest), `status == "pending"`.
  - [x] **AC1 no-race (the headline):** fire e.g. 50 concurrent `ApplyLearning(ctx, "x", "pkRace")` goroutines (`sync.WaitGroup`), then assert `recurrence_count == 50` — proving no lost increments (atomic upsert + single writer). Run under `-race`.
  - [x] **Unkeyed = always new row:** two `ApplyLearning(ctx, "note", "")` calls produce **two** rows (no dedup); assert via a count/`Learnings` query.
  - [x] **Apply via the op contract:** `ApplyMemoryOps(ctx, []contracts.MemoryOp{{Kind: contracts.MemoryOpCaptureLearning, Observation: "from worker", PatternKey: "pk2"}})` produces the learning — proving the worker-proposes→core-applies path end-to-end at the contract level. An op with an unknown `Kind` is skipped without error.
  - [x] **Isolation from messages:** appending conversation messages (4.1) does not affect `learnings`, and vice versa (the two tables are independent in the one db).
  - [x] `go test -race ./...` passes; native + arm64 `CGO_ENABLED=0` builds succeed; `golangci-lint run` → 0 issues; the contracts gob round-trip test still passes.

## Dev Notes

### Architecture constraints (binding)

- **AD-7 — the `learnings` table.** "a **`learnings`** table (`pattern_key` dedup, `recurrence_count`, `status` pending/promoted/pruned, `observation`, timestamps). On a turn the worker may propose `capture_learning(observation, pattern_key?)` (hot path, no extra LLM); core (single writer, AD-6) inserts or increments `recurrence_count` at `status=pending`. On a `pattern_key` match … core increments `recurrence_count`, refreshes `observation` to the latest proposed value, and resets `status=pending`. Concurrent `capture_learning` proposals … are **serialized through core**, never applied concurrently." This story is exactly that table + apply. [Source: ARCHITECTURE-SPINE.md#AD-7]
- **AD-6 — core is the sole writer; the worker only proposes; memory-ops have fixed arg schemas in `contracts/`.** "a `Result` envelope carries *proposed* changes that core validates and applies … memory-ops have **fixed arg schemas in `contracts/`** (`remember`/`rewrite_about`/`log_episode`/`capture_learning`)." This story adds the `capture_learning` schema to `contracts.MemoryOp` and the core apply path; the worker never writes the db. [Source: ARCHITECTURE-SPINE.md#AD-6, contracts/result.go]
- **FR11 — light self-improving learning.** "the worker proposes `capture_learning(...)` … core applies serially … dedup on `pattern_key`." The promotion into curated markdown is the dream cycle (4.4); 4.2 is the capture + dedup. [Source: epics.md#FR11, #Story 4.2]
- **AD-10 / NFR9 — versioned additive contracts.** Adding fields to `MemoryOp` is additive (append-only); the gob round-trip test (1.1) must still pass. [Source: ARCHITECTURE-SPINE.md#AD-10, contracts/contracts_test.go]
- **NFR2 — pure-Go `CGO_ENABLED=0`.** No new dependency — `modernc.org/sqlite` is already in from 4.1; the build stays cgo-free. [Source: epics.md#NFR2]
- **Single writer enforcement (4.1).** The store pins `SetMaxOpenConns(1)`, so all applies serialize on one connection; the atomic upsert is the second layer that makes the dedup race-free regardless. [Source: core/memory/store.go]

### Key design decisions

- **Atomic `ON CONFLICT … DO UPDATE` upsert, not read-then-write.** The dedup increment is one statement, so there is no check-then-act window — this *is* "the reviewer-gate dedup fix" the AC names. Combined with `SetMaxOpenConns(1)` (already serializing), increments cannot be lost even under concurrent proposals (proven by the 50-goroutine test).
- **`UNIQUE(pattern_key)` with nullable column for the unkeyed case.** SQLite treats `NULL`s as distinct in a UNIQUE index, so a `capture_learning` with no `pattern_key` (stored as SQL `NULL`) is always a fresh row — no special-case branch beyond mapping `"" → NULL`. Keyed learnings dedup; unkeyed ones accumulate.
- **Extend the flat `MemoryOp`, don't restructure.** `MemoryOp` is already a flat `Kind`-tagged struct (the project's established style). Add `Observation`/`PatternKey` as additive fields used by the `capture_learning` kind; later kinds add their own fields. This keeps the gob contract additive and avoids a premature per-op type hierarchy.
- **`ApplyMemoryOps` is core's single apply entry point.** It maps the proposed `[]MemoryOp` to store writes, switching on `Kind`. Unknown kinds are skipped (no error) so a forward-version worker proposing a not-yet-implemented op degrades gracefully rather than failing the turn. This is the seam the later dispatch wiring calls once the worker emits ops.
- **Built in isolation (4.1 precedent); status stays `pending`.** The worker doesn't emit `capture_learning` yet (needs LLM structured-output parsing — later), so wiring `dispatch`/`main` to apply ops now would apply nothing in production. Deliver + test the mechanism in isolation; wire it when the worker emits ops. `status` only ever becomes `pending` here — promotion/pruning is the dream cycle (4.4).

### Previous story intelligence (Epic 1–4.1)

- **The store is the home** — `core/memory/store.go` (Story 4.1): `Store{db *sql.DB}`, `Open(path)` runs an idempotent migration and pins `SetMaxOpenConns(1)`; WAL DSN via `net/url`; helpers return `[]T` (empty, not nil-error) on no rows; `scanMessages` pattern for row scanning; deferred `rows.Close()` is wrapped (`defer func(){ _ = rows.Close() }()`) to satisfy errcheck. Add the `learnings` table to the same migration and `learnings.go` beside `store.go`. [Source: core/memory/store.go]
- **Test patterns** — `core/memory/store_test.go`: `openTestStore(t)` helper opening at `filepath.Join(t.TempDir(), "history.db")` with `t.Cleanup(Close)`; stdlib `testing`, table-style asserts, clear failure messages. The read-only `JournalMode(ctx)` test accessor lives in `export_test.go` — add a learnings read accessor as a real method (it's useful to the dream cycle), not a test-only one. [Source: core/memory/store_test.go, core/memory/export_test.go]
- **errcheck + lint gate** — wrap deferred `Close()`/`rows.Close()`; the project's `golangci-lint` flags unchecked returns. [Source: 4-1 Completion Notes]
- **The proposal contract** — `contracts.Result{Reply, MemoryOps []MemoryOp}` and `MemoryOp{Kind}` already exist as the AD-6 proposal channel; the worker (`monolith`, 3.3) returns empty `MemoryOps` today. Extend the contract here. [Source: contracts/result.go, worker/monolith/monolith.go]
- **No new fence / no cycle** — `core/memory` importing `contracts` is fine (contracts imports nothing internal); the core fences guard `/transport`,`/display`,`/broker`,`/worker`, none of which this touches. [Source: core/dispatch/imports_test.go, core/scheduler/imports_test.go]
- **Additive-contract caution** — Story 1.1's gob round-trip test iterates `contracts.AllKinds`; adding fields to `MemoryOp` (a payload sub-struct) is additive and safe, but run that test to confirm. [Source: contracts/contracts_test.go]

### Latest tech information

- **No new external dependency.** Uses `database/sql` + the already-present `modernc.org/sqlite` v1.53.0 (pure Go, `CGO_ENABLED=0`). SQLite UPSERT (`ON CONFLICT … DO UPDATE`, SQLite ≥ 3.24) and `excluded.*` are supported by the bundled SQLite 3.53.x. Nothing to `go get`; no `go.mod` change. [Source: core/memory/store.go, go.mod]

### Project Structure Notes

- New: `core/memory/learnings.go`, `core/memory/learnings_test.go`.
- Modified: `contracts/result.go` (additive `MemoryOp` fields + `MemoryOpCaptureLearning` const), `core/memory/store.go` (add the `learnings` table to the migration).
- Unchanged: `core/memory/store_test.go`/`export_test.go` (4.1 messages tests stay green), the worker, broker, arbiter, dispatch, transports, scheduler, `cmd/shelldon/main.go`. No wiring this story. No `go.mod` change.
- `.golangci.yml` unchanged. The `learnings` table lives in the same `~/.shelldon/history.db`; tests use `t.TempDir()`.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.2] — the AC (worker proposes capture_learning; core applies serially; pattern_key dedup with no race)
- [Source: ...ARCHITECTURE-SPINE.md#AD-7] — the learnings table schema + serial single-writer apply + pattern_key dedup semantics
- [Source: ...ARCHITECTURE-SPINE.md#AD-6] — core sole writer; worker proposes; fixed memory-op arg schemas in contracts/
- [Source: ...ARCHITECTURE-SPINE.md#AD-10, epics.md#NFR9] — additive versioned contracts (gob round-trip must hold)
- [Source: contracts/result.go] — the MemoryOp/Result proposal channel to extend
- [Source: core/memory/store.go, core/memory/store_test.go] — the 4.1 store to extend + the test patterns to mirror
- [Source: worker/monolith/monolith.go] — the worker that proposes (empty MemoryOps today; emits capture_learning in a later step)

## Dev Agent Record

### Agent Model Used

claude-opus-4-8[1m] (Claude Code dev-story workflow; implementation delegated to a golang-expert subagent, verified by the parent)

### Debug Log References

- `go test -race ./...` → **109 passed (21 packages)** (4.1 ended at 103; +6 learnings tests)
- `go test -race ./core/memory/ ./contracts/` → 29 passed; `CGO_ENABLED=0 go test ./core/memory/` → 17 passed (sqlite + new table at runtime, no cgo)
- `CGO_ENABLED=0 go build ./...` (native) + `GOOS=linux GOARCH=arm64` → both success
- `golangci-lint run` → 0 issues
- contracts gob round-trip (`go test ./contracts/`) → 12 passed — the additive `MemoryOp` fields are gob-safe (AD-10/NFR9)
- Implementation spot-verified against spec (atomic UPSERT, `sql.NullString` for empty key, `ApplyMemoryOps` switch with silent skip, learnings table in the shared migration).

### Completion Notes List

- **Contract (`contracts/result.go`).** `MemoryOp` gained additive `Observation`/`PatternKey` fields (Kind kept first) + `const MemoryOpCaptureLearning = "capture_learning"` — the first concrete entry in AD-6's memory-op vocabulary. Additive, so the gob round-trip (Story 1.1) still passes.
- **AC1 — propose → apply serially → dedup, no race.** `(*Store).ApplyLearning` is a single atomic `INSERT … ON CONFLICT(pattern_key) DO UPDATE SET recurrence_count = recurrence_count + 1, observation = excluded.observation, status='pending', updated_at = …`. A new `pattern_key` inserts at `recurrence_count=1, status='pending'`; a repeat increments and refreshes the observation. The atomic upsert + the store's `SetMaxOpenConns(1)` make it race-free — `TestApplyLearning_ConcurrentNoLostIncrements` fires 50 concurrent goroutines on one key and asserts the count lands at exactly 50 under `-race`.
- **Unkeyed = always new row.** Empty `patternKey` → `sql.NullString{Valid:false}` → SQL NULL → never conflicts (SQLite NULLs distinct under UNIQUE), so unkeyed learnings accumulate (`TestApplyLearning_UnkeyedAlwaysNewRow`).
- **Core apply entry point.** `(*Store).ApplyMemoryOps(ctx, []contracts.MemoryOp)` switches on `Kind`, applies `capture_learning`, and **skips unknown kinds with no error** (forward-compatible) — proven by `TestApplyMemoryOps_CaptureLearning` (incl. an unknown `remember` op creating nothing). Read accessors `LearningByPatternKey` + `Learnings(status, n)` added (used by tests + the future dream cycle).
- **`learnings` table** lives in the same `~/.shelldon/history.db` as `messages`, added to the existing idempotent `Open` migration; the two tables are independent (`TestLearningsAndMessagesIndependent`). `status` only ever becomes `pending` here — promotion/pruning is the dream cycle (Story 4.4).
- **Built in isolation (as scoped).** No `dispatch`/`main` wiring — the `monolith` worker doesn't emit `capture_learning` from LLM output yet (structured-output parsing is a later step), so the apply path is delivered + tested in isolation and wired when the worker emits ops. No `go.mod` change (sqlite already present). The worker, broker, arbiter, dispatch, transports, scheduler, and `main` are unchanged.

### File List

- `contracts/result.go` (modified — additive `MemoryOp.Observation`/`PatternKey` + `MemoryOpCaptureLearning` const)
- `core/memory/store.go` (modified — `learnings` table + unique index added to the `Open` migration)
- `core/memory/learnings.go` (new — `Learning`, `ApplyLearning`, `ApplyMemoryOps`, `LearningByPatternKey`, `Learnings`)
- `core/memory/learnings_test.go` (new — 6 tests incl. the 50-goroutine no-race test)
- `_bmad-output/implementation-artifacts/4-2-learnings-table-hot-path-capture-learning.md` (story tracking)
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (status → review)

## Review Findings

### Code review (2026-06-24)

- [ ] [Review][Patch] gob round-trip test doesn't cover non-zero `Observation`/`PatternKey` fields [`contracts/contracts_test.go`] — `TestEnvelopeRoundTrip` uses `MemoryOp{Kind: "remember"}` with zero-valued new fields. Add a case with non-zero strings to verify additive gob encoding holds for the new fields (AD-10/NFR9).
- [x] [Review][Defer] `Learnings(ctx, status, n)` — negative `n` maps to SQLite "no limit" (all rows returned) [`core/memory/learnings.go:102`] — deferred, pre-existing pattern (store.go `Recent` has same signature); acceptable at M1 single-user volume.
- [x] [Review][Defer] `ConcurrentNoLostIncrements` tests Go pool serialization, not bare upsert atomicity [`core/memory/learnings_test.go:65`] — deferred, no runtime bug today; if `MaxOpenConns` is ever relaxed the atomic upsert is the real guard but has no dedicated test at that layer.

## Change Log

- 2026-06-24: Implemented the learnings table + hot-path `capture_learning` (`core/memory/learnings.go`) — the worker proposes via `contracts.MemoryOp`, core applies serially as sole writer with an atomic `ON CONFLICT` dedup upsert (race-free, 50-goroutine test). Additive contract extension (gob-safe). Built in isolation (no wiring yet). AC1 satisfied; status → review.
- 2026-06-24: Addressed code-review findings (1 of 2 fixed, 1 deferred). Fixed: non-positive `n` now returns an empty slice in `Recent`/`Search`/`Learnings` (was SQLite "no limit" → whole table) — guards the dream cycle (4.4) which passes a computed `n` to `Learnings`; added `TestListQueries_NonPositiveLimitReturnsEmpty`. Deferred: the concurrency test exercises pool-serialization + upsert combined (correct for the unchanging single-conn design). 18 memory tests, 110 suite-wide, lint 0. Status → done.
