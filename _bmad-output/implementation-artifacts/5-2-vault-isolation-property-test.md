---
baseline_commit: 279da98c88038eef71ffb177c93b9ba1cbb6f9d8
---
# Story 5.2: Vault + isolation property test

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story.
     Lineage: second story of Epic 5 (M3 — "The Wall"). 5.1 opened the process/uid wall
     (privsep subprocess + UDS+gob transport) and wired+gated the uid-drop but asserted NO
     OS enforcement. 5.2 is that enforcement proof: create the vault excluding the worker uid
     and prove — as a property test on the Pi — that a worker-uid process cannot read it. -->

## Story

As the system,
I want the `vault/` created with directory permissions that exclude the worker uid, and a property test that proves a process running as the worker uid cannot read vault contents,
so that vault isolation is **OS-enforced, not a path filter** — the read is denied by the kernel, closing the M3 confidentiality gate (NFR6, AD-3).

## Context

**Second story of Epic 5 (M3 — "The Wall"); the enforcement proof that 5.1's wall was built to carry.** Story 5.1 landed the *mechanism* of the wall: a uid-separable recycled subprocess behind the unchanged `worker.Worker` seam, with the transport swapped to length-prefixed `gob` over a socketpair UDS, and the uid-drop wired + gated (`worker/privsep/cred_linux.go` sets `SysProcAttr.Credential` only on Linux + root + a configured non-zero uid). 5.1 explicitly **asserted the gating *decision*, not the OS *enforcement*** — it left "OS-enforced uid read-denial" as this story's job (see 5.1 AC3 + Decisions-to-confirm #2). 5.2 collects that debt: it creates the `vault/` with permissions that exclude the worker uid, and proves with a property test that a process *as that uid* is denied the read.

**This is the AD-3 invariant made testable.** AD-3 ("the vault never exists until the worker is across a process wall") has two halves. Half one — *the vault doesn't exist before M3* — is already enforced and tested: `core/memory/curated.go` rejects any `vault/` write with `ErrOwnerOnly` and creates nothing (`curated_test.go:157-162`, `:279-283`), and the dream cycle's `sensitiveLaneEnabled` stays `false` so nothing is ever routed to a vault (`core/dream/dream.go:34-38`, `dream_test.go:199-200`). Half two — *at M3+, `vault/` permissions exclude the worker uid (OS-enforced, not a path-filter)* — is **this story**. The phrase "OS-enforced, not a path-filter" is the whole point: confidentiality must survive a worker that ignores or subverts the `curated.go` path rejection, because the worker is untrusted-by-design (it assembles prompts from web-influenced content). The kernel, not Go code, denies the read.

**Mechanism: a `0700` vault dir owned by the core (parent) uid — any other uid is excluded by the kernel.** The worker subprocess is dropped to a *different, unprivileged* uid (5.1, `SHELLDON_WORKER_UID`). A `vault/` directory created mode `0700` and owned by the core/parent uid is, by POSIX rules, unreadable and untraversable to every other uid — including the worker uid. No ACLs, no xattrs, no path filter: plain owner-exclusion is the simplest correct OS enforcement and is exactly what AD-3 ("`vault/` permissions exclude the worker uid") calls for. The property test proves the negative: a process as the worker uid gets `EACCES` (`fs.ErrPermission`) on both `os.ReadFile(vault/secret)` and traversal of `vault/`.

**The enforcement is real only on the Pi (Linux + root + worker-uid ≠ core-uid); everywhere else it skips with a logged reason.** This mirrors 5.1 exactly. On darwin dev and non-root CI the worker can't actually be dropped to a separate uid, so the worker process runs *as the core uid* and **can** read a `0700` core-owned vault — the property cannot hold and the test must `t.Skip` with a clear reason (not fail, not silently pass). The OS-enforced denial is asserted only where the OS can enforce it: Linux, parent is root, able to drop to a distinct unprivileged uid. A fast, platform-independent **structural** assertion (the vault is created mode `0700`, owned by the creating uid, and `curated.WriteFile`/`AppendFact` still reject it) runs everywhere to guard the wiring on the laptop.

**Vault creation is core-owned and gated on the worker being uid-separated (AD-3).** The vault is **not** created by the bot/LLM path — `curated.go` rejects `vault/` and must keep rejecting it (that disjoint-writer invariant is load-bearing). Core creates the empty `vault/` directly, with the exclusion perms, and only when the worker is actually uid-separated (`SHELLDON_WORKER=privsep` + a non-zero `SHELLDON_WORKER_UID` on Linux+root). Before M3 / without a configured worker uid there is still nothing for a goroutine-worker to read (AD-3 half one holds untouched).

**This story does NOT:**
- turn on the sensitive-classification lane (`sensitiveLaneEnabled` stays `false`) or route any learning into the vault — that is **5.3**. 5.2 creates an *empty, isolated* vault and proves the isolation; it populates nothing.
- wire the worker's memory-read back-channel across the wall, or broker-gated surfacing of vault contents into a prompt (AD-9) — those remain the Epic-5 follow-ons flagged in 5.1.
- change the `worker.Worker` interface, the arbiter, dispatch, the scheduler, the contracts, or the privsep transport (5.1) — 5.2 adds vault creation + a property test; it reshapes no caller.
- relax `curated.go`'s `vault/` rejection — that invariant stays exactly as-is; the property test is **additive**.

## Acceptance Criteria

1. **The vault is created with permissions that exclude the worker uid (OS-enforced, not a path-filter).**
   **Given** the worker is uid-separated (privsep + a configured worker uid distinct from the core uid)
   **When** core ensures the `vault/` directory under the curated memory root
   **Then** `vault/` exists as a directory owned by the core (parent) uid with mode `0700`, so the kernel excludes every other uid — including the worker uid — from reading or traversing it (NFR6/AD-3); **and** the existing `curated.WriteFile`/`AppendFact` `vault/` rejection (`ErrOwnerOnly`, nothing created) is unchanged.

2. **A property test proves a worker-uid process cannot read the vault.**
   **Given** a `vault/` directory created mode `0700` owned by the core uid, containing a secret file, on Linux as root
   **When** a child process dropped to a distinct unprivileged worker uid (via `SysProcAttr.Credential`, the 5.1 mechanism) attempts to read a file inside `vault/` and to traverse `vault/`
   **Then** both attempts are denied by the kernel — the child receives a permission error (`fs.ErrPermission` / `EACCES`), never the secret bytes — proving isolation is OS-enforced (the AD-3 property, NFR6).

3. **Off the enforcing platform the property test skips with a reason; the structural guard runs everywhere; default is unchanged.**
   **Given** a host that is not Linux, or where the parent is not root, or where a distinct worker uid cannot be dropped
   **When** the property test runs
   **Then** it `t.Skip`s with a logged reason (the OS cannot enforce the drop here — proof is Pi-only), **and** a platform-independent structural test still asserts the vault is created `0700` + core-owned and that `curated.go` still rejects `vault/`; **and** with `SHELLDON_WORKER` unset (or no worker uid configured) `main` creates **no** vault and behaves exactly as before (no regression).

## Tasks / Subtasks

- [x] **Task 1 — Core vault creation helper (`core/memory/`)** (AC: 1, 3)
  - [x] Added `EnsureVault()` as a method on `*Curated` (`core/memory/vault.go`): `os.MkdirAll(<root>/vault, 0o700)` + `os.Chmod(0o700)` to pin the mode past umask; returns the absolute vault path. Owned by the core uid by construction. Idempotent (2nd call re-asserts mode, succeeds).
  - [x] Core-direct create — does NOT go through `curated.WriteFile`; the `vault/` rejection (`curated.go:63-66`) is untouched. Disjoint-writer invariant preserved.
  - [x] Stdlib-only (`os`/`path/filepath`/`fmt`); no broker/provider import — LLM-free-core fence intact.

- [x] **Task 2 — Structural test, runs everywhere (`core/memory/vault_test.go`)** (AC: 1, 3)
  - [x] `TestEnsureVault_Perms`: under `t.TempDir()`, asserts `vault/` exists, is a dir, `Mode().Perm() == 0o700`, and idempotency (2nd call succeeds, still `0700`).
  - [x] `TestEnsureVault_DisjointWriterUnchanged`: after `EnsureVault`, `WriteFile`/`AppendFact` to `vault/` STILL return `ErrOwnerOnly` and the vault stays empty (0 entries). Stdlib-only, no-testify, matching project style.

- [x] **Task 3 — The OS-enforced isolation property test, Pi-gated (`core/memory/vault_isolation_linux_test.go`, `//go:build linux`)** (AC: 2, 3)
  - [x] `TestVaultIsolation_WorkerUIDDenied` gates `if os.Geteuid() != 0 { t.Skip(...) }` (the `//go:build linux` tag already excludes non-Linux). Worker uid = `65534` (nobody); choice documented.
  - [x] Uses `os.MkdirTemp` under world-traversable `/tmp` chmod'd `0755` so the `0700` vault is the SOLE denial barrier (not an incidental parent-dir perm masking the result). Seeds `vault/secret.md` (0600, core-owned).
  - [x] Child re-exec idiom (mirrors 5.1's `TestMain`/`IsChild`): copies the test binary into the `0755` root, re-execs it dropped to the worker uid via `SysProcAttr.Credential`, sentinel env carries the secret path. Child probes `ReadFile(secret)` + `ReadDir(vault)`; parent reads the exit code — both `fs.ErrPermission` = PASS, returned bytes = FAIL. Distinguishes "couldn't exec as worker uid" (→ skip, not false pass).
  - [x] Child bounded by exit-code protocol; `defer os.RemoveAll(root)` teardown.

- [x] **Task 4 — Wire vault creation in `main`, gated on uid-separation (`cmd/shelldon/main.go`)** (AC: 1, 3)
  - [x] In the `case "privsep":` branch, when `uid != 0` calls `curated.EnsureVault()` and logs the M3 vault creation; on error `os.Exit(1)` (mirrors the sibling `OpenCurated` failure handling).
  - [x] `SHELLDON_WORKER` unset or `uid == 0` → no vault created (boot smoke confirmed). Zero default-boot change.
  - [x] Updated the `main.go:115-117` comment to reflect the vault is now created with worker-uid-excluding perms.

- [x] **Task 5 — Validation + regression gate** (AC: 1, 2, 3)
  - [x] `go test -race ./...` → 159 pass / 23 packages (was 157 + the 2 new structural tests; the Linux property test is excluded on darwin by the build tag).
  - [x] Native build + `CGO_ENABLED=0 GOOS=linux GOARCH=arm64` build green (Linux test binary cross-compiles clean); `golangci-lint run` → 0 issues; `go vet ./...` clean.
  - [x] Zero-diff confirmed: `worker/privsep/*`, `worker.Worker`, arbiter, dispatch, scheduler, contracts, and `curated.go`'s `vault/` rejection all untouched. `dream.go`'s `sensitiveLaneEnabled` STILL `false`.
  - [x] Boot smoke (prebuilt binary, clean HOME): privsep+uid logs `vault: created uid-isolated (M3)` and the dir exists; privsep-no-uid and default create NO vault.
  - [x] **Pi proof — DONE, PASS.** `TestVaultIsolation_WorkerUIDDenied` ran as root on the Pi (gotchi, aarch64, go1.24.4) via a Mac-cross-compiled arm64 test binary (the Pi has only 416 MiB RAM — compiling modernc/sqlite on-box OOM-hangs it, so `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c` on the Mac + scp is the verification pattern). Result: `--- PASS: TestVaultIsolation_WorkerUIDDenied (0.04s)` — a uid-65534 process was kernel-denied the vault read. AC2 OS-enforcement is proven on hardware.

## Dev Notes

### Architecture constraints (binding)

- **AD-3 — The vault never exists until the worker is across a process wall; at M3+ `vault/` permissions exclude the worker uid (OS-enforced, not a path-filter).** This story implements the second half: "`vault/` permissions exclude the worker uid (OS-enforced, not a path-filter)." The mechanism is a `0700` core-owned dir; the proof is the uid-dropped property test. Half one (vault doesn't exist pre-M3) stays enforced by `curated.go` + `sensitiveLaneEnabled=false`. [Source: ARCHITECTURE-SPINE.md#AD-3 (lines 72-75), #Consistency-Conventions (line 159: "`vault/` … then OS-unreadable to the worker uid")]
- **NFR6 — vault isolation is OS-enforced.** The epic AC names NFR6 directly: the worker process *cannot* read the vault — kernel-denied, not Go-denied. The property test is the NFR6 evidence. [Source: epics.md#Story 5.2]
- **AD-2 / 5.1 — uid-separated recycled subprocess; the drop mechanism already exists.** `worker/privsep/cred_linux.go:applyCredential` sets `SysProcAttr.Credential{Uid,Gid}` on Linux + root + configured uid. 5.2 *reuses this exact mechanism* in the property test (re-exec a child dropped to the worker uid) — do not invent a second drop path. [Source: worker/privsep/cred_linux.go, ARCHITECTURE-SPINE.md#AD-2]
- **AD-1 / NFR3 — LLM-free core; depguard fence.** Vault creation lives in `core/memory` (core side). It must import only stdlib — no broker/provider. The property test re-execs the test binary (stdlib `os/exec` + `syscall`), pulling in nothing that breaches the core fence. [Source: ARCHITECTURE-SPINE.md#AD-1, broker/imports_test.go]
- **AD-6 — one writer (core) for memory; bot proposes only.** Core creating the empty `vault/` dir is consistent with "core is the single writer." The bot/LLM path (`curated.WriteFile`) must remain barred from `vault/`. Creating the dir does NOT open a bot write path to it. [Source: ARCHITECTURE-SPINE.md#AD-6, core/memory/curated.go]
- **AD-9 — broker is sole cred holder; vault surfacing is broker-gated.** Out of scope here: 5.2 creates an *empty isolated* vault and proves isolation. Routing sensitive learnings in (5.3) and surfacing vault contents into a prompt (broker-gated) are later. No broker interaction in this story. [Source: ARCHITECTURE-SPINE.md#AD-9]

### Key design decisions (made; flagged where genuinely forked)

- **`0700` owner-exclusion, not ACLs/xattrs.** The worker is a *different* uid; a `0700` core-owned dir excludes it by plain POSIX rules. Simplest correct OS enforcement, portable to the arm64 Pi, nothing to configure. `os.Chmod` after `MkdirAll` pins the mode past umask.
- **Vault creation is core-direct + gated on `uid != 0`, NOT via `curated.WriteFile`.** Keeps the disjoint-writer invariant intact (bot never touches the vault) and honors AD-3's "vault gated on the worker being uid-separated." Default boot (Monolith+ / no worker uid) creates no vault.
- **Property test is Pi-only by necessity; skips loudly elsewhere.** Real uid-drop needs Linux + root. The structural test (perms + disjoint-writer) runs everywhere to guard the wiring on the laptop; the OS-enforcement assertion runs only where the OS can enforce it. A skip with a reason — never a silent pass — so "proven" is honest. (Same split 5.1 used for the drop.)
- **Re-exec the test binary, dropped to the worker uid (mirror 5.1's `TestMain`/`IsChild`).** The standard Go "re-exec self + env sentinel" idiom, now with `SysProcAttr.Credential` to drop. No second binary, no fixtures.
- **Worker uid default `65534` (nobody) for the test.** Conventional unprivileged uid present on the Pi. Document it; allow override if the Pi uses a dedicated `shelldon-worker` uid (the real production value is an ops concern, not a code constant).

### Previous story / codebase intelligence

- **5.1 left this exact debt.** 5.1 AC3 + Completion Notes: "uid-drop wired + gated, not asserted-enforced … OS-enforced read-denial is Story 5.2's property test on the Pi." `main.go:116` literally says "Story 5.2 adds the matching vault exclusion." This story closes that loop. [Source: 5-1-privsep-lite-worker-subprocess-gob-transport-swap.md AC3/Completion Notes, cmd/shelldon/main.go:116]
- **Vault rejection already tested — extend, don't duplicate.** `curated_test.go:157-162` and `:279-283` already assert `vault/` writes are rejected and nothing is created. Task 2's disjoint-writer assertion should *reuse/extend* that, asserting it STILL holds after `EnsureVault` creates the (now-existing) dir. [Source: core/memory/curated_test.go, core/memory/curated.go:63-66]
- **Dream sensitive lane stays off.** `core/dream/dream.go:34-38` `sensitiveLaneEnabled=false`; `dream_test.go:199-200` asserts no `vault/` path appears after a dream. 5.2 must not flip this — 5.3 does. The new vault dir created by `main` is *outside* the dream's `t.TempDir()` roots, so `dream_test` is unaffected. [Source: core/dream/dream.go, core/dream/dream_test.go]
- **Privsep drop mechanism to mirror.** `worker/privsep/cred_linux.go` (gating: uid configured + `Geteuid()==0`) and `privsep_test.go:42-55` (`TestMain`/`IsChild` child re-exec). The property test's child re-exec + credential drop should read like these. [Source: worker/privsep/cred_linux.go, worker/privsep/privsep_test.go]
- **main wiring point.** `cmd/shelldon/main.go:102` opens curated at `~/.shelldon/memory`; the `privsep` switch is `:119-135`. `EnsureVault` slots into the `case "privsep":` branch after `uid` is resolved. [Source: cmd/shelldon/main.go]
- **No new dependency.** `os`, `os/exec`, `syscall`, `path/filepath`, `runtime`, `io/fs` are all stdlib. No `go.mod` change. [Source: go.mod]

### Latest tech information

- No external libraries. The whole story is Go stdlib: `os.MkdirAll`/`os.Chmod` (mode pinned past umask), `os.ReadFile`/`os.ReadDir` returning `*fs.PathError` wrapping `syscall.EACCES` — assert with `errors.Is(err, fs.ErrPermission)` (the portable, version-stable check). `SysProcAttr.Credential` for the uid drop is unchanged across current Go releases. Nothing here is version-sensitive; no web research warranted.

### Project Structure Notes

- **New:** `core/memory/vault.go` (or add `EnsureVault` to an existing memory file) — the core-direct vault creator. `core/memory/vault_test.go` — structural perms + disjoint-writer (everywhere). `core/memory/vault_isolation_linux_test.go` (`//go:build linux`) — the uid-dropped property test.
- **Modified:** `cmd/shelldon/main.go` — `EnsureVault` call in the `privsep` branch + the `:115-117` comment update. Possibly extend `core/memory/curated_test.go` for the post-create disjoint-writer assertion.
- **Unchanged (must stay zero-diff):** `worker/privsep/*` (5.1 transport), `worker/worker.go`, `core/arbiter/*`, `core/dispatch/*`, `core/scheduler/*`, `contracts/*`, `curated.go`'s `vault/` rejection logic, `dream.go`'s `sensitiveLaneEnabled=false`.
- **Build tags:** the property test is `//go:build linux` so the arm64-Linux build/test path runs it and darwin dev simply doesn't compile it (or compiles a skipping stub). `core/memory/vault.go` is portable (no tag).

### References

- [Source: epics.md#Story 5.2] — the AC: vault excludes worker uid; property test proves the worker process cannot read the vault (NFR6/AD-3).
- [Source: ARCHITECTURE-SPINE.md#AD-3 (72-75), #Consistency-Conventions (159)] — vault gated on uid-separation; at M3+ `vault/` permissions exclude the worker uid, OS-enforced not path-filter, then OS-unreadable to the worker uid.
- [Source: ARCHITECTURE-SPINE.md#AD-2, worker/privsep/cred_linux.go] — the uid-drop mechanism (`SysProcAttr.Credential`, Linux+root-gated) the property test reuses.
- [Source: 5-1-…-gob-transport-swap.md AC3 + Completion Notes, cmd/shelldon/main.go:116] — 5.1 wired+gated the drop and explicitly deferred OS-enforcement proof to 5.2.
- [Source: core/memory/curated.go:63-66, curated_test.go:157-162/279-283] — the existing `vault/` disjoint-writer rejection to preserve and extend.
- [Source: core/dream/dream.go:34-38, dream_test.go:199-200] — `sensitiveLaneEnabled=false`; nothing routed to a vault (stays off; 5.3 flips it).
- [Source: worker/privsep/privsep_test.go:42-55] — the `TestMain`/`IsChild` re-exec idiom to mirror for the child uid-drop in the property test.

### Decisions to confirm (surfaced for Elliot — defaults chosen, override if you disagree)

1. **Vault-creation scope.** Default: a small core `EnsureVault` helper + wire it in `main`'s `privsep` branch (vault becomes a real, gated artifact) **plus** the property test. Alternative: property-test-only (the test fabricates its own vault; `main` untouched). **Recommend the default** — it makes the vault real and gated per AD-3, and keeps the test honest about the production path. ~½ day either way.
2. **Enforcement mechanism.** Default: plain `0700` owned by the core uid (worker is a different uid → excluded). Alternative: explicit per-uid ACL/xattr. **Recommend `0700`** — simplest correct OS enforcement, portable to arm64, exactly what AD-3 specifies. No ACLs.
3. **Property-test location + uid.** Default: `core/memory/vault_isolation_linux_test.go`, child dropped to `65534` (nobody), `t.Skip` off Linux+root. Alternative: live it in `worker/privsep`. **Recommend `core/memory`** — the vault is core-owned; the test asserts a core invariant. Confirm the Pi's intended worker uid if it isn't `nobody`.
4. **Pi proof timing.** The OS-enforcement assertion only *executes* on the Pi. Default: land the code + the skip-everywhere-else behavior now, and record an explicit Pi run result in completion notes (run it on the Pi as part of this story). If the Pi isn't available this sprint, the story ships with the test SKIPPING and that fact stated plainly — not claimed as "proven." Confirm whether a Pi run gates "done."

## Dev Agent Record

### Agent Model Used

claude-opus-4-8[1m] (BMad dev-story workflow; single-orchestrator — the surface is one helper + two test files + a main wiring line).

### Debug Log References

- **Parent-dir perms could mask the property.** `t.TempDir()` is `0700` owned by root, so a worker-uid child would be denied at the *parent* dir before ever reaching the vault — a false PASS that holds even if the vault were `0777`. Fixed by using `os.MkdirTemp` under world-traversable `/tmp` and `chmod 0755` on the root, making the vault's own `0700` the SOLE denial barrier.
- **Test binary not exec'able by the dropped uid.** `go test`'s binary lives in a `0700` go-build temp dir, so `nobody` can't exec `os.Args[0]` directly — `cmd.Run()` would fail to start and look like (but isn't) a denial. Fixed by copying the binary into the `0755` root (`copyExecutable`) and re-exec'ing the copy; a genuine start-failure maps to `t.Skip` (environment can't run the property), never a false pass.
- **Pre-existing stale `~/.shelldon/history.db` blocked the default boot smoke** (`migrate: no such column: convo_id`) — an out-of-sync dev DB, unrelated to 5.2 (fails at `memory.Open`, before the vault code). Worked around by smoking a prebuilt binary against a clean `HOME`.

### Completion Notes List

- **AC1 (vault excludes the worker uid).** `core/memory/vault.go:EnsureVault()` creates `<curated-root>/vault/` as `0700` owned by the core uid (`MkdirAll` + `Chmod` to pin past umask). With the worker dropped to a *different* uid (5.1), `0700` excludes it by plain POSIX rules — OS-enforced, not a path filter. `TestEnsureVault_Perms` asserts the mode deterministically; `TestEnsureVault_DisjointWriterUnchanged` proves the bot `WriteFile`/`AppendFact` path STILL rejects `vault/` (`ErrOwnerOnly`) and the vault stays empty.
- **AC2 (property: worker-uid process is denied the read) — PROVEN on the Pi.** `vault_isolation_linux_test.go` (`//go:build linux`) re-execs a copy of the test binary dropped to uid `65534` via `SysProcAttr.Credential` (the 5.1 mechanism) and asserts the child gets `fs.ErrPermission` on both `ReadFile(vault/secret.md)` and `ReadDir(vault/)`; any returned bytes fail the test. Ran as root on gotchi (aarch64): `--- PASS: TestVaultIsolation_WorkerUIDDenied (0.04s)` — the kernel denied the worker-uid read. Verification pattern: Mac cross-compile (`GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c`) + scp the binary, because the Pi's 416 MiB RAM OOM-hangs an on-box modernc/sqlite compile.
- **AC3 (skips off-platform; structural everywhere; default unchanged).** Off Linux+root the property test skips with a reason (never a silent pass); the structural `0700`/disjoint-writer tests run everywhere. Boot smoke confirms: `SHELLDON_WORKER=privsep` + `SHELLDON_WORKER_UID=65534` creates the vault and logs it; privsep-no-uid and default create no vault (AD-3 — vault gated on genuine uid-separation).
- **Scope honored.** `sensitiveLaneEnabled` stays `false` (5.3 flips it); nothing is routed into the vault — it is created *empty and isolated*. No change to the seam, arbiter, dispatch, scheduler, contracts, or the 5.1 privsep transport. `curated.go`'s `vault/` rejection is unchanged; vault creation is core-direct.
- **Validation:** `go test -race ./...` → 159 pass / 23 packages; native + arm64-linux builds green; `golangci-lint run` → 0; `go vet` clean.
- **`baseline_commit` corrected to `279da98`** (the story-spec commit, the true pre-dev state) from the placeholder `a1da2dd` set during create-story, so code-review diffs only 5.2's work and not 5.1's already-merged privsep package.

### File List

- `core/memory/vault.go` (new — `EnsureVault` + `VaultDir`/`vaultPerm`)
- `core/memory/vault_test.go` (new — structural perms + disjoint-writer, runs everywhere)
- `core/memory/vault_isolation_linux_test.go` (new — `//go:build linux` OS-enforced property test + `TestMain` child entry)
- `cmd/shelldon/main.go` (modified — `curated.EnsureVault()` in the privsep branch when uid≠0 + comment update)

### Review Findings

- [x] [Review][Patch] Full parent environment passed to uid-65534 probe child — secrets leak [`core/memory/vault_isolation_linux_test.go:117`] — FIXED: child now gets a minimal env (`[]string{vaultProbeEnv+"="+secret}`), never `os.Environ()`. Re-proven on the Pi: `--- PASS: TestVaultIsolation_WorkerUIDDenied`.
- [x] [Review][Defer] TOCTOU between `MkdirAll` and `Chmod` in `EnsureVault` [`core/memory/vault.go:32-38`] — deferred, OS limitation; called once at startup before worker exists; negligible risk
- [x] [Review][Defer] Ambient `SHELLDON_VAULT_PROBE` env var can intercept `TestMain`, exit with 0 tests run [`core/memory/vault_isolation_linux_test.go:43`] — deferred, highly unlikely (specific name; misfire exits non-zero unless path happens to be permission-denied to the test user); low-priority hardening
- [x] [Review][Defer] `SHELLDON_WORKER_UID=0` silently skips vault creation with no warning [`cmd/shelldon/main.go:133`] — deferred, intentional per spec ("uid == 0 → no vault"), but worth a slog.Warn; park for Epic 6 ops hardening
- [x] [Review][Defer] `EnsureVault` does not verify vault ownership after `Chmod` [`core/memory/vault.go:38`] — deferred, pre-existing vault with wrong uid is extremely unlikely; hardening concern only

## Change Log

| Date       | Change |
|------------|--------|
| 2026-06-25 | Code-review patch: probe child re-exec'd with a minimal env (sentinel only), not the parent's full `os.Environ()`, so the uid-dropped child can't inherit parent secrets. Re-proven on the Pi (PASS). 4 review defers accepted as-is. |
| 2026-06-25 | Story 5.2 implemented: `core/memory.EnsureVault` creates `vault/` `0700` core-owned (worker-uid excluded, OS-enforced — NFR6/AD-3); wired in `main` gated on `SHELLDON_WORKER=privsep` + a configured worker uid. Structural tests (perms + disjoint-writer) run everywhere; the Linux property test (`//go:build linux`) re-execs a uid-dropped child and asserts `fs.ErrPermission` on the vault read — **PROVEN on the Pi** (`--- PASS: TestVaultIsolation_WorkerUIDDenied`, root, aarch64; kernel denied a uid-65534 read). Validation green: 159 tests -race, arm64 build, lint 0, vet clean. `baseline_commit` corrected to the 5.2-spec commit. Status → review. |
