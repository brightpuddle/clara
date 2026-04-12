package search

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/brightpuddle/clara/internal/search"
)

// IndexMail walks the given directory and indexes all .eml and .emlx files.
func (s *Server) IndexMail(ctx context.Context, mailDir string) error {
	s.log.Info().Str("dir", mailDir).Msg("starting mail indexing")

	var tx *sql.Tx
	var err error

	commit := func() error {
		if tx != nil {
			if err := tx.Commit(); err != nil {
				return err
			}
			tx = nil
		}
		return nil
	}

	begin := func() error {
		if tx == nil {
			tx, err = s.indexer.BeginTx(ctx)
			if err != nil {
				return err
			}
		}
		return nil
	}

	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	count := 0
	err = filepath.WalkDir(mailDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			s.log.Debug().Err(err).Str("path", path).Msg("walk error")
			return nil // skip errors
		}
		if d.IsDir() {
			return nil
		}

		name := d.Name()
		if !strings.HasSuffix(name, ".eml") && !strings.HasSuffix(name, ".emlx") && !strings.HasSuffix(name, ".partial.emlx") {
			return nil
		}

		if err := begin(); err != nil {
			return err
		}

		doc, err := parseEmail(path)
		if err != nil {
			s.log.Debug().Err(err).Str("path", path).Msg("failed to parse email")
			return nil
		}

		if err := s.indexer.IndexWithTx(ctx, tx, doc); err != nil {
			return fmt.Errorf("index with tx: %w", err)
		}
		count++

		if count%1000 == 0 {
			if err := commit(); err != nil {
				return err
			}
			s.log.Info().Int("count", count).Msg("mail indexing progress")
		}
		return nil
	})

	if err != nil {
		return err
	}

	if err := commit(); err != nil {
		return fmt.Errorf("final commit: %w", err)
	}

	s.log.Info().Int("count", count).Msg("mail indexing complete")
	return nil
}

func parseEmail(path string) (*search.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var r io.Reader = f
	if strings.HasSuffix(path, ".emlx") || strings.HasSuffix(path, ".partial.emlx") {
		// .emlx files start with a byte count line
		var count int
		_, err := fmt.Fscanf(f, "%d\n", &count)
		if err != nil {
			// Fallback: just read the whole file if header is missing/weird
			f.Seek(0, 0)
		} else if count > 0 {
			r = io.LimitReader(f, int64(count))
		} else {
			f.Seek(0, 0)
		}
	}

	msg, err := mail.ReadMessage(r)
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}

	header := msg.Header
	subject := header.Get("Subject")
	from := header.Get("From")
	to := header.Get("To")
	date := header.Get("Date")
	messageID := header.Get("Message-ID")

	body, err := io.ReadAll(msg.Body)
	if err != nil {
		// If body read fails, we might still have useful header info
		// but let's try to be consistent
		return nil, fmt.Errorf("read body: %w", err)
	}

	return &search.Document{
		ID: path,
		Data: map[string]string{
			"subject":    subject,
			"from":       from,
			"to":         to,
			"body":       string(body),
			"date":       date,
			"message_id": messageID,
		},
	}, nil
}
