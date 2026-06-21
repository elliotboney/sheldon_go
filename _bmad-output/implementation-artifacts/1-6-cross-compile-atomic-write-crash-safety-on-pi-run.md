---
baseline_commit: af0fbdf
---

# Story 1.6: Cross-compile + atomic-write crash-safety + on-Pi run

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer building shelldon,
I want the spine to cross-compile to a single static arm64 binary, perform crash-safe atomic markdown writes, and pass all four required tests plus the CLI round-trip on the Pi itself,
so that M0 is a real walking skeleton validated on target hardware, not just the laptop (NFR2, AD-10).

## Context

Sixth and final Epic 1 story — **M0 closes here**. Stories 1.1–1.5 built and wired the spine on the laptop: versioned contracts (1.1), the bus hub (1.2), the worker seam + ≤1-worker arbiter (1.3), the suture supervisor root + soul-survives-panic test (1.4), and the CLI transport end-to-end round-trip (1.5). Three of the four required M0 tests already exist (gob round-trip, ≤1-worker, soul-survives-edge-panic). This story delivers the **fourth required M0 test — atomic-write crash-safety** — and then proves the whole thing **on the Pi Zero 2W**, which is the architecture's hard M0 success criterion ("passing ON THE PI, not the laptop").

It has two distinct halves:

1. **Laptop-completable code (AC1, AC2):** add `google/renameio/v2`, an atomic markdown-write helper (AD-7), and the required crash-safety test (the 4th M0 test); make the static-arm64 cross-compile a reproducible one-command build.
2. **Hardware-gated validation (AC3, AC4):** cross-compile the four required test binaries + the `shelldon` binary for arm64, deploy them to the Pi, run the four required tests there, and prove a CLI message round-trips on the Pi.

The atomic-write helper is the **seed of the memory layer** (AD-7) — Epic 4 builds the full sqlite + curated-markdown tree on top. M0 needs only the atomic-write primitive and its crash-safety test, nothing more.

Keep scope tight. This story is the atomic-write primitive + its required test + the reproducible cross-compile + the on-Pi validation. It does **NOT** build the sqlite store, the curated markdown tree, `DIRECTIVE.md`, the dream cycle, or any memory-op vocabulary (all Epic 4); it does **NOT** add the systemd unit / `MemoryHigh`/`OOMPolicy` hardening (deploy-time / Epic 6 — the M0 on-Pi proof runs the binary directly); it does **NOT** wire the atomic-write helper into any runtime path yet (no caller until Epic 4 — the helper + test stand alone for M0); and it does **NOT** add the explicit parent-directory fsync durability step beyond what renameio/v2 provides (atomicity alone satisfies the M0 crash-safety test, AD-7).

> **Hardware gate:** AC3 and AC4 require the Pi Zero 2W reachable over SSH (key auth) with a known host. The laptop work (AC1, AC2, and the deploy/test mechanism) completes without it; the on-Pi run is executed against the real device. If the Pi is not yet reachable, Tasks 0–4 + 6 complete on the laptop and Task 5 (the on-Pi run) is the verification step that finishes the story when hardware is available.

## Acceptance Criteria

1. **Single static arm64 binary, no CGo.**
   **Given** the M0 build
   **When** `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build` runs
   **Then** it produces a single static binary with no CGo dependency (NFR2).

2. **Atomic markdown write is crash-safe.**
   **Given** a markdown write via renameio/v2
   **When** the write is interrupted mid-rename (the required atomic-write crash-safety test, NFR11/AD-7/AD-10)
   **Then** the prior file is left intact — no partial/corrupt file is observable.

3. **The four required M0 tests pass on the Pi.**
   **Given** the binary deployed to the Pi Zero 2W
   **When** the four required M0 tests run ON THE PI (gob round-trip, ≤1-worker, atomic-write crash-safety, soul-survives-edge-panic)
   **Then** all four pass on the Pi, not only the laptop (AD-10).

4. **The CLI round-trip completes on the Pi.**
   **Given** the binary running on the Pi
   **When** a CLI message is sent
   **Then** the inbound→core→worker seam→stub→outbound round-trip completes on the Pi.

## Tasks / Subtasks

