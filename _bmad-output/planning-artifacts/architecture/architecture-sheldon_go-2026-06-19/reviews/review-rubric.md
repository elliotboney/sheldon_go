# Rubric Review — ARCHITECTURE-SPINE.md (sheldon Go rewrite)

**Reviewed:** `architecture-sheldon_go-2026-06-19/ARCHITECTURE-SPINE.md`
**Against:** Python v2 source spine (`architecture-shelldon-2026-06-15`) + language-selection research (2026-06-19)
**Date:** 2026-06-19
**Verdict:** **Strong, ship-with-one-fix.** This is a high-quality port that ratifies the Python logical invariants, correctly collapses the physical multi-process body into one supervised process, and stages the one real security boundary (worker isolation) as a milestone gate rather than dropping it. It clears 6 of 7 checklist criteria cleanly. The one material defect is a **silently dropped Python invariant (Py AD-7, volatile-state checkpointing)**, plus a whole-dimension silence on the **operational/environmental envelope** (no environments/config/observability/logging dimension). Both are fixable with small additions; neither breaks the existing ADs.

---

## Checklist Item 1 — Fixes the real divergence points for the level below (features), misses none

**Pass (high confidence).**

The spine fixes the divergences a feature builder would otherwise re-litigate per feature:
- *Where does the brain live and how isolated is it?* — AD-1 (one process), AD-2 (seam), AD-3 (vault gating). A builder cannot assume process-peer actors at M0 or that the seam is already a wall.
- *How do components talk?* — AD-4 (uniform Envelope, transport swappable, closed header, two routing modes). No ad-hoc point-to-point channels.
- *Who writes state/memory?* — AD-6 (core sole writer; worker proposes only). Prevents the single most common divergence: two writers of the soul.
- *How are concurrency/cost bounded?* — AD-8 (≤1 turn, coalesce, budget/battery gate), AD-13 (scheduler cadence).
- *How do plugins extend without forking core?* — AD-14 (one compile-time registry, hw + behavioral).
- *How does a crash not kill the pet?* — AD-5 (suture per edge, soul survives any single edge failure).

These are the exact seams a feature would otherwise diverge on. No obvious feature-altitude divergence is left unfixed **except** the volatile-state persistence question (see Item 4 / Finding 1) — a builder adding a reflex that mutates mood has no rule telling them whether mood is checkpointed to RAM-then-flushed or written through to disk on every drift tick. That is a real, feature-reachable divergence.

---

## Checklist Item 2 — Every AD's Rule is enforceable and actually prevents its stated divergence

**Pass (high confidence), with two soft spots.**

Most rules are mechanically enforceable, which is the strength of this spine:
- AD-1: `depguard` (CI fails build) + `internal/` package wall — language-level + tool-level, not convention. **Enforceable.**
- AD-3: "no real secret EXISTS until the worker is uid-separated" — a *testable invariant* (vault-population gating), explicitly reframed from "managed risk" to "the exposure window never opens." **Strongly enforceable** — this is the best AD in the spine.
- AD-6: closed region-id type + startup rejection of conflicting claims; sparse patches over fixed dotted paths; fixed memory-op arg schemas. **Enforceable.**
- AD-8: ≤1-worker bound is a **required M0 test** (AD-10). **Enforceable.**
- AD-10: contract round-trip, ≤1-worker, atomic-write crash-safety as required M0 tests; `synctest` for cadence determinism. **Enforceable.**
- AD-14: conflicting claims rejected at startup; manifest is a Go struct. **Enforceable.**

Soft spots (medium):
- **AD-9 (broker sole cred holder).** The rule is strong (RoundTripper idiom, no creds on bus) but its enforcement is idiom + discipline, not a CI gate like AD-1's `depguard`. There is no stated mechanism that *fails the build* if a non-broker package imports a provider SDK or reads a credential. AD-1 pushes provider SDKs behind `broker/internal/llm/`, which helps, but "only broker holds creds" is asserted, not mechanically fenced the way "core imports no LLM" is. Consider a `depguard` rule that only `broker/` may import the keyring/secret package — cheap, and it closes the gap symmetrically with AD-1.
- **AD-5 "soul survives ANY single edge failure"** is an invariant but not yet a *test*. AD-10's required-test trio does not include a crash-isolation/degradation test (e.g. "panic an edge goroutine, assert core + reflexes still tick"). The Python spine had the same gap, so this is inherited, not introduced — but given `recover()`-doesn't-cross-goroutines is called out as the precise Go hazard, a degradation property test belongs in the M0 trio. **Medium.**

