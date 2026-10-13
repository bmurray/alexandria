// Package db opens the SQLite database and applies embedded migrations.
//
// SQLite has one writer. The pool is capped at a single connection so writes
// serialise in Go rather than contend on the file lock; WAL mode keeps
// readers from blocking. At this scale that is the simplest correct setup.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// Open opens (creating if needed) the SQLite file at path with WAL, foreign
// keys and a busy timeout. ":memory:" opens a private in-memory database.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	dsn := path
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("database dir: %w", err)
			}
		}
		q := url.Values{}
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "busy_timeout(5000)")
		q.Add("_pragma", "foreign_keys(ON)")
		q.Add("_pragma", "synchronous(NORMAL)")
		dsn = "file:" + path + "?" + q.Encode()
	} else {
		dsn = "file::memory:?_pragma=foreign_keys(ON)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}

// OpenTest opens a fresh in-memory database with migrations applied. For tests.
func OpenTest(ctx context.Context) (*sql.DB, error) {
	db, err := Open(ctx, ":memory:")
	if err != nil {
		return nil, err
	}
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
