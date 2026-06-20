---
name: 'Adversarial seam review — sheldon (Go rewrite) spine'
type: architecture-review
review-kind: adversarial / divergence-pair hunt
target: '../ARCHITECTURE-SPINE.md'
reviewer: adversarial architecture reviewer
created: '2026-06-19'
verdict: 'Strong spine, but six seams permit two conformant units to build incompatibly. Three are critical.'
---

# Adversarial Seam Review — sheldon (Go rewrite)

**Method.** For each seam I constructed two concrete units one level down (two plugins, two scheduler jobs, two transport adapters, two memory-ops, two display claimers) that each obey **every** AD verbatim, then drove them until they collide. A finding only counts if both units pass every cited AD *and* still produce mutually incompatible code/state. Vague "could be clearer" notes are excluded.

Severity scale: **critical** = silent data/state corruption or a security invariant that doesn't actually hold; **high** = build/runtime breakage or two-owner deadlock; **medium** = ambiguous contract forcing rework; **low** = latent footgun.

---

## Finding 1 — `capture_learning` dedup key collision: two writers of one `learnings` row, no merge rule `[CRITICAL]`

**The two units.**
- **Unit A — Reflection turn** (a turn job, AD-13): proposes `capture_learning(observation="owner prefers terse replies", pattern_key="style.terseness")`.
- **Unit B — Dream turn** (AD-15): in the same idle window also proposes `capture_learning(observation="owner asks for short answers", pattern_key="style.terseness")` while classifying pending learnings.

**ADs both satisfy.** AD-6 (worker only *proposes*, core is sole writer — both do), AD-7 (`pattern_key` dedup + `recurrence_count` increment is exactly the documented hot path), AD-8 (≤1 turn in flight — they run sequentially, both legal), AD-15 (dream reuses the worker/arbiter — legal), AD-13 (both are named turn jobs). Every rule is obeyed.

**The incompatibility.** AD-7 says core "inserts or increments `recurrence_count`" keyed on `pattern_key`, but it **never defines what `observation` becomes on increment**. Unit A and Unit B supply *different* `observation` text under the *same* key. Three conformant core implementations exist and all pass the ADs:
1. keep first observation, increment count (A's text wins, B's insight lost);
2. overwrite with latest observation (B's text wins, A's lost);
3. append/concat observations (unbounded text growth — violates the *spirit* of AD-15 "unbounded memory growth" while violating no rule).

Worse: the dream turn (AD-15 step 2) reads `recurrence_count` to decide promotion ("by impact + recurrence"). If Unit A and Unit B both fire before a dream consolidates, `recurrence_count` reflects *write contention*, not genuine recurrence — a single idle window with two turn jobs inflates the count and falsely promotes a learning into curated markdown (or `vault/`). The "single writer" invariant (AD-6) is preserved at the row level yet the *semantics* of the row diverge by implementation.

**Proposed tightened AD (amend AD-7).** Add to the `learnings` rule: "On `pattern_key` collision core **keeps the earliest `observation` and increments `recurrence_count` by exactly 1 per distinct `turn_id`** (AD-11 fencing); a second `capture_learning` with the same `pattern_key` *within one `turn_id`* is idempotent (no double-count). `observation` is immutable after insert; richer text is the dream turn's job via `rewrite_about`, not a `capture_learning` overwrite. `pattern_key` is normalized (lowercase, trimmed) before lookup." This makes the merge rule a contract, not an implementation choice, and decouples `recurrence_count` from write contention.

---

## Finding 2 — Display region claims: "closed type in `contracts/`" vs "declared in plugin manifest" is a chicken-and-egg with no single registry `[CRITICAL]`

**The two units.**
- **Unit A — XP/leveling widget plugin** (behavioral, AD-14): manifest declares `display region = "widget.xp"`.
- **Unit B — Battery-status widget plugin** (behavioral, AD-14): manifest declares `display region = "widget.status"`.

**ADs both satisfy.** AD-6 ("plugins may **claim** widget regions"; "Region ids are a **closed type in `contracts/`**"; "conflicting claims are rejected at startup"), AD-14 (manifest is a Go struct declaring display regions; "conflicting claims rejected at STARTUP, one writer per display region"). Both plugins claim *distinct* regions — no conflict, both pass.

