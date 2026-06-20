---
id: SPEC-sheldon-go
companions:
  - ../../planning-artifacts/architecture/architecture-sheldon_go-2026-06-19/ARCHITECTURE-SPINE.md
sources:
  - ../../../../sheldon/_bmad-output/specs/spec-openclawgotchi-v2/SPEC.md
---

> **Canonical contract.** This SPEC and the files in `companions:` are the complete, preservation-validated contract for what to build, test, and validate. Source documents listed in frontmatter are for traceability only — consult them only if you need narrative rationale or prose color this contract intentionally omits.

# sheldon (Go rewrite)

## Why

A **vision to realize**, driven by **autonomy and craft**: Elliot wants an E-Ink AI pet that is genuinely his — built ground-up for the enjoyment of building it. At its core, `sheldon` is a **chat-bot pet**: the owner converses with the LLM brain by text, and the conversation runs over a **pluggable chat transport** rather than v1's hardcoded Telegram. It runs on a Raspberry Pi Zero 2W with remote LLMs as the brain; its face and presence live on the E-Ink screen. This spec governs the **Go rewrite** of the design previously specced in Python (`spec-openclawgotchi-v2`): the same chat-first, transport-agnostic pet, re-founded on a Go spine that collapses the Python multi-process body into **one supervised process** (see the architecture companion, AD-1) while preserving the hexagonal LLM-free soul. The rewrite exists to design out v1's documented failures (OOM, zero tests, safety scattered across a 1513-line connector, hardcoded transport) on a clean Go substrate. **Physical embodiment** through the Pi's GPIO (button, BLE presence, sensors) remains a genuinely interesting capability the hardware affords, but it is an *optional, secondary* layer over the chat — not the headline.

## Capabilities

- id: CAP-1
  intent: The owner sends a text message over the pluggable chat transport; this triggers a per-turn brain (remote LLM) that replies in the conversation, while the E-Ink display reflects the pet's face/state.
  success: A message sent by the owner over the initial chat adapter produces an LLM reply in the same conversation within tolerable latency, with the display showing the pet's state, demonstrable end-to-end.

- id: CAP-2
  intent: The pet feels alive between LLM turns via resident reflexes — rule-based micro-behaviors (blink, idle, time-of-day mood) reading a persistent mood/energy/last-interaction struct, independent of the ephemeral brain.
  success: With no LLM turn active (and even with the network down), the pet visibly changes state on a demonstrable schedule.

- id: CAP-3
  intent: OPTIONAL physical sensing — the PiSugar2 button and BLE presence of known devices — is available via the plugin model (CAP-7), not as core interaction. When enabled, a physical event can feed the pet's state and reactions; the conversation itself stays on the chat transport.
  success: With the optional physical-sensing plugin enabled, a physical event (button press, or BLE presence of a paired device) produces an observable pet reaction; with it absent, the chat-bot pet still functions fully.

- id: CAP-4
  intent: The pet acts proactively, initiating behavior with no preceding user input, driven by personality state and environment.
  success: The pet initiates an action (e.g. greeting on presence, a mood-driven idle behavior) with no prior prompt, demonstrable.

- id: CAP-5
  intent: All privileged operations — credential access and the LLM call itself — pass through a single capability broker, which also abstracts the choice of LLM provider. (Tool execution and safety-policy enforcement are deferred — see Non-goals — but the broker is the designated home for both if added later.)
  success: A test demonstrates that accessing credentials or calling the model from outside the broker is impossible by construction; swapping the provider requires no change outside the broker.

