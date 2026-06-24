# Deferred Work

## Deferred from: code review of 4-1-sqlite-conversation-store.md (2026-06-24)

- **`convo_id` not in FTS table — full FTS scan on `Search`** — `Search` does a full FTS scan across all conversations then filters by `convo_id`; correct behavior but O(all messages) at scale. Acceptable at M1 single-user volume; revisit if conversation volume grows significantly or multi-user lands. **(Deferred — genuinely fine at M1.)**

## Deferred from: code review of 4-2-learnings-table-hot-path-capture-learning.md (2026-06-24)

- **`ConcurrentNoLostIncrements` tests Go pool serialization, not bare upsert atomicity** — the 50-goroutine test proves no lost increments via `SetMaxOpenConns(1)` + atomic upsert combined. If `MaxOpenConns` is ever relaxed (e.g. to allow read concurrency), a dedicated test of the upsert atomicity alone would be needed. **(Deferred — design isn't changing; the combined guarantee holds.)**

### Resolved in post-review fixes (2026-06-24)

- ✅ **Non-positive `n` mapped to SQLite "no limit"** — `Recent`, `Search`, and `Learnings` now guard `if n <= 0 { return empty }`, so a non-positive cap returns nothing instead of the whole table. Fixed across all three core/memory list queries (the reviewer noted `Recent` shared the gap; the dream cycle in 4.4 will pass a computed `n` to `Learnings`). Test: `TestListQueries_NonPositiveLimitReturnsEmpty`.

### Resolved in post-review fixes (2026-06-24)

- ✅ **`Open("")` silent in-memory db** — `Open` now rejects an empty path with `memory: empty db path` (no silent data loss). Test: `TestOpen_RejectsEmptyPath`.
- ✅ **Path with `?`/`&` corrupted the WAL DSN** — the DSN is now built via `net/url.URL` (path percent-encoded, pragmas in `RawQuery`), so special chars in the filename can't drop the WAL/synchronous/busy_timeout pragmas. Test: `TestOpen_PathWithSpecialChars` (asserts WAL still active + round-trip).
- ✅ **Raw `DB()` test accessor could bypass FTS triggers** — replaced with a read-only `JournalMode(ctx)` test accessor; no `*sql.DB` is exposed, so tests can't write to `messages` directly.
- ⏭️ **`telego` in `go.mod` diff** — not a 4.1 change; it's the uncommitted Epic 3 session work. Resolves on commit. No action.

## Resolved (Epic 3 retro action items, 2026-06-24)

- ✅ **Context-aware `hub.Publish`** — added `bus.Hub.PublishContext(ctx, env)` (select on `ctx.Done()` vs the buffered send); migrated the `dispatch`, `proactive`, and `telegram` producers. Clears the dominant back-pressure debt theme for the hot message paths. (`compositor.PushFace` boot push and `cli` readLoop still use blocking `Publish` — no ctx in scope, low risk.)
- ✅ **Telegram degraded-transport supervisor crash-loop** — the degraded `transportServe` now blocks on `<-ctx.Done()` (dormant until shutdown) instead of returning the init error immediately, so suture no longer tight-loops a missing/invalid token.

## Deferred from: code review of 3-6-llm-driven-proactive-pings.md (2026-06-23)

- **Budget slot consumed before Submit; transient failures drain daily budget** — `core/turntier/turntier.go:135,143` — `tryConsume` is called before `Arbiter.Submit`; a transient provider failure permanently spends a daily budget slot with no outbound message produced. By-design for M1 (the spec accepts this); revisit in Epic 4 when durable, rollback-capable budgets land.
- **Telegram degraded-transport supervisor crash loop** — `cmd/shelldon/main.go:109-112` — when `telegram.NewFromEnv` fails at init, `transportServe` is set to a function that immediately returns the init error; the supervisor's restart loop retries it indefinitely with no meaningful backoff. Not a 3.6 introduction (3.4 scope) but not previously captured in deferred work.
- **Test outbound channel 4× larger than production** — `core/proactive/proactive_test.go:37` — `make(chan contracts.Envelope, 64)` vs production's 16-slot buffer; tests will not catch a `hub.Publish` deadlock that would occur in production when the channel fills beyond 16 messages.

## Deferred from: code review of 3-5-turn-tier-scheduler-budget-battery-gate (2026-06-22)

- **TOCTOU window on `lastFired` check-and-update** — `core/turntier/turntier.go:120-136` — the cooldown check reads `lastFired` under one lock acquisition and releases before the budget/battery checks, then re-acquires to write `lastFired`. Non-exploitable with the scheduler's single-goroutine-per-job guarantee, but breaks if `run()` is ever called concurrently. Revisit if multi-goroutine job patterns are introduced.
- **`runGatedJob` helper silently discards caller's Config fields** — `core/turntier/turntier_test.go:40-64` — the helper unconditionally overwrites cadence, cooldown, build, arbiter, budget, and power from its own parameters; the `cfg Config` arg is effectively just a name carrier. Future tests pre-populating other Config fields will silently fail. Restructure when adding test cases that need different Config values.
- **Year-boundary (Dec 31→Jan 1) untested for budget daily reset** — `core/turntier/turntier.go:62` — `TestBudget_TryConsumeResetsOnDayBoundary` covers same-year boundary only. The `year*1000+yearDay` formula is correct for year rollover (different year × 1000 + different yearDay yields distinct keys), but no test verifies this. Cover when adding budget stress tests.

## Deferred from: code review of 3-4-telegram-adapter-second-transport (2026-06-22)

- **`Send` is synchronous in Serve's main select** — `transport/telegram/telegram.go` — if the Telegram API is slow or rate-limiting, `Serve` blocks in `Send` and cannot drain `outbound` or respond to `ctx.Done()` until it returns. Consistent with the CLI adapter design; M1 single-owner doesn't produce backpressure. Revisit for multi-user or high-throughput transport.
- **`WithTimeout` on `UpdatesViaLongPolling` may not reach the wire** — `transport/telegram/telegram.go:170` — telego's internal polling loop may override the Timeout field. The AC3 test validates the constant relationship only, not wire behavior. Requires telego source audit + live integration test to confirm.
- **ConvoID encoded as bare decimal string — no transport prefix** — `transport/telegram/telegram.go:156` — if a future multi-transport story routes messages from multiple transports simultaneously, numeric ConvoIDs from different transports could collide. The M1 invariant is single-transport-at-a-time (point-to-point bus); add a `tg:` prefix when multi-transport fan-out lands in a later story.
- **`NewFromEnv` invalid-owner-ID error path untested** — `transport/telegram/telegram.go:94` — the `strconv.ParseInt` failure branch when `SHELLDON_TELEGRAM_OWNER_ID` is not a valid int64 has no test coverage. Low risk (format error is obvious at startup); cover if `NewFromEnv` is called from non-main paths.

## Deferred from: code review of 3-3-real-worker-behind-the-seam (2026-06-22)

- **Empty `turn.Input` forwarded without guard** — `worker/monolith/monolith.go:AssembleAndPropose` — blank input causes a wasted LLM call or 4xx degraded to `ErrAllProvidersFailed`; upstream currently prevents it but no worker-level fence exists. Defer to Epic 4 when input assembly is formalized.
- **Empty `resp.Text` accepted as valid reply** — `worker/monolith/monolith.go:AssembleAndPropose` — provider returning HTTP 200 with empty content propagates a blank reply to dispatch/CLI. No current evidence this occurs; revisit with streaming/response validation.
- **`fakeCompleter.gotReq` written without synchronization** — `worker/monolith/monolith_test.go` — safe today (synchronous tests pass race detector), but any future test that reads `gotReq` after launching `AssembleAndPropose` in a goroutine will race. Add a mutex or restructure.
- **`blockUntilCancel` fake hangs forever if ctx never canceled** — `worker/monolith/monolith_test.go:fakeCompleter.Complete` — `<-ctx.Done()` with no timeout guard; a future test setting this flag without canceling will hang the binary.
- **Cancellation test goroutine leaked on 2-second timeout failure** — `worker/monolith/monolith_test.go:TestAssembleAndPropose_CancellationPropagates` — goroutine exits eventually via buffered channel, but after test teardown may be running. Clean up with `t.Cleanup` + cancel.
- **No mechanical AD-1 import-graph guard** — core's LLM-free constraint is enforced by convention; no `go list -deps` check or depguard rule. Pre-existing gap; revisit alongside `core/dispatch/imports_test.go` pattern.

## Deferred from: code review of 3-2-provider-chain-with-retry-fallback (2026-06-22)

- **`lastErr` is a `retrypolicy.ExceededError` wrapper, not raw provider error** — `broker/broker.go:~112` — when retries exhaust, failsafe-go wraps the last error in `retrypolicy.ExceededError`. Callers using `errors.As` to extract a specific error type must unwrap through that. Not a current bug (ErrAllProvidersFailed chain is correct), but misleading to future maintainers. Add `.ReturnLastFailure()` to the builder or document the wrapping.
- **Max wall-clock per chain unbounded without caller deadline** — `broker/broker.go:Complete` — `perProviderTimeout` (30s) covers the full retry sequence per provider; an N-provider chain can block 30s×N before returning. No overall `Complete` deadline beyond caller context. Acceptable for current single-provider chain; revisit when chain grows (Story 3.4+).
- **`baseURL` trailing slash not sanitized** — `broker/broker.go:New()` — if `SHELLDON_LLM_BASE_URL` is set with a trailing slash, the go-openai SDK may produce a double-slash path (`//chat/completions`). Default constant is fine; sanitize via `strings.TrimRight(baseURL, "/")` before passing to `NewOpenAI`.
- **Empty `Messages` or empty `model` not validated at broker boundary** — `broker/broker.go:Complete` — a `Request{Messages: nil}` or empty model reaches the API and gets a 400 (retried 3×). Input validation at the broker entry point would give a faster, cheaper error. Defer until worker (Story 3.3) establishes what guarantees it provides on inputs.

## Deferred from: code review of 3-1-capability-broker-credential-boundary (2026-06-22)

- **`WalkDir("..")` is cwd-dependent in imports_test** — `broker/imports_test.go:31` — walk root is the package dir's parent, which is correct for `go test` but undocumented; scanned-count guard (≥10) mitigates complete miss. Pre-existing project pattern (dispatch/scheduler same). Accept for now; revisit if tests ever run outside standard `go test`.
- **Reflection test false-positives on future embedded exported types** — `broker/broker_test.go:68` — `TestBroker_ExposesNoRawKeyAccessor` iterates all exported fields; an embedded `sync.Mutex` or similar would trigger a false fail. Not a current bug; fix when/if `Broker` gains an embedded type.
- **`contracts-pure` depguard rule missing `ollama`** — `.golangci.yml` — `ollama` is denied by `provider-sdks-broker-internal-only` but not by the older `contracts-pure` rule. Pre-existing from Story 1.1. Low risk (contracts package is simple); add `ollama` to `contracts-pure` when convenient.

## Deferred from: code review of 2-6-offline-acknowledgement-brainless-alive (2026-06-22)

- **`hub.Publish` blocks → dispatch loop potential deadlock** — `publishReply` uses an unconditional blocking send; if the outbound consumer stops, `Serve` hangs. Pre-existing architectural constraint (16-slot buffer + draining transport is the M0 safety net). File: `core/dispatch/dispatch.go` — `publishReply`.
- **`select` race: valid result discarded when `done` and `tctx.Done()` fire simultaneously** — Go's pseudo-random select may pick `tctx.Done()` even when the worker result is ready at the same tick; the valid result is silently discarded. M0 acknowledged limitation (AD-11 fence = context cancellation + dropped late Result). File: `core/arbiter/arbiter.go` — `Submit` select.
- **Spurious `reflexAck` during shutdown window** — one extra ack can fire if `ctx` is cancelled after `Submit` returns a non-nil error but before the `ctx.Err() != nil` switch check. Benign narrow race; no AC violated. File: `core/dispatch/dispatch.go` — `Serve` switch.
- **`ErrNoRoute` silently discarded in `publishReply`** — `_ = d.hub.Publish(...)` inherits the pre-existing pattern; a missing route registration drops the reply with no visibility. File: `core/dispatch/dispatch.go:77`.
- **Non-cooperative worker goroutine leaks if it ignores `ctx.Done()`** — an abandoned goroutine runs indefinitely if the worker doesn't respect cancellation. Inherent Go limitation; Epic 3 workers must honour context. File: `core/arbiter/arbiter.go` — `Submit` goroutine.
- **`blockingWorker` ignores context — existing concurrency test doesn't cover context-cancellation propagation** — `TestArbiter_AtMostOneInFlight` unblocks via `release` channel, not context; no test exercises ctx cancellation on the blocking worker. File: `core/arbiter/arbiter_test.go`.
- **Timeout tests have no per-test deadline → hang instead of fail** — tests blocking on `<-outbound` or `<-done` have no `time.After` guard; a regression produces a hang. Project-level `go test -timeout` is the current safety net. Files: `core/arbiter/arbiter_test.go`, `core/dispatch/dispatch_test.go`.

## Deferred from: code review of 2-5-reflex-tier-scheduler (2026-06-22)

- **`Serve` with zero registered jobs returns `nil` immediately** — `wg.Wait()` on an empty slice returns at once; `ctx.Err()` is nil if context isn't cancelled; supervisor sees a clean exit and doesn't restart. Not reachable with current main.go usage. File: `core/scheduler/scheduler.go`.
- **`NextDelay` returning 0 causes a busy-loop** — `time.NewTimer(0)` and `timer.Reset(0)` fire immediately; the job goroutine spins at 100% CPU with no yield. Not reachable with current blink/mood implementations. File: `core/scheduler/scheduler.go`.
- **Slow `Run` + short cadence → burst catch-up** — timer reset happens after `fire()` returns; if `Run` outlasts `NextDelay`, the already-expired channel item fires the next tick instantly. Not a concern at M1 cadences (blink ≥4s, mood ≥6h). File: `core/scheduler/scheduler.go`.

## Deferred from: code review of 2-4-mood-drift-reflex (2026-06-21)

- **`MoodDrift.Serve` has no shutdown flush** — a crash between `SetMood` and `Checkpoint()` in the same tick leaves RAM and disk diverged until the next 60s state-checkpoint fence. The periodic checkpoint is the fallback, but it's not a guaranteed AC-16 durability window per tick. File: `core/reflexes/mood.go`.

## Deferred from: code review of 2-3-blink-idle-reflexes (2026-06-21)

- **`hub.Publish` blocks if display channel full/renderer stopped** — `PushFace` has no context-aware escape; `blinkOnce` can hang if the renderer is stopped or restarting. Acknowledged 2.2 architectural constraint; blink respects ≤2 pushes/cycle. Files: `core/reflexes/blink.go`.
- **PCG seed second word always 0** — `rand.NewPCG(uint64(time.Now().UnixNano()), 0)` reduces jitter entropy; jitter is functionally present but PCG stream-select is fixed. File: `cmd/shelldon/main.go`.
- **Wrong-kind envelope on inbound skips `store.Touch()`** — dispatch type-asserts before calling Touch; a wrong-kind envelope would silently skip the idle reset. Theoretical: inbound only receives `KindInboundMessage`. File: `core/dispatch/dispatch.go`.
- **`dispatch_test.go` `<-outbound` has no timeout** — test hangs indefinitely if `worker.Stub{}` fails to respond. File: `core/dispatch/dispatch_test.go:41`.
- **Supervisor restart while idle → immediate blink** — after a panic restart the idle threshold is already elapsed; cosmetic only. File: `core/reflexes/blink.go`.

## Deferred from: code review of 2-2-region-compositor-contract-terminal-ansi-face (2026-06-21)

- **Boot-time push + back-pressure** — `hub.Publish` is a blocking send; single boot push is safe with a 16-slot buffer, but any caller pushing >16 frames before `renderer.Serve` starts will deadlock. Relevant for Story 2.3 blink loop design. Files: `cmd/shelldon/main.go`, `core/compositor/compositor.go`.
- **Write errors silenced in paint()** — `fmt.Fprint` errors discarded with `_, _`; supervisor cannot detect a dead terminal output stream. Would require changing `paint`/`handle`/`Serve` signatures to propagate. File: `display/terminal/terminal.go`.
- **RegionID not structurally closed** — `type RegionID string` uses the same string-alias pattern as `Kind`; Go does not prevent external code from constructing arbitrary `RegionID` values. Consider unexported backing type or constructor-only pattern before Epic 6 plugin region-claims (AD-14). File: `contracts/region.go`.
- **_test.go files excluded from core import guard** — the import test skips `*_test.go` files; a future core test file importing `display/` would pass undetected. File: `core/dispatch/imports_test.go`.

## Deferred from: code review of 1-1-versioned-contracts-gob-round-trip (2026-06-20)

- **AllKinds mutability and Kind-AllKinds sync gap** — unsure which fix to take (unexported+Kinds() vs exported+comment); revisit when a second Kind is added.

- **gob type names include module path** — if module is forked/renamed, existing gob blobs produce "type not registered"; no test guards this. File: `contracts/register.go`.
- **Header.V is defined but nothing reads or gates on it** — intentional per architecture; version negotiation is future work. File: `contracts/envelope.go`.
- **No negative test for gob type-not-registered path** — future bus code should verify the error is catchable rather than a panic. File: `contracts/register.go`.
- **nil Payload in Envelope is untested** — gob behavior with nil interface field is unvalidated; relevant when bus enforces non-nil before encoding. File: `contracts/contracts_test.go`.

## Deferred from: code review of 1-2-core-owned-channel-hub-point-to-point-routing (2026-06-20)

- **Blocking `Publish` with no context/timeout** — intentional per spec Dev Notes; context/turn_id fencing deferred to Story 1.3. File: `core/bus/hub.go:57`.
- **No `Deregister` method** — routes are write-once; revisit when worker crashes require channel replacement. File: `core/bus/hub.go`.
- **`walkFields` missing Map/Interface/Chan descent** — spec acknowledges "structural today"; any future `contracts` type using map or interface fields is invisible to the NFR8 guard. File: `core/bus/hub_test.go:126`.
- **`Payload any` field not reflectively descended** — explicit `seeds` list is the current guard; new payload types must be added manually. File: `core/bus/hub_test.go:116`.
- **Send on closed channel panics in `Publish`** — consequence of absent `Deregister`; if a registrant closes its channel, `Publish` panics. Addressed when Deregister lands. File: `core/bus/hub.go:62`.
- **Hub observability absent** — no `Registered()`, `Len()`, or metrics hook; debugging routing failures requires external tooling. File: `core/bus/hub.go`.
- **Nil `Payload` forwarded without validation** — bus is dumb router; payload nil-checks belong at consumer. File: `core/bus/hub.go:57`.
- **Empty-string `Kind` accepted in `Register`** — zero-value `Envelope` would route to it; validate at ingress if this becomes a concern. File: `core/bus/hub.go:43`.

## Deferred from: code review of 1-3-worker-seam-interface-stub-1-in-flight-arbiter-gate (2026-06-20)

- **`Submit` has no `ctx.Done()` arm** — a context cancelled before slot acquisition returns `ErrTurnInFlight` instead of `ctx.Err()`; callers can't distinguish "slot busy" from "context dead." Related to AD-11 turn fencing; deferred to the turn lifecycle story. File: `core/arbiter/arbiter.go:37-43`.

## Deferred from: code review of 1-4-suture-supervisor-root-soul-survives-edge-panic (2026-06-21)

- **`<-errCh` has no post-drain timeout** — after all edges are removed and supervisor context is cancelled, `<-errCh` blocks with no timeout; a suture internal bug could hang `Root.Serve` forever. File: `core/supervisor/supervisor.go:69`.
- **Test channel receives have no timeout** — `<-flakyStarted` and `<-steady.started` are unbounded blocking receives; tests deadlock instead of fail usefully if suture delays a restart (e.g., unexpected backoff). File: `core/supervisor/supervisor_test.go:63-70`.
- **`logEvent` EventHook panic is unguarded** — a panic inside `logEvent` propagates into suture's recovery machinery rather than being caught by any Guard; currently theoretical (stdlib slog doesn't panic) but unprotected. File: `core/supervisor/supervisor.go:108`.
- **`RemoveAndWait` error silently discarded** — a drain timeout (edge refusing to stop within 5s) produces no log and no error propagation; a stuck edge is invisible to ops during shutdown. File: `core/supervisor/supervisor.go:65`.
- **`logEvent` drops `EventStopTimeout` and other suture events** — only `EventServicePanic` and `EventBackoff` are logged; `EventStopTimeout` in particular is operationally important (signals an edge refused to stop) but currently invisible. File: `core/supervisor/supervisor.go:109`.

