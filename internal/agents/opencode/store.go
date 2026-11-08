package opencode

import (
	"context"
	"database/sql"

	"github.com/cockroachdb/errors"
)

// Store persists which opencode messages are already in the Clara log, so a
// Clara restart does not re-record history. A message id of "" marks a session
// as registered (session.created recorded).
type Store interface {
	LoadSeen(ctx context.Context) (map[string]map[string]bool, error)
	MarkSeen(ctx context.Context, session, messageID string) error
}

// SQLStore is a Store over the daemon database.
type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) (*SQLStore, error) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS opencode_seen (
			session    TEXT NOT NULL,
			message_id TEXT NOT NULL,
			PRIMARY KEY (session, message_id)
		)`)
	if err != nil {
		return nil, errors.Wrap(err, "create opencode_seen schema")
	}
	return &SQLStore{db: db}, nil
}

func (s *SQLStore) LoadSeen(ctx context.Context) (map[string]map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT session, message_id FROM opencode_seen`)
	if err != nil {
		return nil, errors.Wrap(err, "load opencode_seen")
	}
	defer rows.Close()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var sess, id string
		if err := rows.Scan(&sess, &id); err != nil {
			return nil, errors.Wrap(err, "scan opencode_seen")
		}
		if out[sess] == nil {
			out[sess] = map[string]bool{}
		}
		out[sess][id] = true
	}
	return out, rows.Err()
}

func (s *SQLStore) MarkSeen(ctx context.Context, session, messageID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO opencode_seen (session, message_id) VALUES (?, ?)`,
		session, messageID)
	return errors.Wrap(err, "mark opencode message seen")
}
