---
baseline_commit: 3ded414
---

# Story 1.3: Worker seam interface + stub + ≤1-in-flight arbiter gate

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer building shelldon,
I want the `Worker.AssembleAndPropose(ctx, turn) (Result, error)` interface with a stub implementation and an arbiter that admits at most one worker turn,
so that the isolation seam and the ≤1-worker invariant exist from M0 and never reshape callers when the real worker or subprocess swaps in (AD-2, AD-8, NFR4).

## Context

Third Epic 1 story. It stands up the **isolation seam** (the `Worker` interface, AD-2) and the **≤1-worker gate** (the arbiter, AD-8) — the two pieces that make the M3 privsep swap a pure implementation change rather than a redesign. The seam is a Go interface with two eventual implementations behind it (Monolith+ goroutine now; Privsep-lite subprocess at M3); this story ships the interface + a **stub** behind it, plus the arbiter that guarantees turns never run concurrently. The ≤1-worker bound is one of the four **required M0 tests** (AD-10, NFR4).

Keep scope tight. This is the seam + the gate only. It does **not** build prompt assembly or a real LLM worker (3.3), the subprocess/privsep wall (5.1), suture supervision (1.4), the CLI transport (1.5), the scheduler / reflex-vs-turn arbitration / proactive cooldowns (Epic 2–3), or the full per-class catch-up slot with priority ordering (AD-8, adversarial Finding 4 — deferred). The stub reads nothing and proposes a trivial well-formed `Result`.

## Acceptance Criteria

1. **Stub returns a well-formed Result.**
   **Given** the `Worker` interface and a stub implementation behind it
   **When** `AssembleAndPropose` is called for a turn
   **Then** the stub returns a well-formed `Result` with no error.

2. **At most one turn in flight.**
   **Given** one worker turn already in flight
   **When** a second turn is submitted to the arbiter (the required ≤1-worker M0 test, NFR4/AD-8)
   **Then** the second turn is coalesced or rejected and the two turns never run concurrently.

3. **Race-clean under the detector.**
   **Given** the arbiter gate under `go test -race`
   **When** the ≤1-worker test runs
   **Then** no data race is reported.

## Tasks / Subtasks

- [x] **Task 0 — Define the `worker` package and seam interface** (AC: 1)
  - [x] Create `worker/worker.go` with the seam: `type Worker interface { AssembleAndPropose(ctx context.Context, turn contracts.Job) (contracts.Result, error) }`. Use value types (`contracts.Job`/`contracts.Result`) to match the 1.1/1.2 convention. Package doc: this is the swappable isolation seam (AD-2); Monolith+ goroutine now, Privsep-lite subprocess at M3, callers unchanged across the swap
  - [x] `turn` is the turn-job input (`contracts.Job`). The worker **proposes** a `Result` and never writes state/memory — the proposal channel (AD-6)
- [x] **Task 1 — Stub worker behind the seam** (AC: 1)
  - [x] Create `worker/stub.go` with `Stub` implementing `Worker`. It reads nothing (no history/markdown/vault — assembly is deferred to Story 3.3) and returns a well-formed `Result` with no error. Echo the input so later stories have something to render: `Result{Reply: turn.Input}`
  - [x] Add a compile-time interface assertion: `var _ Worker = Stub{}`
  - [x] **RED→GREEN test** (`worker/stub_test.go`): call `Stub{}.AssembleAndPropose(context.Background(), contracts.Job{Input: "hi", ConvoID: "c1"})`; assert `err == nil` and the `Result` is well-formed (`Reply == "hi"`)
- [x] **Task 2 — Arbiter ≤1-in-flight gate** (AC: 2)
  - [x] Create `core/arbiter/arbiter.go` with `Arbiter` holding a constructor-injected `worker.Worker` and a capacity-1 token channel (`chan struct{}`, 1) as the single in-flight token
  - [x] `New(w worker.Worker) *Arbiter`; `Submit(ctx context.Context, turn contracts.Job) (contracts.Result, error)` — non-blocking `select` on the token: acquired → run `w.AssembleAndPropose`, release token on defer; not acquired (a turn is in flight) → return `ErrTurnInFlight` without invoking the worker
  - [x] Define `var ErrTurnInFlight = errors.New(...)`. **Rejection** (not coalescing) is the chosen M0 semantic — see Dev Notes "Coalesce vs reject"; both satisfy AC2
  - [x] Thread `ctx` through to the worker so cancellation propagates to the in-flight turn (foundational for AD-11 fencing, which is otherwise out of scope here)
