package memory_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/elliotboney/shelldon_go/core/memory"
)

// openTestStore opens a Store at a fresh temp-dir db and registers cleanup.
func openTestStore(t *testing.T) *memory.Store {
	t.Helper()
	s, err := memory.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestRecent_MostRecentFirst is AC1 (recency): appended messages come back
// newest-first within a conversation. The created_at + id DESC ordering is
// deterministic even for same-instant inserts (id breaks the tie).
func TestRecent_MostRecentFirst(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	for _, text := range []string{"first", "second", "third"} {
		if _, err := s.Append(ctx, "c1", "owner", text); err != nil {
			t.Fatalf("append %q: %v", text, err)
		}
	}

	got, err := s.Recent(ctx, "c1", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	want := []string{"third", "second", "first"}
	if len(got) != len(want) {
		t.Fatalf("recent returned %d messages, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Content != w {
			t.Errorf("recent[%d] = %q, want %q (most-recent-first)", i, got[i].Content, w)
		}
	}
}

// TestRecent_RespectsLimit checks the n cap.
func TestRecent_RespectsLimit(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	for _, text := range []string{"a", "b", "c", "d"} {
		if _, err := s.Append(ctx, "c1", "owner", text); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	got, err := s.Recent(ctx, "c1", 2)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 2 || got[0].Content != "d" || got[1].Content != "c" {
		t.Fatalf("recent limit=2 = %+v, want [d c]", got)
	}
}

// TestRecent_EmptyConversation returns an empty slice, not an error.
func TestRecent_EmptyConversation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	got, err := s.Recent(ctx, "nope", 10)
	if err != nil {
		t.Fatalf("recent on empty convo errored: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("recent on empty convo = %v, want empty", got)
	}
}

// TestSearch_KeywordMatch is AC1 (FTS5 keyword): only the message containing the
// term is recalled; a non-matching term returns nothing.
func TestSearch_KeywordMatch(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	for _, text := range []string{
		"the weather is nice today",
		"my raspberry pi just booted",
		"good morning",
	} {
		if _, err := s.Append(ctx, "c1", "owner", text); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, err := s.Search(ctx, "c1", "raspberry", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("search 'raspberry' returned %d messages, want 1", len(got))
	}
	if got[0].Content != "my raspberry pi just booted" {
		t.Errorf("search matched %q, want the raspberry message", got[0].Content)
	}

	none, err := s.Search(ctx, "c1", "kangaroo", 10)
	if err != nil {
		t.Fatalf("search non-match errored: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("search 'kangaroo' = %v, want empty", none)
	}
}

// TestConversationIsolation: Recent and Search only return the queried convo's
// messages — convo_id filters both.
func TestConversationIsolation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.Append(ctx, "a", "owner", "alpha apples"); err != nil {
		t.Fatalf("append a: %v", err)
	}
	if _, err := s.Append(ctx, "b", "owner", "beta apples"); err != nil {
		t.Fatalf("append b: %v", err)
	}

	recent, err := s.Recent(ctx, "a", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 1 || recent[0].Content != "alpha apples" {
		t.Fatalf("recent for convo a = %+v, want only alpha", recent)
	}

	// "apples" is in both convos; search scoped to a must return only a's.
	found, err := s.Search(ctx, "a", "apples", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(found) != 1 || found[0].ConvoID != "a" {
		t.Fatalf("search for convo a = %+v, want only convo a", found)
	}
}

// TestSearch_HandlesFTS5SpecialChars proves the phrase-sanitization: input with
// double quotes or FTS5 operator syntax must not error — it's a literal match.
func TestSearch_HandlesFTS5SpecialChars(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.Append(ctx, "c1", "owner", "she said hello world to me"); err != nil {
		t.Fatalf("append: %v", err)
	}

	for _, q := range []string{`hello"world`, "a OR b", "NEAR(", `"unbalanced`, "*", "AND"} {
		if _, err := s.Search(ctx, "c1", q, 10); err != nil {
			t.Errorf("search(%q) errored, want safe no-op match: %v", q, err)
		}
	}

	// A quoted phrase still matches literally.
	got, err := s.Search(ctx, "c1", "hello world", 10)
	if err != nil {
		t.Fatalf("phrase search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("phrase search 'hello world' = %d, want 1", len(got))
	}
}

// TestWALModeActive confirms the WAL DSN pragma took effect.
func TestWALModeActive(t *testing.T) {
	s := openTestStore(t)
	mode, err := s.JournalMode(context.Background())
	if err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

// TestOpen_RejectsEmptyPath: an empty path would map to modernc's transient
// in-memory db (silent data loss), so Open must reject it with no store.
func TestOpen_RejectsEmptyPath(t *testing.T) {
	s, err := memory.Open("")
	if err == nil {
		t.Fatalf("Open(\"\") = nil error, want rejection")
	}
	if s != nil {
		t.Fatalf("Open(\"\") = %v store, want nil", s)
	}
}

// TestOpen_PathWithSpecialChars: a db filename containing ? and & must not corrupt
// the DSN's pragma query — proves the percent-encoded URI keeps WAL active and the
// store still round-trips a message. Falls back to a space-only name if the OS
// can't create the ?/& filename; the load-bearing check is that WAL survives.
func TestOpen_PathWithSpecialChars(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	path := filepath.Join(dir, "weird?name&v2.db")
	s, err := memory.Open(path)
	if err != nil {
		// Some filesystems reject ?/& in names; retry with a space, still proving
		// special chars don't silently drop the pragma.
		path = filepath.Join(dir, "weird name v2.db")
		s, err = memory.Open(path)
		if err != nil {
			t.Fatalf("open store at special-char path: %v", err)
		}
	}
	t.Cleanup(func() { _ = s.Close() })

	mode, err := s.JournalMode(ctx)
	if err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal (pragmas dropped by bad DSN encoding)", mode)
	}

	if _, err := s.Append(ctx, "c1", "owner", "special chars are fine"); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := s.Recent(ctx, "c1", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 1 || got[0].Content != "special chars are fine" {
		t.Fatalf("recent = %+v, want one round-tripped message", got)
	}
}
