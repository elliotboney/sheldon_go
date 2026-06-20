---
baseline_commit: ad81fe849ea313e35e9cb625caff87012d98b49f
---

# Story 1.2: Core-owned channel hub + point-to-point routing

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer building shelldon,
I want a core-owned in-process channel hub that routes a `Job` envelope to its registered destination by `kind`,
so that edges communicate only through the bus and an unknown destination fails safe instead of crashing the soul (AD-1, AD-4).

## Context

This is the **second** Epic 1 story and the **first code that imports the `contracts/` package** from Story 1.1 — so it also validates the `github.com/elliotboney/shelldon_go` module path end-to-end. It stands up the **bus hub** that AD-1 says `core` "hosts": the in-process seam every edge speaks through. Freezing the routing contract now (route by `kind`, fail safe on unknown, never panic) is what lets Stories 1.3 (worker seam + arbiter), 1.4 (suture supervision), and 1.5 (CLI transport) plug in as edges without reshaping the bus.

Keep scope tight. This story is the **dumb router** only: register a destination for a `kind`, deliver an envelope to it, return an error (never panic) when no destination is registered. It does **not** build the worker seam (1.3), the arbiter / ≤1-worker gate (1.3), suture supervision (1.4), the CLI transport (1.5), `turn_id` fencing (1.3/1.4), or broadcast/pub-sub fan-out (a later concern — only point-to-point is in scope here).

## Acceptance Criteria

1. **Point-to-point delivery by kind.**
   **Given** a destination registered for a given `kind` in the point-to-point routing table
   **When** a `Job` addressed by that `kind` is published to the hub
   **Then** it is delivered to exactly that registered destination (and no other).

2. **Unknown destination fails safe.**
   **Given** no destination registered for a `kind`
   **When** a `Job` addressed by that `kind` is published
   **Then** the hub returns a routing error and never panics (AD-4 fail-safe).

3. **No credentials on the bus.**
   **Given** any envelope traversing the hub
   **When** its contents are inspected
   **Then** no credential field is present on the bus (NFR8 — no creds ever on the bus).

## Tasks / Subtasks

- [x] **Task 0 — Create the `core/bus` package** (AC: 1, 2)
  - [x] Create `core/bus/hub.go` with a package doc comment stating: `core` is the LLM-free supervisor root that hosts the bus hub (AD-1); the hub is an in-process Go-channel router passing `contracts.Envelope` values with **no serialization** (AD-4); the channel transport is swappable seed (channel now → UDS+gob at the worker wall in M3) and routing callers must not reshape across that swap
  - [x] Define `Hub` with a routing table `map[contracts.Kind]chan<- contracts.Envelope` guarded by a `sync.RWMutex` (registration vs. publish run on different goroutines — see Dev Notes "Concurrency")
  - [x] Define exported sentinel errors with `errors.New`: `ErrNoRoute` (no destination registered for the kind) and `ErrDuplicateRoute` (a destination is already registered for the kind)
  - [x] Provide a constructor `New() *Hub` that initializes the map
- [x] **Task 1 — Register destinations by kind** (AC: 1)
  - [x] `Register(kind contracts.Kind, dst chan<- contracts.Envelope) error` — store the channel under `kind`
  - [x] Reject a second registration for an already-registered `kind` with `ErrDuplicateRoute` (no silent route clobbering — fail-safe, AD-4). See Dev Notes for why this guard is in scope
  - [x] **RED→GREEN test:** registering a kind succeeds; registering the same kind again returns `ErrDuplicateRoute`; the original destination is unchanged
- [x] **Task 2 — Point-to-point publish by kind** (AC: 1)
  - [x] `Publish(env contracts.Envelope) error` — look up `env.Kind` in the table and deliver the envelope to exactly that channel (a blocking channel send; the registrant owns buffering — see Dev Notes)
  - [x] **RED→GREEN test:** register a receiver channel for `KindJob`; publish an `Envelope{Header{Kind: KindJob, ...}, Payload: Job{...}}`; assert that channel receives a value `reflect.DeepEqual` to what was published, and that a second channel registered for `KindResult` receives nothing
- [x] **Task 3 — Unknown destination fails safe** (AC: 2)
  - [x] `Publish` for a `kind` with no registered destination returns `ErrNoRoute` and does not panic
  - [x] **RED→GREEN test:** publish to a hub with no route for the envelope's kind → assert `errors.Is(err, ErrNoRoute)`; assert the call returns normally (no panic — the test itself failing on panic is sufficient, optionally wrap with a recover-asserting helper)