- [x] **Task 3 — Required ≤1-worker test** (AC: 2)
  - [x] In `core/arbiter/arbiter_test.go`, use a controllable test double `Worker` that, inside `AssembleAndPropose`, increments an `atomic.Int32` in-flight counter, records the max observed, signals it has entered, then blocks on a release channel
  - [x] Start one `Submit` in a goroutine; wait until the double has entered the worker (holds the token); then fire many concurrent `Submit` calls and assert **every one** returns `ErrTurnInFlight` and the **max observed in-flight is exactly 1**; release; assert the first turn's `Result` is returned with no error
  - [x] Assert the gate **reopens**: after the first completes, a fresh `Submit` is admitted and succeeds
- [x] **Task 4 — Prove race-clean** (AC: 3)
  - [x] `go test -race ./core/...` and `go test -race ./worker/...` pass with no data race reported (the token channel and atomics are the only cross-goroutine state)
- [x] **Task 5 — Verify build + lint** (AC: 1, 2, 3)
  - [x] `go build ./...` and `CGO_ENABLED=0 GOARCH=arm64 go build ./...` succeed
  - [x] `go test ./...` passes; `go test -race ./...` passes
  - [x] `golangci-lint run` passes (do not modify `.golangci.yml`; the LLM-free-core fence over `core/` lands in Story 3.1)

## Dev Notes

### Architecture constraints (binding)

