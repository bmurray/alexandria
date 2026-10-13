package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every migrations/*.sql file not yet recorded in
// schema_migrations, in lexical order, each inside one transaction.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT NOT NULL PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied := map[string]bool{}
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if applied[name] {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if err := applyOne(ctx, db, name, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		slog.InfoContext(ctx, "applied migration", slog.String("version", name))
	}
	return nil
}

func applyOne(ctx context.Context, db *sql.DB, name, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range splitStatements(body) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%w\nstatement: %s", err, stmt)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
		return err
	}
	return tx.Commit()
}

// splitStatements splits on ";" at end of line, keeping CREATE TRIGGER
// bodies (BEGIN ... END;) whole. Comment-only lines are dropped.
func splitStatements(body string) []string {
	var out []string
	var cur []string
	inTrigger := false
	flush := func() {
		if s := strings.TrimSpace(strings.Join(cur, "\n")); s != "" {
			out = append(out, s)
		}
		cur = cur[:0]
	}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		upper := strings.ToUpper(t)
		if strings.HasPrefix(upper, "CREATE TRIGGER") {
			inTrigger = true
		}
		if inTrigger {
			cur = append(cur, line)
			if upper == "END;" || upper == "END" {
				inTrigger = false
				flush()
			}
			continue
		}
		if strings.HasSuffix(t, ";") {
			cur = append(cur, strings.TrimSuffix(t, ";"))
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return out
}
