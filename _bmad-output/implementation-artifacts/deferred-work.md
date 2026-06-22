# Deferred Work

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
