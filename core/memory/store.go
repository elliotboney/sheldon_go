package memory

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver (CGO_ENABLED=0); FTS5 compiled in by default
)

// Store is the sqlite conversation-history layer of AD-7's hybrid memory: ordered
// timestamped messages recallable by recency and by FTS5 keyword. It is pure Go
// (modernc.org/sqlite), so the binary stays CGO_ENABLED=0 (NFR2). core is the sole
// writer (AD-6); the store enforces that with a single open connection. The
// curated markdown tree, the learnings table, and the dream cycle build on this in
// later Epic 4 stories.
type Store struct {
	db *sql.DB
}

// Message is one stored conversation turn. CreatedAt is the insert time; ordering
// uses it with the autoincrement id as a tiebreak.
type Message struct {
	ID        int64
	ConvoID   string
	Role      string // free-form at M1, e.g. "owner"/"pet"
	Content   string
	CreatedAt time.Time
}

// Open opens (creating if absent) the sqlite history db at path in WAL mode and
// runs the idempotent schema migration. It pins a single connection so all access
// is serialized — core is the sole writer (AD-6), and this also avoids "database
// is locked" under WAL. The DSN uses modernc's _pragma=name(value) syntax.
func Open(path string) (*Store, error) {
	if path == "" {
		// modernc treats an empty file: path as a transient in-memory db, which
		// silently loses all data on close — reject it outright.
		return nil, fmt.Errorf("memory: empty db path")
	}
	// Percent-encode the path via url.URL so a path containing ?, &, #, or spaces
	// can't bleed into the pragma query and silently drop the WAL/synchronous/
	// busy_timeout settings. Path keeps its / separators; only the unsafe bytes escape.
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     path,
		RawQuery: "_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)",
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("memory: open sqlite: %w", err)
	}
	// Single writer (AD-6): serialize all access; WAL still allows the one
	// connection to read and write without lock contention.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("memory: ping sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// migrate creates the messages table, its recency index, the external-content
// FTS5 index, the triggers that keep the index in sync, and the learnings table
// with its dedup index. All statements are idempotent (IF NOT EXISTS), so Open is
// safe to call against an existing db.
func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	convo_id   TEXT    NOT NULL,
	role       TEXT    NOT NULL,
	content    TEXT    NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_convo_created ON messages(convo_id, created_at);
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content, content='messages', content_rowid='id');
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
	INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.id, old.content);
END;
CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.id, old.content);
	INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE TABLE IF NOT EXISTS learnings (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	pattern_key      TEXT,
	observation      TEXT    NOT NULL,
	recurrence_count INTEGER NOT NULL DEFAULT 1,
	status           TEXT    NOT NULL DEFAULT 'pending',
	created_at       INTEGER NOT NULL,
	updated_at       INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_learnings_pattern_key ON learnings(pattern_key);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("memory: migrate: %w", err)
	}
	return nil
}

// Append stores one message for convoID and returns its new id. created_at is the
// current time in Unix nanoseconds so same-second turns still order exactly. The
// FTS index is maintained by the insert trigger.
func (s *Store) Append(ctx context.Context, convoID, role, content string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (convo_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
		convoID, role, content, time.Now().UnixNano())
	if err != nil {
		return 0, fmt.Errorf("memory: append: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("memory: append id: %w", err)
	}
	return id, nil
}

// Recent returns up to n messages for convoID, most-recent-first. An empty
// conversation yields an empty slice, not an error.
func (s *Store) Recent(ctx context.Context, convoID string, n int) ([]Message, error) {
	if n <= 0 {
		return []Message{}, nil // SQLite reads LIMIT < 0 as "no limit"; a non-positive cap returns nothing
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, convo_id, role, content, created_at
		   FROM messages
		  WHERE convo_id = ?
		  ORDER BY created_at DESC, id DESC
		  LIMIT ?`, convoID, n)
	if err != nil {
		return nil, fmt.Errorf("memory: recent: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMessages(rows)
}

// Search returns up to n messages for convoID whose content matches query via
// FTS5, best-match-then-most-recent first. The query is treated as a literal
// phrase (escaped + quoted) so arbitrary input — punctuation, FTS5 operators —
// can't throw a syntax error or act as a query DSL.
func (s *Store) Search(ctx context.Context, convoID, query string, n int) ([]Message, error) {
	if n <= 0 {
		return []Message{}, nil // SQLite reads LIMIT < 0 as "no limit"; a non-positive cap returns nothing
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.convo_id, m.role, m.content, m.created_at
		   FROM messages_fts f
		   JOIN messages m ON m.id = f.rowid
		  WHERE f.content MATCH ? AND m.convo_id = ?
		  ORDER BY f.rank, m.created_at DESC
		  LIMIT ?`, ftsPhrase(query), convoID, n)
	if err != nil {
		return nil, fmt.Errorf("memory: search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMessages(rows)
}

// ftsPhrase wraps a raw user term as a quoted FTS5 phrase, escaping embedded
// double quotes by doubling them, so any input is a safe literal match.
func ftsPhrase(query string) string {
	return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
}

// scanMessages reads a result set of message columns into a slice.
func scanMessages(rows *sql.Rows) ([]Message, error) {
	msgs := []Message{}
	for rows.Next() {
		var m Message
		var nanos int64
		if err := rows.Scan(&m.ID, &m.ConvoID, &m.Role, &m.Content, &nanos); err != nil {
			return nil, fmt.Errorf("memory: scan: %w", err)
		}
		m.CreatedAt = time.Unix(0, nanos)
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: rows: %w", err)
	}
	return msgs, nil
}
