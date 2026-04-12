package store

import (
	"context"
	"encoding/json"

	"github.com/cockroachdb/errors"
)

// KVValue is a generic JSON-serializable value stored in the KV store.
type KVValue any

// GetKV retrieves a value from the KV store by key.
// It unmarshals the JSON value into the provided destination.
// Returns sql.ErrNoRows if the key does not exist.
func (s *Store) GetKV(ctx context.Context, key string, dest any) error {
	var valueJSON string
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM kv_store WHERE key = ?`, key).Scan(&valueJSON)
	if err != nil {
		return err // includes sql.ErrNoRows
	}
	if err := json.Unmarshal([]byte(valueJSON), dest); err != nil {
		return errors.Wrapf(err, "decode kv value for key %q", key)
	}
	return nil
}

// SetKV stores a JSON-serializable value in the KV store.
func (s *Store) SetKV(ctx context.Context, key string, value any) error {
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return errors.Wrapf(err, "marshal kv value for key %q", key)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO kv_store (key, value_json, updated_at)
		VALUES (?, ?, unixepoch())
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = unixepoch()
	`, key, string(valueJSON))
	return errors.Wrapf(err, "set kv value for key %q", key)
}

// DeleteKV removes a key from the KV store.
func (s *Store) DeleteKV(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM kv_store WHERE key = ?`, key)
	return errors.Wrapf(err, "delete kv value for key %q", key)
}