**The incompatibility.** AD-6 says region ids are a **closed type in `contracts/`** — i.e. an enum/sealed set that `contracts/` owns. AD-14 says the **plugin manifest** declares the regions it claims, and adding a plugin is "recompile + redeploy." These two rules contradict on **who is allowed to mint a new region id**:
- If region ids are genuinely *closed* in `contracts/`, then Unit A cannot introduce `widget.xp` without editing `contracts/` — so the plugin is **not** self-contained, violating AD-14's "plugin owns its manifest / never imports core" spirit, and every new widget is a `contracts/` change (a hidden coupling the map doesn't show).
- If a plugin may mint its own region id in its manifest, then the set is **open**, directly contradicting AD-6's "closed type." Two plugins authored independently can then both pick the string `"widget.status"` (Unit B's id is a generic name a third plugin would plausibly reuse) — and the "rejected at startup" check only fires if *both happen to be compiled into the same binary at the same time*. Because the registry is compile-time-in-`main` (AD-14), a region-id collision is caught only at the final link, not when either plugin is authored — and the collision message has no defined precedence (which plugin "wins" or whether the build fails) and no namespacing rule.

Net: a builder reading AD-6 builds a closed `RegionID` enum in `contracts/`; a builder reading AD-14 builds an open string-keyed manifest field. Both are "correct." They produce **incompatible region-id type systems** (`enum RegionID` vs `string`), and the display compositor written against one will not compile against plugins written against the other.

**Proposed tightened AD (amend AD-6 + AD-14, pick one model).** Recommended: "Region ids are **namespaced strings** owned by the claiming plugin, of the form `<plugin-name>.<region>` (e.g. `xp.bar`). `contracts/` defines the *core-owned* region ids as a closed set (`face`, and any core regions); plugin regions are an **open, plugin-namespaced** space. The compile-time registry (AD-14) rejects (a) any plugin region not prefixed by its own declared plugin name, and (b) any duplicate fully-qualified id — **build fails**, no precedence/winner." This kills the closed-vs-open contradiction and makes collisions structurally impossible across independently authored plugins (the namespace prefix), while keeping `face` closed in `contracts/`.

---

## Finding 3 — Two emitters of one broadcast event-kind with clashing payload shapes `[CRITICAL]`

**The two units.**
- **Unit A — XP plugin** subscribes to broadcast event-kind `message-answered` (named explicitly in AD-14) to award XP.
- **Unit B — A future "streak" behavioral plugin** also subscribes to `message-answered` to track daily streaks; and **core** (the arbiter, on closing a turn) is the natural **emitter** of `message-answered`.

**ADs both satisfy.** AD-4 (broadcast/subscription over a **closed event-kind set**; both subscribe to a kind in that set), AD-14 (manifest declares subscribed broadcast event kinds; `message-answered` is explicitly listed). Both plugins are conformant bus clients speaking only `Envelope`.

