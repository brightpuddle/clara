// Package log is the durable, append-only host event log for the agent core.
package log

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/brightpuddle/clara/internal/agents"
)

// Log stores events in the cockpit_events table. HostSeq is the table's
// autoincrement primary key, so it is gap-free and never reused.
type Log struct {
	db *sql.DB
}

// New ensures the schema exists and returns a Log over db.
func New(db *sql.DB) (*Log, error) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS cockpit_events (
			host_seq INTEGER PRIMARY KEY AUTOINCREMENT,
			id       TEXT NOT NULL UNIQUE,
			host     TEXT NOT NULL,
			session  TEXT NOT NULL DEFAULT '',
			ts       INTEGER NOT NULL,
			type     TEXT NOT NULL,
			payload  TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_cockpit_events_session
			ON cockpit_events(session, host_seq);
	`)
	if err != nil {
		return nil, errors.Wrap(err, "create cockpit_events schema")
	}
	return &Log{db: db}, nil
}

// Append persists ev and returns it with HostSeq set. Live-only event types
// (message.delta) are rejected.
func (l *Log) Append(ctx context.Context, ev agents.Event) (agents.Event, error) {
	if !ev.Type.Logged() {
		return agents.Event{}, errors.Newf("event type %q is live-only and not logged", ev.Type)
	}
	if ev.ID == "" {
		ev.ID = agents.NewID()
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	payload := ev.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	res, err := l.db.ExecContext(ctx,
		`INSERT INTO cockpit_events (id, host, session, ts, type, payload)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.Host, ev.Session, ev.TS.UnixNano(), string(ev.Type), string(payload))
	if err != nil {
		return agents.Event{}, errors.Wrap(err, "append event")
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return agents.Event{}, errors.Wrap(err, "read host_seq")
	}
	ev.HostSeq = seq
	ev.Payload = payload
	return ev, nil
}

// Read returns up to limit events with HostSeq > after, in order.
func (l *Log) Read(ctx context.Context, after int64, limit int) ([]agents.Event, error) {
	return l.query(ctx,
		`SELECT host_seq, id, host, session, ts, type, payload FROM cockpit_events
		 WHERE host_seq > ? ORDER BY host_seq LIMIT ?`, after, clampLimit(limit))
}

// ReadSession is Read restricted to one session.
func (l *Log) ReadSession(
	ctx context.Context, session string, after int64, limit int,
) ([]agents.Event, error) {
	return l.query(ctx,
		`SELECT host_seq, id, host, session, ts, type, payload FROM cockpit_events
		 WHERE session = ? AND host_seq > ? ORDER BY host_seq LIMIT ?`,
		session, after, clampLimit(limit))
}

// LatestSeq returns the highest HostSeq, or 0 for an empty log.
func (l *Log) LatestSeq(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	err := l.db.QueryRowContext(ctx, `SELECT MAX(host_seq) FROM cockpit_events`).Scan(&n)
	if err != nil {
		return 0, errors.Wrap(err, "latest host_seq")
	}
	return n.Int64, nil
}

func clampLimit(n int) int {
	if n <= 0 || n > 1000 {
		return 1000
	}
	return n
}

func (l *Log) query(ctx context.Context, q string, args ...any) ([]agents.Event, error) {
	rows, err := l.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, errors.Wrap(err, "query events")
	}
	defer rows.Close()
	var out []agents.Event
	for rows.Next() {
		var (
			e       agents.Event
			ts      int64
			typ     string
			payload string
		)
		if err := rows.Scan(&e.HostSeq, &e.ID, &e.Host, &e.Session, &ts, &typ, &payload); err != nil {
			return nil, errors.Wrap(err, "scan event")
		}
		e.TS = time.Unix(0, ts).UTC()
		e.Type = agents.EventType(typ)
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, errors.Wrap(rows.Err(), "iterate events")
}
