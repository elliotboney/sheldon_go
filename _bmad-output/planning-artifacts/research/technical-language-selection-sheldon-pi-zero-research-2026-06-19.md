---
stepsCompleted: [1, 2, 3, 4, 5, 6]
lastStep: 6
inputDocuments: []
workflowType: 'research'
research_type: 'technical'
research_topic: 'Language selection for rewriting sheldon on Pi Zero 2W'
research_goals: 'Determine the best language (Go vs Rust vs Zig vs other) for rewriting the sheldon AI E-ink pet bot for Pi Zero 2W, considering RAM constraints, ecosystem maturity, E-ink/GPIO support, LLM API integration, and developer ergonomics'
user_name: 'Elliot'
date: '2026-06-19'
web_research_enabled: true
source_verification: true
---

# Research Report: Technical

**Date:** 2026-06-19
**Author:** Elliot
**Research Type:** technical

---

## Research Overview

This document evaluates whether — and how — to rewrite **sheldon** (the chat-first E-Ink AI pet, currently a Python 3.13 multi-process app) in Go or another fast language, targeting the Raspberry Pi Zero 2W (quad-core Cortex-A53, **512MB RAM**). It was produced through a six-step facilitated technical-research workflow (scope → technology stack → integration patterns → architecture → implementation → synthesis), with every claim verified by parallel web-search agents against current (2024–2026) sources.

**The headline:** Go is the right language. But the more consequential finding is architectural — **most of sheldon's multi-process actor design (fork-server workers, UDS envelope bus) is a workaround for Python's limitations and collapses in Go**, while a *subset* (worker/vault/broker isolation) is a genuine security boundary the rewrite must consciously preserve. The single decision that governs the whole port is whether the threat model treats the LLM worker as untrusted (the spine says yes). The full reasoning and a final recommendation are in the **Research Synthesis & Recommendation** section at the end.

---

## Technical Research Scope Confirmation

**Research Topic:** Language selection for rewriting sheldon on Pi Zero 2W
**Research Goals:** Determine the best language (Go vs Rust vs Zig vs other) for rewriting the sheldon AI E-ink pet bot for Pi Zero 2W, considering RAM constraints, ecosystem maturity, E-ink/GPIO support, LLM API integration, and developer ergonomics

**Technical Research Scope:**

- Architecture Analysis - concurrency models, process/goroutine overhead, memory layout patterns for embedded-ish targets
- Implementation Approaches - how each language handles the fork-server worker pattern, idle loops, LLM API clients
- Technology Stack - Go vs Rust vs Zig (and dark horses), ecosystem maturity for GPIO/E-ink/HTTP on Pi Zero
- Integration Patterns - Waveshare E-ink driver support, Telegram/CLI transport adapters, Claude/OpenAI API clients
- Performance Considerations - RAM baseline, binary size, cold-start latency, GC pauses, syscall overhead

**Research Methodology:**

- Current web data with rigorous source verification
- Multi-source validation for critical technical claims
- Confidence level framework for uncertain information
- Comprehensive technical coverage with architecture-specific insights

**Scope Confirmed:** 2026-06-19

<!-- Content will be appended sequentially through research workflow steps -->

## Technology Stack Analysis

*Research date: 2026-06-19. All claims multi-source verified via parallel web search agents.*

---

### Programming Languages

**Go (Golang)**
- _Memory baseline:_ ~948 KB RSS for hello world; ~5.1 MB for minimal HTTP server; 5–10 MB runtime floor (GC + scheduler + metadata). On Pi Zero 2W (512 MB), set `GOMEMLIMIT=360MiB GOGC=50`.
- _ARM64 status:_ Fully supported since Go 1.5. Target: `GOOS=linux GOARCH=arm64`. Cross-compile is trivial: `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w"`.
- _Known issues:_ Go #39400 (heap crash when ~43 MB free — fine at normal usage on 512 MB); Go #20878 (`time.Sleep` returns late on ARM Linux, open since Go 1.8); do NOT set `GOARM64=v8.0,lse` — Cortex-A53 doesn't support LSE atomics.
- _GC pauses:_ 7–37 ms measured (map-heavy), 20–80 ms worst case. Proportionally worse on 1 GHz A53 vs 3+ GHz desktop. Not a dealbreaker for a chat bot, but avoid if sub-10ms response latency is required.
- _Binary size:_ ~700 KB–2 MB stripped. Runtime floor can't go below ~1.5 MB.
- _Source:_ https://go.dev/wiki/GoArm, https://github.com/golang/go/issues/39400, https://go.dev/doc/gc-guide

**Rust**
- _Memory baseline:_ 1–4 MB RSS idle; confirmed real-world Pi Zero 2W home automation daemon: 10–20 MB RSS (avg ~15 MB) over 15 days. Binary: 3.1 MB with `opt-level="z"` + `lto=true` + `panic="abort"`.
- _ARM64 target:_ `aarch64-unknown-linux-gnu` or `aarch64-unknown-linux-musl`. On-device compilation is painful (1hr+ clean builds). Use `cargo cross` for all cross-compile work.
- _GC:_ Zero. No GC pauses. Predictable latency. Sub-2ms GPIO response confirmed.
- _Tradeoff:_ Borrow checker learning cost is real. Compile times are 30–120s on medium projects, 5–10 min for clean builds. Slower to ship.
- _Binary size:_ ~415 KB stripped hello world; 1.5–2 MB HTTP server; can reach ~93 KB with musl + aggressive release profile.
- _Source:_ https://dev.to/scaraude/home-automation-in-3mb-building-a-rust-system-for-raspberry-pi-zero-56d6, https://kobzol.github.io/rust/cargo/2024/01/23/making-rust-binaries-smaller-by-default.html

**Nim**
- _Current version:_ 2.2.10 (April 2026). Active and stable.
- _ARM64 install:_ Official `nim-2.2.10-linux_arm64.tar.xz` from nim-lang.org. Confirmed working on Pi ARM64 (Nov 2025). **Do not use choosenim on ARM64 — it fails. Download tarball directly.**
- _Memory model:_ ARC/ORC (since Nim 1.4) — no GC runtime overhead. Compiles to C, then to native binary. Small footprint.
- _Stdlib coverage:_ `std/httpclient` (sync + async HTTPS), `std/json` + `std/jsonutils`. HTTP and JSON in stdlib — no third-party packages needed.
- _GPIO:_ No pure-Nim GPIO library for Linux Pi. FFI into `pigpio` or `libgpiod` — clean, one-line C interop. The AkaPi project chose Nim over Python explicitly for performance with C GPIO FFI.
- _Verdict:_ Surprise contender. HTTP+JSON in stdlib, native ARM64 binaries, no GC runtime, moderate learning curve.
- _Source:_ https://nim-lang.org/install_unix.html, https://nim-lang.org/docs/httpclient.html, https://github.com/Dhertz/AkaPi

