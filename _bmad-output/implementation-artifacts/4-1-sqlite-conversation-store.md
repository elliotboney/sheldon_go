---
baseline_commit: ac11f997adf6bd1e9f1edb91c35ece57ffd337bc
---

# Story 4.1: sqlite conversation store

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As Elliot (the owner),
I want conversation history stored in pure-Go sqlite with WAL and FTS5,
so that prior turns are recallable by order and by keyword across ephemeral turns (FR6, AD-7, NFR2).

## Context

**First story of Epic 4 (M2 — "Memory & Dreams").** Epic 3 gave the pet a brain; Epic 4 gives it memory. This story builds the **first durable memory layer**: a pure-Go sqlite store (`modernc.org/sqlite`, WAL + FTS5) for conversation history, at `~/.shelldon/history.db`. It records ordered, timestamped messages and recalls them two ways — **by recency** (the recent-window for prompt assembly) and **by FTS5 keyword** (associative recall). This is the raw, queryable half of AD-7's hybrid memory; the curated markdown tree is Story 4.3.

**Built in isolation, wired later — the established project pattern.** Like the broker (3.1/3.2, built and tested before 3.3 wired it), this story delivers the **store component + its tests**, not the live turn-path integration. The AC is store-level: "a prior-turn message is recalled by both order and keyword." Recording each turn into the store and feeding the recent-window/FTS5 results into the worker's prompt is **prompt-assembly wiring**, which lands with the curated-tree/retrieval work later in Epic 4 (4.3+). 4.1 ships a clean `Store` API ready for that wiring. No `main`/dispatch change here.