**The incompatibility.** AD-4 closes the *set of event kinds* but **never closes the payload schema of each kind**. The `Envelope` header is fixed (`id/v/kind/src/dst/turn_id`) — but the **body** of a `message-answered` event is unspecified. The XP plugin (Unit A) needs `tokens_used` + `latency_ms` to weight XP; the streak plugin (Unit B) needs `chat_id` + `timestamp` to bucket by day; core, the emitter, decides the shape. Three conformant emitters exist:
1. core emits `{turn_id}` only — both plugins break (missing fields they assumed);
2. core emits a fat struct with every field any plugin might want — unbounded, and every new subscriber needs core to add a field (hidden coupling, AD-14's "never import core" is preserved but the *payload* couples them to core anyway);
3. core emits `map[string]any` — type-unsafe across the very bus AD-10 says is "typed contracts at compile time."

This is the **same class of bug AD-4 exists to prevent** ("ad-hoc point-to-point channels edges can't track") but the rule only hardened the *header* and the *kind enum*, not the *per-kind body*. Two plugins authored against two assumed payload shapes for the same `message-answered` kind will both pass review and silently mis-decode (or panic on a missing field, which AD-5 then masks as a plugin crash → "kills its widget, not the soul" — so the bug is *invisible* even at runtime).

**Proposed new AD (AD-16) or amend AD-4.** "Each broadcast event-kind has a **versioned payload struct in `contracts/`** (`event/message_answered.go`), paired 1:1 with its kind enum value — the kind enum and the payload type are co-defined and co-versioned. Subscribers decode into the contract struct (compile-time typed, AD-10), never `map[string]any`. Adding a field is additive (AD-10 `v` rule); a subscriber needing a field the contract lacks must add it to `contracts/` (visible coupling), not assume it." This extends AD-4's header/kind discipline to bodies and closes the silent mis-decode.

---

## Finding 4 — Scheduler job vs arbiter coalescing: "one pending catch-up slot" has no identity rule, so two turn jobs overwrite each other `[HIGH]`

**The two units.**
- **Unit A — `dream` job** (AD-15, idle-triggered) proposes a turn while a reply turn is in flight.
- **Unit B — `proactive-ping` job** (CAP-4, cadence) *also* proposes a turn during that same in-flight window.

**ADs both satisfy.** AD-8 ("events during a turn **coalesce into a single pending catch-up slot** (never a growing backlog)"; "≤1 worker turn in flight"), AD-13 (both are named turn jobs routed through the arbiter; "the scheduler never invokes the worker directly" — both go through the arbiter correctly). Both obey every word.

**The incompatibility.** AD-8 says there is exactly **one** pending catch-up slot. But it never says what happens when two *different-kind* turn jobs both want that one slot. Unit A (dream) and Unit B (proactive-ping) are not interchangeable — a dream consolidates memory; a ping greets the owner. Conformant arbiters diverge:
1. last-writer-wins on the slot — Unit A's dream silently dropped when B arrives 1ms later (and dreams are how memory stops growing unbounded, AD-15 — so dropping them is a slow leak);
2. first-writer-wins — B's proactive ping dropped (owner never gets the scheduled greeting; CAP-4 silently degraded);
3. priority by cost tier — but **no priority order is defined** in the spine, so two builders invent two different orders.

"Coalesce into a single slot" is well-defined only when the coalesced events are *the same kind* (the documented "poke-stampede" case — N identical pokes → 1). It is **undefined for heterogeneous turn jobs**, which AD-13 explicitly makes possible by having many named turn jobs share one arbiter.

**Proposed tightened AD (amend AD-8).** "The single catch-up slot is **keyed by job-kind**: coalescing collapses *same-kind* pending turns to one (the poke-stampede case). Distinct turn-job *kinds* contending for the slot are ordered by an **explicit priority**: reply-turn > dream > proactive-ping (reply is owner-facing and immediate; dream protects memory durability; proactive is most deferrable). A lower-priority pending job is **deferred, not dropped** — re-proposed on the next idle/cadence tick by its scheduler job (AD-13), bounded by its own cooldown. The slot holds at most one job per kind." This preserves "no growing backlog" (bounded by kind count) while making the drop/defer behavior deterministic.

---

## Finding 5 — `chat_id`/`user_id` "non-breaking add" lands in two tables with two shapes; owner identity has no home `[HIGH]`

**The two units.**
- **Unit A — sqlite `messages`/`learnings` schema** (AD-7): "Schema shaped so an owner/`chat_id`/`user_id` key is a non-breaking add."
- **Unit B — chat-transport adapter** (AD-12): "An **owner** identity exists; `chat_id`/`user_id` is a non-breaking schema add."

**ADs both satisfy.** AD-7 (sqlite owns history+learnings; key is a future non-breaking add), AD-12 (adapter emits inbound envelopes; owner identity exists; `chat_id`/`user_id` is a non-breaking add). Both ADs *promise the same future column* but neither says **who defines its type or where the owner identity is the source of truth**.

**The incompatibility.** Two conformant designs:
1. The **transport adapter** is the authority — it stamps `chat_id`/`user_id` onto the inbound-message contract (a `contracts/` field), and core copies it into sqlite. Then `user_id` is *whatever the adapter's backend uses* (a Telegram int64, a CLI string, a future web UUID) — so the sqlite column type is **adapter-dependent**, and a second adapter (CLI) with a different id type breaks the column or forces a stringly-typed lowest-common-denominator.
2. **Core** is the authority — it owns a canonical owner-identity type in `contracts/`, and adapters *map* their backend id into it. Then AD-12's "the adapter holds its own connection credential / owner identity exists" reads as core-owned, and the adapter must translate — but AD-12 never mandates the translation, so an adapter author who read design (1) ships raw `telego` ids into core, **leaking `telego.Update`-shaped data into core** — exactly what AD-12 forbids ("never leak `telego.Update` into core").

So the same two ADs, read in the two orders, produce (a) an adapter that stamps backend-native ids (conformant to AD-7's "non-breaking add," leaks backend shape) vs (b) a core that defines a canonical id (conformant to AD-12's "transport-agnostic contract," requires adapter translation AD-12 never demands). The sqlite column type and the inbound-contract field type **diverge irreconcilably** the moment a second adapter ships.

**Proposed tightened AD (amend AD-12, ref AD-7).** "The **owner/principal identity is a core-owned canonical type in `contracts/`** (e.g. `Principal{ Owner bool; ID string }` where `ID` is an opaque, adapter-namespaced string `<adapter>:<backend-id>`). Adapters **must** map their backend id into this type at the edge (the place AD-12's `telego.Update` non-leak rule already lives); core and sqlite only ever see the canonical type. The future sqlite `principal_id` column is `TEXT` holding the namespaced string (AD-7 non-breaking add resolved to one concrete type). No adapter-native id type ever reaches core or sqlite." This makes the "non-breaking add" a single typed decision instead of two adapters racing to define it.

---

## Finding 6 — Worker reads "read-only history + non-vault markdown" but core is *concurrently* the sole writer — no read-isolation contract `[MEDIUM]`

**The two units.**
- **Unit A — a reply worker turn** reads the recent window from sqlite (AD-7) and `grep`s the markdown tree to assemble a prompt.
- **Unit B — core** (sole writer, AD-6), during that same turn, applies a `capture_learning` from a *prior* just-closed turn (a batched WAL commit, AD-7) and atomically rewrites `about.md` via `renameio` (a dream-promotion from an earlier dream).

**ADs both satisfy.** AD-6 (core sole writer; worker reads read-only — both obey), AD-7 (worker "reads history read-only and the markdown tree minus `vault/`"; WAL + batched commits; atomic markdown writes), AD-2 (worker behind the seam). Nothing is violated as written.

**The incompatibility.** "Read-only" constrains the *worker's intent* (it won't write) but the spine gives **no snapshot/consistency contract** between the concurrent reader (worker) and the concurrent writer (core). Two regimes are both conformant:
1. **sqlite under WAL** gives the worker a consistent read snapshot *per connection/transaction* — fine **only if** the worker opens a read transaction. If the worker (M0–M2 goroutine) shares core's `*sql.DB` handle or reads without a transaction, it can observe a half-batched commit window. AD-7 mandates WAL but never mandates the worker read inside a snapshot transaction.
2. **markdown via `renameio`** is atomic *per file* — but a prompt assembles `DIRECTIVE.md` + `about.md` + `facts/*` as a **multi-file read**. Core can rename `about.md` *between* the worker reading `about.md` and reading `facts/foo.md`, yielding a **torn cross-file view** (post-dream `about.md`, pre-dream `facts/`). Per-file atomicity (AD-7) does not give multi-file consistency, and the spine claims no transaction boundary across the tree.