**Zig**
- _Version:_ Pre-1.0 (0.15.x as of Oct 2025; 1.0 expected mid-to-late 2026). Breaking API changes between minor versions.
- _Stdlib gaps:_ HTTP client requires manual arena allocator management — real-world reports of data corruption when freeing JSON parser arenas (Jan 2025). No GPIO/SPI library in stdlib or community. No async runtime (being redesigned since 0.12).
- _Pi Zero 2W:_ Tier 2 target (aarch64). Cross-compilation works. Static linking recommended.
- _Only E-ink project found:_ `zales/sys-ink` (2 stars) — Waveshare 2.9" system monitor using raw Linux `spidev` ioctl. An app, not a library.
- _Verdict:_ Dead end for this project. Come back post-1.0.
- _Source:_ https://ziglang.org/download/0.14.0/release-notes.html, https://github.com/zales/sys-ink, https://paulosuzart.github.io/blog/2025/01/05/trying-zig-http-client-and-json-parser/

**Other languages evaluated:**
- _Swift:_ Technically works on Pi Zero 2W (aarch64). Hard no: 127 MB static binary footprint (25% of 512 MB RAM before app code runs). Primary ARM community distribution (`futurejones/swift-arm64`) **archived December 2024**.
- _Crystal:_ ARM64 officially supported (v1.19, Apr 2026). Bootstrapping on a new ARM target is a significant hurdle. No practical community presence on Pi.
- _Elixir/Nerves:_ Dark horse. Nerves works on Pi Zero 2W (`rpi3a` or `rpi0_2` targets). BEAM supervision trees are genuinely useful for unattended pet bots (crash recovery, hot reload). Active community (10+ articles 2024–2026). Memory overhead unquantified at 512 MB but reported as workable.
- _C + Meson + cmocka:_ Sub-millisecond startup, ~66 KB binary, universal library support. Install Meson via `pip3` (not `apt` — too old). cmocka is production-grade (used by Samba, OpenVPN). Higher cognitive load for solo dev; viable if you're already fluent in C.
- _Source:_ https://elixirforum.com/t/nerves-on-raspberry-pi-zero-2-w/58659, https://mesonbuild.com/, https://github.com/futurejones/swift-arm64

---

### E-Ink Display Libraries (Waveshare)

**Go:**
- `periph.io/x/devices/v3/epd` — best maintained (Feb 2025 release), pure Go, no CGo, covers 2.13" variants. Part of stable `periph.io` org.
- `ChristianHering/WaveShare` — 7.5" panel, explicitly no-CGo, GPLv3.
- `bay0/edp_2.13_V3` — 2.13" v3 port of Python driver.
- SPI primitive: `periph.io/x/conn/v3/spi` (180+ importers, Feb 2025, zero CGo, trivial cross-compile).
- **No projects use CGo to Waveshare's C SDK** — all are native Go ports of the Python driver.
- _Source:_ https://pkg.go.dev/periph.io/x/devices/v3/epd, https://github.com/ChristianHering/WaveShare

**Rust:**
- `epd-waveshare` (crates.io v0.6.0, Oct 2024) — widest coverage: 1.54", 2.13", 2.66", 2.7", 2.9"v2, 3.7", 4.2", 5.65", 5.83", 7.3", 7.5" (V2/V3/HD).
- `it8951` (v0.5.1, Jun 2026) — large grayscale panels (7.8", 10.3").
- Gotcha: `rppal` (the main SPI/GPIO crate) **retired July 1, 2025**. Use `gpio-cdev` + `linux-embedded-hal` for new projects.
- 3-year gap in epd-waveshare releases (0.5.0 Nov 2021 → 0.6.0 Oct 2024). Pull from Git if you hit bugs.
- _Source:_ https://crates.io/crates/epd-waveshare, https://github.com/golemparts/rppal, https://crates.io/crates/it8951

**Zig:** `sys-ink` raw `spidev` ioctl implementation only (not a library). Budget 1 week to generalize.

**Confirmed real-world Pi projects:**
- `bolausson/RPiZeroW-ePaper-Display` — Rust, 7.3" Spectra 6-color, Pi Zero W. Closest match to target hardware.
- `czuryk/Waveshare-ePaper-10.85-dashboard` — confirmed Pi Zero 2W.
- _Source:_ https://github.com/bolausson/RPiZeroW-ePaper-Display

---

### LLM API / Bot Frameworks

**LLM SDK availability:**

| SDK | Go | Rust |
|---|---|---|
| Anthropic (Claude) | **Official** `anthropics/anthropic-sdk-go` (MIT, Go 1.24+, active Apr 2025) | No official SDK. Unofficial: `bosun-ai/async-anthropic`, `tmikus/anthropic-sdk-rust` (no dominant option) |
| OpenAI | **Official** `openai/openai-go` (v3 beta) + `sashabaranov/go-openai` (10.7k stars, 3,009 dependents, battle-tested) | `async-openai` (community de facto, full OpenAPI coverage, SSE streaming) |

**Go wins on LLM SDKs.** Official Anthropic Go SDK vs. fragmented unofficial Rust crates is not a close call.

**Telegram bot libraries:**

| Language | Library | Stars | Bot API | Status |
|---|---|---|---|---|
| Go | `mymmrac/telego` | Active | Full 1:1 auto-generated | Active 2025 |
| Go | `go-telegram/bot` | Growing | Current | Active, zero-deps |
| Go | `tucnak/telebot` | ~4,500 | 7.1 | Active (small team) |
| Rust | `teloxide` | ~3,800 | 9.1 | Active Oct 2025, FSM built-in |
| Rust | `frankenstein` | ~600 | 9.6 (highest) | Active, low-level |

Community 2025 consensus: `telego` or `go-telegram/bot` for new Go projects. `teloxide` is the clear Rust choice.

---

### GPIO / Hardware Abstraction

**Go:**
- `periph.io/x/host/v3` — Active (Apr 2026). Works on Pi Zero 2W via shared 40-pin layout.
- `warthog618/go-gpiocdev` — **Best long-term choice.** Uses `/dev/gpiochip` character device API (modern kernel-native, replaces deprecated sysfs). Tested on Pi Zero W with Linux 6.4 (Feb 2025).
- `stianeikeland/go-rpio` — **Dead (3+ years). Avoid.**

**Rust:**
- `rppal` — explicit Pi Zero 2W support but **retired July 1, 2025** (frozen at v0.22.1, not broken — just no updates). Fork: `raspi-rust-rppal`.
- `gpio-cdev` + `linux-embedded-hal` — recommended for new projects. Same `/dev/gpiochip` approach as `go-gpiocdev`.

---

### Concurrency Models

| Model | Go (goroutines) | Rust (tokio async) |
|---|---|---|
| Per-task memory (idle) | 2–8 KB | ~64 bytes + future |
| 10k concurrent tasks | 20–80 MB | ~1–2 MB |
| GC overhead | ~10% CPU | 0% |
| Complexity for I/O-bound bot | Simple (spawn goroutines) | Moderate (async/await + ownership) |

