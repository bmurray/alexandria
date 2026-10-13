package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// dbtx is what *sql.DB and *sql.Tx have in common. Store functions take it so
// the service can run a rule inside one transaction. The pool holds a single
// connection, so code inside a transaction must only use that transaction.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// timeLayout is RFC3339 UTC with fixed millisecond precision. A fixed width
// keeps lexical comparison in SQL (due_at < ?) correct.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

func fmtTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad time %q: %w", s, err)
	}
	return t.UTC(), nil
}

func parseNullTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	t, err := parseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func fmtNullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return fmtTime(*t)
}

// scanner is the part of *sql.Row and *sql.Rows that Scan needs.
type scanner interface {
	Scan(dest ...any) error
}

// ---- books --------------------------------------------------------------

// SQL fragments shared by every book read so availability is always derived
// from the same definition.
const (
	openLoansOn    = `(SELECT count(*) FROM loans l WHERE l.book_id = b.id AND l.returned_at IS NULL)`
	waitingHoldsOn = `(SELECT count(*) FROM holds h WHERE h.book_id = b.id AND h.status = 'waiting')`
	bookColumns    = `b.id, b.title, b.author, b.isbn, b.year, b.genre, b.publisher, b.description,
		b.cover_url, b.copies, b.tags, b.created_at, b.updated_at, ` + openLoansOn + `, ` + waitingHoldsOn
)

func scanBook(sc scanner) (*Book, error) {
	var (
		b                  Book
		year               sql.NullInt64
		tags, created, upd string
	)
	if err := sc.Scan(&b.ID, &b.Title, &b.Author, &b.ISBN, &year, &b.Genre, &b.Publisher, &b.Description,
		&b.CoverURL, &b.Copies, &tags, &created, &upd, &b.OnLoan, &b.HoldsWaiting); err != nil {
		return nil, err
	}
	if year.Valid {
		y := int(year.Int64)
		b.Year = &y
	}
	b.Tags = []string{}
	if tags != "" {
		if err := json.Unmarshal([]byte(tags), &b.Tags); err != nil {
			return nil, fmt.Errorf("book %s tags: %w", b.ID, err)
		}
		if b.Tags == nil {
			b.Tags = []string{}
		}
	}
	var err error
	if b.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if b.UpdatedAt, err = parseTime(upd); err != nil {
		return nil, err
	}
	b.Available = max(b.Copies-b.OnLoan, 0)
	return &b, nil
}

func getBook(ctx context.Context, q dbtx, id string) (*Book, error) {
	row := q.QueryRowContext(ctx, `SELECT `+bookColumns+` FROM books b WHERE b.id = ?`, id)
	return scanBook(row)
}