- **The worker is a Go interface — the swappable isolation seam.** "the worker is a Go **interface** — `Worker.AssembleAndPropose(ctx, turn) (Result, error)`. Two implementations ship behind it: Monolith+ (goroutine, `context` timeout + own `recover()`) for M0–M2, Privsep-lite (a long-lived uid-separated recycled subprocess … re-exec of `/proc/self/exe`, not fork) as the M3+ end-state." Story 1.3 ships the interface + the stub; the goroutine/subprocess implementations and `recover()` come later (1.4/3.3/5.1). [Source: ARCHITECTURE-SPINE.md#AD-2]
- **The worker never writes — it proposes.** "The worker **never** writes — a `Result` envelope carries *proposed* changes that core validates and applies." The stub returns a proposed `Result`; nothing in this story mutates state. [Source: ARCHITECTURE-SPINE.md#AD-6]
- **A single arbiter governs turns: ≤1 worker turn in flight.** "a single arbiter in core decides reflex-vs-turn and governs turns: **≤1 worker turn in flight**; events during a turn coalesce into a single pending catch-up slot (never a growing backlog)…". Story 1.3 implements **only** the ≤1-in-flight bound. Reflex-vs-turn arbitration, the per-class catch-up slot, coalescing, priority ordering, and proactive cooldowns require the scheduler and turn-classes that do not exist yet — **out of scope**, deferred to Epic 2–3. [Source: ARCHITECTURE-SPINE.md#AD-8]
- **The ≤1-worker bound is a required M0 test.** "The ≤1-worker bound is a required M0 test (AD-10)." This story owns the second of the four required M0 tests (1.1 owned the gob round-trip). [Source: ARCHITECTURE-SPINE.md#AD-8, #AD-10, SPEC NFR4]
- **Constructor injection, no monkeypatch.** "narrow interfaces over every external seam … wired by constructor injection (no monkeypatch in Go)." The arbiter takes its `Worker` via `New(w)`. [Source: ARCHITECTURE-SPINE.md#AD-10]

### Coalesce vs reject (the AC2 choice)

AC2 permits the second turn to be **coalesced *or* rejected**; both prove "the two never run concurrently." This story **rejects** with `ErrTurnInFlight` because:

- It is the minimal correct ≤1 gate (a capacity-1 token + non-blocking `select`); no pending-slot state, no priority rules.
- The full coalescing design — a catch-up slot **keyed by turn-job class** (reply / dream / proactive-ping) with explicit priority (reply > dream > ping) and *defer-not-drop* semantics — depends on turn-classes and the scheduler that arrive in Epic 2–3. The adversarial-seams review (Finding 4) flags that getting heterogeneous-class priority right is a real design task; building it now, before the classes exist, would be speculative. [Source: ARCHITECTURE-SPINE.md#AD-8, reviews/review-adversarial-seams.md#Finding 4]

When the scheduler lands, `Submit`'s reject path is the natural seam to grow into coalescing — callers already handle a "not admitted now" outcome.

### Recommended shapes (minimal, idiomatic)

```go
// worker/worker.go
type Worker interface {
    AssembleAndPropose(ctx context.Context, turn contracts.Job) (contracts.Result, error)
}

// worker/stub.go
type Stub struct{}
var _ Worker = Stub{}
func (Stub) AssembleAndPropose(_ context.Context, turn contracts.Job) (contracts.Result, error) {
    return contracts.Result{Reply: turn.Input}, nil // echo stub; reads nothing
}

// core/arbiter/arbiter.go
type Arbiter struct {
    w     worker.Worker
    token chan struct{} // capacity 1 = the single in-flight slot
}
func New(w worker.Worker) *Arbiter { return &Arbiter{w: w, token: make(chan struct{}, 1)} }
func (a *Arbiter) Submit(ctx context.Context, turn contracts.Job) (contracts.Result, error) {
    select {
    case a.token <- struct{}{}:
        defer func() { <-a.token }()
        return a.w.AssembleAndPropose(ctx, turn)
    default:
        return contracts.Result{}, ErrTurnInFlight
    }
}
```

### Concurrency & testing

- The capacity-1 token channel is the entire ≤1 mechanism — channel send/receive are synchronized, so the gate is race-free by construction. Do **not** add a mutex on top.
- **The ≤1-worker test uses goroutines + a blocking test-double worker + an `atomic.Int32` max-in-flight counter, run under `go test -race`** — that is exactly what AC3 mandates. Synchronize with a channel the double closes/sends on when it has entered the worker (a happens-before edge), so the assertions are deterministic, not timing-dependent.
- **`testing/synctest` is NOT used here.** It is the tool for deterministic *scheduler-cadence* tests (Epic 2 reflexes/scheduler), per AD-10 — the ≤1-worker overlap test is not cadence-based. Reserve synctest for those stories. [Source: ARCHITECTURE-SPINE.md#AD-10]
- `turn_id` idempotent-close fencing (AD-11, via `context` cancellation) is **out of scope**: there is no turn lifecycle / superseding yet. `Submit` threads `ctx` through so cancellation already propagates; keyed discard of stale `Result`s lands with the turn lifecycle in a later story. [Source: ARCHITECTURE-SPINE.md#AD-11]

### Previous story intelligence (Stories 1.1, 1.2)

- **Conventions:** package doc comment on the primary file; small files per type; **table-driven stdlib `testing` + `reflect.DeepEqual`**, no `testify`; subtests via `t.Run`; `t.Helper()` on helpers; exported sentinel errors via `errors.New` matched with `errors.Is` (1.2's `ErrNoRoute`/`ErrDuplicateRoute`/`ErrNilDestination` pattern). Mirror this in `worker` and `core/arbiter`. [Source: core/bus/hub.go, core/bus/hub_test.go]
- **Imports available:** `github.com/elliotboney/shelldon_go/contracts` — `Job{Input, ConvoID}`, `Result{Reply, MemoryOps}`. `core/bus` exists (the Hub) but 1.3 does not wire the arbiter to the bus — that integration is Story 1.5 (end-to-end round-trip). Keep the arbiter bus-agnostic for now.
- **No import cycle:** `core/arbiter` imports `worker` (for the `Worker` type) and `contracts`; `worker` imports only `contracts`. Acyclic.

### Project Structure Notes

- New packages: `worker/` (`worker.go`, `stub.go`, `stub_test.go`) and `core/arbiter/` (`arbiter.go`, `arbiter_test.go`) — both from the Structural Seed (`core/ … arbiter …` and a top-level `worker/`). Do not scaffold other siblings. [Source: ARCHITECTURE-SPINE.md#Structural Seed]
- `.golangci.yml` unchanged this story.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.3] — ACs, epic goal
- [Source: _bmad-output/planning-artifacts/architecture/architecture-shelldon_go-2026-06-19/ARCHITECTURE-SPINE.md#AD-2] — worker interface, swappable isolation seam
- [Source: ...ARCHITECTURE-SPINE.md#AD-6] — worker proposes, never writes
- [Source: ...ARCHITECTURE-SPINE.md#AD-8] — single arbiter, ≤1 worker turn, catch-up slot (coalescing/priority deferred)
- [Source: ...ARCHITECTURE-SPINE.md#AD-10] — required M0 tests, synctest for cadence tests, constructor injection
- [Source: ...ARCHITECTURE-SPINE.md#AD-11] — turn_id fencing via context (deferred)
- [Source: ...reviews/review-adversarial-seams.md#Finding 4] — heterogeneous catch-up-slot priority (deferred)
- [Source: _bmad-output/specs/spec-shelldon-go/SPEC.md] — NFR4 (≤1-worker)

## Dev Agent Record

### Agent Model Used

claude-opus-4-8[1m] (Claude Opus 4.8, 1M context) via Claude Code dev-story workflow.

### Debug Log References

- TDD: wrote `worker/stub_test.go` and `core/arbiter/arbiter_test.go` first, confirmed RED (undefined `Worker`/`Stub`/`New`/`ErrTurnInFlight`), then the three implementation files to GREEN. gofmt/vet/lint clean on first pass.

### Completion Notes List

- **AC1 satisfied** — `worker.Stub` (echo stub, reads nothing) implements the `Worker` seam; `TestStub_ReturnsWellFormedResult` asserts a no-error, well-formed `Result` (`Reply == Input`). Compile-time `var _ Worker = Stub{}` pins the interface.
- **AC2 satisfied** — `Arbiter` gates turns with a capacity-1 token channel + non-blocking `select`. `TestArbiter_AtMostOneInFlight` holds one turn in flight (blocking test-double signals via an `entered` channel for a happens-before edge), fires 50 concurrent `Submit`s — **all** return `ErrTurnInFlight` — and asserts max observed concurrency is **exactly 1**. Also asserts the gate reopens after completion.
- **AC3 satisfied** — `go test -race ./...` clean; the token channel + `atomic.Int32` counters are the only cross-goroutine state.
- **Design call (cited in spec):** chose **reject** (`ErrTurnInFlight`) over coalesce for the M0 gate — AC2 permits either, and the full per-class catch-up slot + priority (AD-8, adversarial Finding 4) needs turn-classes/scheduler that don't exist yet (Epic 2–3). The reject path is the seam that grows into coalescing.
- **Scope held:** seam interface + stub + ≤1 gate only. No real assembly (3.3), no subprocess/privsep (5.1), no suture (1.4), no bus wiring (the arbiter is bus-agnostic; integration is 1.5), no `turn_id` fencing (ctx is threaded so cancellation propagates; keyed discard deferred to the turn lifecycle). `synctest` intentionally not used — reserved for Epic 2 cadence tests per AD-10.
- **Second of the four required M0 tests delivered** (the ≤1-worker bound; 1.1 delivered the gob round-trip).

### File List

- `worker/worker.go` (new) — `Worker` seam interface
- `worker/stub.go` (new) — `Stub` echo worker + interface assertion
- `worker/stub_test.go` (new) — AC1 well-formed-Result test
- `core/arbiter/arbiter.go` (new) — `Arbiter`, `New`, `Submit`, `ErrTurnInFlight`
- `core/arbiter/arbiter_test.go` (new) — required ≤1-worker / race test
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (modified) — story status → in-progress → review

### Review Findings

- [x] [Review][Patch] Gate-reopens assertion creates a fresh `Arbiter` (`New(w2)`) instead of reusing the original `a` — the deferred `<-a.token` drain on the original is never verified [core/arbiter/arbiter_test.go:90-98] — RESOLVED: reopen assertion now reuses the original `a`, proving its token drained after the first turn completed
- [x] [Review][Defer] `Submit` has no `ctx.Done()` arm — a context cancelled before slot acquisition returns `ErrTurnInFlight` instead of `ctx.Err()` [core/arbiter/arbiter.go:37-43] — deferred, pre-existing

## Change Log

| Date       | Change                                                                 |
|------------|------------------------------------------------------------------------|
| 2026-06-20 | Implemented the `Worker` isolation seam + echo stub and the ≤1-in-flight arbiter gate (`ErrTurnInFlight`), with the required ≤1-worker M0 test green under `-race` (Story 1.3). All tasks complete; build (native+arm64), tests, `-race`, and lint green. |
| 2026-06-20 | Addressed code review — 1 [Patch] resolved (gate-reopen test reuses the original arbiter to verify token drain); 1 finding deferred (Submit lacks a `ctx.Done()` arm). |