**For sheldon's workload** (poll Telegram + ticker loops + LLM HTTP calls): 3–5 concurrent tasks. Go: ~10–40 KB overhead — negligible. Rust's per-task advantage doesn't matter at bot scale. Discord rewrote Go → Rust and saved 7x RAM — relevant at 1M concurrent connections, not for a single-user pet bot.

---

### Technology Adoption Trends

**Migration patterns:** No significant language-level migrations observed in the Pi embedded/pet bot space. Python remains dominant; Go and Rust are "performance upgrade" rewrites (pwnagotchi et al. stay Python; serious GPIO daemons move to Rust or Go).

**Emerging:** Nim is gaining quiet adoption for Pi projects where Python is too slow but Rust/Go feel heavy (AkaPi project, forum discussions Nov 2025). Elixir/Nerves is the niche pick for unattended always-on devices where BEAM fault tolerance matters.

**Legacy:** Python's position is being eroded by Go for "networked services on Pi" workloads. Rust is winning "bare-metal or max-performance" Pi workloads. The C SDK from Waveshare is being ignored by all three modern language communities — they port the Python reference instead.

**Community size (Pi + embedded):** Go > Rust > Nim > Zig for this specific domain in 2026.
- _Source:_ https://dev.to/gabrielanhaia/rust-vs-go-for-ai-infrastructure-in-2026-heres-what-the-benchmarks-actually-say-4j28, https://sinclairtarget.com/blog/2025/08/thoughts-on-go-vs.-rust-vs.-zig/

## Integration Patterns Analysis

*Research date: 2026-06-19. All claims multi-source verified via parallel web search agents. Scoped to sheldon's actual architecture, not generic enterprise patterns.*

---

### ⭐ Headline Finding: The Fork-Worker Pattern Is Likely Unnecessary in Go

This is the most consequential finding for the rewrite. **sheldon's central v1→v2 design decision — ephemeral fork-server workers that run one turn and die so RAM never accumulates — does not transfer to Go, and probably shouldn't.**