- [x] **Task 0 — Add the google/renameio/v2 dependency** (AC: 2)
  - [x] `go get github.com/google/renameio/v2@latest` (latest is **v2.0.2**); commit the updated `go.mod` + `go.sum`. This is the project's **second** external dependency (after suture/v4). renameio is pure-Go — confirm `CGO_ENABLED=0 GOARCH=arm64 GOOS=linux go build ./...` still succeeds
  - [x] Verify the v2 API at go-get time before coding: `renameio.WriteFile(filename string, data []byte, perm os.FileMode, opts ...Option) error`, and the lower-level `renameio.NewPendingFile(path string, opts ...Option) (*PendingFile, error)` with `Write` (from the embedded `*os.File`), `CloseAtomicallyReplace`, and `Cleanup` (used by the crash-safety test). Do **not** add any other dependency this story

- [x] **Task 1 — Atomic markdown-write helper (`core/memory/`)** (AC: 2)
  - [x] Create `core/memory/atomic.go`. Package doc: the seed of the memory layer (AD-7) — Epic 4 builds the sqlite store + curated markdown tree on top; M0 needs only the atomic-write primitive. Note this package is **not yet wired into any runtime path** (no caller until Epic 4); it exists for the required M0 crash-safety test
  - [x] `WriteAtomic(path string, data []byte, perm os.FileMode) error` wrapping `renameio.WriteFile`. Doc the guarantee (AD-7): a reader sees either the prior file or the fully-written new file, never a partial/torn write — renameio writes a temp file in the same dir, fsyncs it, then renames over `path`; the **rename is the atomic commit point**. Note the parent-dir fsync durability add (AD-7) beyond renameio's atomicity is deferred (atomicity alone satisfies the M0 test)

- [x] **Task 2 — Required atomic-write crash-safety test (the 4th M0 test)** (AC: 2)
  - [x] Create `core/memory/atomic_test.go`. **Happy path** (`TestWriteAtomic_ReplacesAtomically`): write `"ORIGINAL"` then `"UPDATED"` via `WriteAtomic` to a `t.TempDir()` path; assert the file reads `"UPDATED"` and **no temp file is left behind** (assert the dir contains exactly the target file)
  - [x] **Crash-safety** (`TestWriteAtomic_CrashSafety` — the required M0 test): write `"ORIGINAL"`; then begin a `renameio.NewPendingFile(path)` write of torn content but **abort with `Cleanup()` BEFORE `CloseAtomicallyReplace()`** — this models a crash/interrupt before the rename commits. Assert the target still reads `"ORIGINAL"` (prior file intact, no partial/corrupt observable) and the dir contains exactly the target file (no orphaned temp). This is the honest test of the property "interrupted mid-rename leaves the prior tree intact" — you cannot kill a real `rename(2)` mid-syscall, so the test exercises the atomic-commit boundary renameio is built on
  - [x] Use a `t.Helper()` `assertOnlyFile(t, dir, name)` that reads the dir and asserts exactly one entry, named `name` (catches both a leftover temp and a partial file at the target). stdlib `testing` + `os.ReadDir`, no testify (1.x convention)
  - [x] `go test -race ./core/memory/` passes

