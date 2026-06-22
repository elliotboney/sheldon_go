---
baseline_commit: efa3ad3
---

# Story 2.3: Blink + idle reflexes

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As Elliot (the owner),
I want the pet to blink at jittered intervals and react when idle,
so that it visibly feels alive between turns even with the network down (FR2, de-vibed AC1).

## Context

**Third story of Epic 2 (M1 — "The Soul").** The pieces are now in place: 2.1 gave the pet persistent state with a `LastInteraction` timestamp and a `Touch()` method (stamped exactly for this story — see its doc comment), and 2.2 gave it a face via `compositor.PushFace` over the region-compositor seam. This story makes the pet **move on its own** — the first resident reflex. It blinks on a jittered cadence while idle, so even with no brain and no network the face is visibly alive between turns.

This is a **reflex-tier** behavior (AD-13): it runs **in-core, no worker, no LLM, cheap CPU**. It reads `personality-state.LastInteraction` to know when the pet is idle, and pushes blink frames through the compositor (2.2). It is the first of two reflexes in Epic 2 (mood-drift is 2.4); the **reflex-tier scheduler (2.5)** will later own these cadences as registered jobs — so this story runs the blink on its own supervised loop now, shaped so 2.5 absorbs it with no rewrite (the loop is a thin `Serve(ctx)`, like the 2.1 checkpoint loop).

**Interpretation of the ACs (flagged — confirm if you disagree):** the two ACs describe one integrated reflex — an **idle-gated, jittered blink**. AC1: when the pet has had no interaction for an idle threshold, it blinks (a blink frame is rendered). AC2: successive inter-blink intervals are jittered, not a fixed constant. To make "react when idle" honest end-to-end, `dispatch` stamps `state.Touch()` on each inbound message so an active conversation resets idleness and pauses ambient blinking.

**This story does NOT:**
- build the reflex-tier scheduler (Story 2.5) — the blink runs its own supervised loop now; 2.5 registers it as a job with no rewrite
- build mood-drift or wire `Mood → Expression` (Story 2.4) — blink frames use `ExpressionNeutral`; 2.4 makes the blink expression-aware
- add other idle behaviors (look-around, stretch, etc.) — the blink is the single idle reaction at M1
- change the compositor or renderer (2.2) — it only *calls* `compositor.PushFace`
- add real turn handling / offline acknowledgement (Story 2.6) — `dispatch` only gains a one-line `Touch()` on inbound

## Acceptance Criteria

1. **Idle → blink.**
   **Given** no inbound message for the idle threshold (verifiable under `testing/synctest` with the fake clock advanced)
   **When** the idle threshold elapses
   **Then** a blink frame is rendered (a face snapshot with eyes closed pushed through the compositor).

2. **Jittered intervals.**
   **Given** repeated blink cycles under the fake clock
   **When** inter-blink intervals are measured
   **Then** the interval is jittered (not a fixed constant) across cycles.

## Tasks / Subtasks

- [x] **Task 1 — Blink reflex (`core/reflexes/`)** (AC: 1, 2)
  - [x] Created `core/reflexes/blink.go` with the package doc (reflex-tier: in-core, no worker/LLM; reads state, pushes via compositor; 2.5 will own cadences).
  - [x] `Blink` (compositor + store + injected `*rand/v2.Rand`); `NewBlink(comp, store, rng)`.
  - [x] Tunable constants commented as story-time config: `blinkIdleThreshold` 5s, `blinkBaseInterval` 4s + `blinkJitter` 3s, `blinkDuration` 200ms.
  - [x] `nextDelay()` = `blinkBaseInterval + time.Duration(rng.Int64N(int64(blinkJitter)))` (math/rand/v2 `*Rand` method; `rand.N` is package-level only).
  - [x] `idle()` = `time.Since(store.Snapshot().LastInteraction) >= blinkIdleThreshold`.
  - [x] `Serve(ctx) error` — `time.NewTimer(nextDelay())` loop; on fire, blink if idle, then `timer.Reset(nextDelay())`; `ctx.Done()` → return `ctx.Err()`.
  - [x] `blinkOnce(ctx)` — push eyes-closed, wait `blinkDuration` (ctx-interruptible), push eyes-open. Local `Face` var is the seam for 2.4's mood expression; push errors logged, not fatal.