**Why Python forks but Go shouldn't:**
- Python's `os.fork()` is cheap (copy-on-write, no exec) AND CPython's allocator fragments and doesn't reliably return memory. Forking per-turn is the natural fix.
- Go inverts both properties: it has **no usable `os.fork()`** (the runtime is multithreaded; bare fork deadlocks on runtime locks — [Go #77032](https://github.com/golang/go/issues/77032) native fork is still just an open proposal), AND it **reclaims memory well** via `GOMEMLIMIT` (Go 1.19+) + the `MADV_DONTNEED` scavenger (Go 1.16+, RSS reflects real usage).
- So in Go you'd pay an *expensive* spawn cost (full exec + runtime re-init) to solve a memory problem the GC already handles. That's negative value.

**What this means architecturally:** Replace "fork-server + ephemeral workers" with **single long-lived process + goroutine-per-turn + `GOMEMLIMIT≈280MiB` + `sync.Pool` for per-turn buffers.** The entire worker-fork apparatus from the v2 spec collapses into ~0 lines.

**The one real exception — fault isolation, not memory:** If an LLM turn can panic, segfault (via cgo), or OOM and that must NOT take down the resident process (blink/idle/mood reflexes), THEN subprocess isolation is justified — but for blast-radius containment, not RAM hygiene. The Go mechanism for that is **re-exec self** (`exec.Command` on `/proc/self/exe`, dispatch on `args[0]`), not fork. Go spawns ~10–50× cheaper than Python (<5ms vs 80–150ms on x86; expect ~15–50ms on the A53), so even if you want it, it's affordable.

- _Verification action on the Pi:_ run the worker with `GOMEMLIMIT=280MiB`, watch `cat /proc/<pid>/status | grep VmRSS` across 50 turns. Flat RSS → fork-worker pattern is dead weight.
- _Caveat (medium confidence):_ Go's GC is **non-moving** (no compaction), so heap fragmentation can pin pages that `GOMEMLIMIT` can't reclaim. `sync.Pool` + pre-allocated slices mitigate; a fresh process per turn is the only *guaranteed* defragment. Profile before assuming it's a problem.
- _Source:_ https://go.dev/doc/gc-guide, https://github.com/golang/go/issues/77032, https://utcc.utoronto.ca/~cks/space/blog/programming/GoNoMemoryFreeing

---

### Chat Transport — Long-Poll vs Webhook

For a Pi Zero behind NAT, single user: **long polling wins decisively.** Webhook needs a public IP + TLS cert + open inbound port (or a Cloudflare/ngrok tunnel). Long-poll is outbound-only, works behind NAT, near-zero setup.

**The one real reliability risk — NAT idle eviction:** Home routers drop idle TCP connections (~60–120s). Telegram's `getUpdates` holds a connection open; if no message arrives within the NAT window, the router silently drops it (no RST/FIN), and the next poll hangs forever — bot looks alive but receives nothing. This is a documented production failure mode.

**Mitigation (telego specifics):**
- Set `Timeout: 25-30` in `GetUpdatesParams` (keeps each poll under the NAT window, forcing reconnect)
- Set `fasthttp.Client.ReadTimeout` to `timeout + 5s`
- Add a **watchdog goroutine**: if no update and no error for >90s, tear down and recreate the `Bot` with a *fresh* `fasthttp.Client` (dead keep-alive connections in the pool cause the infinite stall)

_Source:_ https://grammy.dev/guide/deployment-types, https://pkg.go.dev/github.com/mymmrac/telego

---

### Pluggable Transport Adapter (CLI / Telegram / future)

sheldon's "transport-agnostic adapter contract" maps cleanly onto Go's implicit interfaces — this is where Go shines vs Python.

**The pattern:** A minimal interface, your own message types (never leak `telego.Update` into core):
```go
type Transport interface {
    Run(ctx context.Context, incoming func(Msg) error) error
    Send(ctx context.Context, reply Reply) error
}
```
- Telegram adapter wraps `telego`; CLI adapter wraps `bufio.Scanner` + `os.Stdout`. Core bot logic imports only `Transport` — swap at `main()` init.
- Both telego long-poll and webhook return an identical `<-chan telego.Update`, so transport choice doesn't touch handler code.
- **Reference implementations:** `oklahomer/go-sarah` (cleanest adapter interface — `BotType()` + `Run()` + `SendMessage()`), `42wim/matterbridge` (7.5k stars, 20+ platforms, the production-grade multi-transport router).
- _Source:_ https://github.com/oklahomer/go-sarah, https://github.com/42wim/matterbridge

---

### LLM API Integration — Streaming

Official `anthropics/anthropic-sdk-go` (Go 1.23+) handles SSE cleanly:
- `client.Messages.NewStreaming(ctx, params)` → loop `stream.Next()` / `stream.Current()` / `message.Accumulate(event)`, check `stream.Err()` after.
- **Auto-retries by default (2x, exponential backoff + jitter, respects `Retry-After`)** for connection errors, 408/409/429, ≥500. **Gotcha:** stacks with any outer retry loop — set `option.WithMaxRetries(0)` if you roll your own.
- **Context cancellation just works:** cancel `ctx` → connection closes, `stream.Next()` returns false, `stream.Err()` surfaces `ctx.Err()`. This is how the resident process kills an in-flight turn when battery/budget rules trip.
- _Source:_ https://github.com/anthropics/anthropic-sdk-go, https://platform.claude.com/docs/en/api/sdks/go

---

### LLM Provider Fallback Chain (GLM → Ollama → OpenAI → OpenRouter)

sheldon's "pluggable, ordered provider chain" is hand-rolled in Go — no LiteLLM-equivalent dominates, and you don't need one (~150 lines):
- **`Provider` interface + ordered slice + per-provider circuit breaker.** Iterate in cost order, return first success.
- **One client covers three providers:** `sashabaranov/go-openai` works against GLM/Zhipu, OpenAI, AND OpenRouter just by swapping `BaseURL`:
  - GLM: `https://open.bigmodel.cn/api/paas/v4/` (intl: `https://api.z.ai/api/paas/v4/`), models like `glm-4.6` — *gotcha: rejects empty `content` fields*
  - OpenRouter: `https://openrouter.ai/api/v1`, prefixed models (`anthropic/claude-sonnet-4`)
- **Ollama:** official `github.com/ollama/ollama/api` (the CLI uses it) — pre-v1, lock down the endpoint (CVE GO-2025-4251, missing auth).
- **Circuit breaker:** `failsafe-go` composes retry + breaker + timeout + fallback in one call, respects `Retry-After`. Better fit than `sony/gobreaker` (breaker-only).
- _Source:_ https://failsafe-go.dev/, https://pkg.go.dev/github.com/ollama/ollama/api, https://openrouter.ai/docs/quickstart

---

### Security Boundary — Single Credential Holder (Capability Broker)

sheldon's "one capability broker is the sole holder of LLM creds; nothing else can call a model" maps onto a well-established Go idiom — **wrap an `http.RoundTripper`:**
- Broker holds credentials + an embedded `Transport`. Its `RoundTrip()` clones the request, injects the auth header, delegates downward.
- Expose only `Client() *http.Client`. Hand that pre-authorized client to downstream code, which **never sees the raw key** — they physically can't make an unauthenticated call.
- Composable as "Tripperware" (chain auth + logging + retry RoundTrippers). Real precedent: k8s `client-go` `WrapTransport`, `go-jira` `BasicAuthTransport`.
- **Key storage on the Pi:** `zalando/go-keyring` (no cgo → clean static binary) or `99designs/keyring` if you need its encrypted-file fallback for a headless box with no Secret Service daemon. **Pi gotcha:** Go's GC means you can't reliably zero secrets in memory, and swap-to-SD can persist them — minimize secret lifetime, never pass as CLI args (`ps aux` leak), add redacting `MarshalJSON()` to any struct holding a key.
- _Source:_ https://dev.to/stevenacoffman/tripperwares-http-client-middleware-chaining-roundtrippers-3o00, https://github.com/zalando/go-keyring, https://blog.gitguardian.com/how-to-handle-secrets-in-go/

---

### E-Ink Display Update Protocol

The display is the one component that genuinely benefits from a dedicated goroutine (periph.io `Draw()` is **synchronous and blocks** on the panel's busy pin for the full refresh):
- **One goroutine owns the device** (drivers aren't concurrency-safe). Feed it via a **size-1 buffered channel, drain-and-replace** so newer frames overwrite stale pending ones (e-ink can't keep pace with fast updates). `context.Context` for clean shutdown.
- **Refresh timing (2.13"):** partial ~0.3–0.5s (no flicker), fast ~1.5s, full ~4–5s (flickers). Multi-color (B/W/Red) panels usually **can't** partial-refresh at all.
- **Ghosting cadence:** full refresh every 5–10 partials (Waveshare says 5; real-world tolerates more). **Mandatory floors:** ≥1 full refresh/24h (burn-in prevention), sleep/power-off the panel when idle (leaving it energized damages it).
- _Source:_ https://www.waveshare.com/wiki/2.13inch_e-Paper_HAT_Manual, https://thoughts.gohu.org/posts/2025/epaper-partial-updates/, https://pkg.go.dev/periph.io/x/devices/v3

---

### Integration Summary — How sheldon's v2 Boundaries Map to Go

| sheldon v2 design element | Go translation | Verdict |
|---|---|---|
| Ephemeral fork-server workers | Single process + goroutine/turn + `GOMEMLIMIT` | **Collapses — likely unneeded** |
| Transport-agnostic adapter contract | Implicit interface + per-backend struct | **Natural fit, cleaner than Python** |
| Pluggable ordered provider chain | `Provider` interface + `failsafe-go` + go-openai base-URL swap | Hand-rolled, ~150 lines |
| Single capability broker for creds | `http.RoundTripper` wrapper, expose only `*http.Client` | Idiomatic |
| Resident reflexes (blink/idle/mood) | Long-lived goroutines + tickers | Trivial in Go |
| LLM streaming + cancellation | `anthropic-sdk-go` streaming + `context` | First-class |
| E-ink display | Dedicated goroutine, size-1 drain-replace channel | First-class |

## Architectural Patterns and Design

*Research date: 2026-06-19. Grounded against sheldon's adopted v2 architecture spine (AD-1…AD-15). All Go claims web-verified.*

---

### ⚠️ The Central Architectural Tension

sheldon's v2 spine is **"a multi-process actor model over a typed message bus, around a hexagonal LLM-free core."** The research reveals that **a large fraction of that multi-process machinery exists to work around Python, not to satisfy a requirement** — and in Go it collapses. But a *subset* of it is a genuine security boundary that a naïve single-process Go port would silently destroy. Getting this distinction right is the most important architectural decision in the rewrite.

**What each multi-process decision actually buys, and whether Go still needs it:**

| Spine decision | Why it's multi-process in Python | Go reality |
|---|---|---|
| **AD-3 fork-server workers** | `os.fork()` COW warm-start + bounded RAM (Python OOMs, GC weak) | **Dissolves.** No fork in Go; GC + `GOMEMLIMIT` bound RAM. Becomes goroutine-per-turn. |
| **AD-4 Envelope bus over UDS** | Separate Python processes need IPC; msgspec frames the wire | **Serialization disappears** if single-process — `Envelope`/`Job`/`Result` become plain structs over channels. |
| **AD-2 broker = separate process** | COW credential isolation + prompt-injection egress control | **Partly dissolves, partly real.** Credential-COW reason is gone; *egress isolation from a compromised worker* is a real security reason — but only matters if the worker is also isolated. |
| **AD-6 `vault/` OS-uid exclusion** | Worker runs under less-privileged uid; vault perms exclude it → prompt-injected worker *physically cannot* read secrets | **This is the load-bearing security boundary.** It REQUIRES the worker to be a separate OS process under a different uid. Single-process Go goroutines share one uid and one address space — this protection vanishes entirely. |
| **AD-5 single-writer core** | Prevent cross-process write races | **Trivially better in Go** — one goroutine owns the store, others send proposed writes over a channel. No races by construction. |

**The decision this forces:** How much process isolation does sheldon's *threat model* actually require? Two coherent architectures fall out, and you must pick deliberately — not drift into one.

---

### Option A — Faithful Port (preserve the actor/process model)

Keep core / broker / worker / transport / display / plugin-host as **separate OS processes**, envelope bus over Unix domain sockets, worker under a low-priv uid, vault OS-excluded.

- **Preserves:** every security property of the spine, especially AD-6 vault isolation and AD-2 egress control against a prompt-injected worker. The clean-room threat model the rewrite was designed around stays intact.
- **Costs:** you keep the serialization layer (msgspec → Go structs + length-prefixed frames over UDS), pay process-spawn cost per turn (cheaper than Python — ~15–50ms on the A53 — but non-zero), and write more plumbing. You're using Go as "a faster Python with the same shape."
- **Go fit:** UDS + length-prefixed `encoding/gob` or JSON frames is straightforward. Per-turn worker = re-exec `/proc/self/exe`, not fork.

### Option B — Go-Native Collapse (single supervised process)

One process. Subsystems become **goroutines under a `suture` supervisor tree**. The bus becomes a **channel-based hub** in core. `Envelope`/`Job`/`Result` stay as your typed contract but **never serialize** — they're Go values passed through channels. Worker becomes a goroutine with a hard `context` timeout + `recover()`.

- **Preserves:** the *logical* hexagonal architecture (LLM-free core enforced by `depguard` + `internal/` packages — see below), single-writer memory, graceful degradation, supervised auto-restart of subsystems. The shape of the spine survives even though the process count drops to one.
- **Loses:** **OS-level vault isolation (AD-6) and worker egress isolation (AD-2).** A prompt-injected worker goroutine shares the address space — it can in principle reach the vault and the credentials. You'd be downgrading a *physical* boundary to a *discipline* boundary (code review + import-linter), which is exactly the v1 sin the rewrite set out to fix.
- **Go fit:** this is the idiomatic, simple, fast Go architecture. ~5 `Serve(ctx) error` services under suture, a 40-line channel hub, `signal.NotifyContext` shutdown.

### Option C — Hybrid (recommended for investigation)

Collapse everything that's a *Python workaround* into one process, but keep the **worker (and only the worker) as a separate low-priv OS process** behind the broker. This is the "Sōzu pattern" the research surfaced: **UDS at the one edge that needs isolation, channels everywhere else.**

- core + broker + transport + display + plugin-host = **one supervised process, channel hub** (they're all trusted code).
- **worker = re-exec subprocess under a different uid**, talks to the broker/core over UDS with serialized envelopes. Dies per turn (or pools at ≤1).
- **Preserves the one boundary that matters:** the untrusted-by-design component (the thing assembling prompts from web-influenced content) is the only thing across a process/uid line. Vault OS-exclusion (AD-6) and broker egress control (AD-2) hold. Everything else gets Go's simplicity.
- This keeps ~80% of the serialization/IPC machinery deleted while keeping 100% of the security rationale that actually justified the multi-process design.

**My read:** Option C is almost certainly right, but it hinges on one question only you can answer — **does the threat model treat the worker as untrusted?** The spine says yes (AD-6's whole point is "a prompt-injected worker physically cannot read vault"). If that threat is real, Option B is off the table and the choice is A vs C, where C wins on simplicity. This is the #1 thing to resolve in brainstorming.

---

### Supervision & Lifecycle (applies to all options)

- **`thejerf/suture/v4`** is the idiomatic Go supervisor tree (Erlang-style restart-on-crash; used by Syncthing; actively maintained 2024–25). Service interface is just `Serve(ctx context.Context) error`. Maps 1:1 onto AD-13's "supervised and auto-restarted" transport and AD-8's "crashed plugin kills its own widget, not the soul."
- **Two-layer crash strategy:** suture for in-process per-subsystem restart of *recoverable* failures + **systemd `Restart=always`** for whole-process restart on genuine corruption/OOM. Don't `recover()` everything — let true corruption crash and restart clean.
- **Critical Go fact:** `recover()` does NOT cross goroutines. Every subsystem goroutine needs its own `defer recover()` or one panic kills the whole daemon. suture wraps this for you.
- **Graceful shutdown:** `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` → propagate ctx to every `Serve` → drain in reverse startup order (stop chat input → flush memory → close broker last).
- _Source:_ https://pkg.go.dev/github.com/thejerf/suture/v4, https://github.com/golang/go (signal.NotifyContext)

### Enforcing the LLM-Free Core (AD-1) in Go

AD-1's import-linter has a direct Go equivalent — actually **two complementary mechanisms, both stronger than convention:**
- **`internal/` packages (language-level, free):** put provider SDKs behind `broker/internal/llm/` — Go's compiler *physically* forbids any package outside `broker/` from importing it. No tool needed.
- **`depguard` (via golangci-lint):** deny-list rule — files under `core/` may not import LLM packages; fails CI. This is the closest match to Python's import-linter and gives you the "build fails if core imports a model" guarantee from AD-1.
- **`go-arch-lint`** if you want to model the *whole* hexagon (declare `core mayDependOn nothing-llm`) in YAML.
- _Source:_ https://github.com/OpenPeeDeeP/depguard, https://github.com/fe3dback/go-arch-lint

### Memory Layer (AD-6/AD-7) in Go

- **SQLite: use `modernc.org/sqlite` (pure Go, no cgo).** Confirmed **FTS5 support compiled in by default** — this was the make-or-break for AD-6's conversation recall, and it works with `CGO_ENABLED=0` cross-compile to ARM64. Performance is ~75% of cgo `mattn/go-sqlite3` (worst case ~2x slower on some queries) — irrelevant at sheldon's data scale, and it buys you static-binary cross-compile with zero C toolchain.
- **WAL + batched commits (AD-6):** `PRAGMA journal_mode=WAL` + `synchronous=NORMAL` + explicit transactions for batching. Exactly the SD-wear mitigation the spine calls for; modernc honors all of it (it's transpiled SQLite).
- **Atomic markdown writes (AD-6):** temp + `fsync` + `rename` + **`fsync` the parent dir** (the forgotten step that prevents 0-length files after power loss). Use **`google/renameio/v2`** (Linux-only, handles the dir-fsync for you) — directly satisfies AD-10's required atomic-write crash-safety test.
- _Source:_ https://pkg.go.dev/modernc.org/sqlite, https://github.com/google/renameio

### Typed Contracts (AD-10) in Go

- Python's `msgspec` bundles typing+validation+serialization because Python lacks static types. **Go structs ARE the typed contract at compile time** — the concern splits.
- **Single-process (Option B/C-internal):** `Envelope`/`Job`/`Result` stay as plain structs over channels, zero serialization, full compile-time safety. The AD-10 round-trip test becomes trivial or unnecessary for in-process paths.
- **Cross-process paths (Option A, Option C worker boundary):** structs + length-prefixed `encoding/gob` (Go-only, lowest friction) or JSON (debuggable). Versioning via a `v` header field (already in AD-11's closed envelope header) + additive struct fields.
- _Source:_ https://pkg.go.dev/encoding/gob, https://eli.thegreenplace.net/2019/unix-domain-sockets-in-go/

### Architectural Summary

The spine's *logical* architecture (hexagonal LLM-free core, single-writer memory, capability broker, pluggable transport, supervised edges, cost-tiered scheduler) **survives the port cleanly and is in several places *better* in Go** (interfaces, channels, suture, depguard). The spine's *physical* architecture (5+ processes, UDS bus, fork-server) is mostly a Python artifact that should collapse — **except** the worker/vault/broker isolation, which is a real security boundary the rewrite must consciously decide to keep (Option A/C) or drop (Option B). **Resolve the worker threat-model question first; everything else follows from it.**

## Implementation Approaches and Technology Adoption

*Research date: 2026-06-19. Scoped to a solo-dev clean-room Python→Go rewrite on Pi Zero 2W. Generic enterprise topics (team org, vendor selection) omitted as N/A.*

---

### The Single Highest-Leverage Constraint: Keep `CGO_ENABLED=0`

Everything about the Go-on-Pi workflow being pleasant or painful hinges on one decision: **stay pure-Go.** With `CGO_ENABLED=0`, cross-compile is one trivial command and you ship a static binary. The moment any dependency pulls in cgo, you need an aarch64 cross-toolchain or Docker buildx and the simple loop breaks.

This dictates dependency choices already surfaced in earlier steps — and they all align:
- SQLite → `modernc.org/sqlite` (pure Go), **not** `mattn/go-sqlite3` (cgo)
- GPIO/SPI → `periph.io` (pure Go), **not** cgo bindings
- Build: `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w"`
- _First action before anything:_ `uname -m` on the Pi (confirm `aarch64`), then audit `go.mod` for any cgo-forcing dep.
- _Source:_ https://go.dev/wiki/GoArm

### Dev/Deploy Loop

- **Never compile on the Pi** — the Zero 2W thrashes the SD card and rebuilds are slow. Cross-compile on the laptop, push the binary.
- **Loop:** `watchexec -e go -- task deploy`, where deploy = build → `rsync` (delta-only) → atomic swap → `systemctl restart`. The atomic swap (`mv bot.new bot`) means the service never sees a half-written binary.
- **Taskfile** (taskfile.dev) is the right level for solo dev — declarative, has `deps`/`preconditions`. Ansible/piku are fleet overkill for one Pi.
- _Source:_ https://taskfile.dev, https://github.com/watchexec/watchexec

### Deployment Target: gokrazy vs Raspberry Pi OS + systemd

This is a real fork in the road, and it interacts with the battery-yanked nature of a pet bot:

| | gokrazy (Go appliance OS) | Pi OS + systemd |
|---|---|---|
| Power-loss resilience | **Read-only root resists SD corruption** (relevant — battery gets yanked) | SD corruption risk on hard power-off |
| Updates | A/B atomic OTA | manual / scripted |
| Hardware | SPI/E-ink confirmed working (Jul 2025); **no GPIO edge interrupts — must poll** BUSY/button pins | Full udev, edge detection, every overlay |
| Existing libs | Go-only — reimplement PiSugar2 I2C reads yourself | Can lean on Python E-ink/PiSugar libs if needed |
| Time-to-working | Slower (write Go drivers) | Faster |

**Read:** gokrazy is philosophically perfect for this project (Go-only appliance, OTA, power-resilient) and aligns with the "genuinely owned" ethos — but it forces you to write Go drivers for PiSugar2 and poll instead of interrupt. Pi OS + systemd ships faster. Given the rewrite is already a long-haul ownership project, gokrazy is worth a serious look but **not** on the critical path for M0. Start on Pi OS + systemd, evaluate gokrazy once hardware works.

- **systemd gotcha worth capturing now:** `MemoryMax` alone OOM-kills but does **not** restart — you need `OOMPolicy=stop` + `Restart=always`. Do **not** set `PrivateDevices=true`/`DeviceAllow` — it breaks `/dev/spidev`, `/dev/gpiochip`, `/dev/i2c`. Set `MemoryHigh=180M` < `MemoryMax=220M` for a soft-throttle buffer.
- _Source:_ https://gokrazy.org/platforms/, https://github.com/gokrazy/gokrazy/issues/47

### Testing Strategy — and the Big Python→Go Shift

The v2 spec's "M0 test harness from day one" maps directly onto Go idioms, but **one mental shift dominates everything:** Python's `pytest` + `monkeypatch` culture does not exist in Go. You **cannot reassign functions at runtime.** Every place the Python version monkeypatches (network, `time.Now()`, the LLM call, the display) must become an **explicit interface seam wired via constructor injection** at `main()`.

- This is a *forcing function for good architecture* — and it's exactly what the hexagonal spine already wants. Define narrow interfaces over SPI/GPIO/LLM/clock **now**; pass real impls in `main`, hand-written fakes in tests.
- **Table-driven tests + stdlib `testing`** are the default. `testify/require` as a thin convenience layer. Prefer **stateful hand-written fakes asserting final state** over call-spy mocks (a fake that's hard to write is a signal the interface has too many responsibilities).
- **`testing/synctest` (stable Go 1.25)** is a big deal for this bot: runs goroutines in a bubble with a **fake clock that jumps when all goroutines block** — makes timers, idle timeouts, mood-drift loops, and the scheduler's cadences **deterministic** without `time.Sleep` flakiness. Directly de-flakes AD-14's multi-cadence scheduler tests.
- **`go test -race`** on laptop/CI only (2–20× slower, 5–10× RAM — never on the Pi, never shipped). High value for a bot sharing mood/display/sensor state across goroutines. Pair with synctest.
- periph.io ships test fakes (`gpiotest.Pin`, `spitest.Playback`) so hardware code unit-tests with zero device.
- _Source:_ https://go.dev/blog/synctest, https://quii.gitbook.io/learn-go-with-tests/testing-fundamentals/working-without-mocks

### Concurrency: asyncio → goroutines

The Python v2 is `asyncio`. The dominant trap moving to Go: **in asyncio you opt *out* of structure and cancellation; in Go you opt *in*.**
- No `async`/`await` coloring — write synchronous-looking code, decide concurrency at the call site with `go`.
- **Cancellation is your job:** thread `context.Context` everywhere and check `ctx.Done()` in every `select` loop. AD-12's `turn_id` fencing + AD-9's "kill in-flight turn" become `context` cancellation.
- **`errgroup` ≈ `asyncio.TaskGroup`** (cancel-on-error). `select` combines cancellation with channel I/O.
- **Goroutine leaks** are the classic ex-Python bug: a `select` loop missing a `<-ctx.Done()` case blocks forever — no panic, `-race` won't catch it. Go 1.26's leak profile helps; discipline matters more.
- _Source:_ https://rednafi.com/go/structured-concurrency/, https://eli.thegreenplace.net/2018/go-hits-the-concurrency-nail-right-on-the-head/

### Migration Strategy — Validated by the Literature

The research strongly validates the v2 spec's instincts, with one thing to actively police:
- **This rewrite is the *justified* case.** Joel Spolsky's "never rewrite" warning has a documented exception: a **fundamentally different architecture or data model** (Python→Go + actor-model qualifies). Clean-room discipline — *specify the old behavior before reimplementing* — is exactly the "study v1, re-implement clean" approach. Validated.
- **The trap to police: parity-by-default.** "Feature parity as the goal" is itself a documented failure trigger (forces big-bang cut-over). Keep each milestone's scope deliberately narrow.
- **"M0 harness from day one" = the walking-skeleton pattern.** Make M0 **real production code, end-to-end** (thinnest real slice that builds/deploys/tests through the actual bus), not discardable scaffolding. The spec's instinct is correct; the literature's caution is "don't let M0 be an in-memory fake that bypasses the real bus."
- **Right first milestone for an actor system:** message contracts + a minimal *real* bus + **one async round-trip test through a probe actor** + correlation IDs (your `turn_id`) wired from the start. Confirmed correct by EDA sources. Retrofitting observability/tracing into a decoupled system is much harder later.
- **Keep the Python v1/v2 as a security oracle:** the rewrite is the cheapest moment to re-threat-model (the Option A/B/C decision from step 4 is precisely this). Convert every known v1 incident into a permanent Go regression test. Encode security invariants (vault isolation, default-deny, broker-sole-egress) as property-based tests, not trust in new code.
- _Source:_ https://www.joelonsoftware.com/2000/04/06/things-you-should-never-do-part-i/, https://www.martinfowler.com/articles/patterns-legacy-displacement/, https://www.confluent.io/blog/testing-event-driven-systems/

### Implementation Roadmap (research-grounded, not a commitment)

A sequencing that honors both the spine and the walking-skeleton evidence:
1. **M0 — walking skeleton:** `contracts/` structs + minimal real hub + one CLI transport + one fake worker, end-to-end round-trip test (the AD-10 required tests: contract round-trip, ≤1-worker bound, atomic-write crash-safety). Pure-Go, cross-compiles to the Pi. **Resolve the Option A/B/C process-isolation decision here** — it shapes whether the bus serializes.
2. **M1 — real brain:** broker + provider chain (`failsafe-go` + go-openai base-URL swap) + real worker behind the chosen isolation boundary. Anthropic streaming + `context` cancellation.
3. **M2 — soul:** core reflexes + personality-state + scheduler (AD-14) with `synctest`-tested cadences; arbiter (AD-9).
4. **M3 — memory:** `modernc.org/sqlite` (WAL + FTS5) history + atomic markdown tree (`renameio`); single-writer core.
5. **M4 — face & body:** display compositor goroutine (size-1 drain-replace) + plugin-host + PiSugar2/E-ink on real hardware.

### Success Metrics

- M0 builds with `CGO_ENABLED=0` and the round-trip test passes **on the Pi**, not just the laptop.
- Steady-state RSS stays flat under `GOMEMLIMIT` across 50+ turns (the "fork-worker is dead weight" confirmation from step 3).
- `depguard` fails CI if `core/` imports an LLM package (AD-1 mechanically enforced).
- Vault isolation holds under the chosen architecture (the Option A/B/C security property is testable).

---

# Research Synthesis & Recommendation: Rewriting sheldon in Go

## Executive Summary

sheldon is a chat-first E-Ink AI pet designed to run forever on a 512MB Raspberry Pi Zero 2W. Its Python v2 architecture — a multi-process actor model with fork-server workers, a Unix-domain-socket envelope bus, and a hexagonal LLM-free core — is an elegant response to Python's specific weaknesses: weak memory reclamation, the GIL, and the cheapness of `os.fork()`. This research asked whether a faster language would serve the project better, and concluded **yes, Go** — but with a crucial caveat that reframes the entire effort.

**Go wins the language question decisively** on the axes that matter for this project: it has the only *official* Anthropic SDK among the candidates, mature pure-Go libraries for every hardware and integration need (SQLite-with-FTS5, Waveshare E-Ink, GPIO, Telegram), trivial single-binary cross-compilation to ARM64, and a concurrency model that maps cleanly onto a bot juggling chat, reflexes, and LLM calls. Rust is faster and leaner but pays for it in unofficial LLM SDKs, a just-retired GPIO anchor library (`rppal`), and a steeper solo-dev cost. Nim is a credible dark-horse worth a short spike. Zig, Swift, and Crystal are ruled out (pre-1.0, 127MB binaries, and ARM-bootstrap pain respectively).

**But the language is the smaller finding.** The larger one is that **Go dissolves most of the multi-process machinery the rewrite was built around.** The fork-server pattern (the spine's load-bearing, "non-retrofittable" bet) exists to bound RAM across turns — a problem Go's garbage collector and `GOMEMLIMIT` already solve, while Go can't cheaply fork anyway. The envelope bus over Unix sockets exists because separate Python processes need IPC — in a single Go process, those envelopes become plain structs over channels and the serialization layer vanishes. What does *not* dissolve is the **worker/vault/broker security isolation**: the spine's AD-6 guarantees that a prompt-injected worker *physically cannot* read the secret vault because it runs as a separate OS process under a less-privileged uid. Collapse everything into one Go process and that protection silently disappears — reintroducing exactly the kind of implicit-trust weakness the clean-room rewrite set out to eliminate.

## Key Technical Findings

- **Language: Go.** Official Anthropic SDK, pure-Go SQLite with FTS5 confirmed, periph.io for E-Ink/GPIO, `modernc.org/sqlite` + `periph.io` keep `CGO_ENABLED=0` so cross-compile stays a one-liner. (See *Technology Stack Analysis*.)
- **The fork-worker pattern is dead weight in Go.** No usable `os.fork()`; `GOMEMLIMIT` + the `MADV_DONTNEED` scavenger bound RAM. Replace with single-process + goroutine-per-turn. The one real exception is *fault* isolation (a panicking turn must not kill resident reflexes), addressed by re-exec, not fork. (See *Integration Patterns ⭐ Headline Finding*.)
- **Serialization disappears in-process.** `Envelope`/`Job`/`Result` stay as your typed contract but become Go values over channels — no msgspec, no UDS framing — *except* across whatever process boundary you deliberately keep.
- **One genuine security boundary survives: the worker.** AD-6 vault isolation and AD-2 broker egress control require the untrusted prompt-assembling worker to be a separate low-priv OS process. This is the hinge of the whole port.
- **The logical architecture survives and improves.** Hexagonal LLM-free core → `depguard` + `internal/` packages (CI-enforced, like the Python import-linter). Supervised edges → `suture/v4`. Single-writer memory → one goroutine owning the store. Scheduler cadences → `testing/synctest` deterministic tests.
- **The v2 spec's process instincts are validated.** "M0 harness from day one" = the walking-skeleton pattern; "study v1, build clean" = the justified-rewrite exception to Spolsky's rule. The trap to police is *parity-by-default*.

## The Decision That Governs Everything: Worker Threat Model

Three coherent architectures fall out of the research. They are **not** equally good — the choice is forced by one question.

| | **A — Faithful Port** | **B — Full Collapse** | **C — Hybrid (recommended)** |
|---|---|---|---|
| Process model | All actors stay separate processes + UDS bus | One supervised process, channel hub | Trusted components in one process; **worker** alone is a separate low-priv subprocess |
| Vault isolation (AD-6) | ✅ Preserved | ❌ **Lost** | ✅ Preserved |
| Broker egress control (AD-2) | ✅ Preserved | ❌ Weakened to discipline | ✅ Preserved |
| Serialization/IPC code | All of it kept | All deleted | ~80% deleted (only worker boundary serializes) |
| Go idiom / simplicity | Low ("fast Python") | Highest | High |
| Verdict | Safe but heavy | Simple but insecure | **Best balance** |

**The governing question:** *Does sheldon's threat model treat the LLM worker as untrusted?* The spine answers yes — AD-6's entire purpose is "a prompt-injected worker physically cannot read vault." **If that threat is real, Option B is off the table**, and the choice is A vs C, where **C wins on simplicity** by keeping only the one boundary that earns its cost. This is the first thing to settle in brainstorming, because it determines whether the bus serializes at all.

## Strategic Recommendations

1. **Adopt Go.** Lock `CGO_ENABLED=0`; commit to `modernc.org/sqlite` + `periph.io` as the foundation that keeps the build loop trivial. *(First action: run `uname -m` on the Pi to confirm `aarch64`.)*
2. **Choose Option C (Hybrid) pending a threat-model confirmation.** Collapse core/broker/transport/display/plugin-host into one supervised process; keep the worker as a re-exec subprocess under a different uid behind the broker. Preserves 100% of the security rationale, deletes ~80% of the IPC machinery.
3. **Make M0 a real walking skeleton:** `contracts/` structs + a minimal *real* hub + one transport + one async round-trip test (the AD-10 required trio: contract round-trip, ≤1-worker bound, atomic-write crash-safety), building `CGO_ENABLED=0` and passing **on the Pi**.
4. **Design for Go's testing model up front:** narrow interfaces over every external seam (SPI, GPIO, LLM, clock) with constructor injection — there is no monkeypatch. Use `testing/synctest` for the scheduler's cadences.
5. **Spike Nim for 2 hours** before fully committing, if curious — it has HTTP+JSON in stdlib, native ARM64, no GC runtime, smaller binaries. Likely still loses to Go on SDK/ecosystem, but cheap to confirm.

## Document Map (where the detail lives)

1. **Technology Stack Analysis** — language-by-language comparison (Go/Rust/Nim/Zig/+others), E-Ink libraries, LLM/bot frameworks, GPIO, concurrency, adoption trends.
2. **Integration Patterns Analysis** — ⭐ the fork-worker finding, transport/long-poll, adapter pattern, streaming, provider fallback, capability broker, E-Ink update protocol.
3. **Architectural Patterns and Design** — the A/B/C tension in full, supervision (`suture`), LLM-free-core enforcement, memory layer, typed contracts.
4. **Implementation Approaches** — `CGO_ENABLED=0` constraint, dev/deploy loop, gokrazy vs systemd, testing & the asyncio→goroutines shift, migration strategy, the 5-milestone roadmap.

## Risk Assessment

- **Highest risk — silently dropping the security boundary.** Choosing Option B (or drifting into it for simplicity) reintroduces the implicit-trust weakness the rewrite exists to fix. *Mitigation:* settle the threat-model question explicitly; encode vault isolation as a property-based test.
- **Second-system / parity creep.** Rebuilding "everything v1 had" forces a big-bang that never ships. *Mitigation:* narrow per-milestone scope; the spine's "Deferred" list is already good discipline.
- **cgo contamination.** One cgo dep breaks the clean cross-compile loop. *Mitigation:* audit `go.mod`; the recommended deps are all pure-Go.
- **Heap fragmentation (medium, low-likelihood).** Go's non-moving GC can pin pages `GOMEMLIMIT` can't reclaim. *Mitigation:* `sync.Pool` for per-turn buffers; profile RSS across 50+ turns before assuming it's a problem — and that test also confirms the fork-worker pattern is unneeded.
- **Unverified at hardware level.** All RAM/latency figures are x86 proxies or single real-world reports; no Pi-Zero-2W-specific Go GC benchmark exists. *Mitigation:* the M0 success metric is "passes on the Pi," not the laptop.

## Conclusion & Next Steps

Go is the correct language for sheldon, but the research's real value is in showing that **the rewrite is more of an architectural decision than a language one.** The hexagonal soul of the v2 spine survives the port and gets cleaner; the multi-process body mostly dissolves into idiomatic Go, leaving one security boundary that must be kept by intent rather than inherited by accident.

**Immediate next steps:**
1. **Brainstorm the Option A/B/C decision** (recommended: `bmad-brainstorming`) — resolve the worker threat-model question; everything else follows from it.
2. **Confirm hardware reality** — `uname -m` on the Pi, audit dependencies for cgo.
3. **Optional 2-hour Nim spike** if you want to close that door deliberately.
4. **Then move to architecture** — adapt the existing AD-1…AD-15 spine to the chosen Go process model (the logical ADs survive; the physical ones get rewritten around the A/B/C choice).

---

**Technical Research Completion Date:** 2026-06-19
**Research Method:** 6-step facilitated technical research; ~14 parallel web-search agents; multi-source verification with adversarial checks on load-bearing claims.
**Confidence:** High on language/ecosystem and architecture-mapping findings; Medium on Pi-Zero-2W-specific performance numbers (extrapolated — flagged inline, to be confirmed on hardware at M0).