- [x] **Task 4 — No credentials on the bus (NFR8 guard)** (AC: 3)
  - [x] Add a reflection-based invariant test that walks the field graph of `contracts.Envelope` (including `Header`, `Job`, `Result`, `MemoryOp`) and fails if any field name matches a credential pattern (case-insensitive: `token`, `secret`, `key`, `password`, `passwd`, `credential`, `apikey`, `auth`)
  - [x] This guarantee is structural today (1.1's contracts define no such field); the test guards the invariant as payloads grow. Document that intent in the test
- [x] **Task 5 — Concurrency safety under `-race`** (AC: 1, 2)
  - [x] Test: from multiple goroutines, concurrently `Register` distinct kinds and `Publish` to registered kinds (with receiver goroutines draining), plus `Publish` to unregistered kinds expecting `ErrNoRoute`
  - [x] Run under `go test -race ./core/...`; assert no data race and no panic. (The required ≤1-worker `-race` test is Story 1.3; this is the hub's own race-cleanliness)
- [x] **Task 6 — Verify build + lint** (AC: 1, 2, 3)
  - [x] `go build ./...` and `CGO_ENABLED=0 GOARCH=arm64 go build ./...` both succeed (the hub is the first internal importer of `contracts/` — confirms the module path)
  - [x] `go test ./...` passes and `go test -race ./core/...` passes
  - [x] `golangci-lint run` passes (the existing depguard rule scopes to `**/contracts/**` only; extending the LLM-free fence to `core/` is deferred to Story 3.1 — do not change `.golangci.yml` here)

## Dev Notes

### Architecture constraints (binding)

- **`core` is the LLM-free supervisor root and hosts the bus hub.** "Edges are goroutine actors; `core` (bus hub, arbiter, reflexes, state, memory, scheduler) is the supervisor root and imports no LLM/provider modules." The hub lives under `core/`. [Source: ARCHITECTURE-SPINE.md#AD-1]
- **The bus passes `Envelope`/`Job`/`Result` as uniform Go structs over a core-owned in-process channel hub — NO in-process serialization.** Pass `contracts.Envelope` by value; do not gob-encode inside the hub. [Source: ARCHITECTURE-SPINE.md#AD-4]
- **Transport is swappable seed.** "The TRANSPORT under the seam is swappable seed: channel now → UDS + `encoding/gob` at the worker wall at M3, reshaping no caller." Keep the hub API (Register/Publish over `contracts.Envelope`) transport-agnostic so the M3 swap touches no caller. [Source: ARCHITECTURE-SPINE.md#AD-4]
- **Route by `kind`, not `dst`.** The closed header is `id/v/kind/src/dst/turn_id`; point-to-point routing keys on **`kind` → destination**. AC1 is explicit: "a destination registered for a given `kind` … a `Job` addressed by that `kind` … delivered to exactly that registered destination." `dst` is carried for tracing/observability; it is **not** the routing key in this story. (If multi-destination-per-kind is ever needed, that is a later contract change — do not pre-build it.) [Source: ARCHITECTURE-SPINE.md#AD-4, epics.md#Story 1.2]
- **Fail safe, never panic.** An unknown destination returns a routing error; the hub never panics. This is the hub's own logic-level safety and is distinct from AD-5 suture supervision (which restarts *panicked edge goroutines* and arrives in Story 1.4). The hub must not rely on supervision to be safe. [Source: epics.md#Story 1.2, ARCHITECTURE-SPINE.md#AD-5]
- **No credentials ever on the bus.** `Job` envelopes carry no creds; the broker injects them internally at egress (Story 3.1). No `Envelope`/`Job`/`Result` field may carry a credential. [Source: ARCHITECTURE-SPINE.md#AD-9, SPEC NFR8]

### Recommended hub shape (minimal, idiomatic)

```go
package bus

type Hub struct {
    mu     sync.RWMutex
    routes map[contracts.Kind]chan<- contracts.Envelope
}

func New() *Hub { return &Hub{routes: map[contracts.Kind]chan<- contracts.Envelope{}} }

func (h *Hub) Register(kind contracts.Kind, dst chan<- contracts.Envelope) error // ErrDuplicateRoute on re-register
func (h *Hub) Publish(env contracts.Envelope) error                              // routes by env.Kind; ErrNoRoute if unknown
```

- **Destinations are channels the registrant creates and owns** — the registrant chooses buffered vs. unbuffered. The hub only sends; it does not create channels. This keeps backpressure decisions at the edge and matches "channel hub."
- **`Publish` is a blocking channel send** for a registered kind (natural point-to-point backpressure to a single consumer). Context-aware delivery and `turn_id` fencing (AD-11, via `context` cancellation) are **out of scope here** — they arrive with the worker seam/arbiter in Story 1.3. Do not add a `context` parameter speculatively.
- **`ErrDuplicateRoute` is a deliberate in-scope safety guard**, not scope creep: silently overwriting a route would let a second edge hijack another's traffic — exactly the "fail safe" intent of AD-4. It costs one map lookup and one test.

### Concurrency

- Registration (startup/edge wiring) and publish (steady state) run on **different goroutines**, so the routing table needs a guard. Use `sync.RWMutex`: write-lock in `Register`, read-lock in `Publish` to look up the channel, then release the lock **before** the blocking channel send (do not hold the mutex across the send, or a slow receiver stalls all routing). [Source: ARCHITECTURE-SPINE.md#AD-6 single-writer discipline]
- The hub must be clean under `go test -race` (Task 5). The required ≤1-worker `-race` test belongs to Story 1.3 — do not implement the arbiter here.

### Previous story intelligence (Story 1.1)

- **Conventions to match** (1.1 established them): package doc comment on the primary file; types/behaviors split across small files; **table-driven tests with stdlib `testing` + `reflect.DeepEqual`** — no `testify`; subtests via `t.Run`; helpers marked `t.Helper()`. Mirror this style in `core/bus`. [Source: contracts/ package, contracts/contracts_test.go]
- **`contracts` import path:** `github.com/elliotboney/shelldon_go/contracts`. Types available: `Envelope{Header; Payload}`, `Header{ID, V, Kind, Src, Dst, TurnID}`, `Kind` (`KindJob`, `KindResult`, plus `AllKinds`), `Job{Input, ConvoID}`, `Result{Reply, MemoryOps}`, `MemoryOp{Kind}`. `Payload` is a closed marker interface; concrete payloads are gob-registered via `contracts` `init()`. [Source: contracts/envelope.go, contracts/job.go, contracts/result.go]
- **gob round-trip is already covered in 1.1** — the hub does not serialize, so no new gob tests are needed here. The adversarial-seams review's "test gob from M0" note is satisfied by `contracts/contracts_test.go`.
- **`AllKinds`** exists and the 1.1 review flagged its mutability/sync gap as deferred — do not depend on mutating it; treat it read-only.

### Testing standards

- Table-driven + stdlib `testing`; `reflect.DeepEqual` for envelope equality (mirrors 1.1). No hardware, no network, no clock — pure in-process routing.
- Tests live in `core/bus/hub_test.go` (package `bus`, white-box, matching 1.1's same-package test style).
- For delivery tests, run the receiver in a goroutine (or use a buffered channel of size 1) so the blocking `Publish` send completes; assert receipt with a short select/`<-ch` rather than an unbounded block.
- Run `go test -race ./core/...` for Task 5.

### Project Structure Notes

- New package: `core/bus/` — `hub.go` (+ `hub_test.go`). This is the first `core/` sibling from the Structural Seed (`core/ contracts/ broker/ worker/ transport/ display/ plugins/`). Do **not** scaffold the other siblings — they arrive with their stories. [Source: ARCHITECTURE-SPINE.md#Structural Seed]
- Greenfield, no variance to reconcile. `.golangci.yml` is unchanged this story (its depguard rule matches only `**/contracts/**`).

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.2] — ACs, epic goal
- [Source: _bmad-output/planning-artifacts/architecture/architecture-shelldon_go-2026-06-19/ARCHITECTURE-SPINE.md#AD-1] — LLM-free core hosts the bus hub
- [Source: ...ARCHITECTURE-SPINE.md#AD-4] — uniform Envelope over core-owned in-process channel hub, no serialization, transport-as-seed
- [Source: ...ARCHITECTURE-SPINE.md#AD-5] — suture supervision (Story 1.4; distinct from hub fail-safe)
- [Source: ...ARCHITECTURE-SPINE.md#AD-9] — broker injects creds; none on the bus
- [Source: ...ARCHITECTURE-SPINE.md#Structural Seed] — `core/` package layout
- [Source: _bmad-output/specs/spec-shelldon-go/SPEC.md] — NFR8 (no creds on bus)
- [Source: ...reviews/review-adversarial-seams.md] — broadcast payload-versioning warning (broadcast is out of scope for 1.2; noted for later event-kind stories)

## Dev Agent Record

### Agent Model Used

claude-opus-4-8[1m] (Claude Opus 4.8, 1M context) via Claude Code dev-story workflow.

### Debug Log References

- TDD: wrote `core/bus/hub_test.go` first and confirmed RED (build failed — `New`/`ErrNoRoute`/`ErrDuplicateRoute` undefined), then `core/bus/hub.go` to GREEN.
- `gofmt` initially flagged `hub_test.go` (trailing-comment alignment on the two concurrent `Publish` lines); ran `gofmt -w`. golangci-lint clean.
- **Refined AC3 credential patterns during implementation:** dropped the bare `key` and `auth` tokens I had listed in the task notes — they false-positive on legitimate non-secret fields (`turn_id` is fine, but `author`/`pattern_key`-style names would trip). Kept real credential shapes: `token`, `secret`, `password`, `passwd`, `credential`, `apikey`, `api_key`, `accesskey`, `access_key`, `privatekey`, `private_key`, `bearer`. Rationale documented in the test.

### Completion Notes List

- **AC1 satisfied** — `TestPublish_PointToPoint`: an `Envelope{Kind: KindJob}` published to a hub with `KindJob`→jobCh and `KindResult`→resultCh is delivered (`reflect.DeepEqual`) to jobCh only; resultCh receives nothing. Routing keys on `env.Kind` per the AC; `dst` rides along untouched for tracing.
- **AC2 satisfied** — `TestPublish_NoRoute`: publishing a kind with no registered destination returns `ErrNoRoute` (checked via `errors.Is`) inside a `recover`-guarded call that fails the test on any panic.
- **AC3 satisfied** — `TestEnvelopeCarriesNoCredentials`: reflection walks the full field graph of `Envelope` (→`Header`), `Job`, and `Result` (→`MemoryOp`) and fails on any credential-like field name. Passes — the contracts carry no creds (NFR8).
- **Added safety beyond the raw ACs (cited in spec):** `ErrDuplicateRoute` makes re-registering a kind fail safe instead of silently clobbering a route (AD-4). Covered by `TestRegister_DuplicateRoute`.
- **Concurrency:** `Hub` guards its table with `sync.RWMutex`; `Publish` releases the read lock **before** the blocking channel send so a slow receiver can't stall routing. `TestHub_ConcurrentRaceClean` runs 32 goroutines of concurrent registered+unregistered publishes; green under `go test -race`.
- **First internal consumer of `contracts/`** — confirms the `github.com/elliotboney/shelldon_go` module path resolves end-to-end after the rename.
- **Scope held:** dumb point-to-point router only. No worker/arbiter (1.3), no supervision (1.4), no CLI transport (1.5), no `turn_id` fencing, no broadcast/pub-sub, no `context` parameter (deferred per AD-11). `.golangci.yml` unchanged (LLM-free-core fence over `core/` is Story 3.1).

### File List

- `core/bus/hub.go` (new) — `Hub`, `New`, `Register`, `Publish`, `ErrNoRoute`, `ErrDuplicateRoute`, `ErrNilDestination`
- `core/bus/hub_test.go` (new) — point-to-point, no-route, duplicate-route, no-creds reflection, and `-race` concurrency tests
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (modified) — story status → in-progress → review

## Review Findings

- [x] [Review][Patch] `Register` accepts a nil channel — stored silently, panics on first `Publish` [core/bus/hub.go:43] — RESOLVED: added `ErrNilDestination`; `Register` rejects a nil channel before touching the table (`TestRegister_NilDestination`)
- [x] [Review][Patch] `ErrNoRoute` carries no diagnostic context — `env.Kind` lost on error [core/bus/hub.go:56–62] — RESOLVED: `Publish` now returns `fmt.Errorf("%w: %q", ErrNoRoute, env.Kind)`; `errors.Is` still matches, kind asserted in `TestPublish_NoRoute`

- [x] [Review][Defer] Blocking `Publish` with no context/timeout — intentional per spec Dev Notes; context/turn_id fencing deferred to Story 1.3
- [x] [Review][Defer] No `Deregister` method — out of story scope; revisit when worker restarts require route removal
- [x] [Review][Defer] `walkFields` missing `Map`/`Interface`/`Chan` descent — spec acknowledges "structural today"; future payload types must be added to seeds
- [x] [Review][Defer] `Payload any` field not reflectively descended — same structural limitation; explicit seeds list is the current guard
- [x] [Review][Defer] Send on closed channel panics in `Publish` — consequence of absent `Deregister`; addressed when Deregister lands
- [x] [Review][Defer] Hub observability (no `Registered()`, no metrics hook) — out of story scope
- [x] [Review][Defer] Nil `Payload` forwarded without validation — bus is dumb router; payload validation belongs at consumer
- [x] [Review][Defer] Empty-string `Kind` accepted in `Register` — pedantic for in-process use; validate at ingress if needed

## Change Log

| Date       | Change                                                                 |
|------------|------------------------------------------------------------------------|
| 2026-06-20 | Implemented the core-owned channel hub: point-to-point routing by `kind`, fail-safe `ErrNoRoute`/`ErrDuplicateRoute`, NFR8 no-creds guard, race-clean (Story 1.2). All 7 tasks complete; build (native+arm64), tests, `-race`, and lint green. |
| 2026-06-20 | Addressed code review findings — 2 [Patch] resolved (`ErrNilDestination` guard on `Register`; `ErrNoRoute` now wraps the offending kind); 8 findings deferred. core/bus tests 5→6, all suites green. |