- [x] **Task 2 — Reset idleness on interaction (`core/dispatch/`)** (AC: 1)
  - [x] Added `*state.Store` to `Dispatcher` and `dispatch.New(hub, arb, inbound, store)`; `store.Touch()` called on each inbound message **before** the arbiter submit. `core/dispatch`→`core/state` is core→core (import test unaffected).

- [x] **Task 3 — Wire into `cmd/shelldon/main.go`** (AC: 1, 2)
  - [x] `rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))`; `blink := reflexes.NewBlink(comp, store, rng)`.
  - [x] `dispatch.New(hub, arb, inbound, store)`.
  - [x] `root.Add(supervisor.Guard("reflex-blink", blink.Serve))` after `display-terminal` (drained first).

- [x] **Task 4 — Tests (`testing/synctest`, stdlib, no testify)** (AC: 1, 2)
  - [x] `core/reflexes/blink_test.go`: AC1 `TestServe_BlinksWhenIdle` (synctest — past idle threshold + several intervals → eyes-closed AND eyes-open frames pushed); AC2 `TestNextDelay_Jittered` (seeded rng → values vary, all in range); `TestIdle_GatedByThreshold` (synctest — not idle initially, idle after threshold). The idle gate is proven via `idle()` directly rather than a fragile Serve-timing assertion.
  - [x] `core/dispatch/dispatch_test.go` (new): `TestServe_TouchesStateOnInbound` — old `LastInteraction`, one inbound through `Serve`, reply received, assert `LastInteraction` advanced.
  - [x] `transport/cli/cli_test.go` updated for the new `dispatch.New` signature (regression fix from Task 2).
  - [x] `go test -race ./...` passes; native + arm64 `CGO_ENABLED=0` builds succeed; `golangci-lint run` → 0 issues (`.golangci.yml` unchanged).

### Review Findings

- [x] [Review][Patch] Eyes left closed if ctx cancelled mid-blink — `blinkOnce` returns on `ctx.Done()` during `blinkDuration` sleep without pushing `EyesOpen: true`, leaving the terminal frozen with eyes closed on any cancellation (shutdown or supervisor restart). Fix: push eyes-open unconditionally before return. [`core/reflexes/blink.go:81-100`] — FIXED: the eyes-open push now runs after both the `ctx.Done()` and `timer.C` branches (reflex edge drains before the renderer, so the push still lands). Guarded by `TestBlinkOnce_ReopensEyesEvenWhenCancelled`.
- [x] [Review][Defer] `hub.Publish` blocks indefinitely if display channel full/renderer stopped — `PushFace` has no context-aware escape; blink respects ≤2 pushes/cycle per 2.2 constraint. [`core/reflexes/blink.go:83,97`] — deferred, pre-existing
- [x] [Review][Defer] PCG seed second word always 0 — `rand.NewPCG(uint64(time.Now().UnixNano()), 0)` fixes stream-select; jitter is present but entropy is half the PCG space. [`cmd/shelldon/main.go`] — deferred, pre-existing
- [x] [Review][Defer] Wrong-kind envelope on inbound skips `store.Touch()` — type assertion before Touch; theoretical since inbound only receives `KindInboundMessage`. [`core/dispatch/dispatch.go:49`] — deferred, pre-existing
- [x] [Review][Defer] `dispatch_test.go` `<-outbound` has no timeout — test hangs if stub fails to respond. [`core/dispatch/dispatch_test.go:41`] — deferred, pre-existing
- [x] [Review][Defer] Supervisor restart while idle → immediate blink on first timer fire; cosmetic only. [`core/reflexes/blink.go:61`] — deferred, pre-existing

## Dev Notes

### Architecture constraints (binding)

