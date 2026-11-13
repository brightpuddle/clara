package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/ncruces/go-sqlite3/driver"
)

// DefaultRetention is how long audit and run history is kept.
const DefaultRetention = 30 * 24 * time.Hour

// sqliteDSN adds the pragmas every connection needs. WAL keeps readers (the
// daemon's own queries, the sqlite3 CLI) from blocking behind a writer.
func sqliteDSN(path string) string {
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return path
	}
	return "file:" + path +
		"?_pragma=journal_mode(wal)&_pragma=busy_timeout(10000)&_pragma=synchronous(normal)"
}

// isNewDatabase reports whether path has no database yet.
func isNewDatabase(path string) bool {
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return false
	}
	st, err := os.Stat(path)
	return err != nil || st.Size() == 0
}

// initFreshDatabase creates path with incremental auto-vacuum, which lets Prune
// hand space back to the OS without a stop-the-world VACUUM. The setting only
// takes effect while the database is empty (existing files need one VACUUM), and
// before WAL mode writes the header, so it runs once here, ahead of the normal
// connection pool.
func initFreshDatabase(path string) error {
	db, err := driver.Open("file:"+path, nil)
	if err != nil {
		return errors.Wrap(err, "create sqlite database")
	}
	defer db.Close()
	for _, stmt := range []string{
		`PRAGMA auto_vacuum = INCREMENTAL`,
		`VACUUM`, // applies the setting to the empty database
		`PRAGMA journal_mode = wal`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return errors.Wrapf(err, "init database: %s", stmt)
		}
	}
	return nil
}

type pruneTarget struct {
	table string
	where string // %d-free predicate with one ? for the cutoff
	unit  func(time.Time) int64
}

func seconds(t time.Time) int64 { return t.Unix() }
func millis(t time.Time) int64  { return t.UnixMilli() }

var pruneTargets = []pruneTarget{
	{"tool_calls", "called_at < ?", millis},
	{"trigger_runs", "started_at < ?", millis},
	{"intent_events", "created_at < ?", seconds},
	{"intent_replay_history", "created_at < ?", seconds},
	{"intent_runs", "updated_at < ? AND status NOT IN ('running','waiting')", seconds},
}

// Prune deletes audit and run history older than cutoff, in batches so writers
// are never blocked for long, then returns freed pages to the OS. It returns the
// number of rows deleted.
func (s *Store) Prune(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	if batch <= 0 {
		batch = 5000
	}
	var total int64
	for _, tg := range pruneTargets {
		q := fmt.Sprintf(
			`DELETE FROM %[1]s WHERE rowid IN (SELECT rowid FROM %[1]s WHERE %[2]s LIMIT ?)`,
			tg.table, tg.where,
		)
		for {
			res, err := s.db.ExecContext(ctx, q, tg.unit(cutoff), batch)
			if err != nil {
				return total, errors.Wrapf(err, "prune %s", tg.table)
			}
			n, _ := res.RowsAffected()
			total += n
			if n < int64(batch) {
				break
			}
		}
	}
	if err := s.reclaim(ctx); err != nil {
		return total, err
	}
	return total, nil
}

// reclaim releases free pages in chunks; it is a no-op unless the database was
// created or vacuumed with auto_vacuum=incremental.
func (s *Store) reclaim(ctx context.Context) error {
	for {
		var free int
		if err := s.db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&free); err != nil {
			return errors.Wrap(err, "read freelist")
		}
		if free == 0 {
			return nil
		}
		if _, err := s.db.ExecContext(ctx, `PRAGMA incremental_vacuum(5000)`); err != nil {
			return errors.Wrap(err, "incremental vacuum")
		}
		var after int
		if err := s.db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&after); err != nil {
			return errors.Wrap(err, "read freelist")
		}
		if after >= free { // auto_vacuum is off; nothing more can be released
			return nil
		}
	}
}