## Deferred from: code review of 1-5-cli-transport-adapter-end-to-end-round-trip (2026-06-21)

- **Silent `hub.Publish` error paths** — `_ = d.hub.Publish(...)` (dispatch) and `_ = a.hub.Publish(...)` (cli) drop `ErrNoRoute` with no log. Harmless at M0 (routes are statically registered at startup, so `ErrNoRoute` is unreachable at runtime), but worth a `slog.Warn` once AD-17 observability lands. Files: `core/dispatch/dispatch.go:53`, `transport/cli/cli.go:60`.
- **`readLoop` goroutine not stoppable on ctx cancellation** — blocks on `bufio.Scanner.Scan()` until stdin EOF; cannot be cancelled by the supervisor's shutdown. Intentional, documented M0 deferral — a cancelable stdin is not an M0 concern; revisit if/when needed. File: `transport/cli/cli.go:41`.

## Deferred from: code review of 2-1-personality-state-struct-periodic-checkpoint (2026-06-21)

- **`Store.path` not validated in `New`** — empty-string path accepted silently; fails at first checkpoint instead of construction. File: `core/state/state.go:51`.
- **`assertOnlyFile` test helper duplicated** — mirrors `core/memory/atomic_test.go`; a shared internal/testutil would eliminate the copy. File: `core/state/checkpoint_test.go:15`.
- **Float64 `!=` comparison in test helpers** — fragile if future reflex arithmetic touches Mood/Energy values. File: `core/state/checkpoint_test.go:42`.
- **No bounds/range enforcement in `SetMood`** — NaN/Inf is stored, checkpointed as JSON null, and silently replaces state on restore. File: `core/state/state.go:62`.
- **Double write on shutdown when ticker and ctx.Done both ready** — Go's non-deterministic select can fire the ticker case before ctx.Done; benign extra SD write but counters NFR11 frugality. File: `core/state/checkpoint.go:62-71`.