- **AD-13 — Reflexes are the cheap cost tier.** "**reflex jobs** (mood drift, blink) run **in-core, no LLM, cheap CPU**." The blink is a reflex job: no worker invocation, no broker, no LLM. "Incoming messages/events bypass the scheduler (immediate)" — the `Touch()` on inbound is core's immediate handling, not a scheduled job. [Source: ARCHITECTURE-SPINE.md#AD-13]
- **AD-13 / Story 2.5 — one tier-shaped scheduler, no core-loop refactor.** Story 2.5 adds the reflex-tier scheduler and registers blink/mood-drift as jobs "with NO core-loop refactor — Yui's condition." So this story's blink loop must be a self-contained `Serve(ctx)` the scheduler can later own, not a bespoke control structure. [Source: epics.md#Story 2.5, ARCHITECTURE-SPINE.md#AD-13]
- **AD-16 — RAM state drives reflexes; core is sole writer.** The blink reads `personality-state.LastInteraction` (RAM working copy, AD-16) via `store.Snapshot()`; it never writes state. `dispatch` writes `LastInteraction` via `store.Touch()` — core is the single writer (AD-6). [Source: ARCHITECTURE-SPINE.md#AD-16, AD-6]
- **AD-6 — Display is push-only; blink goes through the compositor.** The reflex pushes frames via `compositor.PushFace`; it never touches the renderer. Each push gets the next monotonic seq; the renderer drops stale frames (built in 2.2). [Source: ARCHITECTURE-SPINE.md#AD-6]
- **AD-10 — synctest for cadence; clock is not a seam here.** "Use `testing/synctest` for deterministic scheduler-cadence tests." The blink uses real `time.NewTimer`; synctest fakes the clock (as in the 2.1 checkpoint loop). No clock interface. Randomness IS injected (`*rand/v2.Rand`) so jitter is deterministic in tests — this is the one external seam this story injects. [Source: ARCHITECTURE-SPINE.md#AD-10]
- **NFR2 / NFR13 — pure-Go, offline.** `math/rand/v2` is stdlib (no dependency). The reflex has no network dependency — it is exactly the "alive with the network down" behavior (NFR13). arm64 `CGO_ENABLED=0` build stays green. [Source: ARCHITECTURE-SPINE.md#NFR2, NFR13]
- **Structural Seed — package placement.** `core/ … reflexes/` is the named seed package. New: `core/reflexes/` (blink now; mood-drift joins in 2.4). [Source: ARCHITECTURE-SPINE.md#Structural Seed]

### Key design decisions

- **One idle-gated blink reflex, not two components.** AC1 ("no inbound for the idle threshold → blink") + AC2 (jitter) describe a single behavior: blink at jittered intervals while idle. `idle()` reads `LastInteraction`; `nextDelay()` jitters. Splitting into separate "blink" and "idle" components would be over-design for M1.
- **`Touch()` on inbound is required for honest idle detection.** Without it, `LastInteraction` only ever set at boot, so the pet would never stop idle-blinking when spoken to — contradicting "react when idle." One line in `dispatch.Serve`. This is the seam 2.1's `Touch()` was built for.
- **Blink frames are neutral-expression for now.** `Mood → Expression` is Story 2.4. Keep `blinkOnce`'s face in a local var so 2.4 swaps the expression source (read `store.Snapshot().Mood` → expression) without reshaping the blink.
- **Injected `*rand/v2.Rand` for deterministic jitter.** Tests seed it; `main` seeds from `time.Now().UnixNano()`. Asserting "not all equal" + range bounds proves jitter without flakiness.

### Previous story intelligence (Stories 2.1, 2.2)

- **synctest cadence pattern (mirror the 2.1 checkpoint loop):** `synctest.Test(t, func)`, start the loop in a goroutine, `time.Sleep(...)` to fake-advance, `synctest.Wait()` before asserting, cancel + join the goroutine before the bubble returns (a lingering goroutine fails the bubble). Construct any `time.Now()`-dependent state **inside** the bubble. [Source: core/state/checkpoint_test.go, core/state/checkpoint.go:61]
- **Supervised-loop shape (mirror checkpoint/dispatch/cli):** thin `Serve(ctx) error` returning `ctx.Err()` on cancel, wrapped by `supervisor.Guard`. Use `time.NewTimer`/`NewTicker` + `select` with `ctx.Done()`. [Source: core/state/checkpoint.go, core/dispatch/dispatch.go, cmd/shelldon/main.go]
- **Compositor API:** `compositor.New(hub)`; `PushFace(contracts.Face{Expression, EyesOpen}) error` publishes `KindFaceSnapshot`. Blink toggles `EyesOpen`. [Source: core/compositor/compositor.go]
- **State API:** `store.Snapshot().LastInteraction` (read), `store.Touch()` (stamp now). Both lock-guarded; safe from the reflex goroutine concurrently with dispatch. [Source: core/state/state.go:56,71]
- **main start/drain order:** `state-checkpoint`, `core-dispatch`, `cli-transport`, `display-terminal`. Add `reflex-blink` last. [Source: cmd/shelldon/main.go]
- **Review finding carried from 2.2 (watch this):** `hub.Publish` is a blocking send; the display channel is buffered at 16. The blink pushes ≤2 frames per cycle and the renderer drains continuously, so this is safe — but do not introduce a tight push loop that could outrun the 16-slot buffer before the renderer consumes. [Source: 2-2 Review Findings]
- **No new dependency** since 1.6 (suture/v4 + renameio/v2). 2.3 adds none (`math/rand/v2` is stdlib). [Source: go.mod]

### Project Structure Notes

- New: `core/reflexes/` (`blink.go`, `blink_test.go`), `core/dispatch/dispatch_test.go`.
- Modified: `core/dispatch/dispatch.go` (add `*state.Store`, `Touch()` on inbound), `cmd/shelldon/main.go` (rng, `NewBlink`, pass `store` to `dispatch.New`, register `reflex-blink` edge).
- `.golangci.yml` unchanged. No `go.mod`/`go.sum` change.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.3] — ACs; "feels alive between turns even with the network down"
- [Source: ...ARCHITECTURE-SPINE.md#AD-13] — reflex cost tier (in-core, no LLM); one tier-shaped scheduler (2.5 owns cadences, no refactor)
- [Source: ...ARCHITECTURE-SPINE.md#AD-16] — RAM state drives reflexes; LastInteraction is the idle signal
- [Source: ...ARCHITECTURE-SPINE.md#AD-6] — display push-only via compositor; monotonic seq, drop stale
- [Source: ...ARCHITECTURE-SPINE.md#AD-10] — testing/synctest for cadence; inject external seams (rng), no monkeypatch
- [Source: ...ARCHITECTURE-SPINE.md#NFR13] — degrade-gracefully-offline; reflexes are the offline aliveness
- [Source: core/state/state.go, core/compositor/compositor.go, core/state/checkpoint.go] — Touch/Snapshot, PushFace, the synctest loop pattern to mirror

## Dev Agent Record

### Agent Model Used

claude-opus-4-8[1m] (Claude Opus 4.8, 1M context) via Claude Code dev-story workflow.

### Debug Log References

- TDD: `core/reflexes/blink_test.go` first (RED — `NewBlink`/constants undefined), then `blink.go` to GREEN; 3 tests pass under `-race`.
- `math/rand/v2`: the injected `*rand.Rand` exposes `Int64N` (the generic `rand.N` is package-level only), so `nextDelay` uses `time.Duration(rng.Int64N(int64(blinkJitter)))`.
- synctest: idle/blink tests construct the `state.Store` **inside** the bubble so `LastInteraction = time.Now()` uses bubble time; the Serve goroutine is cancelled + joined (`<-done`) before the bubble returns. The blink test channel is buffered (64) so the renderer-less test never blocks `PushFace`.
- **Regression fixed:** adding `*state.Store` to `dispatch.New` broke `transport/cli/cli_test.go` (old 3-arg call) — updated it to construct a store. Caught by the build/lint gate, not shipped.

### Completion Notes List

- **AC1 satisfied (idle → blink).** `Blink.Serve` waits a jittered delay and, when `idle()` (no interaction for `blinkIdleThreshold`), pushes an eyes-closed frame then reopens. `TestServe_BlinksWhenIdle` proves a blink (eyes-closed) frame and a reopen frame are pushed once idle, under `testing/synctest`.
- **AC2 satisfied (jittered intervals).** `nextDelay` = base + `rng.Int64N(jitter)`; `TestNextDelay_Jittered` proves successive delays vary and stay in `[base, base+jitter)`.
- **Idle gate honest end-to-end.** `idle()` reads `LastInteraction`; `dispatch` stamps `store.Touch()` on every inbound message (before the arbiter submit) so an active conversation resets idleness and pauses ambient blinking. `TestIdle_GatedByThreshold` proves the gate; `core/dispatch/dispatch_test.go` proves the `Touch` wiring.
- **Reflex-tier, offline (AD-13/NFR13).** No worker, no broker, no LLM, no network — pure in-core aliveness. It pushes through the 2.2 compositor (monotonic seq, renderer drops stale); the compositor/renderer are unchanged.
- **Shaped for Story 2.5.** The blink is a thin `Serve(ctx)` loop wrapped by `supervisor.Guard`, so the reflex-tier scheduler can register it as a job with no rewrite (Yui's no-core-loop-refactor condition).
- **Scope held:** blink reflex + idle reset + wiring only. No mood-drift / `Mood→Expression` (2.4 — blink frames are neutral; the local `Face` var is the seam), no scheduler (2.5), no other idle behaviors, no compositor/renderer changes. The 2.2 back-pressure caveat is respected (≤2 pushes/cycle, well under the 16-slot buffer).
- **No new dependency** (`math/rand/v2` is stdlib). Native + arm64 `CGO_ENABLED=0` builds green.
- **Validation:** `go test -race -count=1 ./...` → all packages pass, no data race (added `core/reflexes` + `core/dispatch` behavior tests, 38 test funcs total); `golangci-lint run` → 0 issues.

### File List

- `core/reflexes/blink.go` (new) — idle-gated jittered blink reflex (`Blink`, `NewBlink`, `nextDelay`, `idle`, `Serve`, `blinkOnce`)
- `core/reflexes/blink_test.go` (new) — jitter, idle-gate (synctest), idle→blink (synctest) tests
- `core/dispatch/dispatch.go` (modified) — `Dispatcher` gains `*state.Store`; `Touch()` on each inbound message
- `core/dispatch/dispatch_test.go` (new) — inbound stamps `LastInteraction`
- `transport/cli/cli_test.go` (modified) — updated for the new `dispatch.New` signature (regression fix)
- `cmd/shelldon/main.go` (modified) — seed rng, construct `NewBlink`, pass `store` to `dispatch.New`, register supervised `reflex-blink` edge
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (modified) — story status → in-progress → review

## Change Log

| Date       | Change                                                                 |
|------------|------------------------------------------------------------------------|
| 2026-06-21 | Added the pet's first resident reflex: `core/reflexes.Blink`, an idle-gated jittered blink that pushes eyes-closed→open face frames through the 2.2 compositor while idle (in-core, no LLM, offline aliveness — AD-13/NFR13). `dispatch` now stamps `state.Touch()` on each inbound message so conversation resets idleness (the seam 2.1's `Touch()` was built for). Jitter via an injected `math/rand/v2.Rand`; cadence verified under `testing/synctest`. Wired into `main` as the supervised `reflex-blink` edge. No new dependency; native + arm64 builds green, `-race` suite passes, lint 0 issues (Story 2.3). |
| 2026-06-21 | Code review: 1 patch resolved (eyes now reopen unconditionally even when a blink is cancelled mid-flight, guarded by `TestBlinkOnce_ReopensEyesEvenWhenCancelled`), 5 findings deferred. Gate re-run green. |