func insertBook(ctx context.Context, q dbtx, b *Book, createdBy string) error {
	tags, _ := json.Marshal(b.Tags)
	_, err := q.ExecContext(ctx, `INSERT INTO books
		(id, title, author, isbn, year, genre, publisher, description, cover_url, copies, tags, created_at, updated_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.Title, b.Author, b.ISBN, nullInt(b.Year), b.Genre, b.Publisher, b.Description, b.CoverURL,
		b.Copies, string(tags), fmtTime(b.CreatedAt), fmtTime(b.UpdatedAt), nullString(createdBy))
	return err
}

func updateBook(ctx context.Context, q dbtx, b *Book) error {
	tags, _ := json.Marshal(b.Tags)
	_, err := q.ExecContext(ctx, `UPDATE books SET title = ?, author = ?, isbn = ?, year = ?, genre = ?, publisher = ?,
		description = ?, cover_url = ?, copies = ?, tags = ?, updated_at = ? WHERE id = ?`,
		b.Title, b.Author, b.ISBN, nullInt(b.Year), b.Genre, b.Publisher, b.Description, b.CoverURL,
		b.Copies, string(tags), fmtTime(b.UpdatedAt), b.ID)
	return err
}

func deleteBook(ctx context.Context, q dbtx, id string) (bool, error) {
	res, err := q.ExecContext(ctx, `DELETE FROM books WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func countBooks(ctx context.Context, q dbtx) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM books`).Scan(&n)
	return n, err
}

func listGenres(ctx context.Context, q dbtx) ([]Genre, error) {
	rows, err := q.QueryContext(ctx, `SELECT genre, count(*) FROM books WHERE genre <> '' GROUP BY genre ORDER BY genre COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Genre{}
	for rows.Next() {
		var g Genre
		if err := rows.Scan(&g.Name, &g.Count); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---- users ---------------------------------------------------------------

func userExists(ctx context.Context, q dbtx, id string) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id = ?`, id).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func countUsers(ctx context.Context, q dbtx) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// ---- loans ---------------------------------------------------------------

const loanSelect = `SELECT l.id, l.book_id, l.user_id, l.checked_out_at, l.due_at, l.returned_at,
	b.title, b.author, b.cover_url, u.name, u.email
	FROM loans l JOIN books b ON b.id = l.book_id JOIN users u ON u.id = l.user_id`

func scanLoan(sc scanner, now time.Time) (*Loan, error) {
	var (
		l        Loan
		out, due string
		returned sql.NullString
	)
	if err := sc.Scan(&l.ID, &l.BookID, &l.UserID, &out, &due, &returned,
		&l.Book.Title, &l.Book.Author, &l.Book.CoverURL, &l.User.Name, &l.User.Email); err != nil {
		return nil, err
	}
	l.Book.ID, l.User.ID = l.BookID, l.UserID
	var err error
	if l.CheckedOutAt, err = parseTime(out); err != nil {
		return nil, err
	}
	if l.DueAt, err = parseTime(due); err != nil {
		return nil, err
	}
	if l.ReturnedAt, err = parseNullTime(returned); err != nil {
		return nil, err
	}
	l.Overdue = l.ReturnedAt == nil && l.DueAt.Before(now)
	return &l, nil
}

func queryLoans(ctx context.Context, q dbtx, now time.Time, where string, args ...any) ([]Loan, error) {
	rows, err := q.QueryContext(ctx, loanSelect+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Loan{}
	for rows.Next() {
		l, err := scanLoan(rows, now)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func getLoan(ctx context.Context, q dbtx, now time.Time, id string) (*Loan, error) {
	return scanLoan(q.QueryRowContext(ctx, loanSelect+` WHERE l.id = ?`, id), now)
}

func insertLoan(ctx context.Context, q dbtx, l *Loan, actorID string) error {
	_, err := q.ExecContext(ctx, `INSERT INTO loans (id, book_id, user_id, checked_out_at, due_at, returned_at, checked_out_by)
		VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		l.ID, l.BookID, l.UserID, fmtTime(l.CheckedOutAt), fmtTime(l.DueAt), nullString(actorID))
	return err
}

func markReturned(ctx context.Context, q dbtx, loanID string, at time.Time, actorID string) error {
	_, err := q.ExecContext(ctx, `UPDATE loans SET returned_at = ?, returned_by = ? WHERE id = ?`,
		fmtTime(at), nullString(actorID), loanID)
	return err
}

func deleteReturnedLoans(ctx context.Context, q dbtx, bookID string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM loans WHERE book_id = ? AND returned_at IS NOT NULL`, bookID)
	return err
}

func countOpenLoans(ctx context.Context, q dbtx, bookID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM loans WHERE book_id = ? AND returned_at IS NULL`, bookID).Scan(&n)
	return n, err
}

func userHasOpenLoan(ctx context.Context, q dbtx, bookID, userID string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM loans WHERE book_id = ? AND user_id = ? AND returned_at IS NULL`, bookID, userID).Scan(&n)
	return n > 0, err
}

// loanWhere translates a LoanFilter into a WHERE clause.
func loanWhere(f LoanFilter, now time.Time) (string, []any) {
	var conds []string
	var args []any
	switch f.Status {
	case LoanOpen:
		conds = append(conds, "l.returned_at IS NULL")
	case LoanOverdue:
		conds = append(conds, "l.returned_at IS NULL AND l.due_at < ?")
		args = append(args, fmtTime(now))
	case LoanReturned:
		conds = append(conds, "l.returned_at IS NOT NULL")
	}
	if f.UserID != "" {
		conds = append(conds, "l.user_id = ?")
		args = append(args, f.UserID)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

func listLoans(ctx context.Context, q dbtx, now time.Time, f LoanFilter) (Page[Loan], error) {
	where, args := loanWhere(f, now)
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM loans l `+where, args...).Scan(&total); err != nil {
		return Page[Loan]{}, err
	}
	// Open loans first, then by most recent activity.
	order := ` ORDER BY (l.returned_at IS NULL) DESC, l.checked_out_at DESC, l.id LIMIT ? OFFSET ?`
	items, err := queryLoans(ctx, q, now, where+order, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return Page[Loan]{}, err
	}
	return Page[Loan]{Items: items, Total: total}, nil
}

// ---- holds ---------------------------------------------------------------

// Position is the 1-based rank among the book's active holds ordered by
// placement time (id breaks ties); inactive holds report 0.
const holdSelect = `SELECT h.id, h.book_id, h.user_id, h.placed_at, h.ready_at, h.status,
	CASE WHEN h.status IN ('waiting','ready') THEN
		(SELECT count(*) FROM holds h2 WHERE h2.book_id = h.book_id AND h2.status IN ('waiting','ready')
			AND (h2.placed_at < h.placed_at OR (h2.placed_at = h.placed_at AND h2.id <= h.id)))
	ELSE 0 END,
	b.title, b.author, b.cover_url, u.name, u.email
	FROM holds h JOIN books b ON b.id = h.book_id JOIN users u ON u.id = h.user_id`

