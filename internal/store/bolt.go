package store

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"go.etcd.io/bbolt"
)

// BoltStoreManager manages localized bbolt databases for Clara Actuators.
type BoltStoreManager struct {
	baseDir string
	mu      sync.Mutex
	dbs     map[string]*bbolt.DB
}

// NewBoltStoreManager creates a manager pointing to the state directory.
func NewBoltStoreManager(baseDir string) (*BoltStoreManager, error) {
	// Expand home directory if needed.
	if len(baseDir) > 0 && baseDir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get user home directory")
		}
		baseDir = filepath.Join(home, baseDir[1:])
	}
	
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, errors.Wrap(err, "failed to create state base directory")
	}

	return &BoltStoreManager{
		baseDir: baseDir,
		dbs:     make(map[string]*bbolt.DB),
	}, nil
}

// GetStore retrieves (or opens) the Bolt DB for a specific Actuator.
func (m *BoltStoreManager) GetStore(actuatorID string) (*BoltStore, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if db, ok := m.dbs[actuatorID]; ok {
		return &BoltStore{db: db}, nil
	}

	dbPath := filepath.Join(m.baseDir, actuatorID+".db")
	db, err := bbolt.Open(dbPath, 0o600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open BoltDB for actuator %q", actuatorID)
	}

	// Create default bucket
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("state"))
		return err
	})
	if err != nil {
		db.Close()
		return nil, errors.Wrapf(err, "failed to initialize state bucket for actuator %q", actuatorID)
	}

	m.dbs[actuatorID] = db
	return &BoltStore{db: db}, nil
}

// CloseAll closes all open databases.
func (m *BoltStoreManager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for id, db := range m.dbs {
		if err := db.Close(); err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to close db for %s", id))
		}
	}
	m.dbs = make(map[string]*bbolt.DB)
	if len(errs) > 0 {
		return errs[0] // Return first error
	}
	return nil
}

// BoltStore is a thread-safe wrapper around a single Bolt database bucket.
type BoltStore struct {
	db *bbolt.DB
}

// Get retrieves the byte value associated with key. Returns nil if not found.
func (s *BoltStore) Get(ctx context.Context, key string) ([]byte, error) {
	var val []byte
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("state"))
		if b == nil {
			return errors.New("bucket not found")
		}
		res := b.Get([]byte(key))
		if res != nil {
			val = make([]byte, len(res))
			copy(val, res)
		}
		return nil
	})
	return val, err
}

// Put stores the value associated with key.
func (s *BoltStore) Put(ctx context.Context, key string, val []byte) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("state"))
		if b == nil {
			return errors.New("bucket not found")
		}
		return b.Put([]byte(key), val)
	})
}

// Delete removes the key from the store.
func (s *BoltStore) Delete(ctx context.Context, key string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("state"))
		if b == nil {
			return errors.New("bucket not found")
		}
		return b.Delete([]byte(key))
	})
}