---

## Checklist Item 3 — Nothing under Deferred could let two units diverge

**Pass (high confidence).**

Walked every Deferred bullet for load-bearing-ness:
- *Privsep-lite activation (M3+)* — deferred **behind the unchanged `Worker` seam (AD-2)** and the unchanged Envelope contract (AD-4). The seam is decided now; only the implementation flips. No divergence: callers cannot tell which impl is behind the interface.
- *Threat-model confirmation before M3* — the *basis* (worker untrusted) is settled in-principle in AD-3; only the explicit confirmation + property test is deferred to the M3 gate. The invariant is fixed now.
- *WASM/wazero worker* — third impl behind the **same seam**. No divergence.
- *gokrazy* — deployment target deferred; Pi OS + systemd is decided for M0–M4. (But see Item 6 — the *dimension* is under-specified, even though this bullet is correctly deferred.)
- *Additional transport adapters / multi-pet / multi-user* — schema is shaped now for the non-breaking `chat_id`/`user_id` add (AD-7/AD-12). The contract that prevents divergence is decided; only adapters are deferred.
- *Heap-fragmentation guard, reflex/scheduler catalogue, credit numbers, battery thresholds, promotion heuristics, model ids* — all genuinely runtime/story-time tuning, not invariants. Correctly deferred.

Nothing load-bearing was deferred. The Deferred section is disciplined and each item names the AD that keeps it convergent. This is a model Deferred list.

---

## Checklist Item 4 — Ratifies rather than contradicts the Python source spine; no new Go AD weakens an inherited invariant; all Python ADs carried or consciously re-decided

**Partial — one Python AD silently dropped. This is the headline finding.**

Full Python AD → Go AD cross-walk (Python has **15** ADs, AD-1..AD-15 — note the prompt's "11 Python ADs" undercounts):

| Python AD | Subject | Go disposition | Status |
|---|---|---|---|
| Py AD-1 | Hexagonal LLM-free core | Go AD-1 (folds in fork collapse) | Carried + strengthened |
| Py AD-2 | Broker sole trust boundary | Go AD-9 | Carried (renumbered) |
| Py AD-3 | Fork-server ephemeral workers | Go AD-2 (seam) + Go AD-1 | **Consciously re-decided** (fork → seam/goroutine→subprocess) |
| Py AD-4 | Envelope bus only seam | Go AD-4 (folds in Py AD-11) | Carried |
| Py AD-5 | Core sole writer | Go AD-6 | Carried |
| Py AD-6 | Hybrid memory (sqlite + md) | Go AD-7 (+ AD-3 vault staging) | Carried + re-staged |
| **Py AD-7** | **Volatile state in RAM, checkpointed** | **— none —** | **SILENTLY DROPPED** |
| Py AD-8 | One plugin model | Go AD-14 | Carried (re-decided dynamic→compile-time) |
| Py AD-9 | Arbiter governs the brain | Go AD-8 | Carried |
| Py AD-10 | Versioned contracts + tests | Go AD-10 | Carried |
| Py AD-11 | Closed header + routing modes | Go AD-4 (folded) | Carried |
| Py AD-12 | Turn identity & idempotent close | Go AD-11 | Carried |
| Py AD-13 | Chat transport pluggable | Go AD-12 | Carried |
| Py AD-14 | Scheduler / autonomous mind | Go AD-13 | Carried |
| Py AD-15 | Dreaming & consolidation | Go AD-15 | Carried |
| (new) | Crash-isolation / suture | Go AD-5 | New Go AD (carries Py degradation semantics) |
| (new) | Vault threat-trigger staging | Go AD-3 | New Go AD (grounds Py AD-6 in Go staging) |

**FINDING 1 (CRITICAL — silently dropped invariant): Python AD-7 has no Go counterpart.**

Py AD-7 rules: *"the personality-state struct and the working window live in RAM, checkpointed periodically to one small file… RAM state is never the source of truth"* — its whole purpose is **preventing SD-card wear from high-frequency reflex/state churn** (mood drift, blink, the reflex jobs that tick constantly with no LLM).

The Go spine addresses SD-wear for the **memory** layer (sqlite WAL + `synchronous=NORMAL` + batched commits in AD-7; atomic markdown via `renameio` in AD-7), but **says nothing about where the high-churn personality-state lives or whether it is checkpointed vs written-through.** AD-6 establishes core as sole writer of personality-state; the Consistency Conventions table covers memory persistence in detail but is silent on volatile-state persistence. The Structural Seed shows `core/ … state/` as a package but no disk/RAM/checkpoint decision.

Why this matters in Go specifically: the reflex jobs in AD-13 ("mood drift, blink … in-core, no LLM, cheap CPU") are exactly the high-frequency mutators Py AD-7 was written to protect the SD card from. Go's `core` is now a single long-lived process holding all of this in goroutine-owned memory — the RAM-residence half of Py AD-7 is *arguably* now implicit (it's all in-process Go structs). **But the "checkpointed periodically to one small file, not written-through" half is a genuine, unstated invariant.** Without it, a feature builder adding a reflex has no rule preventing a per-tick disk write that wears the SD card — the precise failure Py AD-7 existed to fence. This is a feature-reachable divergence (Item 1) caused by a dropped inherited invariant (Item 4).

