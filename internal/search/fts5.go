package search

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

type IndexSchema struct {
	Name    string
	Columns []string
}

type Document struct {
	ID   string
	Data map[string]string
}

type SearchResult struct {
	ID    string
	Score float64
}

type Indexer struct {
	db     *sql.DB
	schema *IndexSchema
}

func NewIndexer(path string, schema *IndexSchema) (*Indexer, error) {
	db, err := driver.Open(path, nil)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	indexer := &Indexer{
		db:     db,
		schema: schema,
	}

	if err := indexer.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return indexer, nil
}

func (idx *Indexer) initSchema() error {
	contentTable := quote(idx.schema.Name + "_content")
	idxTable := quote(idx.schema.Name + "_idx")

	var cols []string
	for _, col := range idx.schema.Columns {
		cols = append(cols, fmt.Sprintf("%s TEXT", quote(col)))
	}

	createContent := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id TEXT PRIMARY KEY,
		%s
	)`, contentTable, strings.Join(cols, ",\n"))

	if _, err := idx.db.Exec(createContent); err != nil {
		return fmt.Errorf("create content table: %w", err)
	}

	createIdx := fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(
		%s,
		content=%s,
		content_rowid='rowid'
	)`, idxTable, strings.Join(quoteAll(idx.schema.Columns), ", "), contentTable)

	if _, err := idx.db.Exec(createIdx); err != nil {
		return fmt.Errorf("create fts5 table: %w", err)
	}

	// Trigger to keep index in sync
	// INSERT trigger
	insertTrigger := fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER INSERT ON %s BEGIN
		INSERT INTO %s(rowid, %s) VALUES (new.rowid, %s);
	END`, quote(idx.schema.Name+"_ai"), contentTable, idxTable, strings.Join(quoteAll(idx.schema.Columns), ", "), strings.Join(prefix(quoteAll(idx.schema.Columns), "new."), ", "))

	if _, err := idx.db.Exec(insertTrigger); err != nil {
		return fmt.Errorf("create insert trigger: %w", err)
	}

	// DELETE trigger
	deleteTrigger := fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER DELETE ON %s BEGIN
		INSERT INTO %s(%s, rowid, %s) VALUES('delete', old.rowid, %s);
	END`, quote(idx.schema.Name+"_ad"), contentTable, idxTable, idxTable, strings.Join(quoteAll(idx.schema.Columns), ", "), strings.Join(prefix(quoteAll(idx.schema.Columns), "old."), ", "))

	if _, err := idx.db.Exec(deleteTrigger); err != nil {
		return fmt.Errorf("create delete trigger: %w", err)
	}

	// UPDATE trigger
	updateTrigger := fmt.Sprintf(
		`CREATE TRIGGER IF NOT EXISTS %s AFTER UPDATE ON %s BEGIN
		INSERT INTO %s(%s, rowid, %s) VALUES('delete', old.rowid, %s);
		INSERT INTO %s(rowid, %s) VALUES (new.rowid, %s);
	END`,
		quote(idx.schema.Name+"_au"),
		contentTable,
		idxTable,
		idxTable,
		strings.Join(quoteAll(idx.schema.Columns), ", "),
		strings.Join(prefix(quoteAll(idx.schema.Columns), "old."), ", "),
		idxTable,
		strings.Join(quoteAll(idx.schema.Columns), ", "),
		strings.Join(prefix(quoteAll(idx.schema.Columns), "new."), ", "),
	)

	if _, err := idx.db.Exec(updateTrigger); err != nil {
		return fmt.Errorf("create update trigger: %w", err)
	}

	return nil
}

func (idx *Indexer) Index(ctx context.Context, doc *Document) error {
	contentTable := quote(idx.schema.Name + "_content")

	cols := []string{"id"}
	vals := []any{doc.ID}
	placeholders := []string{"?"}

	for _, col := range idx.schema.Columns {
		cols = append(cols, quote(col))
		vals = append(vals, doc.Data[col])
		placeholders = append(placeholders, "?")
	}

	query := fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)",
		contentTable, strings.Join(cols, ", "), strings.Join(placeholders, ", "))

	if _, err := idx.db.ExecContext(ctx, query, vals...); err != nil {
		return fmt.Errorf("index document: %w", err)
	}

	return nil
}

func (idx *Indexer) Search(
	ctx context.Context,
	queryStr string,
	limit int,
) ([]SearchResult, error) {
	contentTable := quote(idx.schema.Name + "_content")
	idxTable := quote(idx.schema.Name + "_idx")

	results := []SearchResult{}

	// Using rank to sort by relevance
	query := fmt.Sprintf(`
		SELECT c.id, rank
		FROM %s c
		JOIN %s i ON i.rowid = c.rowid
		WHERE %s MATCH ?
		ORDER BY rank
	`, contentTable, idxTable, idxTable)

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := idx.db.QueryContext(ctx, query, queryStr)
	if err != nil {
		return results, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var res SearchResult
		if err := rows.Scan(&res.ID, &res.Score); err != nil {
			return results, fmt.Errorf("scan result: %w", err)
		}
		results = append(results, res)
	}

	return results, nil
}

func (idx *Indexer) Delete(ctx context.Context, id string) error {
	contentTable := quote(idx.schema.Name + "_content")
	query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", contentTable)

	if _, err := idx.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("delete from index: %w", err)
	}

	return nil
}

func (idx *Indexer) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return idx.db.BeginTx(ctx, nil)
}

func (idx *Indexer) IndexWithTx(ctx context.Context, tx *sql.Tx, doc *Document) error {
	contentTable := quote(idx.schema.Name + "_content")

	cols := []string{"id"}
	vals := []any{doc.ID}
	placeholders := []string{"?"}

	for _, col := range idx.schema.Columns {
		cols = append(cols, quote(col))
		vals = append(vals, doc.Data[col])
		placeholders = append(placeholders, "?")
	}

	query := fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)",
		contentTable, strings.Join(cols, ", "), strings.Join(placeholders, ", "))

	if _, err := tx.ExecContext(ctx, query, vals...); err != nil {
		return fmt.Errorf("index document in tx: %w", err)
	}

	return nil
}

func (idx *Indexer) Close() error {
	return idx.db.Close()
}

func prefix(ss []string, p string) []string {
	res := make([]string, len(ss))
	for i, s := range ss {
		res[i] = p + s
	}
	return res
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteAll(ss []string) []string {
	res := make([]string, len(ss))
	for i, s := range ss {
		res[i] = quote(s)
	}
	return res
}
