![shelldon logo](assets/img/logo.png)

# sheldon (Go)

> An E-Ink AI pet for the Raspberry Pi Zero 2W — chat-first, remote-LLM brain, a face that lives on your desk. A ground-up **Go rewrite** of the Python [`sheldon`](https://github.com/elliotboney/shelldon) v2.

## Status

**Planning complete — implementation not yet started.**

This repo currently holds the planning stack, not code. The decisions below are settled and validated; the first code milestone (M0) is a walking skeleton with the test harness from day one.

| Phase | Artifact | State |
|---|---|---|
| Technical research | `_bmad-output/planning-artifacts/research/` | ✅ Go selected over Rust/Nim/Zig |
| Brainstorming | `_bmad-output/brainstorming/` | ✅ Worker-isolation seam decided |
| Architecture | `_bmad-output/planning-artifacts/architecture/` | ✅ Spine final (AD-1…AD-17) |
| Spec | `_bmad-output/specs/spec-sheldon-go/` | ✅ SPEC.md (CAP-1…CAP-11), validated |
| Epics & stories | — | ⏭️ next |
| Implementation | — | ⏭️ M0 walking skeleton |

## What it is

sheldon is a tiny AI pet you **talk to** — a little face on an E-Ink screen that talks back, has moods, remembers you, and acts on its own. You converse with a real LLM brain by text over a **pluggable chat transport** (not hardcoded to any one service), while the pet's face and mood live on a Waveshare E-Ink display. It runs fully in your terminal (zero hardware) *or* on a palm-sized Raspberry Pi.

The full capability contract is in [`_bmad-output/specs/spec-sheldon-go/SPEC.md`](_bmad-output/specs/spec-sheldon-go/SPEC.md).

## Why Go, and why a rewrite

The Python v2 design was a multi-process actor model built to work around Python's weaknesses (OOM under 512MB, the GIL, the cheapness of `os.fork()`). The technical research found that **most of that machinery is a Python workaround that collapses in Go** — bounded RAM comes free from the GC, reflexes are just goroutines — while one piece (isolating the untrusted LLM worker from the secret vault) is a genuine security boundary that must be kept on purpose.

The resulting architecture (one sentence):

> **A single supervised Go process — a hexagonal LLM-free core with goroutine actors over an in-process typed channel bus, and the untrusted brain behind a swappable isolation seam.**

The key move: the worker (the untrusted, prompt-assembling brain) sits behind a Go interface. It ships as an in-process goroutine for M0–M2 (Monolith+) and swaps to a uid-separated subprocess at M3 (Privsep-lite) **without reshaping any caller**. The secret vault doesn't even exist until that process wall does — so the exposure window never opens.

Full reasoning: [`ARCHITECTURE-SPINE.md`](_bmad-output/planning-artifacts/architecture/architecture-sheldon_go-2026-06-19/ARCHITECTURE-SPINE.md) (and the interactive walkthrough beside it).

## Stack

- **[Go](https://go.dev)** (`CGO_ENABLED=0`, `GOARCH=arm64`) — single static binary, trivial cross-compile to the Pi
- **[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)** (pure-Go, FTS5) — conversation history + learnings
- **[periph.io](https://periph.io)** — Waveshare E-Ink + GPIO
- **[thejerf/suture/v4](https://github.com/thejerf/suture)** — supervisor tree (the soul survives a broken limb)
- **[failsafe-go](https://failsafe-go.dev)** — provider chain retry/fallback
- **[anthropics/anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go)**, **[sashabaranov/go-openai](https://github.com/sashabaranov/go-openai)** (GLM/OpenAI/OpenRouter), **[ollama/ollama](https://github.com/ollama/ollama)** — pluggable brain
- **[mymmrac/telego](https://github.com/mymmrac/telego)** — initial chat transport
- Platform: **[Raspberry Pi OS](https://www.raspberrypi.com/software/)** 64-bit + **[systemd](https://systemd.io)** (**[gokrazy](https://gokrazy.org)** deferred)

## Lineage & attribution

sheldon sits at the end of a short lineage:

- **[pwnagotchi](https://pwnagotchi.ai/)** (by [@evilsocket](https://github.com/evilsocket)) pioneered the E-Ink "virtual pet on a Pi Zero" form factor.
- **[openclawgotchi](https://github.com/turmyshevd/openclawgotchi)** (MIT, by [Dmitry Turmyshev](https://github.com/turmyshevd)) made it a chat pet with an LLM brain.
- **[Python `sheldon`](https://github.com/elliotboney/shelldon)** was the v2 clean-room rebuild on a new spine.
- **`sheldon` (Go)** — this repo — is the v3 rewrite in Go.

v1 is studied as reference, never copied. MIT attribution to Dmitry Turmyshev is retained (a `LICENSE` and `NOTICE` ship with the implementation).

## Layout

```
_bmad-output/          # the planning stack (this is what's here now)
  planning-artifacts/
    research/          # language selection: Go vs Rust vs Nim vs Zig
    architecture/      # ARCHITECTURE-SPINE.md (AD-1…AD-17) + walkthrough
  brainstorming/       # the worker-isolation session + keepsake
  specs/spec-sheldon-go/  # SPEC.md — the capability contract
```

The Go source tree (`core/ broker/ worker/ transport/ display/ plugins/ contracts/ tests/`) arrives with M0.