*Severity rationale:* CRITICAL on the "silently dropped" axis (the rubric explicitly asks to flag silent drops), HIGH on real-world impact (SD-wear on a battery-yanked always-on device is a named project concern). Either re-decide it consciously ("volatile state is in-process RAM; checkpoint cadence is runtime config, not written-through") or add a one-line AD/convention. Even a conscious *removal* with a sentence would satisfy the rubric; the problem is the **silence**.

**No Go AD weakens an inherited logical invariant.** Checked each carried AD: single-writer (AD-6), no-creds-on-bus (AD-9), turn fencing (AD-11), broker sole egress (AD-9), vault OS-exclusion (AD-3) — all preserved at equal or stronger strength. AD-3 in fact *strengthens* Py AD-6's vault property from "uid perms exclude worker" (a managed boundary) to "the secret does not EXIST until the wall exists" (the window never opens). That is a strict improvement, correctly flagged as an adaptation. The two new Go ADs (AD-3, AD-5) add invariants without contradicting any inherited one.

---

## Checklist Item 5 — Covers the spec's capabilities (CAP-1..CAP-11 all map)

**Pass (high confidence).**

All 11 CAPs are bound in frontmatter (`binds: CAP-1..CAP-11`) and every one has a row in the Capability → Architecture Map with both a "Lives in" and a "Governed by" column:

- CAP-1 → AD-12/2/9/8/4/11 · CAP-2 → AD-1/6/8 · CAP-3 → AD-14 · CAP-4 → AD-8/13 · CAP-5 → AD-9/4 · CAP-6 → AD-6/7/15/3 · CAP-7 → AD-14/6 · CAP-8 → AD-9/8 · CAP-9 → AD-6/14 · CAP-10 → AD-13/8 · CAP-11 → AD-15/7.

The mapping matches the Python spine's CAP table one-for-one (same CAP set, governed-by columns updated for the renumbered Go ADs). No CAP is orphaned and no CAP maps to a non-existent AD. The governing-AD choices are sound — e.g. CAP-6 correctly pulls in AD-3 (vault staging) alongside the memory ADs, which the Python table did via Py AD-6 alone. **Solid.**