- id: CAP-6
  intent: Context persists across ephemeral turns via a HYBRID memory — (a) a sqlite store holding the conversation-history (ordered, timestamped messages with FTS5 keyword recall; single-owner now, schema shaped so chat_id/user_id can be added non-breaking later) and a `learnings` table of captured observations (dedup by pattern_key, recurrence_count, status pending; see CAP-11's capture/promote pipeline), and (b) a filesystem markdown curated layer: a rewritable about.md doc, discrete facts/, a people/ directory (people the owner MENTIONS in conversation — not humans detected via BLE), and a broker-gated vault/, curated by the LLM (no vector DB). Separately, a human-only `DIRECTIVE.md` is owner-authored and read by the bot as authoritative (injected into every prompt) but NEVER written by the bot — the pet's owner-controlled "constitution." The bot may fully rewrite its own about.md; the directive file is off-limits to it. sqlite is raw+queryable; markdown is curated+durable; the dream cycle (CAP-11) bridges them. Writer sets are disjoint: core owns about.md + curated tree + sqlite; the owner solely owns DIRECTIVE.md.
  success: A message stored in a prior turn is recallable (by order and by FTS5 keyword search) and a fact curated into markdown demonstrably influences a later turn's behavior.

- id: CAP-7
  intent: Extensions beyond core are added as plugins under ONE generalized plugin model covering hardware AND behavioral plugins — a single plugin kind that can emit events, SUBSCRIBE to broadcast event kinds (message-answered, tool-used, day-alive...), own PRIVATE plugin state (its own, not core's soul/memory), and CLAIM a display region. Plugins speak only the bus contract and never import core. XP/leveling is an example optional behavioral plugin (subscribes to events, owns XP/level state, draws a status-bar widget in a claimed region).
  success: A new plugin (hardware or behavioral) can be added and exercised without changing `core/`, and the build still passes the LLM-free-core enforcement.

- id: CAP-8
  intent: When an LLM call fails (error, timeout, rate-limit), the broker retries and/or falls back to the next configured provider so the turn still completes; if every provider fails, the pet degrades to reflex-only (CAP-2).
  success: Injecting a provider failure (e.g. a GLM 500 or timeout) results in the turn completing via a fallback provider, demonstrable.

- id: CAP-9
  intent: The chat conversation runs over a pluggable chat transport behind a transport-agnostic message contract — Telegram is not hardcoded (v1's flaw). One initial adapter ships; further adapters are added later without touching core. The transport adapter holds its own connection credential (e.g. bot token); the broker still holds model + tool credentials.
  success: The initial chat adapter carries owner messages and pet replies end-to-end; a second (stub) adapter can be swapped in by adding an adapter only, with no change to `core/` and LLM-free-core enforcement still passing.

- id: CAP-10
  intent: The pet has an autonomous background life — a core-resident scheduler runs named jobs at independent cadences (interval / cron-style / idle-triggered), replacing v1's single heartbeat loop. Jobs are cost-tiered: cheap in-core reflex jobs (mood drift, blink) need no LLM; few cooldown-gated turn jobs (reflection, dreaming, proactive messages) cost a worker turn + LLM and run within a daily credit/turn budget. The scheduler is battery-aware (reads PiSugar2 power state): it stretches cadences and skips non-essential LLM turns on battery or low charge, livelier when plugged in. Incoming messages/events bypass the scheduler (immediate); heartbeat is just one job, not the engine.
  success: Distinct scheduled behaviors fire at differing cadences (a reflex job and an LLM turn job observably run on independent schedules); background LLM/credit spend stays bounded by the budget; and on simulated battery / low charge the scheduler demonstrably stretches cadences and skips non-essential LLM turns.

- id: CAP-11
  intent: The pet improves over time via light self-improving learning. During normal turns the worker proposes a `capture_learning(observation, pattern_key?)` memory-op (hot path, no extra LLM); core writes a row to the sqlite `learnings` table (dedup by pattern_key, increment recurrence_count, status pending). In the dream cycle (a scheduled worker turn, CAP-10) the LLM classifies pending learnings and promotes durable/high-value ones — judged by impact + recurrence, not a rigid count — into curated markdown (about.md/facts); sensitive ones route to the broker-gated vault; the rest are pruned. Light scope: no ERRORS/FEATURE_REQUESTS taxonomy, no promotion-to-CLAUDE.md or skill-extraction.
  success: A recurring captured observation is promoted to durable curated memory after a dream cycle and demonstrably influences a later turn's behavior.

## Constraints

- 512MB RAM ceiling (Pi Zero 2W) bounds every design choice.
- Nothing accumulates across turns: per-turn worker memory is bounded by Go's GC under `GOMEMLIMIT` (≈280MiB), not by process death — there is no cheap `fork` in Go. v1's documented OOM is the defining failure being designed out, now via bounded GC.
- The worker (the untrusted, prompt-assembling brain) lives behind a swappable isolation seam — a goroutine in Monolith+ (M0–M2), a uid-separated subprocess in Privsep-lite (M3+) — without reshaping any caller. ≤1 worker turn in flight at all times.
- The secret `vault/` does not EXIST until the worker is across a process/uid wall (M3); before then there is nothing a goroutine-worker could read. Vault isolation is OS-enforced once active.
- Single static binary: `CGO_ENABLED=0`, `GOARCH=arm64`. Dependencies must stay pure-Go (e.g. `modernc.org/sqlite` for FTS5, `periph.io` for GPIO/SPI) so cross-compile remains a one-line build.
- E-Ink refresh latency is in seconds, not frames; behaviors and animations must tolerate it.
- The brain is pluggable behind the broker; default provider is **GLM**. The supported set includes Ollama (self-hosted over LAN), OpenAI/ChatGPT, OpenRouter, and Anthropic.
- Remote-LLM network dependency: there is no brain when offline (including self-hosted Ollama over the LAN). The pet must degrade gracefully — resident reflexes (CAP-2) keep running without the LLM.
- Default hardware is the **Waveshare V4 E-Ink screen** (output) and the **PiSugar2 battery HAT** (power + button). Everything beyond this is a plugin (CAP-7).
- BLE presence is **pair-first**: a device counts as "present" only if previously paired (keyed on its stable BLE address, labelled with a friendly name). Arbitrary nearby devices are never scanned or logged — the privacy boundary for an always-on desk device.
- SD-card write wear: high-frequency state stays in RAM (periodically checkpointed to one file). Memory is hybrid: sqlite is scoped to conversation-history + learnings (messages + FTS5) and must use WAL with batched commits; the curated markdown layer (about.md/facts/vault) is written atomically (temp + fsync + rename + parent-dir fsync). No vector DB.
- `core/` must remain LLM-free, mechanically enforced — `depguard` (via golangci-lint) plus `internal/` packaging fail the build if `core/` imports provider/LLM modules; provider SDKs live behind `broker/internal/`.
- The chat transport is pluggable behind a transport-agnostic message contract; Telegram (or any one transport) must never be hardcoded into core. Single owner now — core owns the conversation-identity schema, and chat_id/user_id keys can be added later without a breaking change; adapters map their native id into that schema at the edge.
- Credential split: the chat transport adapter holds its OWN connection credential (e.g. bot token); the capability broker remains the sole holder of MODEL and TOOL credentials and the sole egress to models/tools. No credentials ever cross the bus.
- Core is the sole WRITER of all state and memory, including the sqlite store; workers may only READ history and non-vault markdown and propose writes via `Result`.
- Typed, versioned contracts (`Envelope`/`Job`/`Result`, Go structs) with a test harness present from the first milestone (M0). v1 shipped with zero tests. Required M0 tests: contract gob round-trip, the ≤1-worker bound, atomic-write crash-safety, and soul-survives-a-single-edge-panic.
- Battery + credit-aware autonomy: no unbounded background LLM spend. Background turn jobs (reflection, dreaming, proactive messages) are cooldown-gated and bounded by a daily credit/turn budget, and the scheduler backs off on battery (CAP-10).
- Built ground-up; v1 is reference only (study the guts, own the spine), never a code source. MIT attribution to Dmitry Turmyshev is retained (README/NOTICE plus the MIT notice).

## Non-goals

- Running an LLM on the Pi itself (on-device inference). Self-hosted models such as Ollama are allowed only as remote endpoints over the LAN.
- Always-on audio / microphone listening (deferred — too much for a battery-powered Pi Zero for now).
- Sound output (deferred — none in the default build for now).
- Vector database.
- Docker or Node.
- On-device camera vision.
- Group chat / multi-user / web interface in the initial build (architected-for via the pluggable-transport adapter model and a non-breaking conversation schema, but not implemented now — build single-owner).
- Copying v1 code (v1 is conceptual reference only).
- Runtime/dynamic plugin loading: plugins are registered at compile time (Go has no clean dynamic loading); "add a plugin" means recompile + redeploy. A subprocess plugin-host is a deferred option behind the same bus contract.
- XP / gamification in core: XP/leveling is an OPTIONAL behavioral plugin (CAP-7), not core, and not necessarily in the default build.
- Tool execution / the v1 "40+ tool patterns" (deferred — v2 is chat-first; the pet converses but does not yet call tools). When added, tools route through the capability broker per CAP-5; the `tool-used` broadcast event (CAP-7 plugins) stays a no-op until then.
- v1-style safety-list content policy (deferred — single-owner personal device; the broker stays the designated enforcement point if a safety layer is added later).
- gokrazy deployment in the initial build (Pi OS + systemd ships M0–M4; gokrazy is a deferred target once hardware works).

## Success signal

`sheldon` runs continuously on the Pi Zero 2W as a single supervised Go process, holding a multi-turn text conversation with the owner over the pluggable chat transport without OOM (the defining v1 failure), feeling alive between LLM turns through resident reflexes shown on the E-Ink face — and surviving any single edge failure (a transport or display crash) without the soul dying. Optionally, with physical-sensing plugins enabled, it reacts to presence and button without being prompted. It is a chat-bot pet Elliot built from scratch — a transport-agnostic conversational core with a face on the desk, freed from v1's hardcoded Telegram.

## Assumptions

- Primary IO = the pluggable chat transport (the owner's text conversation in and the pet's replies out), with the Waveshare V4 display as the pet's face/state surface (out). Physical input — the PiSugar2 button and BLE presence — is OPTIONAL, arriving via the plugin model (CAP-3/CAP-7), not core. No sound in or out in the default build.
- "M0" denotes the first build milestone — a real walking skeleton, end-to-end through the real bus, with the test harness present from the start, building `CGO_ENABLED=0` and passing on the Pi.
- "Dreaming" and the scheduler are autonomous background behavior, bounded by the battery + credit budget (CAP-10/CAP-11).
- The architecture companion (`ARCHITECTURE-SPINE.md`, AD-1…AD-17) is the authority on HOW these capabilities are realized in Go; this SPEC stays WHAT-level.

## Open Questions

- Threat-model confirmation that the worker is untrusted is settled in principle (architecture AD-3), but the explicit confirmation plus a vault-isolation property test is the gate for activating Privsep-lite at M3 — not an M0 blocker. Resolve before M3.
