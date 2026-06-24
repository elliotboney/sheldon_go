package memory

import "context"

// JournalMode reports the active SQLite journal_mode for white-box test assertions
// (e.g. confirming the WAL pragma took effect). Read-only and test-only — it
// deliberately exposes no raw handle, so tests can't bypass the FTS triggers by
// writing to messages directly.
func (s *Store) JournalMode(ctx context.Context) (string, error) {
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return "", err
	}
	return mode, nil
}
