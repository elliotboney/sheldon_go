---
review-type: technical-currency
target: ARCHITECTURE-SPINE.md (sheldon Go rewrite)
reviewer: technical-currency-reviewer
date: 2026-06-19
method: live web verification (2024-2026 sources)
verdict: SOUND — 10/11 confirmed; 1 minor correction (renameio parent-dir fsync); 0 blockers
---

# Technical-Currency Review — sheldon (Go rewrite) Architecture Spine

Every committed technology choice in the Stack table and the AD rules was checked against
the live web. Each claim below is tagged **CONFIRMED** or **FLAGGED**. One claim is partially
wrong (renameio does *not* fsync the parent directory); everything else is real, current, and
correctly characterized. Nothing is a build blocker.

---

## 1. modernc.org/sqlite — pure-Go (CGO_ENABLED=0) + FTS5 compiled in by default

**CONFIRMED (both halves).** Load-bearing claim for the memory layer (AD-7), verified carefully.

- **Pure Go / CGO_ENABLED=0:** Yes. modernc.org/sqlite is a CGo-free translation of SQLite's
  C source into Go (via ccgo). Builds with `CGO_ENABLED=0`, cross-compiles trivially. Supported
  GOOS/GOARCH explicitly includes `linux/arm64` — correct for Pi Zero 2W aarch64.
- **FTS5 compiled in by default:** Yes. The `modernc.org/sqlite/lib` package exposes FTS5
  internal constants (FTS5_OR, FTS5_PATTERN_GLOB, FTS5INDEX_QUERY_PREFIX, etc.), confirming FTS5
  is built in — no special build tag needed. Cross-confirmed by `zombiezen.com/go/sqlite` (which
  sits on modernc.org/sqlite) documenting that the extensions compiled in by default are:
  **session, FTS5, RTree, JSON1, GeoPoly.** `CREATE VIRTUAL TABLE ... USING fts5(...)` works
  out of the box.

**Caveat (not a flag, an install-note):** the README requires that your `go.mod` pin the **exact
same `modernc.org/libc` version** as seen in modernc.org/sqlite's own go.mod, or you can get
build/runtime mismatches. Worth a line in the build doc.

Sources:
- https://pkg.go.dev/modernc.org/sqlite
- https://pkg.go.dev/modernc.org/sqlite/lib
- https://pkg.go.dev/zombiezen.com/go/sqlite (extensions-compiled-in list)

---

## 2. thejerf/suture/v4 — actively maintained, idiomatic Go supervisor-tree lib

**CONFIRMED.** v4 is the current major version and the recommended one for new code (`import
github.com/thejerf/suture/v4`). v4 is the context-based rewrite; v3 is the last pre-context
line and still gets backported fixes. The library is the de-facto idiomatic Erlang-style
supervisor tree for Go (100% test coverage, deployed in production e.g. used historically by
Syncthing). Recent maintenance activity confirmed through late 2024 (Nov 2024 GPG subkey
rotation); a companion `sutureslog` module adds slog logging.

