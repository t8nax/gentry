// Package state is the state store of Gentry: one SQLite database per machine,
// ~/.gentry/state/state.db, that holds tasks, questions, the event journal and
// the rest of the operator's state.
//
// Several gentry processes write the store at once: the agent, the operator
// and the panel. Every write is a transaction; a transaction that fails because
// the store is busy or on an I/O error is retried as a whole. An event is
// written in the same transaction as the change it reports.
package state

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/t8nax/gentry/internal/home"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// migrations are the schema scripts in order: migrations[i] takes the store
// from schema i to schema i+1. Tests append to it to simulate a newer Gentry.
var migrations = loadMigrations()

func loadMigrations() []string {
	names, err := fs.Glob(schemaFiles, "schema/*.sql")
	if err != nil {
		panic(err)
	}
	sort.Strings(names)
	scripts := make([]string, len(names))
	for i, name := range names {
		b, err := schemaFiles.ReadFile(name)
		if err != nil {
			panic(err)
		}
		scripts[i] = string(b)
	}
	return scripts
}

// SchemaVersion returns the schema version this build of Gentry knows.
func SchemaVersion() int { return len(migrations) }

// Retry limits for a write that fails because the store is busy or on an I/O
// error. On the prototype eight processes lost 3 of 1600 writes without retries
// and none of 3200 with them.
const (
	maxAttempts = 10
	minPause    = 5 * time.Millisecond
	maxPause    = 50 * time.Millisecond
)

// ErrNotExist means the store has not been created yet.
var ErrNotExist = errors.New("state store does not exist")

// NewerError means the store was created by a newer Gentry.
type NewerError struct {
	Path      string
	Schema    int // schema version of the store
	Supported int // the latest schema version this Gentry knows
}

func (e *NewerError) Error() string {
	return fmt.Sprintf("%s: schema %d is newer than %d", e.Path, e.Schema, e.Supported)
}

// UnavailableError means the store cannot be opened or written, even after
// retries.
type UnavailableError struct {
	Path string
	Err  error
}

func (e *UnavailableError) Error() string { return fmt.Sprintf("%s: %v", e.Path, e.Err) }
func (e *UnavailableError) Unwrap() error { return e.Err }

// Path returns the path of the store in the data root.
func Path() (string, error) {
	root, err := home.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "state", "state.db"), nil
}

// Store is an open state store.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens the store at path for writing. It creates the store if it does
// not exist and brings an older schema up to date, saving a copy of the store
// before the change.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, &UnavailableError{Path: path, Err: err}
	}
	s, err := open(path, false)
	if err != nil {
		return nil, err
	}
	if err := s.migrate(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// OpenRead opens the store at path for reading only. It never creates or
// changes the store: it returns ErrNotExist if there is none, and does not
// update an older schema.
func OpenRead(path string) (*Store, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotExist
	}
	s, err := open(path, true)
	if err != nil {
		return nil, err
	}
	var v int
	err = s.retry(func() error {
		var err error
		v, err = schemaVersion(s.db)
		return err
	})
	if err == nil && v > SchemaVersion() {
		err = &NewerError{Path: path, Schema: v, Supported: SchemaVersion()}
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func open(path string, readOnly bool) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
		// Take the write lock at BEGIN: a deferred transaction that upgrades
		// from a read lock can fail with SQLITE_BUSY at once, without waiting.
		q.Set("_txlock", "immediate")
	}
	dsn := "file:" + filepath.ToSlash(path) + "?" + q.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, &UnavailableError{Path: path, Err: err}
	}
	// One connection per process: transactions of one gentry run never overlap.
	db.SetMaxOpenConns(1)
	return &Store{db: db, path: path}, nil
}

// Close closes the store.
func (s *Store) Close() error { return s.db.Close() }

// Path returns the path of the store.
func (s *Store) Path() string { return s.path }

// schemaVersion reads the schema version: 0 for a store without schema.
func schemaVersion(q interface {
	QueryRow(string, ...any) *sql.Row
}) (int, error) {
	var n int
	if err := q.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'meta'`).Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	var v string
	err := q.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(v)
}

func (s *Store) migrate() error {
	var v int
	if err := s.retry(func() error {
		var err error
		v, err = schemaVersion(s.db)
		return err
	}); err != nil {
		return s.unavailable(err)
	}
	switch {
	case v > SchemaVersion():
		return &NewerError{Path: s.path, Schema: v, Supported: SchemaVersion()}
	case v == SchemaVersion():
		return nil
	case v > 0:
		if err := s.backup(v); err != nil {
			return s.unavailable(err)
		}
	}
	return s.Write(func(tx *Tx) error {
		// Another process may have migrated the store meanwhile.
		v, err := schemaVersion(tx.tx)
		if err != nil {
			return err
		}
		if v > SchemaVersion() {
			return &NewerError{Path: s.path, Schema: v, Supported: SchemaVersion()}
		}
		for ; v < SchemaVersion(); v++ {
			if _, err := tx.tx.Exec(migrations[v]); err != nil {
				return fmt.Errorf("schema %d: %w", v+1, err)
			}
		}
		_, err = tx.tx.Exec(`INSERT INTO meta (key, value) VALUES ('schema_version', ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`, strconv.Itoa(v))
		return err
	})
}

// backup saves a copy of the store at schema v before the schema changes.
func (s *Store) backup(v int) error {
	dst := fmt.Sprintf("%s.schema-%d.bak", s.path, v)
	if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return s.retry(func() error {
		_, err := s.db.Exec(`VACUUM INTO ?`, dst)
		return err
	})
}

// Tx is a write transaction.
type Tx struct {
	tx *sql.Tx
}

// Exec runs a statement in the transaction.
func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.Exec(query, args...)
}

// QueryRow runs a query that returns at most one row.
func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRow(query, args...)
}

// Write runs fn in a transaction and commits it. If the store is busy or an
// I/O error occurs, the whole transaction is retried, so fn must not have
// effects outside the transaction. An error returned by fn rolls the
// transaction back and is returned as is.
func (s *Store) Write(fn func(tx *Tx) error) error {
	var fnErr error
	err := s.retry(func() error {
		fnErr = nil
		sqlTx, err := s.db.BeginTx(context.Background(), nil)
		if err != nil {
			return err
		}
		if err := fn(&Tx{tx: sqlTx}); err != nil {
			sqlTx.Rollback()
			if retryable(err) {
				return err
			}
			fnErr = err
			return nil
		}
		return sqlTx.Commit()
	})
	if fnErr != nil {
		return fnErr
	}
	if err != nil {
		return s.unavailable(err)
	}
	return nil
}

// retry runs fn until it succeeds, fails with an error that is not worth
// retrying, or the attempts run out.
func (s *Store) retry(fn func() error) error {
	var err error
	for attempt := 1; ; attempt++ {
		err = fn()
		if err == nil || !retryable(err) || attempt == maxAttempts {
			return err
		}
		time.Sleep(minPause + rand.N(maxPause-minPause))
	}
}

func (s *Store) unavailable(err error) error {
	var ne *NewerError
	var ue *UnavailableError
	if errors.As(err, &ne) || errors.As(err, &ue) {
		return err
	}
	return &UnavailableError{Path: s.path, Err: err}
}

// retryable reports whether err is a busy store or an I/O error of SQLite.
func retryable(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code() & 0xff {
	case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED, sqlite3.SQLITE_IOERR:
		return true
	}
	return false
}