## Deferred from: code review of 1-6-cross-compile-atomic-write-crash-safety-on-pi-run (2026-06-21)

- **`WriteAtomic` gives opaque error when parent directory does not exist** — `renameio.WriteFile` fails with an OS error if the directory doesn't exist; no caller exists in M0 so this is latent. Revisit when Epic 4 wires the first real call site. File: `core/memory/atomic.go`.
- **`test:pi` runs stale Pi binaries if `deploy` was not run first** — `test:pi` has no dep on `deploy`; running it standalone tests whatever binaries are currently on the Pi. Dev-tool UX concern; a README note or Taskfile dep on a version-stamp check would address it. File: `Taskfile.yml`.
- **`WriteAtomic` silently drops ACLs/xattrs on target file** — `rename(2)` replaces the inode; ACLs or extended attributes on the original file are not copied to the temp before rename. Pre-existing `rename(2)` behavior; irrelevant for M0's crash-safety test but worth documenting when Epic 4 introduces real file ownership. File: `core/memory/atomic.go`.
- **`&&`-chained test binaries in `test:pi` silently skip later tests when any earlier test fails** — if `contracts.test` fails, the remaining three (including `memory.test -test.run TestWriteAtomic_CrashSafety`) are never reached; the task exits non-zero but gives no signal about which tests ran. Dev-tool UX concern; running with `; true` or separate `task` invocations would surface all failures. File: `Taskfile.yml`.
- **`TestWriteAtomic_CrashSafety` couples to `renameio.PendingFile` internals** — the test calls `renameio.NewPendingFile`/`Cleanup()` directly rather than through `WriteAtomic`; if `WriteAtomic` is ever reimplemented without `renameio`, the test continues to pass without verifying the new implementation. Best practical approach for M0 (real crash injection requires OS-level fault injection); revisit if memory layer is ever re-implemented. File: `core/memory/atomic_test.go`.
- **`WriteAtomic` perm argument subject to umask on first write** — `renameio.WriteFile` passes `perm` to the underlying temp-file `Create`; effective mode is `perm & ^umask`. A caller expecting exact `0o600` on a system with `umask=0177` gets `0o400`. No M0 caller exists; Epic 4 should document this or use `WithStaticPermissions`/`IgnoreUmask` when file ownership matters. File: `core/memory/atomic.go`.
- **`run:pi` task has no output assertion** — `printf "ping shelldon\n" | timeout -s TERM 2 ./shelldon` exits 0 on clean SIGTERM regardless of whether output was echoed; a hung or silent shelldon produces a false-green result. Dev-tool manual verification task — visual output is the check. Consider adding `| grep -q "ping shelldon"` for a lightweight CI-safe variant. File: `Taskfile.yml:run:pi`.
- **`timeout -s TERM 2` in `run:pi` sends SIGTERM only** — if shelldon ignores or delays SIGTERM, the process survives on the Pi indefinitely; the next `run:pi` may fail or behave unexpectedly. GNU `timeout --kill-after=1s 2s` adds a SIGKILL safety net. Low-severity dev-tool concern. File: `Taskfile.yml:run:pi`.