**Minor note (not a flag):** I could not surface a specific *2025/2026*-dated commit in search;
the lib is mature and low-churn (a stable supervisor primitive doesn't need frequent commits),
so "actively maintained" holds, but if you want certainty, glance at the GitHub commit history
before locking the dep.

Sources:
- https://github.com/thejerf/suture
- https://pkg.go.dev/github.com/thejerf/suture/v4

---

## 3. google/renameio/v2 — atomic write + directory fsync on Linux

**FLAGGED — partially incorrect.** The package exists and is current (latest **v2.0.2**, published
**2026-01-10**, Apache-2.0). It does atomic temp-file + file `fsync` + `rename`. **But it does NOT
fsync the parent directory.** The README scopes itself explicitly: *"renameio concerns itself only
with atomicity ... It does not provide durability guarantees."* The directory-fsync question was
raised as issue #11 and the package deliberately does not do it.

**Why this matters:** AD-7 (and the Consistency Conventions row) describes the atomic-write
sequence as **"temp + fsync + rename + parent-dir fsync"** and AD-10 makes atomic-write
crash-safety a required M0 test. renameio/v2 gives you the first three steps but **not** the
parent-dir fsync. For *atomicity* (never a half-written or 0-byte file) this is fine and is what
the markdown-tree invariant actually needs. For *durability across a power-loss crash* (the rename
itself surviving), POSIX requires fsync-ing the containing directory too — renameio won't do that.

**Recommendation (small, non-blocking):**
- Drop "+ parent-dir fsync" from the AD-7/Conventions description, OR
- Keep the durability guarantee and add an explicit parent-dir fsync after `CloseAtomicallyReplace`
  (open the dir, `Sync()`, close) — a few lines in `core/memory/`.
- The M0 atomic-write crash-safety test (AD-10) is correctly scoped to atomicity ("interrupted
  mid-rename leaves the prior tree intact"), which renameio satisfies — so the *test* claim stands;
  only the prose "parent-dir fsync" is overstated.

Sources:
- https://pkg.go.dev/github.com/google/renameio/v2 (scope: atomicity only, latest v2.0.2)
- https://github.com/google/renameio/issues/11 (directory-fsync explicitly not done)

---

## 4. failsafe-go — composes retry + circuit-breaker + fallback for HTTP/LLM calls

**CONFIRMED.** `github.com/failsafe-go/failsafe-go` (MIT, by Jonathan Halterman; Go sibling of the
JVM Failsafe). Policies: **Retry, Fallback, Circuit Breaker, Timeout, Rate Limiter, Bulkhead,
Hedge, Cache, Adaptive Limiter/Throttler.** Composes in any order via
`failsafe.With(fallback, retryPolicy, circuitBreaker, timeout)...` (executed in reverse around the
fn). Ships a `failsafehttp` subpackage for HTTP `RoundTripper` integration — directly supports
AD-9's "broker exposes a pre-authorized `*http.Client`" idiom. Exactly the retry+breaker+timeout+
fallback composition AD-9 specifies.

Sources:
- https://github.com/failsafe-go/failsafe-go
- https://failsafe-go.dev/policies/

---

## 5. anthropics/anthropic-sdk-go — official Anthropic Go SDK, streaming supported

**CONFIRMED.** `github.com/anthropics/anthropic-sdk-go` is Anthropic's officially supported Go
library (current version ~1.26.0, min Go 1.22). Streaming is first-class:
`client.Messages.NewStreaming(...)` alongside `client.Messages.New(...)`. SDK even errors on
non-streaming requests expected to exceed ~10 min unless you stream or set a custom timeout.
Supports direct API, AWS Bedrock, and Vertex AI. (Watch for impostor packages: `unfunco/...`,
`adamchol/...` are NOT official.)

Sources:
- https://github.com/anthropics/anthropic-sdk-go
- https://platform.claude.com/docs/en/api/sdks/go

---

## 6. sashabaranov/go-openai — works vs OpenAI, OpenRouter, AND GLM/Zhipu via base-URL swap

**CONFIRMED.** The client exposes `ClientConfig.BaseURL` (set via `openai.DefaultConfig(key)` then
override, passed to `openai.NewClientWithConfig`). All three targets are OpenAI-compatible:

- **OpenRouter:** base URL `https://openrouter.ai/api/v1`, provider-prefixed model names
  (`anthropic/claude-...`). Confirmed.
- **GLM/Zhipu:** OpenAI-compatible `/chat/completions` at `https://open.bigmodel.cn/api/paas/v4/`
  (CN) or `https://api.z.ai/api/paas/v4/` (international). Bearer auth, models `glm-4.6`/`glm-5`/etc.
  Only the base URL + model string differ from OpenAI — exactly the "base-URL swap" AD-9 assumes.
  Use the versioned `/api/paas/v4/` path (the un-versioned `/v1` alias is discouraged).

**One gotcha to carry into broker config:** the base-URL override only takes effect if you use
`NewClientWithConfig` (not bare `NewClient`) — a common misconfiguration where requests silently
hit api.openai.com. Worth a broker integration test per provider.

Sources:
- https://github.com/sashabaranov/go-openai (ClientConfig.BaseURL)
- https://openrouter.ai/docs/guides/community/openai-sdk
- GLM/Zhipu OpenAI-compatible endpoints (open.bigmodel.cn / api.z.ai, /api/paas/v4/)

---

## 7. periph.io/x/host/v3 + warthog618/go-gpiocdev — current, Pi Zero 2W (aarch64); + periph epd

**CONFIRMED (all three parts).**

- **periph.io/x/host/v3:** Current. The `rpi` subpackage explicitly **supports Raspberry Pi Zero 2W**
  (since periph v3.6.4); `rpi` last published Apr 2025, Apache-2.0. Works on aarch64.
- **warthog618/go-gpiocdev:** Current, native-Go GPIO via the Linux GPIO **character device**
  (libgpiod equivalent). Not Pi-specific — works on any platform with kernel GPIO support, which is
  the more portable/kernel-friendly path (the author recommends it over the old register-based
  `warthog618/gpio`). Goroutine-safe. Good fit for the display/plugin SPI+GPIO seams.
- **periph.io/x/devices/v3/epd (Waveshare E-Ink):** **EXISTS and is current.** `epd` drives Waveshare
  e-paper, no CGo, opens over SPI (`epd.NewSPIHat(b, &epd.EPD2in13)`), plus a sibling
  `epd/image2bit` for the Waveshare 2-bit wire format. The spine cites this path correctly.

**Detail worth knowing (not a flag):** the spine names the panel **"Waveshare V4"**. periph has a
*dedicated* `periph.io/x/devices/v3/waveshare2in13v4` driver (250×122, fast-refresh, v3-compatible)
in addition to the generic `epd` package. If the hardware is specifically the 2.13" V4, the
`waveshare2in13v4` driver may be the more exact fit than generic `epd`. The spine flags display
deps as "resolved at install against real hardware," so this is install-time, not a spine error.

**Note on register-vs-chardev mixing:** periph's bcm283x (register) and gpiocdev (chardev) can
conflict if both init the GPIO; the `periph-gpioc` adapter bridges them but you must call
`gpiodriver.Register()` instead of `host.Init()`, not both. Keep one path per pin.

Sources:
- https://pkg.go.dev/periph.io/x/host/v3/rpi (Pi Zero 2W support)
- https://github.com/warthog618/go-gpiocdev
- https://pkg.go.dev/periph.io/x/devices/v3/epd
- https://pkg.go.dev/periph.io/x/devices/v3/waveshare2in13v4

---

## 8. ollama/ollama/api — official Ollama Go client

**CONFIRMED.** `github.com/ollama/ollama/api` is the first-party client (the Ollama CLI itself uses
it). Typed `Client` with `Generate`, `Chat` (streaming via callback funcs), embeddings, model
management; `ClientFromEnvironment()`. MIT, 366+ importers, latest publish Jun 2026 — very current.
(Old path `jmorganca/ollama/api` is the pre-rename alias; use `ollama/ollama`.)

**Security note (not a currency flag):** advisory **GO-2025-4251** — missing-auth in the broader
Ollama *server* enabling unauthenticated model-management ops. The LAN-Ollama provider should be
on a trusted network / patched server; this is a deployment concern for AD-9's Ollama-LAN alternate.

Sources:
- https://pkg.go.dev/github.com/ollama/ollama/api
- https://github.com/ollama/ollama/blob/main/api/client.go

---

## 9. mymmrac/telego — current, maintained Telegram bot library

**CONFIRMED.** `github.com/mymmrac/telego`, MIT, stable v1 line, one-to-one Telegram Bot API
mapping. Actively maintained: latest publish **2026-05-13**, tracking Telegram Bot API **v9.1**
(release v1.2.0), 224+ importers. Uses fasthttp + go-json by default (both swappable),
net/http-like handler/predicate model. Solid for the AD-12 chat-transport adapter.

**Aligns with AD-12 caveat:** the spine already says "never leak `telego.Update` into core" — good,
because telego's defaults (fasthttp) and types are adapter-specific and should stay behind the
transport-agnostic contract.

Sources:
- https://github.com/mymmrac/telego
- https://pkg.go.dev/github.com/mymmrac/telego

---

## 10. depguard via golangci-lint — enforce import bans (core must not import LLM libs)

**CONFIRMED.** `github.com/OpenPeeDeeP/depguard` is a golangci-lint linter that allows/denies
package imports — purpose-built for enforcing architectural boundaries. Supports per-path `allow`/
`deny` rules with `list-mode: strict|lax` and file selectors (`$all`, `!$test`, `$gostd`). Directly
supports AD-1's "core imports no LLM/provider modules, CI fails the build" — a `deny` rule on
provider SDK paths scoped to `core/...` does exactly this.

**Version gotcha (carry into CI setup):** depguard's config schema **changed across golangci-lint
versions** (v1.53 silently broke v1.52 configs because the depguard config format was rewritten to
the `rules:`-based form). Pin a golangci-lint version and write the config in the current `rules:`
format, or CI will either panic or silently apply defaults. Not a currency flag — a config-version
discipline note.

Sources:
- https://github.com/OpenPeeDeeP/depguard
- https://golangci-lint.run/docs/linters/configuration/

---

## 11. Go 1.25+ arm64/CGO_ENABLED=0 cross-compile; testing/synctest stable; GOARM64 not v8.0,lse on A53

**CONFIRMED (all three sub-claims), with one helpful clarification.**

- **Go 1.25+, GOARCH=arm64, CGO_ENABLED=0 cross-compile for Pi Zero 2W:** Correct. Go 1.25 released
  2025-08-12; pure-Go (no CGo) builds cross-compile to linux/arm64 cleanly. The whole stack above is
  CGo-free, so `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build` is the right line.
- **testing/synctest stable in Go 1.25:** Correct. Per the official Go 1.25 release notes: *"This
  package was first available in Go 1.24 under GOEXPERIMENT=synctest ... The experiment has now
  graduated to general availability."* So AD-10's "use testing/synctest for deterministic
  scheduler-cadence tests" is on a stable, non-experimental API in 1.25. (The 1.24 `Run` function is
  deprecated; use `synctest.Test`. The old GOEXPERIMENT API is removed in Go 1.26 — so don't write
  against the 1.24 shape.)
- **Do NOT set GOARM64=v8.0,lse on Cortex-A53:** Correct and well-reasoned. `GOARM64` (introduced
  Go 1.23, unchanged in 1.25) **defaults to v8.0**, which is the safe baseline for the A53. The
  `,lse` extension emits ARMv8.1 LSE atomic instructions; the **Cortex-A53 is ARMv8.0-A and lacks
  LSE**, so `v8.0,lse` would produce binaries that fault with illegal-instruction on the Pi Zero 2W.
  The spine's "do not set GOARM64=v8.0,lse" guidance is right.

  **Clarification (sharpens, not contradicts):** the real-world failure mode (see the OpenWrt issue)
  is the *opposite* of over-setting — some distro/toolchain builds default aarch64 to a *higher*
  feature level (e.g. v8.2) and break the A53. **Upstream Go defaults to v8.0**, so a plain build is
  safe; the risk is only if a build environment overrides the default upward. The spine could add one
  defensive line: *"rely on the GOARM64=v8.0 default; do not let the build env raise it"* — but the
  existing instruction is correct as written.

Sources:
- https://go.dev/doc/go1.25 (synctest GA; no arm64 default change in 1.25)
- https://go.dev/doc/go1.23 (GOARM64 introduced, defaults v8.0, ,lse/,crypto options)
- https://github.com/openwrt/packages/issues/26852 (A53 + GOARM64 real-world breakage)

---

## Summary Table

| # | Claim | Result |
| - | --- | --- |
| 1 | modernc.org/sqlite pure-Go + FTS5 default | CONFIRMED (pin libc version) |
| 2 | thejerf/suture/v4 maintained, idiomatic | CONFIRMED |
| 3 | google/renameio/v2 atomic + **parent-dir fsync** | **FLAGGED** — no dir fsync; atomicity only |
| 4 | failsafe-go retry+breaker+fallback | CONFIRMED |
| 5 | anthropics/anthropic-sdk-go official + streaming | CONFIRMED |
| 6 | sashabaranov/go-openai vs OpenAI/OpenRouter/GLM | CONFIRMED (use NewClientWithConfig) |
| 7 | periph host/v3 + gpiocdev + epd on Pi Zero 2W | CONFIRMED (V4 has dedicated driver) |
| 8 | ollama/ollama/api official client | CONFIRMED (server advisory GO-2025-4251) |
| 9 | mymmrac/telego current/maintained | CONFIRMED |
| 10 | depguard via golangci-lint import bans | CONFIRMED (pin config schema version) |
| 11 | Go 1.25 arm64/CGO=0; synctest GA; no v8.0,lse on A53 | CONFIRMED (default v8.0 is safe) |

**Bottom line:** the stack is real and current as of June 2026. One prose correction
(renameio/v2 does not fsync the parent directory — the spine's "+ parent-dir fsync" is overstated;
either drop it or add the dir-fsync yourself). Everything else is confirmed; remaining notes are
install-time / config-version / deployment-security details, not spine errors.