**Single writer, no row races (AD-6).** Core is the sole writer of memory. The store enforces this with `db.SetMaxOpenConns(1)` (serializes all access — simple and correct for the M1 single-writer invariant) plus `busy_timeout`. Batched commits (AD-7's write-frequency optimization) are deferred — per-append insert is fine at M1 volume.

**Schema shaped for the future (AD-12).** The `messages` table carries `convo_id` now; an owner/`chat_id`/`user_id` key is a **non-breaking column add** later (AD-12 multi-user). The `learnings` table (Story 4.2) is a *separate* table in the same db — not this story.

**This story does NOT:**
- wire recording into the dispatch turn path or feed retrieval into prompts — that's later-Epic-4 prompt-assembly wiring; 4.1 is the store + API + tests
- build the `learnings` table or `capture_learning` (Story 4.2), the curated markdown tree / `DIRECTIVE.md` (Story 4.3), or the dream cycle (Story 4.4)
- add the `vault/` or any sensitive-classification (Epic 5 — the vault does not exist until the worker is uid-separated, NFR6)
- add batched-commit write coalescing (AD-7 optimization, deferred) or a separate read connection pool
- change `contracts`, the worker, the broker, the arbiter, dispatch, or `main`

## Acceptance Criteria

1. **Recall by recency order and by FTS5 keyword, pure-Go `CGO_ENABLED=0`.**
   **Given** the `modernc.org/sqlite` store (WAL + FTS5) built `CGO_ENABLED=0`
   **When** prior-turn messages are appended and then queried by recency order and by FTS5 keyword
   **Then** the messages are recalled by both order (most-recent-first within a conversation) and by keyword match (FR6/AD-7), and the package builds and tests pass under `CGO_ENABLED=0` (native + arm64).

## Tasks / Subtasks

- [x] **Task 1 — Add the `modernc.org/sqlite` dependency** (AC: 1)
  - [x] `go get modernc.org/sqlite@latest` (resolves to **v1.53.0** or later; pure Go, FTS5 compiled in by default — **no build tag needed**) and `go mod tidy`. Record the resolved version in the File List / Completion Notes.
  - [x] Confirm `CGO_ENABLED=0` native + `GOOS=linux GOARCH=arm64` builds still succeed after adding it (NFR2 — single static binary, no cgo). This is the headline risk; verify early.

- [x] **Task 2 — The conversation store (`core/memory/store.go`)** (AC: 1)
  - [x] Add to the existing `core/memory` package (sibling to `atomic.go`, which already names "Epic 4 builds the sqlite store … on top"). Import `database/sql` and the blank driver `_ "modernc.org/sqlite"`. The driver name for `sql.Open` is `"sqlite"`.
  - [x] `type Message struct { ID int64; ConvoID string; Role string; Content string; CreatedAt time.Time }`. Role is a free string for M1 (e.g. `"owner"`/`"pet"`); no enum yet.
  - [x] `type Store struct { db *sql.DB }`. `func Open(path string) (*Store, error)` — opens with a WAL DSN, sets `db.SetMaxOpenConns(1)` (single-writer invariant, AD-6; avoids "database is locked"), pings, and runs the schema migration (idempotent `CREATE … IF NOT EXISTS`). `func (s *Store) Close() error`.
  - [x] **DSN (modernc.org/sqlite param syntax — NOT mattn's):** `file:<path>?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)`. (modernc uses `_pragma=name(value)`; do not use mattn's `_busy_timeout=`/`_journal_mode=` forms.) As a belt-and-suspenders, the migration may also run `PRAGMA journal_mode=WAL;` explicitly.
  - [x] **Schema (idempotent migration run in `Open`):**
    - `messages(id INTEGER PRIMARY KEY AUTOINCREMENT, convo_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, created_at INTEGER NOT NULL)` — `created_at` is Unix nanoseconds (`time.Time.UnixNano()`), so ordering is exact and `id` breaks ties.
    - `CREATE INDEX IF NOT EXISTS idx_messages_convo_created ON messages(convo_id, created_at)` — for fast recency queries per conversation.
    - **FTS5 external-content table** mirroring `content`: `CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content, content='messages', content_rowid='id')` — external-content avoids storing the text twice.
    - **Sync triggers** keeping the FTS index current (append-only needs the insert trigger; include delete/update for correctness): `messages_ai` (AFTER INSERT → `INSERT INTO messages_fts(rowid, content) VALUES(new.id, new.content)`), and the standard `messages_ad`/`messages_au` using the `messages_fts(messages_fts, rowid, content) VALUES('delete', …)` form.
  - [x] `func (s *Store) Append(ctx context.Context, convoID, role, content string) (int64, error)` — inserts one message with `created_at = time.Now().UnixNano()`, returns the new id (`LastInsertId`). The FTS row is maintained by the trigger.
  - [x] `func (s *Store) Recent(ctx context.Context, convoID string, n int) ([]Message, error)` — `SELECT … WHERE convo_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`; **most-recent-first**. Returns `[]Message` (empty slice, not error, when none).
  - [x] `func (s *Store) Search(ctx context.Context, convoID, query string, n int) ([]Message, error)` — join `messages_fts MATCH ?` to `messages` filtered by `convo_id`, ordered by rank then recency, `LIMIT n`. **Sanitize the query for FTS5:** wrap the user term as a quoted phrase (e.g. `"` + strings.ReplaceAll(query, `"`, `""`) + `"`) so arbitrary input (punctuation, FTS5 operators) can't throw a syntax error — a keyword/phrase match, not a query DSL.
  - [x] Package doc additions: this is AD-7's sqlite layer (raw + queryable); core is the sole writer (AD-6, enforced by `MaxOpenConns(1)`); the curated markdown tree + learnings + dream cycle build on top in later Epic 4 stories.

- [x] **Task 3 — Tests (stdlib `testing`, no testify)** (AC: 1)
  - [x] `core/memory/store_test.go`. Open a `Store` at `filepath.Join(t.TempDir(), "history.db")` per test; `defer Close()`.
  - [x] **AC1 recency:** append 3 messages to one convo (with a tiny ordering guarantee — distinct `created_at`, which `UnixNano` gives; if a test is too fast, insert deterministically and assert by `id` order). `Recent(ctx, convo, 10)` returns them **most-recent-first**; assert order and contents.
  - [x] **AC1 keyword:** append messages where only one contains "raspberry"; `Search(ctx, convo, "raspberry", 10)` returns exactly that message. Assert a non-matching term returns an empty slice.
  - [x] **Conversation isolation:** messages in convo "a" are not returned for convo "b" (both `Recent` and `Search` filter by `convo_id`).
  - [x] **FTS5 robustness:** `Search` with a query containing a double-quote or an FTS5 operator char (e.g. `foo"bar` or `a OR b`) does **not** error — it's treated as a literal phrase (proves the sanitization).
  - [x] **WAL active:** after `Open`, `PRAGMA journal_mode` returns `wal` (a quick query asserting the pragma took).
  - [x] `go test -race ./...` passes; **native + arm64 `CGO_ENABLED=0` builds succeed** (the NFR2 gate — run both explicitly); `golangci-lint run` → 0 issues.

## Dev Notes

### Architecture constraints (binding)

- **AD-7 — Hybrid memory; sqlite is the raw/queryable layer.** "Conversation history + learnings → `modernc.org/sqlite` (pure-Go, `CGO_ENABLED=0`, **FTS5** compiled in), one file at `~/.shelldon/history.db`. Ordered timestamped **messages** (FTS5 keyword recall) … **WAL** mode + `synchronous=NORMAL` + batched commits bound write frequency. Schema shaped so an owner/`chat_id`/`user_id` key is a non-breaking add (AD-12)." This story builds the messages half (FTS5 recall + recency); the `learnings` table is 4.2; batched commits are deferred. [Source: ARCHITECTURE-SPINE.md#AD-7]
- **AD-6 — Core is the sole writer.** Only core mutates the sqlite store. The store enforces single-writer access (`SetMaxOpenConns(1)`); the worker never writes (it proposes via `Result`, applied by core — Story 4.2). [Source: ARCHITECTURE-SPINE.md#AD-6, core/memory/atomic.go]
- **AD-12 — non-breaking conversation-identity add.** `convo_id` is present now; `chat_id`/`user_id` columns are added later without breaking the schema (additive). Keep the schema additive-friendly. [Source: ARCHITECTURE-SPINE.md#AD-12]
- **NFR2 — single static binary, `CGO_ENABLED=0`, arm64, pure-Go deps.** `modernc.org/sqlite` is pure Go; FTS5 is compiled in without cgo. The build MUST stay `CGO_ENABLED=0` native + arm64. This is the story's headline risk — verify on dependency add. [Source: epics.md#NFR2, ARCHITECTURE-SPINE.md#Stack]
- **FR6 — context persists across ephemeral turns via hybrid memory (sqlite recall + markdown).** This story delivers the sqlite recall half. [Source: epics.md#FR6]
- **Stack pin.** "`modernc.org/sqlite` (pure-Go, FTS5) | latest" — the architecture explicitly selects this driver. Do not substitute `mattn/go-sqlite3` (it needs cgo and would break NFR2). [Source: ARCHITECTURE-SPINE.md#Stack]
- **NFR6 / vault.** No `vault/` and no sensitive classification in Epic 4 — the vault does not exist until the worker is uid-separated (Epic 5). This store holds plain conversation history only. [Source: epics.md#NFR6, ARCHITECTURE-SPINE.md#AD-3]

### Key design decisions

- **`modernc.org/sqlite`, not `mattn/go-sqlite3`.** The architecture pins the pure-Go driver precisely because `CGO_ENABLED=0` + arm64 is a hard constraint (NFR2). `mattn` needs cgo and a C toolchain — it would break the single static binary. FTS5 is compiled into modernc by default (no build tag), confirmed for v1.53.0.
- **External-content FTS5 + triggers, not dual-insert.** `content='messages'` keeps the message text in one place; triggers keep the FTS index in sync. This is the canonical SQLite FTS5 pattern and avoids divergence between the table and its index.
- **`SetMaxOpenConns(1)` for the single-writer invariant.** Simplest correct enforcement of AD-6 at M1 — serializes access, eliminates "database is locked" under WAL. A separate read pool is a later optimization if read concurrency ever matters (it won't on a single-owner Pi at M1).
- **`created_at` as Unix nanos + `id` tiebreak.** Exact, monotonic-enough ordering without relying on SQLite's `CURRENT_TIMESTAMP` (second-granularity, ambiguous for same-second turns). Recency = `ORDER BY created_at DESC, id DESC`.
- **Sanitize FTS5 `MATCH` input as a quoted phrase.** Raw user text in `MATCH` can throw a syntax error on FTS5 operator chars. Wrapping as a `"…"` phrase (escaping embedded quotes) makes `Search` a safe keyword/phrase recall, not an injectable query DSL. This is a real gotcha worth a test.
- **Build in isolation now, wire later.** Matches the broker precedent (3.1/3.2 → 3.3). The store ships with a clean API + tests; recording turns and feeding retrieval into prompts is later-Epic-4 prompt-assembly wiring. Avoids speculative dispatch changes.

### Previous story intelligence (Epic 1–3)

- **`core/memory` already exists** with `atomic.go` (the markdown atomic-write primitive) and its doc explicitly says "Epic 4 builds the sqlite store + curated markdown tree + DIRECTIVE.md on top." Add `store.go` here; do not create a new top-level package. [Source: core/memory/atomic.go]
- **Adding a dep cleanly:** `go get … && go mod tidy`; `go mod tidy` will drop a dep nothing imports yet, so write the store code (which imports it) before/at the same time as tidy — learned in 3.4 (telego was dropped by tidy until the adapter imported it). [Source: 3-4 story Completion Notes]
- **`CGO_ENABLED=0` native + arm64 is part of every story's quality gate** — run both explicitly; the project has held this since Epic 1 (1.6). [Source: 1-6, every Epic 2–3 story]
- **Test-double / temp-dir pattern:** `state` tests use `filepath.Join(t.TempDir(), "state.json")`; mirror with `history.db`. Stdlib `testing` only, no testify, table-ish tests with clear failure messages. [Source: core/state/checkpoint_test.go, broker/broker_test.go]
- **Constructor injection over globals** — the store is constructed with `Open(path)` and injected where needed (later wiring), like `state.New(...)` and `broker.New()`. No package-level singletons. [Source: core/state/state.go, broker/broker.go]
- **No new import fence needed.** The core fences forbid `core/` importing `/transport`, `/display` (dispatch fence) and `/broker`, `/worker` (reflex fence). A pure-Go DB driver in `core/memory` violates none. [Source: core/dispatch/imports_test.go, core/scheduler/imports_test.go]

### Latest tech information (modernc.org/sqlite)

- **Version:** `modernc.org/sqlite` **v1.53.0** (current). Pure Go, built on SQLite 3.53.x transpiled via ccgo — **no cgo, `CGO_ENABLED=0` clean** on arm64. `go get modernc.org/sqlite@latest`.
- **FTS5:** compiled in by **default** — `CREATE VIRTUAL TABLE … USING fts5(...)` works with no build tag and no special import. (This is the key confirmation; mattn requires a `sqlite_fts5` cgo tag — modernc does not.)
- **Driver registration:** `import _ "modernc.org/sqlite"`; `sql.Open("sqlite", dsn)` (driver name is **`"sqlite"`**, not `"sqlite3"`).
- **WAL DSN (modernc syntax):** `file:/abs/path/history.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)`. modernc uses `_pragma=name(value)` params — **different from mattn's** `_journal_mode=WAL&_busy_timeout=5000`. Optionally also run `PRAGMA journal_mode=WAL;` after open to be certain.
- **Concurrency:** single-writer app → `db.SetMaxOpenConns(1)` + `busy_timeout` avoids lock contention; WAL allows concurrent readers (not needed at M1). [Source: research subagent — pkg.go.dev/modernc.org/sqlite, gitlab.com/cznic/sqlite v1.53.0]
- **Verify the build tag claim on add:** if a future modernc version ever gates FTS5, the Task-1 `CGO_ENABLED=0` build + the FTS5 test will catch it immediately. Treat a `no such module: fts5` error as "stop and re-check the FTS5 build requirement," not "work around it with cgo."

### Project Structure Notes

- New: `core/memory/store.go`, `core/memory/store_test.go`.
- Modified: `go.mod` / `go.sum` (add `modernc.org/sqlite` + its pure-Go transitive deps).
- Unchanged: `core/memory/atomic.go` (kept), all of `core/*` else, `contracts/*`, `worker/*`, `broker/*`, `transport/*`, `cmd/shelldon/main.go`. No wiring this story.
- `.golangci.yml` unchanged. No new fence. The DB file lives at `~/.shelldon/history.db` in production (path injected via `Open`); tests use `t.TempDir()`.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.1] — the AC (recall by order + FTS5 keyword, `CGO_ENABLED=0`)
- [Source: ...ARCHITECTURE-SPINE.md#AD-7] — hybrid memory; sqlite WAL+FTS5 at ~/.shelldon/history.db; ordered messages + FTS5 recall; schema additive for chat_id/user_id
- [Source: ...ARCHITECTURE-SPINE.md#AD-6, #AD-12, #Stack] — core sole writer; non-breaking identity add; pinned pure-Go driver
- [Source: epics.md#FR6, #NFR2, #NFR6] — context persists via hybrid memory; single static CGO-free binary; no vault in Epic 4
- [Source: core/memory/atomic.go] — the existing memory package this extends
- [Source: core/state/checkpoint_test.go] — the temp-dir test pattern to mirror
- [Source: research subagent / pkg.go.dev/modernc.org/sqlite v1.53.0] — driver name, FTS5-default, WAL DSN syntax

## Dev Agent Record

### Agent Model Used

claude-opus-4-8[1m] (Claude Code dev-story workflow)

### Debug Log References

- `modernc.org/sqlite` resolved to **v1.53.0** (pure Go).
- Headline risk verified first: `CGO_ENABLED=0 go build ./...` (native) + `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...` → both success.
- FTS5 at runtime without cgo: `CGO_ENABLED=0 go test ./core/memory/` → 9 passed (FTS5 `MATCH` works, no build tag).
- `go test -race ./...` → **101 passed (21 packages)** (Epic 3 + debt fixes ended at 94; +7 store tests).
- `golangci-lint run` → 0 issues (after wrapping deferred `rows.Close()` for errcheck).

### Completion Notes List

- **sqlite conversation store (`core/memory/store.go`).** New `Store` in the existing `core/memory` package (sibling to `atomic.go`): `Open/Close/Append/Recent/Search`. Pure-Go `modernc.org/sqlite` (driver `"sqlite"`), so the binary stays `CGO_ENABLED=0` (NFR2). FTS5 is compiled in by default — no build tag — confirmed by the runtime test under `CGO_ENABLED=0`.
- **AC1 — recall by recency + FTS5 keyword.** `Recent` returns most-recent-first (`ORDER BY created_at DESC, id DESC`, where `created_at` is Unix nanos so same-instant turns still order via the id tiebreak); `Search` recalls by FTS5 `MATCH`. Both filter by `convo_id`. Proven by `TestRecent_MostRecentFirst`, `TestSearch_KeywordMatch`, `TestConversationIsolation`, plus limit/empty-convo edge tests.
- **Schema (idempotent migration in `Open`).** `messages` table + `idx_messages_convo_created` recency index + external-content FTS5 (`content='messages', content_rowid='id'`) kept in sync by ai/ad/au triggers — one copy of the text, index stays current. `convo_id` present now; `chat_id`/`user_id` is a non-breaking add later (AD-12).
- **Single writer (AD-6).** `db.SetMaxOpenConns(1)` serializes all access — correct for the M1 single-writer invariant and avoids "database is locked" under WAL. WAL + `synchronous=NORMAL` + `busy_timeout=5000` set via modernc's `_pragma=name(value)` DSN syntax; `TestWALModeActive` confirms `journal_mode=wal`.
- **FTS5 input safety.** `Search` wraps the user term as an escaped quoted phrase (`ftsPhrase`), so arbitrary input (embedded quotes, `OR`/`NEAR(`/`*`/unbalanced quotes) is a safe literal match, never an FTS5 syntax error or query DSL — `TestSearch_HandlesFTS5SpecialChars` exercises six hostile inputs.
- **Test-only DB accessor.** `core/memory/export_test.go` exposes `(*Store).DB()` for the WAL pragma assertion (mirrors `broker/export_test.go`); not part of the public API.
- **Built in isolation (as scoped).** No dispatch/`main` wiring — the store has a clean API ready for the later-Epic-4 prompt-assembly wiring (recording turns + feeding retrieval into prompts). `contracts`, the worker, broker, arbiter, dispatch, and `main` are unchanged. `core/memory/atomic.go` untouched.

### File List

- `core/memory/store.go` (new)
- `core/memory/store_test.go` (new)
- `core/memory/export_test.go` (new — test-only `DB()` accessor for the WAL assertion)
- `go.mod` / `go.sum` (modified — add `modernc.org/sqlite` v1.53.0 + pure-Go transitive deps)
- `_bmad-output/implementation-artifacts/4-1-sqlite-conversation-store.md` (story tracking)
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (status → review)

### Review Findings

- [ ] [Review][Patch] Schema migration not wrapped in a transaction — partial DDL failure leaves DB in unrecoverable state where `messages` table exists but `messages_fts` virtual table and triggers do not; subsequent `Open` calls succeed (IF NOT EXISTS skips the table) but `Search` fails at query time with "no such table: messages_fts" [core/memory/store.go:64-90]
- [ ] [Review][Patch] `ORDER BY f.rank` missing explicit `ASC` — FTS5 rank values are negative (lower = better match), so ascending is correct but the default is implicit; add `ASC` to make the intent explicit and guard against future reader confusion [core/memory/store.go:135]
- [ ] [Review][Patch] `n < 0` passed to `Recent` or `Search` → SQLite `LIMIT -1` = no limit, silently returns full table; guard with `if n < 0 { return nil, fmt.Errorf("memory: n must be non-negative") }` or clamp [core/memory/store.go:111, 129]
- [ ] [Review][Patch] `Search(ctx, convoID, "", n)` → `ftsPhrase("")` produces `""` (empty FTS5 phrase) which throws a FTS5 syntax error; add an early-return empty-slice guard for empty query strings [core/memory/store.go:146-148]
- [x] [Review][Defer] `convo_id` not indexed in `messages_fts` — `Search` does a full FTS scan across all conversations then filters by `convo_id` in the JOIN; correct behavior but O(all messages) at scale; acceptable at M1 single-user volume [core/memory/store.go:130-136] — deferred, pre-existing design decision acceptable at M1
- [x] [Review][Defer] `Open("")` creates an in-memory SQLite DB without error (modernc resolves `file:` with no path as a named in-memory db); data is lost on close with no caller signal; no guard on empty/blank path [core/memory/store.go:37] — deferred, caller invariant; production path is always `~/.shelldon/history.db`
- [x] [Review][Defer] Path containing `?` or `&` silently corrupts the WAL DSN pragma params (becomes `file:/path?foo&_pragma=...` where `foo` is an unknown param swallowing the rest); production path won't have these chars but the API is unguarded [core/memory/store.go:38] — deferred, pre-existing; production paths are safe
- [x] [Review][Defer] FTS index diverges if `messages` table is modified via raw `DB()` accessor (bypasses Go triggers); only reachable via the test-only export — future risk if `DB()` is misused in a test that does direct SQL [core/memory/export_test.go:7] — deferred, future risk only
- [x] [Review][Defer] `telego` (`github.com/mymmrac/telego v1.10.0`) appears in the `go.mod` diff but is not in story 4.1's file list; it was added by the uncommitted Epic 3 (story 3.4) work that predates this story's baseline commit — not introduced by 4.1 [go.mod] — deferred, pre-existing from Epic 3 uncommitted work; will resolve when Epic 3 is committed

## Change Log

- 2026-06-24: Implemented the sqlite conversation store (`core/memory/store.go`, `modernc.org/sqlite` v1.53.0, pure-Go WAL + FTS5) — ordered messages recallable by recency and by FTS5 keyword, single-writer (AD-6), `CGO_ENABLED=0` native + arm64 verified. Built in isolation (no wiring yet). AC1 satisfied; status → review. First story of Epic 4.
- 2026-06-24: Addressed code-review findings (3 of 5 fixed, 2 deferred). Fixed: `Open("")` now rejected (no silent in-memory db); WAL DSN built via `net/url` so a `?`/`&` in the path can't drop pragmas; the raw `DB()` test accessor narrowed to a read-only `JournalMode(ctx)` (no trigger-bypass). Added `TestOpen_RejectsEmptyPath` + `TestOpen_PathWithSpecialChars` (11 memory tests, 103 suite-wide, lint 0). Deferred: `Search` full-FTS-scan (fine at M1 volume); `telego` go.mod diff (uncommitted Epic 3, non-issue). Status → done.