At M3 this *worsens*: the worker is a subprocess reading the same files/db across the process wall while core writes — the goroutine-era assumption that "it's all one process, reads are basically fine" silently breaks, and AD-3/AD-2 promise the seam swap "reshapes no caller." A read-isolation bug that's masked by single-process memory at M0 surfaces only at M3.

**Proposed new AD (AD-17) or amend AD-7.** "The worker's reads are **snapshot-consistent for the duration of one turn**: (a) sqlite reads occur inside a single read transaction opened at turn start (WAL reader snapshot); (b) the markdown tree is read against a **turn-pinned view** — either core defers tree-mutating writes (`renameio` renames, dream promotions) until no turn is in flight (AD-8's ≤1-turn bound makes this a single gate), or the worker is handed a pinned manifest of file versions at turn start. Per-file atomicity (AD-7) is necessary but not sufficient; cross-file turn consistency is the contract. This holds identically across the AD-2 seam (goroutine and subprocess)." Coupling tree-writes to the no-turn-in-flight gate is the cheap option and reuses AD-8.

---

## Inherited-invariant sanity check (Go AD vs Python invariant)

I checked each Go AD against the Python invariant it claims to adapt. Two places where the Go decision **weakens or muddies** an inherited invariant:

1. **AD-1 collapses Python's multi-process body into one process — and in doing so makes AD-5's crash-isolation claim weaker than it reads.** Python got crash isolation *for free* from real process walls (a dead broker process can't corrupt core's heap). Go AD-5 leans on `suture` + `defer recover()`, but **`recover()` only catches panics, not memory corruption, deadlocks, or a goroutine spinning on a lock** — and AD-1 explicitly notes "Go `recover()` does NOT cross goroutines." So "the soul survives ANY single edge failure" is **strictly weaker** than the Python invariant it inherits: a goroutine edge that deadlocks (not panics) or corrupts shared memory (e.g. a data race on a shared slice handed over the bus) takes core down, and `suture` never fires because nothing panicked. The Python AD-8/AD-13 degradation semantics are claimed as "carried," but the *enforcement mechanism* is materially weaker until M3 puts the worker behind a real wall. **This is acceptable for the untrusted worker (AD-3 gates the real threat to M3) but the spine over-claims for the *other* edges** (broker/display/transport/plugins) which stay goroutines forever. Recommend AD-5 add: "edge actors share no mutable memory with core except via `Envelope` values transferred (not aliased) over the bus; a value placed on the bus is owned by the receiver — no concurrent access to a sent struct" — i.e. an explicit **ownership-transfer rule** so AD-5's "survives any single edge failure" doesn't quietly depend on no-data-race discipline that nothing enforces.