(Note: CAP definitions themselves were not re-read from a SPEC file — the Go spine has no `sources` link to a Go SPEC, only to the Python spine and the research. Verified consistency against the Python spine's identical CAP set. If a Go-specific SPEC exists with revised CAP wording, this mapping should be re-confirmed against it. **Low** — likely fine since the CAP set is inherited verbatim.)

---

## Checklist Item 6 — Every initiative-altitude dimension is decided, deferred, or an open question — especially the operational/environmental envelope

**Partial — the operational/environmental dimension is largely silent. Second finding.**

Dimensions an initiative spine owns, and their status here:

| Dimension | Status |
|---|---|
| Component decomposition / boundaries | Decided (Paradigm, AD-1, namespace map, Structural Seed) |
| Inter-component communication | Decided (AD-4) |
| State/data ownership & persistence | Mostly decided (AD-6/7) — **gap: volatile-state persistence, Finding 1** |
| Trust / security boundary | Decided (AD-2/3/9), staged |
| Concurrency / supervision / failure | Decided (AD-5/8/11) |
| Cost / power / scheduling envelope | Decided (AD-8/13) |
| Extensibility (plugins) | Decided (AD-14) |
| Stack / build / toolchain | Decided (Stack table, build flags) |
| **Deployment** | Partial — Pi OS + systemd named (Stack + Structural Seed footnote: `MemoryHigh`/`MemoryMax`/`OOMPolicy`/`Restart`); gokrazy deferred. Adequate. |
| **Environments (dev / prod, laptop / Pi)** | **Mostly silent.** The cross-compile/deploy loop, "never compile on the Pi," "M0 passes on the Pi not the laptop," `watchexec`/Taskfile/`rsync` atomic-swap — all in the research, **none in the spine.** "Build" line gives the compile command but no dev↔Pi loop or environment model. |
| **Observability / logging / metrics** | **Whole dimension silent.** No AD, no convention, no Deferred bullet on how the always-on pet logs, surfaces errors operationally, or is monitored. The only error-handling statement is "errors surface as a `Result` error variant (never a panic across the bus)" — that is an internal-contract rule, not an operational-observability decision. For an unattended device that runs forever and gets its battery yanked, "how do I know it's wedged / what happened before a crash" is an initiative-altitude concern. |
| **Config / secrets resolution** | Partial — "config + secrets resolve only inside the broker" (Conventions) covers *model/tool* secrets; transport's own credential is in AD-12. But *where config lives* (file? env? `~/.shelldon/`?) and how it is loaded is unstated. Py spine was equally thin here, so partly inherited. |

**FINDING 2 (HIGH): Observability/logging/operational-monitoring is a whole dimension left silent.** Not decided, not deferred, not an open question. For an always-on, headless, battery-powered device this is a load-bearing initiative concern (matches the project's own "runs forever" framing). At minimum it should be an explicit Deferred bullet or open question; ideally a one-line convention (e.g. "structured logging to journald via systemd; no remote telemetry M0"). The rubric explicitly calls out the operational/environmental envelope as the thing to check, and this is the clearest miss.

**FINDING 3 (MEDIUM): Environments / dev-deploy loop is silent in the spine.** The research has a full, decided dev/deploy loop (cross-compile on laptop, push binary, atomic swap, never build on Pi) and even a success metric ("M0 passes on the Pi"). AD-10 references "passing on the Pi" but the spine never states the laptop↔Pi environment split or the deploy mechanism. This is arguably story-time, but the *constraint* ("never compile on the Pi; `CGO_ENABLED=0` keeps cross-compile a one-liner") is an invariant the whole stack depends on and deserves at least a convention line. Lower severity than Finding 2 because the `CGO_ENABLED=0` constraint *is* captured in the Stack section, which implicitly carries it.

---

## Checklist Item 7 — Terse and convergent (invariants first, seed minimal); flag bloat / rationale-leakage

**Pass with minor bloat. Low-severity findings.**

Overall the spine is appropriately terse and invariant-first; structure is clearly marked as seed; the "Decisions, not rationale (that lives in `.memlog.md`)" banner is honored in most ADs. Strengths: the AD `Prevents:` lines are crisp, the Conventions table is dense and load-bearing, the Deferred list names its governing AD per bullet.

Minor bloat / rationale-leakage (all **LOW**, cosmetic):
- **Design Paradigm section runs long** and re-explains the Python→Go collapse rationale ("The Python v2 spine's *logical* hexagon survives… its *physical* multi-process body… collapses"). This is *why*, not *what* — it reads like research-synthesis prose imported into the spine. A spine could state the paradigm in 2–3 sentences and let `.memlog.md` hold the collapse rationale. Not harmful, but it is the one place the spine narrates instead of decides.
- **Stack "Tuning" note** (`GOMEMLIMIT≈280MiB`, `GOGC=50`, "do **not** set `GOARM64=v8.0,lse` — Cortex-A53 lacks LSE atomics") is explicitly tagged "runtime, not invariant" — good self-awareness, but the *reason* ("Cortex-A53 lacks LSE atomics") is rationale that belongs in memlog. Keeping the directive is fine; the justification is leakage. Trivial.
- **AD parentheticals** ("(Adapts Python AD-X)", "(Keystone Go decision…)") are useful provenance for *this* review but are lineage-rationale; defensible to keep during a port since they aid exactly the AD-carry audit in Item 4. Net positive here; flagging only for completeness.

No structural bloat (no speculative ADs, no over-abstraction). The seed (Structural Seed mermaid + file tree) is minimal and matches the ADs. Convergence is good.

---

## Findings Summary (by severity)

| # | Severity | Finding | Location |
|---|---|---|---|
| 1 | **CRITICAL** (silent drop) / HIGH (impact) | **Python AD-7 (volatile-state in RAM, checkpointed; SD-wear prevention) is silently dropped** — no Go AD or convention governs where high-churn personality-state lives or that it is checkpointed vs written-through. Reflex jobs (AD-13) are exactly its target churn. Re-decide consciously or add a one-line convention. | Item 4; cross-cuts AD-6, AD-13, Conventions |
| 2 | **HIGH** | **Observability / logging / operational-monitoring is a whole silent dimension** — not decided, deferred, or an open question. Load-bearing for an unattended always-on device. Add at least a Deferred bullet or a convention line (e.g. journald). | Item 6 |
| 3 | **MEDIUM** | **Environments / dev-deploy loop silent in the spine** — laptop↔Pi split, "never compile on the Pi," atomic-swap deploy are decided in research but absent from spine. The `CGO_ENABLED=0` constraint is captured in Stack, partially mitigating. | Item 6 |
| 4 | **MEDIUM** | **AD-9 (broker sole cred holder) lacks a CI-enforced fence** symmetric to AD-1's `depguard`. "Only broker holds creds" is idiom + assertion; consider a `depguard` rule restricting the secret/keyring import to `broker/`. | Item 2 |
| 5 | **MEDIUM** | **AD-5 "soul survives any single edge failure" has no required M0 test** — given `recover()` doesn't cross goroutines is the named Go hazard, a degradation/crash-isolation property test belongs in AD-10's M0 trio. (Inherited gap from Python.) | Item 2 |
| 6 | **LOW** | **Config-resolution location unstated** beyond "secrets resolve inside the broker" — where config lives/loads (file/env/`~/.shelldon/`) is silent. (Partly inherited from Python.) | Item 6 |
| 7 | **LOW** | **Minor rationale-leakage / length** — Design Paradigm narrates the Python→Go collapse; Stack tuning note carries the LSE-atomics justification. Cosmetic; move *why* to `.memlog.md`. | Item 7 |
| 8 | **LOW** | **CAP definitions not re-verified against a Go SPEC** (no `sources` link to one) — confirmed consistent with the inherited Python CAP set instead. Re-confirm if a Go SPEC exists. | Item 5 |

## Bottom line

A disciplined, enforceable, convergent port that gets the hard things right — the worker-isolation staging (AD-2/AD-3) is genuinely better than the Python original, the Deferred list is exemplary, and the CAP coverage is complete. **One required fix before "final":** consciously re-decide or restate **Python AD-7's volatile-state/SD-wear invariant** (Finding 1) — it is the only inherited invariant that vanished without a word. **One strong recommendation:** give the **operational/observability dimension** (Finding 2) at least a sentence so it is decided-or-deferred, not silent. The remaining findings are tightening, not blocking.