func scanHold(sc scanner) (*Hold, error) {
	var (
		h      Hold
		placed string
		ready  sql.NullString
	)
	if err := sc.Scan(&h.ID, &h.BookID, &h.UserID, &placed, &ready, &h.Status, &h.Position,
		&h.Book.Title, &h.Book.Author, &h.Book.CoverURL, &h.User.Name, &h.User.Email); err != nil {
		return nil, err
	}
	h.Book.ID, h.User.ID = h.BookID, h.UserID
	var err error
	if h.PlacedAt, err = parseTime(placed); err != nil {
		return nil, err
	}
	if h.ReadyAt, err = parseNullTime(ready); err != nil {
		return nil, err
	}
	return &h, nil
}

func queryHolds(ctx context.Context, q dbtx, where string, args ...any) ([]Hold, error) {
	rows, err := q.QueryContext(ctx, holdSelect+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Hold{}
	for rows.Next() {
		h, err := scanHold(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

func getHold(ctx context.Context, q dbtx, id string) (*Hold, error) {
	return scanHold(q.QueryRowContext(ctx, holdSelect+` WHERE h.id = ?`, id))
}

func insertHold(ctx context.Context, q dbtx, h *Hold) error {
	_, err := q.ExecContext(ctx, `INSERT INTO holds (id, book_id, user_id, placed_at, ready_at, status) VALUES (?, ?, ?, ?, ?, ?)`,
		h.ID, h.BookID, h.UserID, fmtTime(h.PlacedAt), fmtNullTime(h.ReadyAt), h.Status)
	return err
}

func setHoldStatus(ctx context.Context, q dbtx, id, status string) error {
	_, err := q.ExecContext(ctx, `UPDATE holds SET status = ? WHERE id = ?`, status, id)
	return err
}

// readyHold is the slice of a ready hold the checkout rule needs.
type readyHold struct {
	ID, UserID string
}

func readyHolds(ctx context.Context, q dbtx, bookID string) ([]readyHold, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, user_id FROM holds WHERE book_id = ? AND status = 'ready' ORDER BY placed_at, id`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []readyHold
	for rows.Next() {
		var h readyHold
		if err := rows.Scan(&h.ID, &h.UserID); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func userHasActiveHold(ctx context.Context, q dbtx, bookID, userID string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM holds WHERE book_id = ? AND user_id = ? AND status IN ('waiting','ready')`, bookID, userID).Scan(&n)
	return n > 0, err
}

// promoteNextHold moves the oldest waiting hold on bookID to ready when a
// copy is free for it (copies minus open loans minus holds already ready).
// It reports the promoted hold id, or "" when nothing changed.
func promoteNextHold(ctx context.Context, q dbtx, bookID string, now time.Time) (string, error) {
	var free int
	err := q.QueryRowContext(ctx, `SELECT b.copies - `+openLoansOn+`
		- (SELECT count(*) FROM holds h WHERE h.book_id = b.id AND h.status = 'ready')
		FROM books b WHERE b.id = ?`, bookID).Scan(&free)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil // book deleted; holds cascade
	}
	if err != nil {
		return "", err
	}
	if free <= 0 {
		return "", nil
	}
	var id string
	err = q.QueryRowContext(ctx, `SELECT id FROM holds WHERE book_id = ? AND status = 'waiting' ORDER BY placed_at, id LIMIT 1`, bookID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	_, err = q.ExecContext(ctx, `UPDATE holds SET status = 'ready', ready_at = ? WHERE id = ?`, fmtTime(now), id)
	return id, err
}

// lapseReadyHolds marks ready holds not picked up by cutoff as lapsed and
// returns the affected book ids (one entry per lapsed hold).
func lapseReadyHolds(ctx context.Context, q dbtx, cutoff time.Time) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, book_id FROM holds WHERE status = 'ready' AND ready_at IS NOT NULL AND ready_at < ?`, fmtTime(cutoff))
	if err != nil {
		return nil, err
	}
	var ids, books []string
	for rows.Next() {
		var id, book string
		if err := rows.Scan(&id, &book); err != nil {
			rows.Close()
			return nil, err
		}
		ids, books = append(ids, id), append(books, book)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if err := setHoldStatus(ctx, q, id, HoldLapsed); err != nil {
			return nil, err
		}
	}
	return books, nil
}

// ---- dashboard -----------------------------------------------------------

func dashboardCounts(ctx context.Context, q dbtx, now time.Time) (*Dashboard, error) {
	var d Dashboard
	err := q.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM books),
		(SELECT coalesce(sum(copies), 0) FROM books),
		(SELECT count(*) FROM loans WHERE returned_at IS NULL),
		(SELECT count(*) FROM loans WHERE returned_at IS NULL AND due_at < ?),
		(SELECT count(*) FROM holds WHERE status = 'waiting'),
		(SELECT count(*) FROM users)`, fmtTime(now)).
		Scan(&d.Books, &d.Copies, &d.OpenLoans, &d.Overdue, &d.HoldsWaiting, &d.Members)
	if err != nil {
		return nil, err
	}
	return &d, nil
}
