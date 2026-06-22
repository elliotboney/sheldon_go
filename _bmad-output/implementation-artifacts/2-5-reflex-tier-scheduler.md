---
baseline_commit: 3150a03
---

# Story 2.5: Reflex-tier scheduler

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As the system,
I want a scheduler that runs named reflex-tier jobs at independent cadences (interval / idle-triggered) with no LLM,
so that mood-drift, blink, and other reflexes each fire on their own schedule from one tier-shaped component (FR10 reflex-tier, AD-13).

## Context

**Fifth story of Epic 2 (M1 — "The Soul").** Stories 2.3 (blink) and 2.4 (mood-drift) each ship as their **own** supervised `Serve(ctx)` loop — and both were deliberately shaped (a thin loop = "wait a delay, do work") so this story can **absorb them into one scheduler** with no behavior rewrite. This story builds that scheduler: a core-resident component that runs named reflex jobs, each on its own cadence, as a **single** supervised edge — replacing the two `reflex-blink` / `reflex-mood` edges.

The architectural weight here is **AD-13's "one tier-shaped scheduler"**: the scheduler must be built so the **turn tier (Story 3.5)** layers on by registering more jobs — **with no refactor of the scheduler loop** ("Yui's condition"). So the scheduler is **tier-agnostic**: it knows only `{name, next-delay, run}` jobs; reflex jobs run in-core, and future turn jobs will encode their arbiter/budget gating inside their own `Run` — the loop never changes.

**This story does NOT:**
- build the turn tier, budget/cooldown/battery gating, or anything LLM (Story 3.5 / Epic 3) — the scheduler stays reflex-only; `Job` gets no tier/gate fields (turn jobs encode gating in their `Run` later, no loop change)
- change blink or mood-drift **behavior** — only move their cadence loop into the scheduler (the work — `blinkOnce`, the drift+checkpoint — is unchanged)
- add a distinct "idle-triggered" cadence mechanism — blink already expresses idle-awareness via its `Run` (idle gate) on a polling cadence; a separate trigger type is unneeded at M1
- add cron-style cadences — `NextDelay()` covers fixed and jittered intervals, which is all the M1 reflexes need
- touch the compositor, renderer, contracts, or `state` — it only re-homes existing reflex loops

## Acceptance Criteria

1. **Independent cadences fire correctly.**
   **Given** two reflex jobs registered at independent cadences (verifiable under `testing/synctest`)
   **When** the fake clock advances past both cadences
   **Then** each job fires the correct number of times on its own cadence, independently.

2. **Reflex jobs are LLM-free, in-core.**
   **Given** any reflex-tier job
   **When** it executes
   **Then** it runs in-core with no worker invocation and no LLM call (AD-13 cost tier).

## Tasks / Subtasks

