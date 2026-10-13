package db

import (
	"context"
	"testing"
)

func TestMigrateAndFTS(t *testing.T) {
	ctx := context.Background()
	d, err := OpenTest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// Migrations are idempotent.
	if err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO books (id,title,author,created_at,updated_at) VALUES ('b1','Dune','Frank Herbert','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM books_fts WHERE books_fts MATCH 'herbert'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("fts rows = %d", n)
	}
	if _, err := d.ExecContext(ctx, `UPDATE books SET author='F. Herbert' WHERE id='b1'`); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM books_fts WHERE books_fts MATCH 'frank'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale fts rows = %d", n)
	}
}

func TestSplitStatements(t *testing.T) {
	got := splitStatements("-- c\nCREATE TABLE a (x);\nCREATE TRIGGER t AFTER INSERT ON a BEGIN\n  INSERT INTO a VALUES (1); INSERT INTO a VALUES (2);\nEND;\n")
	if len(got) != 2 {
		t.Fatalf("got %d statements: %q", len(got), got)
	}
}
