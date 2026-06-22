---
baseline_commit: d80cb57
---

# Story 3.1: Capability broker + credential boundary

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As the system,
I want model/tool credentials held only inside the broker and a build that fails if anything outside `broker/internal/` imports a provider SDK,
so that a prompt-injected worker can never reach secrets or call models directly (FR5, NFR3, NFR8, AD-9).

## Context

**First story of Epic 3 (M1 — "The Brain").** Epic 2 made the pet alive with zero LLM; Epic 3 adds the brain. Before any LLM code lands, this story builds the **trust boundary** the rest of the epic plugs into: the broker is the *sole* holder of model/tool credentials and the *only* egress to models (AD-9). Building it first means every later Epic 3 component (provider chain 3.2, real worker 3.3) is constructed inside an already-enforced credential fence — a prompt-injected worker (the one untrusted-by-design component) can never reach a raw key or call a provider directly.

The boundary has **three enforced layers**, one per AC:
1. **The broker exposes only a pre-authorized `*http.Client`** whose transport injects auth — callers get a ready-to-use client, never the raw key (AC1, the AD-9 idiom).
2. **A build-failing `depguard` fence:** no package outside `broker/internal/` may import a provider/LLM SDK (AC2, mirrors the AD-1 enforcement already guarding `contracts/`).
3. **No credential ever rides the bus** — `Job`/`Result`/`Envelope` carry no key; the broker injects creds internally at egress (AC3, NFR8).

**Double-fencing by design.** Secret-touching code lives under `broker/internal/`, so Go's own `internal/` visibility rule already bars `core/`, `worker/`, and every edge from importing it — independent of depguard. depguard then adds the build-failing rule for provider *SDKs* specifically. Two orthogonal fences guard the same secret.

**Why this is mostly a fence, not yet a caller.** At 3.1 there is no provider SDK in `go.mod` and no worker calling the broker — those arrive in 3.2 (provider chain) and 3.3 (real worker). So 3.1 ships the credential-holding broker, its auth-injecting client, and the fence that activates the moment 3.2 adds an SDK. The broker is **not wired into `main.go`** yet (no caller until 3.3).

**Retro action item folded in (Epic 2 → 3.1):** wire **AD-17 `log/slog` observability** in the broker now — log credential *resolution* (present / missing at init) and never the secret value. The broker is the first component that needs structured operational logging; 3.2's provider fallbacks build on it.

**This story does NOT:**
- import or wire any provider/LLM SDK (anthropic-sdk-go, go-openai, ollama) — that is Story 3.2; 3.1 only erects the fence they will live behind. No `go.mod` change.
- build the provider chain, retry/fallback, or base-URL/GLM-default selection (Story 3.2) — the auth-injecting client is generic at 3.1
- build the real worker or wire the broker to any caller (Story 3.3) — the broker has no consumer yet; it is constructed and tested in isolation
- build tool egress / safety policy (later Epic 3+) — 3.1 is the credential boundary + model-egress *seam*, model/tool *execution* comes later
- populate a vault or touch sensitive classification (Epic 5; AD-3 — the vault does not exist until the worker is uid-separated at M3)
- touch `core/`, `worker.Stub`, reflexes, scheduler, or contracts' shape (AC3 only *asserts* contracts carry no creds — they already don't)

## Acceptance Criteria

1. **Pre-authorized client, never the raw key.**
   **Given** the broker holding a model credential
   **When** it exposes a client to callers
   **Then** it returns only a pre-authorized `*http.Client` whose `http.RoundTripper` injects auth on each request, and there is no exported path to the raw key (NFR8/AD-9).

2. **Provider-SDK fence is build-failing.**
   **Given** an import of a provider/LLM SDK added to `core/` or any package outside `broker/internal/`
   **When** `depguard` runs in the build (`golangci-lint run`)
   **Then** the build fails (NFR3/AD-9).

3. **No credentials on the bus.**
   **Given** any `Job` (or other `Envelope` payload) leaving the bus
   **When** its fields are inspected
   **Then** it carries no credential — the broker injects creds internally at egress (NFR8).