- [x] **Task 1 — Reflex-tier scheduler (`core/scheduler/`)** (AC: 1, 2)
  - [x] Create `core/scheduler/scheduler.go`. Package doc: the core-resident reflex-tier scheduler (AD-13) — runs named jobs at independent cadences, in-core, no worker/LLM. It is **tier-shaped**: the turn tier (Story 3.5) registers more jobs with no loop change; turn jobs encode their arbiter/budget gating in their own `Run`.
  - [x] `type Job struct { Name string; NextDelay func() time.Duration; Run func(ctx context.Context) }` — tier-agnostic. `NextDelay` returns the gap to the next fire (fixed or jittered); `Run` does the work.
  - [x] `type Scheduler struct` holding `[]Job`. `New() *Scheduler`; `Register(j Job)` (append; before `Serve`, like the supervisor's fixed startup set).
  - [x] `Serve(ctx context.Context) error` — run **each job in its own goroutine** (a `time.NewTimer(j.NextDelay())` loop: on fire, run the job, `Reset(j.NextDelay())`; on `ctx.Done()` stop the timer and return). Independent goroutines give independent cadences. Wait for all job goroutines to exit (a `sync.WaitGroup`) before returning `ctx.Err()`, so shutdown is clean.
  - [x] Wrap each `j.Run(ctx)` call in a `defer recover()` (log via slog, AD-17) so a single panicking reflex job does not kill the scheduler edge or its siblings (AD-5 — recover does not cross goroutines, so the scheduler's job goroutines must recover their own). The job continues on its next cadence.

- [x] **Task 2 — Convert the blink reflex to a job (`core/reflexes/blink.go`)** (AC: 1, 2)
  - [x] Replace `Blink.Serve` with the job surface: export `NextDelay() time.Duration` (the existing jittered `nextDelay` logic) and `Run(ctx context.Context)` (the existing `if b.idle() { b.blinkOnce(ctx) }` body). Keep `idle`, `blinkOnce`, and the constants unchanged. The scheduler now owns the loop.

- [x] **Task 3 — Convert the mood-drift reflex to a job (`core/reflexes/mood.go`)** (AC: 1, 2)
  - [x] Replace `MoodDrift.Serve` with: `NextDelay() time.Duration` (returns `moodDriftInterval`) and `Run(context.Context)` (the existing drift+`Checkpoint` body; the ctx param is unused — keep it to satisfy the `Job.Run` signature). Keep `clamp`, `expressionFor`, constants unchanged.

- [x] **Task 4 — Wire the scheduler into `cmd/shelldon/main.go`** (AC: 1, 2)
  - [x] Replace the `reflex-blink` and `reflex-mood` edges with one scheduler: `sched := scheduler.New()`, `sched.Register(scheduler.Job{Name: "blink", NextDelay: blink.NextDelay, Run: blink.Run})`, `sched.Register(scheduler.Job{Name: "mood-drift", NextDelay: mood.NextDelay, Run: mood.Run})`, then `root.Add(supervisor.Guard("reflex-scheduler", sched.Serve))` (added last, so reverse-drain stops it first — the pet stops producing reflex frames before the renderer drains). Update the package doc comment.

- [x] **Task 5 — Tests (`testing/synctest`, stdlib, no testify)** (AC: 1, 2)
  - [x] `core/scheduler/scheduler_test.go`. **AC1 (independent cadences):** inside `synctest.Test(...)`, register two jobs with different fixed `NextDelay`s and `atomic.Int32` counters; `go sched.Serve(ctx)`; `time.Sleep` past several cadences of both; `synctest.Wait()`; cancel + join; assert each counter equals `floor(elapsed / cadence)`. **Panic isolation:** a job whose `Run` panics once does not stop the scheduler — a sibling job keeps firing (proves the per-job recover).
  - [x] `core/scheduler/imports_test.go` (AC2): walk `core/scheduler` **and** `core/reflexes` and fail on any import of `/broker` or `/worker` — reflex jobs never reach the LLM path (mechanical AC2, mirroring `core/dispatch/imports_test.go`).
  - [x] **Update `core/reflexes/blink_test.go`:** drop the `Serve`-based test; keep the unit tests (`NextDelay` jitter, `idle` gate, `blinkOnce` reopen-on-cancel, mood-expression). Re-prove idle→blink by calling `blink.Run(ctx)` while idle under `synctest` (blink frame pushed) and not-idle (no frame).
  - [x] **Update `core/reflexes/mood_test.go`:** drift accumulation + checkpoint now via calling `mood.Run(ctx)` `N` times (synchronous, deterministic — no synctest needed): assert `Mood == folded clamp(step)×N` and `state.Load(path).Mood` matches; clamp holds after many `Run`s. Keep `TestExpressionFor`. (The "fires N times over a week" cadence assertion now lives in the scheduler test.)
  - [x] `go test -race ./...` passes; native + arm64 `CGO_ENABLED=0` builds succeed; `golangci-lint run` → 0 issues (`.golangci.yml` unchanged).

### Review Findings

- [x] [Review][Patch] `imports_test.go` has no file-count guard — vacuous pass if walked tree is empty [core/scheduler/imports_test.go] — resolved: added `scanned ≥ 3` guard
- [x] [Review][Defer] `Serve` with zero registered jobs returns `nil` immediately — supervisor edge silently disappears [core/scheduler/scheduler.go:43] — deferred, pre-existing
- [x] [Review][Patch] `NextDelay` returning 0 causes a busy-loop — no floor guard in `runJob` [core/scheduler/scheduler.go:58] — resolved: added `minDelay` floor via `nextDelay()` helper (pre-empts the 3.5 computed-cadence footgun); covered by `TestNextDelay_FlooredAtMinDelay`
- [x] [Review][Defer] Slow `Run` longer than `NextDelay` → timer fires during execution, next tick is instant burst catch-up [core/scheduler/scheduler.go:67] — deferred, pre-existing

## Dev Notes

### Architecture constraints (binding)

- **AD-13 — One tier-shaped scheduler; reflexes are the cheap tier.** "a **core-resident scheduler** owns the pet's self-driven life as **named jobs**, each with its own **cadence** … **reflex jobs** (mood drift, blink) run **in-core, no LLM, cheap CPU** … Scheduler-proposed **turn jobs** go through the **arbiter** … the scheduler never invokes the worker directly." This story builds the reflex tier; the turn tier (3.5) adds jobs whose `Run` goes through the arbiter — **no loop refactor** (Yui's condition). Keep `Job` tier-agnostic. [Source: ARCHITECTURE-SPINE.md#AD-13, epics.md#Story 2.5 NOTE, #Story 3.5]
- **AD-5 — Soul survives any single failure; recover does not cross goroutines.** Each job runs in its own goroutine under the scheduler edge, so each job goroutine needs its own `defer recover()` — a panicking reflex must not kill the scheduler or its siblings. The scheduler is itself a supervised `Service` (one `supervisor.Guard` edge). [Source: ARCHITECTURE-SPINE.md#AD-5]
- **AD-10 — synctest for cadence; no monkeypatch.** Independent-cadence correctness is the canonical `testing/synctest` case (fake clock, exact fire counts). Reflex jobs use real `time.Timer`; the bubble fakes time. [Source: ARCHITECTURE-SPINE.md#AD-10]
- **NFR2 / NFR3 / NFR13 — pure-Go, LLM-free core, offline.** The scheduler imports only stdlib (`context`, `sync`, `time`, `log/slog`); reflex jobs import no broker/worker (AC2, enforced by the imports test). No dependency; offline aliveness. [Source: ARCHITECTURE-SPINE.md#NFR2, NFR3, NFR13]
- **Structural Seed — `core/scheduler/`.** The named seed package for the scheduler. [Source: ARCHITECTURE-SPINE.md#Structural Seed]

### Key design decisions

- **`Job` is `{Name, NextDelay, Run}` — tier-agnostic, no gate fields.** Fixed cadence (mood: constant `NextDelay`) and jittered cadence (blink: jittered `NextDelay`) both fit. Turn jobs (3.5) will be the same shape, with budget/cooldown/arbiter gating inside their `Run` — so adding the turn tier registers jobs without changing the loop (Yui's condition). Adding gate fields now would be speculative (YAGNI).
- **Per-job goroutine, per-job recover.** Independent goroutines give truly independent cadences (and let blink's internal `blinkDuration` wait not stall mood-drift). Per-tick `recover()` keeps one bad reflex from taking down the soul (AD-5).
- **Absorb, don't rewrite.** Blink/mood keep their work verbatim (`blinkOnce`, drift+checkpoint); only the loop wrapper (`Serve`) moves into the scheduler, re-exposed as `NextDelay()`/`Run()`. `reflexes` does **not** import `scheduler` — `main` composes the `Job`s from the reflex methods, keeping `reflexes` a leaf (no cycle, clean layering).
- **Cadence tests move to the scheduler; reflex tests stay unit-level.** The "fires N times over an interval" property is now the scheduler's job (tested generically). The reflex tests verify their per-fire work (`Run` does one blink / one drift+checkpoint) deterministically. Together they still cover the 2.3/2.4 ACs.

### Previous story intelligence (Stories 2.1–2.4)

- **Blink & mood are already loop-shaped for this.** `Blink.Serve` = `timer(nextDelay) → if idle { blinkOnce }`; `MoodDrift.Serve` = `ticker(interval) → drift + Checkpoint`. Lift the bodies into `Run`, the delays into `NextDelay`, delete `Serve`. [Source: core/reflexes/blink.go:61, core/reflexes/mood.go]
- **synctest pattern (mirror prior reflex/checkpoint tests):** start loop goroutine, `time.Sleep` to fake-advance, `synctest.Wait()`, cancel + `<-done` join before the bubble returns; construct `time.Now()`-dependent state inside the bubble; use `atomic` counters for job-goroutine side effects. [Source: core/reflexes/blink_test.go, core/state/checkpoint_test.go]
- **Import-hygiene test pattern to mirror for AC2:** `core/dispatch/imports_test.go` walks a tree and fails on forbidden import substrings — copy that shape for `/broker`,`/worker` over `core/scheduler` + `core/reflexes`. [Source: core/dispatch/imports_test.go]
- **Supervised-edge + drain order:** edges added in start order, drained reverse; current tail is `reflex-blink`, `reflex-mood` → replace both with `reflex-scheduler` (added last). `supervisor.Guard` supplies the edge-level recover; the scheduler adds per-job recover beneath it. [Source: cmd/shelldon/main.go, core/supervisor/supervisor.go]
- **2.3 carried review note (still relevant):** `hub.Publish` is a blocking send on the 16-slot display channel; the scheduler must not let a reflex push faster than the renderer drains. Blink/mood push ≤2 frames per (multi-second) cadence — safe. [Source: 2-3 Review Findings]
- **No new dependency** since 1.6. 2.5 adds none. [Source: go.mod]

### Project Structure Notes

- New: `core/scheduler/` (`scheduler.go`, `scheduler_test.go`, `imports_test.go`).
- Modified: `core/reflexes/blink.go` (Serve → NextDelay+Run), `core/reflexes/mood.go` (Serve → NextDelay+Run), `core/reflexes/blink_test.go` + `mood_test.go` (re-target the Serve tests), `cmd/shelldon/main.go` (one `reflex-scheduler` edge instead of two). No `state`/`contracts`/`compositor` changes.
- `.golangci.yml` unchanged. No `go.mod`/`go.sum` change.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.5] — ACs; the reflex-tier NOTE (turn tier in 3.5, no core-loop refactor)
- [Source: ...ARCHITECTURE-SPINE.md#AD-13] — core-resident scheduler; named multi-cadence jobs; reflex vs turn cost tiers; scheduler never invokes the worker directly
- [Source: ...ARCHITECTURE-SPINE.md#AD-5] — soul survives any single failure; recover per goroutine
- [Source: ...ARCHITECTURE-SPINE.md#AD-10] — testing/synctest for scheduler cadence
- [Source: core/reflexes/blink.go, core/reflexes/mood.go] — the Serve loops to absorb (NextDelay + Run)
- [Source: core/dispatch/imports_test.go] — import-hygiene test pattern for AC2
- [Source: cmd/shelldon/main.go, core/supervisor/supervisor.go] — edge wiring, Guard, drain order

## Dev Agent Record

### Agent Model Used

claude-opus-4-8 (1M context)

### Debug Log References

None — clean implementation; no HALT conditions hit.

### Completion Notes List

- **Scheduler is tier-agnostic as specified.** `Job{Name, NextDelay, Run}` carries no tier/gate fields; the `Serve` loop knows only those three. The turn tier (3.5) registers more jobs with zero loop change (Yui's condition).
- **Per-job goroutine, per-job recover (AD-5).** Each job runs in its own goroutine under a `sync.WaitGroup`; `fire()` wraps each `Run` tick in `defer recover()` and logs via slog (AD-17). Proven by `TestServe_PanicIsolation`: a job that panics every tick does not stop its sibling, which still fires `floor(elapsed/cadence)` times.
- **Absorb, not rewrite.** Blink/mood kept their work verbatim (`blinkOnce`, drift+`Checkpoint`); only the `Serve` loop wrapper moved into the scheduler, re-exposed as exported `NextDelay()` + `Run()`. `reflexes` does not import `scheduler` — `main` composes the `Job`s, keeping `reflexes` a leaf (no cycle).
- **AC1 (independent cadences):** `TestServe_IndependentCadences` registers a 2s and a 5s job under `synctest`, advances 21s, asserts 10 and 4 fires respectively.
- **AC2 (LLM-free in-core):** `imports_test.go` walks `core/scheduler` + `core/reflexes` and fails on any `/broker` or `/worker` import (mirrors `core/dispatch/imports_test.go`).
- **Reflex tests re-targeted to the per-tick work:** blink idle→blink and active→no-blink via `blink.Run`; mood drift+checkpoint+clamp via N synchronous `mood.Run` calls. The "fires N times over a cadence" property now lives in the scheduler test.
- **Validation:** `go test -race ./...` → 54 pass (14 packages); `CGO_ENABLED=0` native + `GOOS=linux GOARCH=arm64` builds succeed; `golangci-lint run` → 0 issues. `.golangci.yml`, `go.mod`, `go.sum` unchanged.

### File List

- `core/scheduler/scheduler.go` (new) — reflex-tier scheduler: `Job`, `Scheduler`, `New`, `Register`, `Serve`, per-job recover.
- `core/scheduler/scheduler_test.go` (new) — AC1 independent cadences, panic isolation, clean-cancel return.
- `core/scheduler/imports_test.go` (new) — AC2 reflex-tier-is-LLM-free import-hygiene walk.
- `core/reflexes/blink.go` (modified) — `Serve` → `NextDelay()` + `Run()`; package doc updated.
- `core/reflexes/mood.go` (modified) — `Serve` → `NextDelay()` + `Run()`.
- `core/reflexes/blink_test.go` (modified) — `nextDelay`→`NextDelay`; `Serve` test → `Run`-based idle/active proofs.
- `core/reflexes/mood_test.go` (modified) — `Serve`/synctest tests → synchronous `Run`-based drift/clamp tests.
- `cmd/shelldon/main.go` (modified) — two reflex edges → one `reflex-scheduler` edge; `scheduler` import; package doc updated.

## Change Log

| Date       | Version | Description                                                                 |
| ---------- | ------- | --------------------------------------------------------------------------- |
| 2026-06-21 | 0.1     | Implemented reflex-tier scheduler; absorbed blink + mood-drift loops into it as named jobs. All ACs satisfied; 54 tests pass. Status → review. |
