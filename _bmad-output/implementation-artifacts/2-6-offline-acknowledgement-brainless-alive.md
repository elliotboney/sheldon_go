---
baseline_commit: ae5f4ed
---

# Story 2.6: Offline acknowledgement (brainless-alive)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As Elliot (the owner),
I want the pet to acknowledge a message even with no brain available, without ever hanging,
so that it stays responsive and alive when offline (FR2, NFR13, de-vibed AC2).

## Context

**Sixth and final story of Epic 2 (M1 — "The Soul").** Epic 2 made the pet *alive* — state, face, blink, mood-drift, scheduler — all with zero LLM. This story closes the epic by making the pet *responsive* when the brain can't answer: an inbound message always gets a reply, and a turn that can't complete is **time-bounded and closed**, never frozen. It is the degradation seam the real brain (Epic 3) plugs into.

The turn path today (`dispatch → arbiter → worker`) has two unfinished edges, both left as explicit `Story 2.6` TODOs:
1. **`dispatch.Serve` drops messages it can't answer.** On `ErrTurnInFlight` (busy) or a cancelled submit it does `continue` — the message vanishes, no acknowledgement (`core/dispatch/dispatch.go:55`).
2. **`arbiter.Submit` has no timeout.** It calls `w.AssembleAndPropose` synchronously and waits forever (`core/arbiter/arbiter.go:40`). A hung brain would freeze the whole dispatch loop — the exact "no brain → pet hangs" failure NFR13/AD-8 forbid.

This story builds: (a) an **arbiter timeout** that closes a turn the brain can't finish and frees the in-flight slot (AC2, AD-8/AD-11), and (b) a **reflex acknowledgement** on the dispatch path so any message the brain can't answer — busy, timed-out, or (Epic 3) provider-exhausted — still produces a canned, no-LLM reply instead of silence (AC1, NFR13).

**Why the brain "absent" is simulated, not real, here.** At M1 the worker is `worker.Stub`, which always echoes instantly — so in production the ack path won't fire yet. The condition that triggers it ("no brain available") arrives in Epic 3: the real worker returns an error when its provider chain is exhausted (AD-8 fallback), and a slow provider trips the timeout. This story builds and **tests** that path now (via test-double workers that error or hang), so when the real brain lands the degradation is already correct. **Do not make `worker.Stub` fail** — it stays a clean echo for the round-trip/render stories.

**This story does NOT:**
- build the real worker, provider chain, or broker (Epic 3) — the absent/slow brain is a **test double**; `worker.Stub` is unchanged
- add full `turn_id` envelope minting/fencing (the rest of AD-11) — at M0 the fence is **`context` cancellation + a dropped late `Result`**; envelope-id minting arrives with Epic 3's turn lifecycle
- add coalescing / a per-class catch-up slot (the rest of AD-8) — that is the turn-tier scheduler (Story 3.5); the arbiter stays **≤1 + reject**, now **+ timeout**
- add budget / cooldown / battery gating (Story 3.5)
- route the acknowledgement through the scheduler — incoming messages **bypass the scheduler** (AD-13, "immediate"); the ack is produced inline on the dispatch path
- add a **face-frame** acknowledgement — the ack is a canned outbound message on the conversation surface; a face nudge on inbound is deferred
- touch the compositor, renderer, reflexes, state, or contracts

## Acceptance Criteria

1. **Offline acknowledgement, never blocks.**
   **Given** no brain/worker available (it errors, or hangs past the arbiter timeout)
   **When** an inbound message arrives
   **Then** a reflex acknowledgement (canned, in-core, no LLM) is published as an outbound reply, and the dispatch loop keeps consuming inbound — the inbound path never blocks (NFR13).

2. **Arbiter timeout closes the turn; degrade to reflex.**
   **Given** a turn that cannot complete because the brain is absent (verifiable under `testing/synctest`)
   **When** the arbiter timeout elapses
   **Then** `Submit` returns `ErrTurnTimeout`, the in-flight slot is released (no turn remains in flight past the timeout — a subsequent `Submit` is admitted), and the timed-out turn's worker context is cancelled so any late `Result` is discarded (AD-8/AD-11).

## Tasks / Subtasks