## Tasks / Subtasks

- [x] **Task 1 — Auth-injecting transport inside `broker/internal/` (`broker/internal/authtransport/authtransport.go`)** (AC: 1)
  - [x] New package `authtransport`. `type Transport struct` wrapping a base `http.RoundTripper` and holding the credential (unexported). `New(key string, base http.RoundTripper) *Transport` (base defaults to `http.DefaultTransport` when nil).
  - [x] Implement `RoundTrip(req *http.Request) (*http.Response, error)`: **clone the request** (`req.Clone(req.Context())`), set `Authorization: Bearer <key>` on the clone, delegate to the base transport. Generic at 3.1; provider-specific shaping is 3.2.
  - [x] Package doc: secret-touching code under `broker/internal/`; Go's `internal/` rule bars every package outside `broker/` from importing it (AD-9).

- [x] **Task 2 — The broker: sole cred holder, exposes only a client (`broker/broker.go`)** (AC: 1)
  - [x] New package `broker`. `type Broker struct` holding the prepared `*http.Client` (the key lives in the unexported transport, not on the struct).
  - [x] `New() *Broker` — resolves the credential **only inside the broker** from `os.Getenv("SHELLDON_LLM_API_KEY")` (tunable const). Builds the `*http.Client` with `authtransport.New(key, nil)`. Logs via `slog` whether the credential is **present or missing** (AD-17), never the value. Missing key is non-fatal (degrades to reflex; no panic) — returns a broker with an empty-bearer client rather than an error, since 3.1 has no caller to handle the error yet.
  - [x] `Client() *http.Client` — the **only** exported access. No exported method/field/`String()` surfaces the raw key (AC1, asserted by `TestBroker_ExposesNoRawKeyAccessor`).
  - [x] Package doc: sole holder of model/tool creds + only model egress (AD-9); creds resolve only here; no credential on the bus (NFR8).

- [x] **Task 3 — Build-failing provider-SDK fence (`.golangci.yml`)** (AC: 2)
  - [x] Added `provider-sdks-broker-internal-only` depguard rule: denies the three provider SDK module paths under `files: ["**", "!**/broker/internal/**"]`. Kept the existing `contracts-pure` rule.
  - [x] Commented that the rule is vacuously satisfied at 3.1 and activates when 3.2 adds the first SDK.

- [x] **Task 4 — Tests (stdlib, no testify)** (AC: 1, 2, 3)
  - [x] **`broker/broker_test.go` (AC1):** `t.Setenv` + `httptest.NewServer` asserts the client injects `Bearer <key>`; a reflective test asserts `Broker` has no key-shaped exported method or exported field; a missing-key case asserts `New()` does not panic and returns a non-nil client.
  - [x] **`broker/internal/authtransport/authtransport_test.go` (AC1):** asserts header injection **and** that the caller's request is not mutated (the `req.Clone` contract).
  - [x] **`broker/imports_test.go` (AC2, go-test mirror):** walks the repo from `..`, skips `broker/internal/`, fails on any provider-SDK import elsewhere; scanned-count guard (`≥10`) prevents a vacuous pass. Mirrors `core/dispatch` / `core/scheduler` imports tests.
  - [x] **AC3 — no creds on the bus:** already covered — `core/bus/hub_test.go` `TestEnvelopeCarriesNoCredentials` seeds `contracts.Job{}` (plus `Result`/`Envelope`) and walks for credential-named fields. No change needed; `Job` carries only `Input`/`ConvoID`.
  - [x] `go test -race ./...` → 65 pass; native + arm64 `CGO_ENABLED=0` builds succeed; `golangci-lint run` → 0 issues.

## Dev Notes

### Architecture constraints (binding)