2. **AD-4's "NO in-process serialization" weakens the contract-fidelity guarantee that Python's UDS bus enforced structurally.** Python serialized every envelope across the UDS wall, which *forced* the contract to be wire-faithful on every hop and made AD-10's round-trip test exercise the real path continuously. Go passes structs by reference in-process (M0–M2) and only serializes at the M3 worker wall. AD-10 keeps a round-trip test, but it now tests a path **most envelopes never take** until M3. Risk: a contract that's gob-incompatible (unexported field, an `any`, a channel-in-a-struct, a non-gob-registered interface impl) builds and passes M0–M2 green, then **fails only at M3** when the seam swaps to UDS+gob — the exact "M3 reshapes a caller" outcome AD-2/AD-4 promise won't happen. This doesn't contradict a Python invariant so much as **defer its enforcement**. Recommend AD-10 tighten: "the contract round-trip test runs gob encode/decode over **every** `Envelope`/`Job`/`Result`/event-payload type from M0 (not only the worker-boundary types), in CI, so gob-incompatibility is caught at M0, not discovered at the M3 seam swap." This restores the structural guarantee Python got for free from always-on serialization.

No Go AD outright *contradicts* a Python invariant. The two above **defer or weaken enforcement**; both are closable with the tightenings noted.

---

## Summary table

| # | Seam | Severity | Core defect | Closing AD |
| --- | --- | --- | --- | --- |
| 1 | `learnings` dedup merge | critical | `observation` fate + `recurrence_count` semantics on `pattern_key` collision undefined → false promotions | Amend AD-7: immutable observation, +1 per distinct `turn_id`, normalized key |
| 2 | Display region id type | critical | AD-6 "closed type in `contracts/`" vs AD-14 "declared in manifest" → enum-vs-string fork | Amend AD-6/AD-14: plugin-namespaced open strings, `face` closed in `contracts/` |
| 3 | Broadcast event payload | critical | AD-4 closes the kind set but not per-kind body → silent mis-decode, masked by AD-5 | New AD-16: versioned payload struct per kind, co-versioned with the enum |
| 4 | Arbiter catch-up slot | high | "one slot" undefined for heterogeneous turn jobs → dream or ping silently dropped | Amend AD-8: slot keyed by job-kind, explicit priority, defer-not-drop |
| 5 | `chat_id`/`user_id` add | high | Two ADs promise one column, neither owns its type → adapter-native id leaks or column forks | Amend AD-12: core-owned canonical `Principal`, adapter-namespaced opaque id |
| 6 | Worker read isolation | medium | "read-only" ≠ snapshot-consistent; multi-file torn reads; worsens at M3 | New AD-17: turn-pinned snapshot reads; gate tree-writes on no-turn-in-flight |
| S1 | AD-5 crash isolation | (sanity) | `recover()` weaker than Python process walls for non-worker edges | AD-5 add bus ownership-transfer / no-aliasing rule |
| S2 | AD-4/AD-10 serialization | (sanity) | gob-incompat deferred to M3 instead of M0 | AD-10: round-trip gob test over all contract types from M0 |

**Verdict.** The spine is well-constructed and most seams are genuinely closed — the header/kind enum/turn-id/single-writer discipline is tight. But it consistently **hardens identity and structure while leaving the *payload/merge/priority semantics* one level below open**, and that is exactly where two conformant units diverge: the body of an event (F3), the merge of a row (F1), the type of an id (F2, F5), the contents of a slot (F4), the consistency of a read (F6). Findings 1–3 are silent-corruption class and should block M0 contract-freeze. The two sanity findings are deferral-of-enforcement traps that detonate at the M3 seam swap the spine promises is invisible.