- [x] **Task 3 — Reproducible static-arm64 cross-compile (AC: 1)** (AC: 1)
  - [x] Add a `Taskfile.yml` at the repo root (the architecture's named build/deploy driver — "watchexec/Taskfile drives the loop"). `build` target: `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o dist/shelldon ./cmd/shelldon` (`-s -w` strips debug info for a smaller binary on the SD-constrained Pi, per the spine build line). Do **not** set `GOARM64=v8.0,lse` (Cortex-A53 lacks LSE atomics)
  - [x] Verify the output is a static arm64 ELF with no CGo: `file dist/shelldon` → `ELF 64-bit LSB executable, ARM aarch64, version 1 (SYSV), statically linked, … stripped` (a `CGO_ENABLED=0` Go binary links no libc; ~2.3 MB). Added `/dist/` to `.gitignore` (build artifacts are not committed)
  - [x] The raw command also works standalone (`task` is **not** installed locally — the Taskfile documents the commands; the equivalent one-liners in Dev Notes were used directly this pass)

- [x] **Task 4 — On-Pi deploy + test mechanism (laptop side of AC: 3, 4)** (AC: 3, 4)
  - [x] Extend `Taskfile.yml` with a configurable `PI_HOST` var (default `gotchi`; override via env) and `PI_DIR` (default `shelldon_go`, relative to the remote home — **not** `~/shelldon`, which is the legacy v2 install)
  - [x] `build:tests` target: cross-compile the **four required** M0 test binaries for arm64 with `go test -c` (pure-Go, so `CGO_ENABLED=0 GOARCH=arm64` cross-compiles cleanly): `./contracts` → `dist/contracts.test`, `./core/arbiter` → `dist/arbiter.test`, `./core/memory` → `dist/memory.test`, `./core/supervisor` → `dist/supervisor.test`
  - [x] `deploy` target: `rsync -a dist/` to `PI_HOST:PI_DIR` (cross-compile on the laptop, never compile on the Pi — NFR2/spine). `test:pi` target: ssh and run each test binary filtered to its required test. **No `-race` on the Pi** (race detector is laptop/CI-only per AD-10, and needs CGo)
  - [x] `run:pi` target (AC4): ssh and `printf 'ping shelldon\n' | timeout -s TERM 2 ./shelldon` — the binary echoes the line then exits cleanly on SIGTERM (the 1.5 round-trip, now on the Pi)

- [x] **Task 5 — Execute on the Pi Zero 2W (AC: 3, 4)** (AC: 3, 4) — **HARDWARE VALIDATED**
  - [x] `gotchi` reachable over SSH (aarch64, Pi OS / Debian, kernel 6.12.75 rpi-v8). Deployed to `gotchi:~/shelldon_go` and ran the four required test binaries — **all four PASS on the Pi** (output captured in the Dev Agent Record). The AD-10 hard criterion is met: M0 passes on target hardware, not only the laptop
  - [x] Ran the CLI round-trip on the Pi (AC4) — `printf 'ping shelldon\nhello from the pi\n' | ./shelldon` echoed both lines back through the full inbound→core→arbiter→stub→outbound→CLI path on `gotchi`, clean SIGTERM exit. Output captured
  - [x] (Hardware was reachable this pass, so no deferral — AC3/AC4 satisfied with real Pi output)

- [x] **Task 6 — Laptop build/test/race/lint gate (no regressions)** (AC: 1, 2)
  - [x] `go build ./...` and `CGO_ENABLED=0 GOARCH=arm64 GOOS=linux go build ./...` succeed
  - [x] `go test ./...` and `go test -race ./...` pass (the new crash-safety test plus all prior tests — no regressions; 8 packages green)
  - [x] `golangci-lint run` passes (do not modify `.golangci.yml`) — 0 issues

## Dev Notes

### Architecture constraints (binding)

- **Atomic markdown writes via renameio/v2; the crash-safety test is required at M0.** "Every write is **atomic** via `google/renameio/v2` (temp + rename), which provides **atomicity**; for power-loss **durability** an explicit **parent-directory fsync** follows the rename (**atomicity alone satisfies the M0 crash-safety test, AD-10**; the dir-fsync is the durability add)." The M0 required test: "**atomic-write crash-safety** (a write interrupted mid-`rename` leaves the prior tree intact, AD-7)." This story owns the **fourth** required M0 test. [Source: ARCHITECTURE-SPINE.md#AD-7, #AD-10]
- **Single static binary; pure-Go deps; one-line cross-compile.** "Single static binary — `CGO_ENABLED=0`, `GOARCH=arm64`; **pure-Go deps only** … so cross-compile stays a **one-line build**." Build line: `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w"`. Tuning (runtime, not build): `GOMEMLIMIT≈280MiB`, `GOGC=50`; **do NOT set `GOARM64=v8.0,lse`** (Cortex-A53 lacks LSE atomics). [Source: ARCHITECTURE-SPINE.md#NFR2, #Stack/Build]
- **M0 must pass ON THE PI, not the laptop.** "M0 is a **real walking skeleton** end-to-end through the real hub, building `CGO_ENABLED=0` and passing **on the Pi**, not the laptop." Plus "narrow interfaces … no monkeypatch," and "`go test -race` on **CI/laptop only**" — so the Pi runs the tests without `-race`. [Source: ARCHITECTURE-SPINE.md#AD-10]
- **SD-card write-wear shaping.** "markdown writes are atomic (temp + fsync + rename + parent-dir fsync via `renameio/v2`). No vector DB." The M0 helper covers temp+fsync+rename (renameio); the parent-dir fsync durability add is noted, deferred. [Source: ARCHITECTURE-SPINE.md#NFR11]
- **Dev/deploy loop: cross-compile on the laptop, rsync, never compile on the Pi.** "cross-compile on the laptop (never on the Pi), `rsync` the binary with an atomic swap, `systemctl restart`; `watchexec`/Taskfile drives the loop." For the M0 proof the binary is run directly (not via systemd) — the systemd unit (`MemoryHigh=180M` < `MemoryMax=220M`, `OOMPolicy=stop`, `Restart=always`; **do NOT set `PrivateDevices`**) is deploy-time hardening, out of scope here. [Source: ARCHITECTURE-SPINE.md#Structural Seed, #Deploy]

### renameio/v2 API (verify at go-get; v2.0.2)

- `renameio.WriteFile(filename string, data []byte, perm os.FileMode) error` — the helper. Atomic: temp in the same dir → fsync → `rename` over `filename`.
- `renameio.NewPendingFile(path string, opts ...Option) (*PendingFile, error)` — the crash-safety test uses this to model an interrupt:
  - `(*PendingFile) Write([]byte) (int, error)` — write torn content to the temp.
  - `(*PendingFile) CloseAtomicallyReplace() error` — fsync + atomic rename (the commit point). **The test does NOT call this.**
  - `(*PendingFile) Cleanup() error` — abort: remove the temp, leave the target untouched. **The test calls this** to simulate "crash before rename."
- The temp file lives in the **same directory** as the target (required for an atomic `rename` on the same filesystem). After `Cleanup()` it is gone — `assertOnlyFile` confirms it.

### The crash-safety test, precisely (AC2)

The property is "a write interrupted mid-`rename` leaves the prior tree intact." A real `rename(2)` is itself atomic at the OS level — you cannot observe it half-done. The failure mode the test must exercise is therefore: **the process dies after writing the temp but before the rename commits.** renameio's `PendingFile` makes that boundary explicit — write to the temp, then `Cleanup()` (abort) instead of `CloseAtomicallyReplace()` (commit). The target file must still hold the prior content and there must be no partial file at the target path. This is deterministic, needs no fault injection into syscalls, and is `-race` clean.

```go
// core/memory/atomic.go
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
    return renameio.WriteFile(path, data, perm) // temp → fsync → atomic rename
}

// core/memory/atomic_test.go (the required crash-safety test)
func TestWriteAtomic_CrashSafety(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "about.md")
    if err := WriteAtomic(path, []byte("ORIGINAL"), 0o644); err != nil {
        t.Fatalf("seed write: %v", err)
    }
    pf, err := renameio.NewPendingFile(path)
    if err != nil {
        t.Fatalf("pending: %v", err)
    }
    if _, err := pf.Write([]byte("PARTIAL / TORN CONTENT")); err != nil {
        t.Fatalf("write temp: %v", err)
    }
    if err := pf.Cleanup(); err != nil { // abort BEFORE the rename commits
        t.Fatalf("cleanup: %v", err)
    }
    got, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("prior file must still exist: %v", err)
    }
    if string(got) != "ORIGINAL" {
        t.Fatalf("target = %q, want ORIGINAL — interrupted write corrupted the prior file", got)
    }
    assertOnlyFile(t, dir, "about.md") // no orphaned temp / partial at target
}
```

### On-Pi mechanism (AC3/AC4)

Cross-compile everything on the laptop; the Pi only runs binaries (no Go toolchain on the Pi). `go test -c` produces a runnable test binary per package; filtering with `-test.run <Name>` runs exactly the required test. The four required tests and their binaries:

| Required M0 test | Package | Binary | `-test.run` |
| --- | --- | --- | --- |
| gob round-trip | `contracts` | `contracts.test` | `TestEnvelopeRoundTrip` |
| ≤1-worker-in-flight | `core/arbiter` | `arbiter.test` | `TestArbiter_AtMostOneInFlight` |
| atomic-write crash-safety | `core/memory` | `memory.test` | `TestWriteAtomic_CrashSafety` |
| soul-survives-edge-panic | `core/supervisor` | `supervisor.test` | `TestRoot_SoulSurvivesSingleEdgePanic` |

Equivalent raw commands (Taskfile wraps these):

```bash
# AC1 — static arm64 binary
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o dist/shelldon ./cmd/shelldon
file dist/shelldon   # → ELF 64-bit LSB executable, ARM aarch64, statically linked

# Cross-compile the four required test binaries
for p in contracts core/arbiter core/memory core/supervisor; do
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o dist/$(basename $p).test ./$p
done

# Deploy + run on the Pi (PI_HOST reachable via SSH key auth)
rsync -a dist/ "$PI_HOST:/home/pi/shelldon/"
ssh "$PI_HOST" 'cd /home/pi/shelldon && \
  ./contracts.test  -test.run TestEnvelopeRoundTrip          -test.v && \
  ./arbiter.test    -test.run TestArbiter_AtMostOneInFlight  -test.v && \
  ./memory.test     -test.run TestWriteAtomic_CrashSafety    -test.v && \
  ./supervisor.test -test.run TestRoot_SoulSurvivesSingleEdgePanic -test.v'
# AC4 — CLI round-trip on the Pi
ssh "$PI_HOST" 'cd /home/pi/shelldon && printf "ping shelldon\n" | timeout -s TERM 2 ./shelldon'
```

`timeout -s TERM 2` (coreutils, present on Pi OS) sends SIGTERM after 2s so the supervised process echoes the piped line, then drains and exits cleanly (the 1.4 reverse-drain shutdown, now on the Pi).

### Previous story intelligence (Stories 1.1–1.5)

- **Conventions to mirror:** package doc comment on the primary file; small files per type; **table-driven stdlib `testing`**, no `testify`; `t.Run` subtests; `t.Helper()` on helpers (`assertOnlyFile`); `t.TempDir()` for filesystem tests (auto-cleaned). [Source: contracts/, core/*]
- **First external dep was suture/v4 (1.4); 1.5 added none.** renameio/v2 is the **second** — both pure-Go, so the one-line cross-compile (NFR2) holds. Confirm `go.sum` updates are committed.
- **The cross-compile already passes** (`CGO_ENABLED=0 GOARCH=arm64 GOOS=linux go build ./...` was green in 1.4 and 1.5). AC1's new work is making it a **reproducible, verifiable** build artifact (`-ldflags`, `file` check, Taskfile), not getting it to compile.
- **The CLI round-trip already works on the laptop** (1.5: `printf '…' | shelldon` echoed + clean SIGTERM). AC4 re-runs that on the Pi — same binary path, real hardware.
- **No new runtime wiring.** The atomic-write helper has **no caller** in M0 — it exists for the required test and as the Epic 4 seed. Do not wire it into the dispatch/CLI path (nothing writes markdown yet).

### Project Structure Notes

- New: `core/memory/` (`atomic.go`, `atomic_test.go`), `Taskfile.yml` (repo root), `dist/` (gitignored build output, not committed). `core/memory` matches the Structural Seed (`core/ … memory/(owner)`); Epic 4 extends it with sqlite + the curated markdown tree. Do not scaffold sqlite/markdown-tree code now. [Source: ARCHITECTURE-SPINE.md#Structural Seed]
- `.golangci.yml` unchanged this story.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.6] — ACs, epic goal
- [Source: ...ARCHITECTURE-SPINE.md#AD-7] — atomic markdown via renameio/v2; atomicity vs durability (dir-fsync); memory-layer ownership
- [Source: ...ARCHITECTURE-SPINE.md#AD-10] — four required M0 tests; M0 passes ON THE PI; -race laptop/CI-only; no monkeypatch
- [Source: ...ARCHITECTURE-SPINE.md#NFR2] — single static binary, CGO_ENABLED=0, GOARCH=arm64, pure-Go deps, one-line build
- [Source: ...ARCHITECTURE-SPINE.md#NFR11] — SD write-wear; atomic markdown writes (temp+fsync+rename+parent-dir fsync)
- [Source: ...ARCHITECTURE-SPINE.md#Stack/Build, #Structural Seed/Deploy] — build flags, GOMEMLIMIT/GOGC, no GOARM64 LSE; cross-compile + rsync, never compile on the Pi; systemd unit (deferred)
- [Source: contracts/contracts_test.go, core/arbiter/arbiter_test.go, core/supervisor/supervisor_test.go] — the three existing required tests (names for the on-Pi run)
- [Source: _bmad-output/specs/spec-shelldon-go/SPEC.md] — NFR2 (static binary), NFR9/NFR11 (required tests, atomic writes)

## Dev Agent Record

### Agent Model Used

claude-opus-4-8[1m] (Claude Opus 4.8, 1M context) via Claude Code dev-story workflow.

### Debug Log References

- Verified renameio/v2 v2.0.2 API in the module cache before coding (`WriteFile(name, data, perm, ...opts)`, `NewPendingFile`, `CloseAtomicallyReplace`, `Cleanup`).
- TDD: wrote `core/memory/atomic_test.go` first (RED — `WriteAtomic` undefined), then `atomic.go` to GREEN; both memory tests pass under `-race`.
- `task` is **not** installed on the laptop, so the Taskfile's commands were run directly (raw equivalents documented in the story). `gotchi` reachable: `aarch64`, `Linux gotchi 6.12.75+rpt-rpi-v8 … aarch64`.
- **Captured on-Pi output (AC3, four required M0 tests on `gotchi`):**
  ```
  --- PASS: TestEnvelopeRoundTrip (job/result/inbound-message/outbound-message)
  --- PASS: TestArbiter_AtMostOneInFlight
  --- PASS: TestWriteAtomic_CrashSafety
  2026/06/21 14:38:47 ERROR edge panicked service=(supervisor.serviceFunc)… restarting=true panic="injected edge panic"
  --- PASS: TestRoot_SoulSurvivesSingleEdgePanic
  ```
  (The ERROR line is the expected slog panic log from the soul-survives test — the test PASSED.)
- **Captured on-Pi output (AC4, CLI round-trip on `gotchi`):**
  ```
  $ printf 'ping shelldon\nhello from the pi\n' | timeout -s TERM 2 ./shelldon
  ping shelldon
  hello from the pi
  ```

### Completion Notes List

- **AC1 satisfied (static arm64 binary).** `dist/shelldon` is `ELF 64-bit LSB executable, ARM aarch64, version 1 (SYSV), statically linked, … stripped` (~2.3 MB) — `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w"`. No CGo, no libc linkage. `Taskfile.yml build` is the reproducible driver.
- **AC2 satisfied (atomic-write crash-safety — the 4th required M0 test).** `core/memory.WriteAtomic` wraps `renameio.WriteFile` (temp → fsync → atomic rename). `TestWriteAtomic_CrashSafety` writes `ORIGINAL`, then aborts a `PendingFile` with `Cleanup()` **before** `CloseAtomicallyReplace()` (models a crash before the rename commits) and asserts the target still reads `ORIGINAL` with no orphaned temp — the honest test of "interrupted mid-rename leaves the prior file intact." Green under `-race` on the laptop AND on the Pi.
- **AC3 satisfied (four required M0 tests pass ON THE PI).** Cross-compiled the four test binaries for arm64 (`go test -c`), rsync'd to `gotchi:~/shelldon_go`, ran each filtered to its required test — all four PASS (output above). The AD-10 hard criterion is met: M0 is validated on target hardware, not only the laptop. (No `-race` on the Pi, per AD-10.)
- **AC4 satisfied (CLI round-trip ON THE PI).** Piped two lines to `./shelldon` on `gotchi`; both echoed back through inbound→core→arbiter→stub→outbound→CLI, clean SIGTERM exit.
- **Second external dependency** added: `github.com/google/renameio/v2 v2.0.2` (pure-Go; the one-line arm64 cross-compile still holds). `go.sum` updated.
- **Scope held:** atomic-write primitive + its required test + the reproducible cross-compile + the on-Pi validation only. **No** sqlite store, curated markdown tree, `DIRECTIVE.md`, dream cycle, or memory-op vocabulary (Epic 4); **no** systemd unit / `MemoryHigh`/`OOMPolicy` hardening (deploy-time — the M0 proof runs the binary directly); the atomic-write helper has **no runtime caller** in M0 (Epic 4 seed); **no** parent-dir fsync durability add beyond renameio (AD-7 defer).
- **Deploy guardrail honored:** deployed only to `~/shelldon_go`; left `~/shelldon` (legacy v2) untouched.
- **Validation:** native + arm64 builds OK; `go test -race -count=1 ./...` → 8 packages pass, no data race; `golangci-lint run` → 0 issues. **M0 is complete — all four required tests + the CLI round-trip pass on the Pi.**

### File List

- `core/memory/atomic.go` (new) — `WriteAtomic` atomic markdown-write helper (renameio/v2)
- `core/memory/atomic_test.go` (new) — happy-path + the required atomic-write crash-safety test (4th M0 test)
- `Taskfile.yml` (new) — build / build:tests / deploy / test:pi / run:pi (PI_HOST=gotchi, PI_DIR=shelldon_go)
- `.gitignore` (modified) — ignore `/dist/` (cross-compiled build artifacts)
- `go.mod` (modified) — add `github.com/google/renameio/v2 v2.0.2`
- `go.sum` (modified) — checksums for renameio/v2
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (modified) — story status → in-progress → review

### Review Findings

- [x] [Review][Patch] PI_HOST/PI_DIR interpolated unquoted into remote ssh command string — shell metacharacters in env-override values could inject commands on the Pi [Taskfile.yml:deploy/test:pi/run:pi] — FIXED: host quoted in double quotes, dir quoted in remote single quotes across deploy/test:pi/run:pi, and `printf` switched to single quotes so override values can't break out of the remote shell command
- [x] [Review][Defer] WriteAtomic gives opaque error when parent directory does not exist [core/memory/atomic.go] — deferred, pre-existing renameio behavior; no runtime caller in M0
- [x] [Review][Defer] test:pi runs whatever binaries are currently on the Pi — stale if deploy was not run first [Taskfile.yml:test:pi] — deferred, dev-tool UX concern not a code bug
- [x] [Review][Defer] WriteAtomic silently drops ACLs/xattrs on target file via rename(2) inode replace [core/memory/atomic.go] — deferred, pre-existing rename(2)/renameio behavior; irrelevant to M0 scope
- [x] [Review][Defer] &&-chained test binaries in test:pi silently skip later tests when any earlier test fails [Taskfile.yml:test:pi] — deferred, dev-tool operational concern; all four run or none if first fails
- [x] [Review][Defer] TestWriteAtomic_CrashSafety couples to renameio.PendingFile internals — does not call WriteAtomic at the failure point [core/memory/atomic_test.go] — deferred, best practical approach for M0; real crash injection requires OS-level fault injection
- [x] [Review][Defer] WriteAtomic perm argument is subject to umask on first write [core/memory/atomic.go] — deferred, no M0 caller; Epic 4 concern when real file ownership matters
- [x] [Review][Defer] run:pi task has no output assertion — silent pass if shelldon hangs or produces no output [Taskfile.yml:run:pi] — deferred, dev-tool manual verification task; visual output is the check
- [x] [Review][Defer] timeout -s TERM 2 in run:pi sends SIGTERM only; no SIGKILL follow-up if process hangs [Taskfile.yml:run:pi] — deferred, low-severity dev-tool concern on Pi

## Change Log

| Date       | Change                                                                 |
|------------|------------------------------------------------------------------------|
| 2026-06-21 | Closed M0: added `core/memory.WriteAtomic` (renameio/v2 atomic write) and the **fourth required M0 test** — atomic-write crash-safety (abort a `PendingFile` before the rename commits → prior file intact), green under `-race`. Added `Taskfile.yml` (build/deploy/test:pi/run:pi) producing a single static arm64 binary (`-ldflags="-s -w"`, ~2.3 MB, no CGo). Deployed to `gotchi:~/shelldon_go` and ran the four required M0 tests + the CLI round-trip **on the Pi Zero 2W** — all pass on real hardware (AD-10's hard criterion). Second external dependency: `google/renameio/v2 v2.0.2`. Laptop gate (native+arm64 build, `-race` over 8 packages, lint 0 issues) green (Story 1.6). |