- **AD-9 — Broker is the sole trust boundary: sole cred holder + model/tool egress.** "the broker is the **only** holder of MODEL + TOOL credentials… Idiom: broker exposes only a pre-authorized `*http.Client` (an `http.RoundTripper` that injects auth); downstream code never sees the raw key. A **`depguard` rule enforces that only `broker/internal/` may import provider/LLM SDKs** — build-failing, not just idiom. `Job` envelopes carry **no creds**; the broker injects them internally — **no credentials ever on the bus**." All three ACs come straight from this. The ordered provider chain (failsafe-go, GLM default) is Story 3.2; 3.1 is the boundary it lands inside. [Source: ARCHITECTURE-SPINE.md#AD-9]
- **AD-1 — LLM-free core; provider SDKs behind `broker/internal/`, depguard-enforced.** The fence mirrors the existing core-stays-LLM-free enforcement; 3.1 extends depguard from "contracts pure" to "provider SDKs only under `broker/internal/`." [Source: ARCHITECTURE-SPINE.md#AD-1, .golangci.yml]
- **NFR8 — no credentials on the bus.** `Job`/`Result`/`Envelope` carry no key; the broker injects at egress. The existing reflective NFR8 guard in `core/bus/hub_test.go` is the test home for AC3. [Source: ARCHITECTURE-SPINE.md#AD-4, Consistency Conventions; core/bus/hub_test.go]
- **NFR3 — pure-Go, offline-capable, no creds leak.** depguard runs in `golangci-lint` (the build); the rule is build-failing. [Source: ARCHITECTURE-SPINE.md#AD-9]
- **AD-17 — observability via `log/slog`.** The broker logs credential resolution (present/missing) and, later, provider fallbacks — never the secret. This story wires the broker's slog usage (Epic 2 retro action item). [Source: ARCHITECTURE-SPINE.md#AD-17, epic-2-retro-2026-06-22.md]
- **AD-3 — vault does not exist until the worker is uid-separated (M3).** 3.1 holds only the model API credential (env-resolved); no vault, no sensitive-classification lane. [Source: ARCHITECTURE-SPINE.md#AD-3]
- **Structural Seed — `broker/` + `broker/internal/llm/`.** "`broker/internal/llm/` holds provider SDKs." 3.1 creates `broker/` and `broker/internal/` (the `authtransport` package now; `llm` SDK packages land in 3.2). [Source: ARCHITECTURE-SPINE.md#Structural Seed]

### Key design decisions

- **Auth as an `http.RoundTripper`, secret under `broker/internal/`.** The architecture names this idiom exactly. Placing the key-holding transport under `broker/internal/` double-fences it: Go's `internal/` rule bars all non-`broker/` packages at compile time, *and* depguard bars provider SDKs there. The broker's public surface is just `New()` + `Client()`.
- **`Client()` is the entire public API at 3.1.** No key getter, no provider specifics. A caller (3.3) receives a `*http.Client` already carrying auth and uses it like any client. This is the smallest surface that satisfies AC1 and can't leak the key.
- **Fence now, SDKs later — both depguard *and* an import-walk test.** This repo already pairs depguard with `imports_test.go` walks (contracts, dispatch, scheduler). 3.1 adds both for the provider fence so the invariant is enforced at lint *and* in `go test`, and is locked before 3.2 introduces the first SDK. Both pass vacuously until then — documented, not hidden.
- **Env-resolved single credential.** M0 needs one model key; base-URL/provider selection (GLM default via go-openai base-URL swap) is 3.2. Resolving from env keeps secrets out of source and inside the broker (AD-9 "config + secrets resolve only inside the broker").
- **No `main.go` wiring at 3.1.** The broker has no caller until the real worker (3.3). Wiring it now would create an unused edge; 3.3 constructs and connects it. 3.1 proves the broker in isolation via tests.

### Previous story intelligence (Epic 1–2)

- **Import-walk test pattern to mirror for AC2:** `core/dispatch/imports_test.go` and `core/scheduler/imports_test.go` walk a tree (`parser.ParseFile` with `ImportsOnly`) and fail on forbidden import substrings. Copy that shape for the provider-SDK paths over "everything outside `broker/internal/`". Include the **scanned-file-count guard** from Story 2.5's review (fail if the walk scanned 0 files — no vacuous pass on a mis-rooted walk). [Source: core/dispatch/imports_test.go, core/scheduler/imports_test.go, 2-5 Review Findings]
- **depguard v2 config shape is already in `.golangci.yml`** (`rules:` → named rule → `files:` + `deny:` with `pkg`/`desc`). Add the new rule beside `contracts-pure`; `files` negation (`"!**/broker/internal/**"`) scopes the exception. [Source: .golangci.yml]
- **`contracts.Job` already carries no credential** — `Input`/`ConvoID` only, with a comment that the broker injects creds at egress (Story 3.1). AC3 locks this via the existing NFR8 guard rather than changing `Job`. [Source: contracts/job.go]
- **NFR8 reflective guard exists** in `core/bus/hub_test.go` (`walkFields` + explicit `seeds` list); a 1.2 review note flags it descends an explicit seed list (new payload types must be added). Ensure `Job` is seeded. [Source: core/bus/hub_test.go, 1-2 Review Findings]
- **slog is the project logger (AD-17)**, used by the supervisor, reflexes, scheduler, and arbiter (`slog.Error`/`slog.Warn`). Match that idiom; structured attrs, no secret values. [Source: core/supervisor/supervisor.go, core/scheduler/scheduler.go]
- **No provider SDK in `go.mod` yet** (only suture + renameio). 3.1 adds none; 3.2 adds the first. [Source: go.mod]
- **`go.mod` is `go 1.25`**; builds run `CGO_ENABLED=0` native + `GOOS=linux GOARCH=arm64`. golangci-lint binary is at `~/go/bin/golangci-lint` (not on PATH via the proxy). [Source: go.mod, Epic 2 dev runs]

### Project Structure Notes

- New: `broker/broker.go`, `broker/broker_test.go`, `broker/imports_test.go`, `broker/internal/authtransport/authtransport.go`, `broker/internal/authtransport/authtransport_test.go`.
- Modified: `.golangci.yml` (add the provider-SDK fence rule); possibly `core/bus/hub_test.go` (seed `Job` into the NFR8 walk if not already present — test-only).
- No `go.mod`/`go.sum` change (no SDK yet). No `core/`, `worker/`, `contracts/`, or `main.go` change.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.1] — the three ACs (pre-authorized client; depguard build-fail; no creds on bus)
- [Source: ...ARCHITECTURE-SPINE.md#AD-9] — sole cred holder; auth-injecting RoundTripper idiom; depguard `broker/internal/` rule; no creds on the bus
- [Source: ...ARCHITECTURE-SPINE.md#AD-1, #AD-17, #AD-3, #Structural Seed] — LLM-free fence; slog observability; no vault pre-M3; broker/internal/ seed
- [Source: .golangci.yml] — existing depguard `contracts-pure` rule to extend
- [Source: core/dispatch/imports_test.go, core/scheduler/imports_test.go] — import-walk test pattern for AC2 (+ the 2.5 scanned-count guard)
- [Source: contracts/job.go, core/bus/hub_test.go] — Job carries no creds; the NFR8 reflective guard for AC3
- [Source: _bmad-output/implementation-artifacts/epic-2-retro-2026-06-22.md] — AD-17 slog folded into the broker (action item)

## Dev Agent Record

### Agent Model Used

claude-opus-4-8 (1M context)

### Debug Log References

None. One lint fixup mid-run: unchecked `resp.Body.Close()` in the new tests (errcheck) → `_ = resp.Body.Close()`.

### Completion Notes List

- **Credential boundary is double-fenced as designed.** The key lives only in `broker/internal/authtransport.Transport` (unexported field). Go's `internal/` rule bars `core/`, `worker/`, and every edge from importing it; the broker's public surface is just `New()` + `Client()`. `TestBroker_ExposesNoRawKeyAccessor` reflectively asserts no key-shaped exported method or exported field exists.
- **Auth-injection idiom (AD-9):** `RoundTrip` clones the request (`req.Clone`) before setting `Authorization: Bearer <key>`, honoring the `http.RoundTripper` no-mutation contract — proven by `TestRoundTrip_DoesNotMutateCallerRequest`. `Client()` returns a ready-to-use `*http.Client`; callers never touch the key.
- **depguard fence (AC2)** added as `provider-sdks-broker-internal-only` (`files: ["**", "!**/broker/internal/**"]`) for the three provider SDK paths. Vacuously satisfied now (no SDK in `go.mod`); activates when 3.2 adds the first. Backed by a go-test mirror (`broker/imports_test.go`) with a `≥10` scanned-count guard (the Story 2.5 vacuous-pass fix) so the invariant is enforced in the suite too.
- **AC3 needed no code** — `core/bus/hub_test.go`'s `TestEnvelopeCarriesNoCredentials` already seeds `Job`/`Result`/`Envelope` and walks for credential-named fields. `Job` carries only `Input`/`ConvoID`. Confirmed, not modified.
- **AD-17 slog folded in** (Epic 2 retro action item): the broker logs credential *presence/absence* at `New()` — never the value.
- **Design call — missing key is non-fatal, `New()` returns no error.** The story floated `New() (*Broker, error)`; I dropped the error because 3.1 has no caller to handle it, and AD-8 wants degrade-to-reflex over crash. The broker constructs with an empty-bearer client and logs the absence; 3.2/3.3 decide fallback when they wire it. Revisit if 3.3 wants explicit error propagation.
- **Not wired into `main.go`** — no caller until the real worker (3.3); wiring now would be an unused edge.
- **Validation:** `go test -race ./...` → 65 pass (16 packages); `CGO_ENABLED=0` native + `GOOS=linux GOARCH=arm64` builds succeed; `golangci-lint run` → 0 issues. No `go.mod`/`go.sum` change (no SDK yet).

### File List

- `broker/broker.go` (new) — `Broker`, `New`, `Client`; env-resolved credential; slog at init.
- `broker/internal/authtransport/authtransport.go` (new) — auth-injecting `http.RoundTripper`; holds the key, clones the request.
- `broker/broker_test.go` (new) — AC1 inject-credential, missing-key-no-panic, no-raw-key-accessor.
- `broker/internal/authtransport/authtransport_test.go` (new) — AC1 header injection + no-mutation contract + empty-key omits-header (review fix).
- `broker/internal/authtransport/authtransport.go` (review fix) — omit `Authorization` header when no key.
- `broker/imports_test.go` (review fix) — skip unparseable files instead of aborting the fence walk.
- `broker/imports_test.go` (new) — AC2 provider-SDK fence (go-test mirror) + scanned-count guard.
- `.golangci.yml` (modified) — added `provider-sdks-broker-internal-only` depguard rule.

## Review Findings

- [x] [Review][Patch] Empty bearer header injected when key is "" — `authtransport/authtransport.go:RoundTrip` — resolved: `RoundTrip` omits the `Authorization` header when `t.key == ""`; covered by `TestRoundTrip_OmitsHeaderWhenNoKey`.
- [x] [Review][Patch] Parse error in any `.go` file aborts the entire SDK-fence walk — `broker/imports_test.go:41` — resolved: a parse error now skips that file (`return nil`) so one unparseable file can't disable the whole fence.
- [x] [Review][Defer] `WalkDir("..")` is cwd-dependent — `broker/imports_test.go:31` — deferred, pre-existing project pattern (dispatch/scheduler tests same); scanned-count guard mitigates vacuous pass
- [x] [Review][Defer] Reflection test would false-positive on future embedded exported types — `broker/broker_test.go:68` — deferred, pre-existing; Broker has no embedded types today; latent only
- [x] [Review][Defer] `contracts-pure` depguard rule missing `ollama` — `.golangci.yml` — deferred, pre-existing from Story 1.1, not introduced by this diff

## Change Log

| Date       | Version | Description                                                                 |
| ---------- | ------- | --------------------------------------------------------------------------- |
| 2026-06-22 | 0.1     | Capability broker + credential boundary: sole-cred-holder broker exposing only an auth-injecting client; depguard + go-test fence for provider SDKs under broker/internal/; AD-17 slog. All ACs satisfied; 65 tests pass. Status → review. |