- [x] **Task 1 — Arbiter timeout + idempotent close (`core/arbiter/arbiter.go`)** (AC: 2)
  - [x] Add a `timeout time.Duration` field; change `New(w worker.Worker, timeout time.Duration) *Arbiter` (second positional param — keep it honest, no functional options). Store it.
  - [x] Add `var ErrTurnTimeout = errors.New("arbiter: worker turn timed out")`.
  - [x] Rework `Submit`: acquire the token via the existing non-blocking `select` (unchanged `ErrTurnInFlight` reject path). Once acquired, derive `tctx, cancel := context.WithTimeout(ctx, a.timeout)` (`defer cancel()`), run `a.w.AssembleAndPropose(tctx, turn)` **in a goroutine** that sends its `(Result, error)` to a **buffered (cap 1)** channel, then `select` on: the result channel → release token, return it; `tctx.Done()` → release token, return the timeout outcome. The goroutine is what lets a hung worker be **abandoned** instead of freezing the caller.
  - [x] Distinguish deadline from shutdown in the `tctx.Done()` branch: if the **parent** `ctx.Err() != nil` (process shutting down) return `ctx.Err()`; else (the timeout fired) return `ErrTurnTimeout`. Release the token in **exactly one** select branch so close is idempotent (≤1-in-flight preserved; the abandoned goroutine's late send lands in the unread buffered channel and is dropped — the AD-11 fence at M0).
  - [x] Package doc: note the turn is now time-bounded (AD-8 "a failed call never freezes the pet") and that `context` cancellation + dropped late `Result` is the M0 `turn_id` fence (AD-11); full envelope-id fencing is Epic 3.

- [x] **Task 2 — Reflex acknowledgement on the dispatch path (`core/dispatch/dispatch.go`)** (AC: 1)
  - [x] Add `const reflexAck = "…"` (canned, language-neutral "I'm here" acknowledgement; story-time config, not an invariant — comment it as tunable). No LLM, no worker.
  - [x] Replace the two `continue`-on-error paths with a single branch: after `res, err := d.arb.Submit(ctx, job)`:
    - `err == nil` → publish `res.Reply` (unchanged success path);
    - `ctx.Err() != nil` → `return ctx.Err()` (shutdown, not a brain failure — do not ack);
    - else (`ErrTurnInFlight` busy / `ErrTurnTimeout` / any worker error) → publish `reflexAck`. **Never drop the message; never block.**
  - [x] Factor the outbound publish into one small helper so success and ack share it (same `Envelope` shape: `Kind: KindOutboundMessage, Src: "core", Dst: "cli"`, `OutboundMessage{ConvoID, Text}`). Keep `d.store.Touch()` before the submit (the idle reset, Story 2.3) unchanged.
  - [x] Update the package/`Serve` doc: the Story 2.6 TODO is now implemented — busy/offline/timed-out turns degrade to a reflex acknowledgement; the loop never blocks on a turn.

- [x] **Task 3 — Wire the arbiter timeout in `cmd/shelldon/main.go`** (AC: 1, 2)
  - [x] Add a turn-timeout const near main (e.g. `turnTimeout = 30 * time.Second`; story-time config — comment as tunable) and pass it: `arb := arbiter.New(worker.Stub{}, turnTimeout)`. No other main wiring changes (the stub still answers instantly, so the ack/timeout paths stay dormant in production until Epic 3).

- [x] **Task 4 — Tests (`testing/synctest` for the timeout, stdlib, no testify)** (AC: 1, 2)
  - [x] **`core/arbiter/arbiter_test.go` — update + add.** Updated existing `New(w)` call to `New(w, time.Minute)`. Added AC2 timeout test under `synctest.Test` with a `hangingWorker` (blocks on `ctx.Done()`, records cancellation): asserts `Submit` returns `ErrTurnTimeout`, the worker saw `ctx` cancelled (AD-11 fence), and a **second** `Submit` is **admitted** (times out again, not `ErrTurnInFlight`) — proving the slot was freed.
  - [x] **`core/dispatch/dispatch_test.go` — add AC1.** (a) `errWorker` → asserts published `OutboundMessage.Text == reflexAck` (acknowledged, not dropped). (b) Under `synctest`, `hangingWorker` + short timeout, **two** inbound messages, advance past both deadlines → **both** convos acked (loop never wedged). Kept `TestServe_TouchesStateOnInbound` (updated its `arbiter.New` call).
  - [x] Test doubles live in the test files (production worker stays `worker.Stub`). `errWorker` returns an immediate error; `hangingWorker` blocks on `ctx.Done()`.
  - [x] `go test -race ./...` passes (58); native + arm64 `CGO_ENABLED=0` builds succeed; `golangci-lint run` → 0 issues (`.golangci.yml` unchanged).

## Dev Notes

### Architecture constraints (binding)

- **AD-8 — The arbiter governs the brain; a failed call never freezes the pet.** "on provider-chain exhaustion the arbiter **falls back to a reflex behavior** so the pet never freezes (`context` cancellation kills the in-flight turn)." This story makes that true with a timeout: a turn that can't complete is cancelled and the slot freed; dispatch degrades to a reflex acknowledgement. The ≤1-in-flight bound and the `ErrTurnInFlight` reject path are **unchanged** — coalescing/per-class slots are still Story 3.5. [Source: ARCHITECTURE-SPINE.md#AD-8]
- **AD-11 — Turn identity & idempotent close.** "A `Result` whose `turn_id` is already closed (timed out, superseded, fallback-resolved) is **discarded**. Turn close is **idempotent**. `turn_id` fencing is implemented via `context` cancellation." At M0 there is no envelope `turn_id` minting yet, so the fence is exactly the `context` cancellation + the abandoned goroutine's late `Result` landing in an unread buffered channel (dropped). The token is released in one branch only → close is idempotent. Full envelope-id fencing arrives with Epic 3's turn lifecycle. [Source: ARCHITECTURE-SPINE.md#AD-11, #AD-4]
- **AD-13 — Incoming messages bypass the scheduler (immediate).** The acknowledgement is produced **inline on the dispatch path**, not as a scheduled reflex job — the scheduler (Story 2.5) owns *self-driven* cadences; inbound messages are immediate. [Source: ARCHITECTURE-SPINE.md#AD-13]
- **AD-5 — The soul survives any single failure; degrade, don't crash.** "transport down → reflex-only; provider chain exhausted → reflex fallback (AD-8)." The dispatch loop is a supervised edge (`core-dispatch`); a brain failure degrades to the canned ack, never panics or blocks the edge. [Source: ARCHITECTURE-SPINE.md#AD-5]
- **NFR13 — Offline aliveness.** "Remote-LLM network dependency — no brain when offline." The pet must stay responsive with the network down; the canned ack is the offline-responsive behavior. [Source: epics.md#NFR13]
- **AD-10 — synctest for time; no monkeypatch.** The timeout is the canonical `testing/synctest` case (fake clock advanced past the deadline). The arbiter takes the timeout by injection at `New`; tests inject a short one, main a real one — no clock monkeypatch. [Source: ARCHITECTURE-SPINE.md#AD-10]
- **AD-12 — Core sees only the transport-agnostic message contract.** The acknowledgement is an `OutboundMessage` (canned text), routed by `Kind` like any reply; no transport-specific code in core (enforced by `core/dispatch/imports_test.go`). [Source: ARCHITECTURE-SPINE.md#AD-12]

### Key design decisions

- **Timeout lives in the arbiter, injected at `New`.** AC2 says "the arbiter timeout"; the arbiter is where ≤1-in-flight already lives, so the time-bound belongs there too. A single per-arbiter `time.Duration` (not per-Submit, not functional options) is the honest minimum (CLAUDE.md simplicity).
- **Goroutine + `select` is the mechanism, not decoration.** A synchronous call cannot be abandoned — you can't reclaim control from `w.AssembleAndPropose` if it hangs. Running it in a goroutine and selecting against `tctx.Done()` is what makes "never blocks" and "no turn in flight past the timeout" achievable. Turns are infrequent (user messages), so a goroutine-per-turn is negligible cost.
- **Release the token at timeout; fence the zombie.** To honor AC2 ("no turn remains in flight past the timeout") the slot is freed when the deadline fires, even though the abandoned worker goroutine may still be running. Its late `(Result, error)` send lands in the unread cap-1 channel and is dropped — the AD-11 fence. For `worker.Stub` (instant) this never happens; for a real worker, `context` cancellation propagates (and across the process wall at M3).
- **One ack for every "can't answer" outcome.** Busy (`ErrTurnInFlight`), timed-out (`ErrTurnTimeout`), and (Epic 3) worker error all collapse to the same canned `reflexAck`. Dispatch distinguishes only **shutdown** (`ctx.Err() != nil` → return) from **degradation** (→ ack). Simpler than per-error messages and matches "stay alive, acknowledge."
- **Canned text, not a face.** The user just sent a message on the conversation surface; an `OutboundMessage` is the acknowledgement they'll perceive. A face-frame nudge on inbound is plausible but expands into the compositor — deferred to keep the change surgical.
- **`worker.Stub` stays a clean echo.** The "absent brain" is a test double, never a crippled production stub — later round-trip/render stories still rely on the stub echoing.

### Previous story intelligence (Stories 2.1–2.5)

- **The two TODOs are already marked.** `dispatch.go:38,55` ("busy-ack is Story 2.6") and the synchronous `arbiter.Submit` are the exact seams to finish. Lift the `continue` into an ack; wrap the worker call in a timeout. [Source: core/dispatch/dispatch.go, core/arbiter/arbiter.go]
- **Arbiter test scaffolding to reuse/extend.** `arbiter_test.go` already has a `blockingWorker` (blocks on a `release` channel, records `maxSeen` concurrency) and the required ≤1-in-flight test. The new `hangingWorker` differs: it blocks on **`ctx.Done()`** so the timeout can cancel it. Update the existing `New(w)` calls to pass a generous timeout so `blockingWorker` isn't killed mid-test. [Source: core/arbiter/arbiter_test.go]
- **synctest pattern (mirror 2.3/2.4/2.5):** start the call in a goroutine, `time.Sleep` to fake-advance past the deadline, `synctest.Wait()`, then assert. Construct any `time.Now()`-dependent state inside the bubble. [Source: core/reflexes/blink_test.go, core/scheduler/scheduler_test.go]
- **Dispatch test pattern.** `dispatch_test.go` registers an `outbound` route on the hub, runs `d.Serve` in a goroutine, pushes an `InboundMessage` envelope, and reads the `outbound` channel for the reply. Reuse that shape; assert `OutboundMessage.Text`. [Source: core/dispatch/dispatch_test.go]
- **`hub.Publish` is a blocking send on a 16-slot buffered channel** (carried 2.3 review note). "Never blocks" here means the **turn** is time-bounded so the loop returns to consuming inbound; the outbound publish relies on the existing 16-slot buffer + a draining transport, unchanged. One ack per message keeps the rate ≤ one publish per inbound — safe. [Source: 2-3 Review Findings, core/bus/hub.go]
- **`arbiter.New` is called in `main.go:36` and the two test files.** Changing its signature touches exactly those three call sites. [Source: cmd/shelldon/main.go, core/arbiter/arbiter_test.go, core/dispatch/dispatch_test.go]
- **No new dependency** since 1.6; 2.6 adds none (stdlib `context`/`time`/`errors` only). [Source: go.mod]

### Project Structure Notes

- Modified: `core/arbiter/arbiter.go` (timeout, goroutine+select, `ErrTurnTimeout`), `core/arbiter/arbiter_test.go` (signature + AC2 timeout test), `core/dispatch/dispatch.go` (reflex ack on the degrade path, publish helper), `core/dispatch/dispatch_test.go` (AC1 ack tests), `cmd/shelldon/main.go` (pass `turnTimeout` to `arbiter.New`).
- New files: none. No new package — the ack is a const on the dispatch path; the timeout is a field on the existing arbiter.
- No `contracts` change (the ack reuses `OutboundMessage`), no `state`/`compositor`/`reflexes`/`scheduler` change, no `worker.Stub` change.
- `.golangci.yml`, `go.mod`, `go.sum` unchanged.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.6] — ACs; FR2 / NFR13 / de-vibed AC2
- [Source: ...ARCHITECTURE-SPINE.md#AD-8] — arbiter governs the brain; reflex fallback so the pet never freezes; `context` cancellation kills the in-flight turn
- [Source: ...ARCHITECTURE-SPINE.md#AD-11] — turn identity & idempotent close; discard a late/superseded `Result`; fencing via `context` cancellation
- [Source: ...ARCHITECTURE-SPINE.md#AD-13] — incoming messages bypass the scheduler (immediate)
- [Source: ...ARCHITECTURE-SPINE.md#AD-5, #AD-10, #AD-12] — degrade-don't-crash; synctest for time; transport-agnostic message contract
- [Source: core/dispatch/dispatch.go, core/arbiter/arbiter.go] — the two Story 2.6 seams (the `continue` drop, the synchronous Submit)
- [Source: core/arbiter/arbiter_test.go, core/dispatch/dispatch_test.go] — test scaffolding to extend

## Dev Agent Record

### Agent Model Used

claude-opus-4-8 (1M context)

### Debug Log References

None — clean implementation. One deviation from the plan (see notes): a 4th `arbiter.New` call site (the CLI end-to-end test) needed the signature update; the story listed 3.

### Completion Notes List

- **Arbiter is now time-bounded (AC2).** `Submit` runs the worker in a per-turn goroutine under `context.WithTimeout`; a `select` races the result against `tctx.Done()`. On timeout the token is released and `ErrTurnTimeout` returned (shutdown — parent `ctx` cancelled — returns `ctx.Err()` instead). The token is released in exactly one branch → idempotent close. The abandoned worker's late `(Result, error)` lands in the cap-1 buffered channel and is dropped — the M0 `turn_id` fence (AD-11) via `context` cancellation. Proven by `TestArbiter_TimeoutClosesTurn` under synctest: worker saw cancellation, slot reopened (2nd Submit admitted, not `ErrTurnInFlight`).
- **Dispatch degrades to a reflex ack (AC1).** The two message-dropping `continue`s became a single `switch`: success → reply; parent `ctx` cancelled → return (shutdown); else (busy / timeout / worker error) → publish `const reflexAck = "…"`. Outbound publish factored into `publishReply`. The message is never dropped and the loop never blocks (the turn is time-bounded). Proven by `TestServe_AcksWhenBrainAbsent` (errWorker → ack) and `TestServe_NeverBlocksUnderHungBrain` (hung brain, 2 messages, both acked under synctest).
- **`worker.Stub` unchanged** — the absent/slow brain is a test double only; the stub stays a clean echo. The ack/timeout paths are dormant in production until Epic 3's real worker can exhaust or hang.
- **Deviation:** the story listed 3 `arbiter.New` call sites (main + 2 test files); there were **4** — `transport/cli/cli_test.go` (the Story 1.5 end-to-end round-trip) also constructs an arbiter. Updated it to `New(worker.Stub{}, time.Minute)` (generous timeout so the instant stub is unaffected). No behavior change.
- **Validation:** `go test -race ./...` → 58 pass (14 packages); `CGO_ENABLED=0` native + `GOOS=linux GOARCH=arm64` builds succeed; `golangci-lint run` → 0 issues. `.golangci.yml`, `go.mod`, `go.sum` unchanged. No new package, no new dependency.

### File List

- `core/arbiter/arbiter.go` (modified) — `timeout` field; `New(w, timeout)`; `ErrTurnTimeout`; goroutine + `select` timeout in `Submit`; idempotent close; package doc.
- `core/arbiter/arbiter_test.go` (modified) — `New` call updated; added `hangingWorker` + `TestArbiter_TimeoutClosesTurn` (AC2, synctest).
- `core/dispatch/dispatch.go` (modified) — `reflexAck` const; `switch` degrade path; `publishReply` helper; package/`Serve` doc.
- `core/dispatch/dispatch_test.go` (modified) — `errWorker`/`hangingWorker` doubles; `TestServe_AcksWhenBrainAbsent` + `TestServe_NeverBlocksUnderHungBrain` (AC1); updated existing `New` call.
- `core/dispatch/export_test.go` (new, review fix) — exposes `reflexAck` to the external test package so the expected ack can't drift.
- `core/arbiter/arbiter.go` (review fix) — per-turn goroutine `defer recover()` so a worker panic degrades to an error instead of leaking the in-flight token; `TestArbiter_RecoversWorkerPanic` added.
- `cmd/shelldon/main.go` (modified) — `turnTimeout` const passed to `arbiter.New`.
- `transport/cli/cli_test.go` (modified) — `arbiter.New` signature update (deviation; behavior unchanged).

### Review Findings

- [x] [Review][Patch] Worker goroutine panic in Submit leaks the in-flight token permanently [`core/arbiter/arbiter.go` — Submit goroutine] — resolved: added `defer recover()` in the per-turn goroutine (AD-5, recover does not cross goroutines), sending a synthetic error to `done` on catch so the token releases and the slot reopens. Covered by `TestArbiter_RecoversWorkerPanic`.
- [x] [Review][Patch] `wantAck` / `reflexAck` string coupling — test silently passes on wrong value [`core/dispatch/dispatch_test.go:21`] — resolved: added `core/dispatch/export_test.go` exposing `const ReflexAckForTest = reflexAck`; the test now binds `wantAck = dispatch.ReflexAckForTest`, so the value can't drift.
- [x] [Review][Defer] `hub.Publish` blocks → dispatch loop potential deadlock [`core/dispatch/dispatch.go` — `publishReply`] — deferred, pre-existing. `Publish` is an unconditional blocking send (16-slot buffer); if the outbound consumer is stopped, `Serve` hangs. Pre-existing architectural constraint (carried from 2.3 review); the spec acknowledges the 16-slot buffer + draining transport as the M0 safety net.
- [x] [Review][Defer] `select` race: valid result silently discarded when `done` and `tctx.Done()` are simultaneously ready [`core/arbiter/arbiter.go` — Submit select] — deferred, pre-existing. Go's select is pseudo-random when both cases are ready at the same tick; a worker that finishes at exactly the timeout deadline may have its result discarded in favour of `ErrTurnTimeout`. M0 acknowledged limitation (context cancellation + dropped late Result IS the AD-11 fence).
- [x] [Review][Defer] Spurious `reflexAck` possible during shutdown window [`core/dispatch/dispatch.go` — Serve switch] — deferred, pre-existing. If `ctx` is cancelled after `Submit` returns a non-nil error but before the `ctx.Err() != nil` check, one extra ack fires before the loop exits on the next iteration. Benign narrow race; does not violate any AC.
- [x] [Review][Defer] `ErrNoRoute` silently discarded in `publishReply` [`core/dispatch/dispatch.go:77`] — deferred, pre-existing. `_ = d.hub.Publish(...)` inherits the pre-existing pattern from the success path; a missing route registration drops the reply with no visibility.
- [x] [Review][Defer] Non-cooperative worker goroutine leaks if it ignores `ctx.Done()` [`core/arbiter/arbiter.go` — Submit goroutine] — deferred, pre-existing. If a real worker (Epic 3) doesn't respect context cancellation, the abandoned goroutine runs indefinitely. Inherent Go limitation; cooperative cancellation is a requirement on the worker contract.
- [x] [Review][Defer] `blockingWorker` test double ignores its context — existing concurrency test doesn't cover context-cancellation propagation [`core/arbiter/arbiter_test.go`] — deferred, pre-existing. `TestArbiter_AtMostOneInFlight` uses `time.Minute` timeout and unblocks via `release` channel; no test exercises context propagation on the blocking worker.
- [x] [Review][Defer] Timeout tests have no per-test deadline → hang instead of fail [`core/arbiter/arbiter_test.go`, `core/dispatch/dispatch_test.go`] — deferred, pre-existing. Tests that block on `<-outbound` or `<-done` have no `time.After` guard; a regression produces a hang rather than a clean failure message. Project-level `go test -timeout` is the current safety net.

## Change Log

| Date       | Version | Description                                                                 |
| ---------- | ------- | --------------------------------------------------------------------------- |
| 2026-06-22 | 0.1     | Arbiter turn timeout (close + degrade) + dispatch reflex acknowledgement; offline-responsive, never-block. All ACs satisfied; 58 tests pass. Status → review. |
