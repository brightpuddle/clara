// Package hub is the aggregated, hub-side event log. It assigns the global
// seq and de-duplicates on (host, host_seq).
package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/brightpuddle/clara/internal/agents"
)

// Hub stores events from all hosts in the cockpit_hub_events table.
type Hub struct {
	db *sql.DB
}

// New ensures the schema exists and returns a Hub over db.
func New(db *sql.DB) (*Hub, error) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS cockpit_hub_events (
			seq      INTEGER PRIMARY KEY AUTOINCREMENT,
			host     TEXT NOT NULL,
			host_seq INTEGER NOT NULL,
			id       TEXT NOT NULL,
			session  TEXT NOT NULL DEFAULT '',
			ts       INTEGER NOT NULL,
			type     TEXT NOT NULL,
			payload  TEXT NOT NULL
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_cockpit_hub_events_host
			ON cockpit_hub_events(host, host_seq) WHERE host_seq > 0;
		CREATE INDEX IF NOT EXISTS idx_cockpit_hub_events_session
			ON cockpit_hub_events(session, seq);
	`)
	if err != nil {
		return nil, errors.Wrap(err, "create cockpit_hub_events schema")
	}
	return &Hub{db: db}, nil
}

// Ingest appends events not yet seen (by host and host_seq) in a single
// transaction and returns only the newly stored ones with Seq set.
func (h *Hub) Ingest(ctx context.Context, evs []agents.Event) ([]agents.Event, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "begin ingest")
	}
	defer tx.Rollback() //nolint:errcheck
	var out []agents.Event
	for _, ev := range evs {
		payload := ev.Payload
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}
		var one int
		err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM cockpit_hub_events WHERE host = ? AND host_seq = ?`,
			ev.Host, ev.HostSeq).Scan(&one)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Wrap(err, "check duplicate")
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO cockpit_hub_events
			   (host, host_seq, id, session, ts, type, payload)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			ev.Host, ev.HostSeq, ev.ID, ev.Session, ev.TS.UnixNano(), string(ev.Type),
			string(payload))
		if err != nil {
			return nil, errors.Wrap(err, "ingest event")
		}
		seq, err := res.LastInsertId()
		if err != nil {
			return nil, errors.Wrap(err, "read seq")
		}
		ev.Seq, ev.Payload = seq, payload
		out = append(out, ev)
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "commit ingest")
	}
	return out, nil
}

// Append stores a hub-generated event (host_seq 0), such as host.status.
func (h *Hub) Append(ctx context.Context, ev agents.Event) (agents.Event, error) {
	if ev.ID == "" {
		ev.ID = agents.NewID()
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	if len(ev.Payload) == 0 {
		ev.Payload = json.RawMessage(`{}`)
	}
	ev.HostSeq = 0
	res, err := h.db.ExecContext(ctx,
		`INSERT INTO cockpit_hub_events (host, host_seq, id, session, ts, type, payload)
		 VALUES (?, 0, ?, ?, ?, ?, ?)`,
		ev.Host, ev.ID, ev.Session, ev.TS.UnixNano(), string(ev.Type), string(ev.Payload))
	if err != nil {
		return agents.Event{}, errors.Wrap(err, "append hub event")
	}
	if ev.Seq, err = res.LastInsertId(); err != nil {
		return agents.Event{}, errors.Wrap(err, "read seq")
	}
	return ev, nil
}

// HostCursor returns the highest host_seq ingested for host, or 0.
func (h *Hub) HostCursor(ctx context.Context, host string) (int64, error) {
	var n sql.NullInt64
	err := h.db.QueryRowContext(ctx,
		`SELECT MAX(host_seq) FROM cockpit_hub_events WHERE host = ?`, host).Scan(&n)
	if err != nil {
		return 0, errors.Wrap(err, "host cursor")
	}
	return n.Int64, nil
}

// Read returns up to limit events with Seq > after, in order.
func (h *Hub) Read(ctx context.Context, after int64, limit int) ([]agents.Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := h.db.QueryContext(ctx,
		`SELECT seq, host, host_seq, id, session, ts, type, payload
		 FROM cockpit_hub_events WHERE seq > ? ORDER BY seq LIMIT ?`, after, limit)
	if err != nil {
		return nil, errors.Wrap(err, "query hub events")
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
		if err := rows.Scan(&e.Seq, &e.Host, &e.HostSeq, &e.ID, &e.Session, &ts, &typ,
			&payload); err != nil {
			return nil, errors.Wrap(err, "scan hub event")
		}
		e.TS = time.Unix(0, ts).UTC()
		e.Type = agents.EventType(typ)
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, errors.Wrap(rows.Err(), "iterate hub events")
}
