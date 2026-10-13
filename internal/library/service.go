package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bmurray/alexandria/internal/handler"
	"github.com/bmurray/alexandria/internal/id"
	"github.com/bmurray/alexandria/internal/realtime"
)

// Defaults used when Options leave a value unset.
const (
	DefaultLoanDays       = 21
	DefaultHoldPickupDays = 7
	DefaultPageSize       = 24
	MaxPageSize           = 100
	MaxLoanPageSize       = 200
)

// Topic is the realtime topic catalog changes are published on.
const Topic = "books"

// Service holds the library rules. It is safe for concurrent use.
type Service struct {
	db   *sql.DB
	hub  *realtime.Hub // may be nil (tests)
	opts Options
	// clock is swapped in tests to move time.
	clock func() time.Time
}

// NewService wires the domain to its database and change broadcaster.
func NewService(db *sql.DB, hub *realtime.Hub, opts Options) *Service {
	if opts.LoanDays <= 0 {
		opts.LoanDays = DefaultLoanDays
	}
	if opts.HoldPickupDays <= 0 {
		opts.HoldPickupDays = DefaultHoldPickupDays
	}
	return &Service{db: db, hub: hub, opts: opts, clock: time.Now}
}

// Options returns the effective settings.
func (s *Service) Options() Options { return s.opts }

// Start runs startup work: seeding the catalog when SeedOnEmpty is set and
// there are no books. Call once at startup.
func (s *Service) Start(ctx context.Context) error {
	if !s.opts.SeedOnEmpty {
		return nil
	}
	n, err := s.seedIfEmpty(ctx)
	if err != nil {
		return fmt.Errorf("seed catalog: %w", err)
	}
	if n > 0 {
		slog.InfoContext(ctx, "seeded catalog", slog.Int("books", n))
	}
	return nil
}

// now is the current time at the precision the store keeps.
func (s *Service) now() time.Time { return s.clock().UTC().Truncate(time.Millisecond) }

// publish announces a catalog change on the books topic.
func (s *Service) publish(ctx context.Context, action, bookID string) {
	if s.hub == nil {
		return
	}
	s.hub.Publish(ctx, Topic, map[string]string{"action": action, "id": bookID})
}

// inTx runs fn inside a transaction, committing when it returns nil.
func (s *Service) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func notFound(what string) error {
	return handler.NewStatusError(http.StatusNotFound, what+" not found")
}
func conflict(msg string) error { return handler.NewStatusError(http.StatusConflict, msg) }

// mapNoRows turns sql.ErrNoRows into a 404 for what.
func mapNoRows(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return notFound(what)
	}
	return err
}

// ---- books --------------------------------------------------------------

// ListBooks searches and pages the catalog. Text search goes through FTS5 and
// falls back to LIKE if the FTS engine rejects the query.
func (s *Service) ListBooks(ctx context.Context, q Query) (Page[Book], error) {
	q.Text = strings.TrimSpace(q.Text)
	q.Genre = strings.TrimSpace(q.Genre)
	if q.Limit <= 0 {
		q.Limit = DefaultPageSize
	}
	q.Limit = min(q.Limit, MaxPageSize)
	q.Offset = max(q.Offset, 0)

	page, err := listBooks(ctx, s.db, q, true)
	if err != nil && q.Text != "" {
		slog.WarnContext(ctx, "fts search failed; using LIKE", slog.String("q", q.Text), slog.String("error", err.Error()))
		page, err = listBooks(ctx, s.db, q, false)
	}
	return page, err
}

// GetBook returns one book with availability.
func (s *Service) GetBook(ctx context.Context, bookID string) (*Book, error) {
	b, err := getBook(ctx, s.db, bookID)
	return b, mapNoRows(err, "book")
}

// CreateBook validates and stores a new book.
func (s *Service) CreateBook(ctx context.Context, in BookInput, createdBy string) (*Book, error) {
	if err := normalizeInput(&in); err != nil {
		return nil, err
	}
	now := s.now()
	b := &Book{
		ID: id.New(), Title: in.Title, Author: in.Author, ISBN: in.ISBN, Year: in.Year, Genre: in.Genre,
		Publisher: in.Publisher, Description: in.Description, CoverURL: in.CoverURL, Copies: in.Copies, Tags: in.Tags,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := insertBook(ctx, s.db, b, createdBy); err != nil {
		return nil, err
	}
	s.publish(ctx, "created", b.ID)
	return getBook(ctx, s.db, b.ID)
}

// UpdateBook applies the present fields of p and validates the result.
// Copies may not drop below the number currently on loan.
func (s *Service) UpdateBook(ctx context.Context, bookID string, p BookPatch) (*Book, error) {
	var out *Book
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		b, err := getBook(ctx, tx, bookID)
		if err != nil {
			return mapNoRows(err, "book")
		}
		in := BookInput{
			Title: b.Title, Author: b.Author, ISBN: b.ISBN, Year: b.Year, Genre: b.Genre, Publisher: b.Publisher,
			Description: b.Description, CoverURL: b.CoverURL, Copies: b.Copies, Tags: b.Tags,
		}
		applyPatch(&in, p)
		if err := normalizeInput(&in); err != nil {
			return err
		}
		if in.Copies < b.OnLoan {
			return ValidationError{Problems: []string{fmt.Sprintf("copies cannot be fewer than the %d currently on loan", b.OnLoan)}}
		}
		b.Title, b.Author, b.ISBN, b.Year, b.Genre = in.Title, in.Author, in.ISBN, in.Year, in.Genre
		b.Publisher, b.Description, b.CoverURL, b.Copies, b.Tags = in.Publisher, in.Description, in.CoverURL, in.Copies, in.Tags
		b.UpdatedAt = s.now()
		if err := updateBook(ctx, tx, b); err != nil {
			return err
		}
		// More copies may free a waiting hold.
		if _, err := promoteNextHold(ctx, tx, bookID, b.UpdatedAt); err != nil {
			return err
		}
		out, err = getBook(ctx, tx, bookID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, "updated", bookID)
	return out, nil
}

func applyPatch(in *BookInput, p BookPatch) {
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	set(&in.Title, p.Title)
	set(&in.Author, p.Author)
	set(&in.ISBN, p.ISBN)
	set(&in.Genre, p.Genre)
	set(&in.Publisher, p.Publisher)
	set(&in.Description, p.Description)
	set(&in.CoverURL, p.CoverURL)
	if p.Year.Set {
		in.Year = p.Year.Value
	}
	if p.Copies != nil {
		in.Copies = *p.Copies
	}
	if p.Tags != nil {
		in.Tags = *p.Tags
	}
}

// DeleteBook removes a book; 409 while it has open loans. Its holds and its
// returned-loan history go with it (loans.book_id is ON DELETE RESTRICT, so
// the history is removed explicitly in the same transaction).
func (s *Service) DeleteBook(ctx context.Context, bookID string) error {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		open, err := countOpenLoans(ctx, tx, bookID)
		if err != nil {
			return err
		}
		if open > 0 {
			return conflict(fmt.Sprintf("book has %d open loan(s); return them first", open))
		}
		if err := deleteReturnedLoans(ctx, tx, bookID); err != nil {
			return err
		}
		ok, err := deleteBook(ctx, tx, bookID)
		if err != nil {
			return err
		}
		if !ok {
			return notFound("book")
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, "deleted", bookID)
	return nil
}

// Genres lists genres in use with counts.
func (s *Service) Genres(ctx context.Context) ([]Genre, error) { return listGenres(ctx, s.db) }

// ---- validation ----------------------------------------------------------

// Field limits.
const (
	maxTitle       = 300
	maxAuthor      = 200
	maxGenre       = 100
	maxPublisher   = 200
	maxDescription = 5000
	maxURL         = 2000
	maxCopies      = 1000
	maxTags        = 20
	maxTagLen      = 50
	maxYear        = 2100
)

// normalizeInput trims and canonicalises in, returning a ValidationError that
// lists every problem rather than just the first.
func normalizeInput(in *BookInput) error {
	var problems []string
	in.Title = strings.TrimSpace(in.Title)
	in.Author = strings.TrimSpace(in.Author)
	in.Genre = strings.TrimSpace(in.Genre)
	in.Publisher = strings.TrimSpace(in.Publisher)
	in.Description = strings.TrimSpace(in.Description)
	in.CoverURL = strings.TrimSpace(in.CoverURL)

	if in.Title == "" {
		problems = append(problems, "title is required")
	} else if utf8.RuneCountInString(in.Title) > maxTitle {
		problems = append(problems, fmt.Sprintf("title must be at most %d characters", maxTitle))
	}
	if in.Author == "" {
		problems = append(problems, "author is required")
	} else if utf8.RuneCountInString(in.Author) > maxAuthor {
		problems = append(problems, fmt.Sprintf("author must be at most %d characters", maxAuthor))
	}
	isbn, ok := NormalizeISBN(in.ISBN)
	if !ok {
		problems = append(problems, "isbn must be 10 or 13 digits (X allowed as the last character of an ISBN-10)")
	}
	in.ISBN = isbn
	if in.Year != nil && (*in.Year < 0 || *in.Year > maxYear) {
		problems = append(problems, fmt.Sprintf("year must be between 0 and %d", maxYear))
	}
	if utf8.RuneCountInString(in.Genre) > maxGenre {
		problems = append(problems, fmt.Sprintf("genre must be at most %d characters", maxGenre))
	}
	if utf8.RuneCountInString(in.Publisher) > maxPublisher {
		problems = append(problems, fmt.Sprintf("publisher must be at most %d characters", maxPublisher))
	}
	if utf8.RuneCountInString(in.Description) > maxDescription {
		problems = append(problems, fmt.Sprintf("description must be at most %d characters", maxDescription))
	}
	if in.CoverURL != "" {
		u, err := url.Parse(in.CoverURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(in.CoverURL) > maxURL {
			problems = append(problems, "coverUrl must be an http(s) URL")
		}
	}
	if in.Copies < 1 {
		problems = append(problems, "copies must be at least 1")
	} else if in.Copies > maxCopies {
		problems = append(problems, fmt.Sprintf("copies must be at most %d", maxCopies))
	}
	in.Tags = NormalizeTags(in.Tags)
	if len(in.Tags) > maxTags {
		problems = append(problems, fmt.Sprintf("at most %d tags", maxTags))
	}
	for _, t := range in.Tags {
		if utf8.RuneCountInString(t) > maxTagLen {
			problems = append(problems, fmt.Sprintf("tag %q is longer than %d characters", t, maxTagLen))
			break
		}
	}
	if len(problems) > 0 {
		return ValidationError{Problems: problems}
	}
	return nil
}

// NormalizeISBN strips dashes and spaces and upper-cases a trailing x. It
// accepts the empty string, 10 characters (digits with an optional final X)
// or 13 digits; ok is false for anything else.
func NormalizeISBN(s string) (isbn string, ok bool) {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
	if s == "" {
		return "", true
	}
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == 'X' && i == 9 && len(s) == 10:
		default:
			return s, false
		}
	}
	return s, len(s) == 10 || len(s) == 13
}

// NormalizeTags trims, lower-cases, drops empties and de-duplicates while
// keeping first-seen order. The result is never nil.
func NormalizeTags(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || slices.Contains(out, t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ---- loans ---------------------------------------------------------------

// Checkout lends a copy of bookID to userID. actorID is who performed it
// (a librarian may check out for another member). It fails with 409 when no
// copy is free, when the member already has the book, or when the remaining
// copies are reserved by ready holds for other members. A ready hold for this
// member is fulfilled by the checkout.
func (s *Service) Checkout(ctx context.Context, bookID, userID, actorID string) (*Loan, error) {
	var out *Loan
	now := s.now()
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		b, err := getBook(ctx, tx, bookID)
		if err != nil {
			return mapNoRows(err, "book")
		}
		if ok, err := userExists(ctx, tx, userID); err != nil {
			return err
		} else if !ok {
			return notFound("user")
		}
		if has, err := userHasOpenLoan(ctx, tx, bookID, userID); err != nil {
			return err
		} else if has {
			return conflict("this member already has this book on loan")
		}
		if b.Available <= 0 {
			return conflict("no copies available")
		}
		ready, err := readyHolds(ctx, tx, bookID)
		if err != nil {
			return err
		}
		mine := ""
		others := 0
		for _, h := range ready {
			if h.UserID == userID {
				mine = h.ID
			} else {
				others++
			}
		}
		if mine == "" && b.Available-others <= 0 {
			return conflict("the remaining copies are reserved for members with holds")
		}
		l := &Loan{ID: id.New(), BookID: bookID, UserID: userID, CheckedOutAt: now, DueAt: now.AddDate(0, 0, s.opts.LoanDays)}
		if err := insertLoan(ctx, tx, l, actorID); err != nil {
			return err
		}
		if mine != "" {
			if err := setHoldStatus(ctx, tx, mine, HoldFulfilled); err != nil {
				return err
			}
		}
		out, err = getLoan(ctx, tx, now, l.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, "checkout", bookID)
	return out, nil
}

// Return closes a loan and, when a hold is waiting, reserves the freed copy
// for the member at the head of the queue.
func (s *Service) Return(ctx context.Context, loanID, actorID string) (*Loan, error) {
	var out *Loan
	now := s.now()
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		l, err := getLoan(ctx, tx, now, loanID)
		if err != nil {
			return mapNoRows(err, "loan")
		}
		if l.ReturnedAt != nil {
			return conflict("loan already returned")
		}
		if err := markReturned(ctx, tx, loanID, now, actorID); err != nil {
			return err
		}
		if _, err := promoteNextHold(ctx, tx, l.BookID, now); err != nil {
			return err
		}
		out, err = getLoan(ctx, tx, now, loanID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, "return", out.BookID)
	return out, nil
}

// ListLoans pages loans matching f (open first, newest first).
func (s *Service) ListLoans(ctx context.Context, f LoanFilter) (Page[Loan], error) {
	switch f.Status {
	case "", LoanOpen, LoanOverdue, LoanReturned:
	default:
		return Page[Loan]{}, ValidationError{Problems: []string{"status must be open, overdue or returned"}}
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	f.Limit = min(f.Limit, MaxLoanPageSize)
	f.Offset = max(f.Offset, 0)
	return listLoans(ctx, s.db, s.now(), f)
}

// GetLoan returns one loan.
func (s *Service) GetLoan(ctx context.Context, loanID string) (*Loan, error) {
	l, err := getLoan(ctx, s.db, s.now(), loanID)
	return l, mapNoRows(err, "loan")
}

// UserLoans is a member's open loans followed by their history, newest first.
func (s *Service) UserLoans(ctx context.Context, userID string) ([]Loan, error) {
	return queryLoans(ctx, s.db, s.now(),
		`WHERE l.user_id = ? ORDER BY (l.returned_at IS NULL) DESC, l.checked_out_at DESC, l.id`, userID)
}

// BookLoans is the open loans on a book, oldest due first.
func (s *Service) BookLoans(ctx context.Context, bookID string) ([]Loan, error) {
	return queryLoans(ctx, s.db, s.now(),
		`WHERE l.book_id = ? AND l.returned_at IS NULL ORDER BY l.due_at, l.id`, bookID)
}

// ---- holds ---------------------------------------------------------------

// PlaceHold queues userID for bookID. It fails with 409 when a copy could be
// checked out right now, when the member already holds or has the book.
func (s *Service) PlaceHold(ctx context.Context, bookID, userID string) (*Hold, error) {
	var out *Hold
	now := s.now()
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		b, err := getBook(ctx, tx, bookID)
		if err != nil {
			return mapNoRows(err, "book")
		}
		if ok, err := userExists(ctx, tx, userID); err != nil {
			return err
		} else if !ok {
			return notFound("user")
		}
		if has, err := userHasActiveHold(ctx, tx, bookID, userID); err != nil {
			return err
		} else if has {
			return conflict("this member already has a hold on this book")
		}
		if has, err := userHasOpenLoan(ctx, tx, bookID, userID); err != nil {
			return err
		} else if has {
			return conflict("this member already has this book on loan")
		}
		ready, err := readyHolds(ctx, tx, bookID)
		if err != nil {
			return err
		}
		if b.Available-len(ready) > 0 {
			return conflict("book is available; check it out instead")
		}
		h := &Hold{ID: id.New(), BookID: bookID, UserID: userID, PlacedAt: now, Status: HoldWaiting}
		if err := insertHold(ctx, tx, h); err != nil {
			return err
		}
		out, err = getHold(ctx, tx, h.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, "hold", bookID)
	return out, nil
}

// CancelHold withdraws an active hold. Cancelling a ready hold passes the
// reserved copy to the next member waiting.
func (s *Service) CancelHold(ctx context.Context, holdID string) error {
	var bookID string
	now := s.now()
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		h, err := getHold(ctx, tx, holdID)
		if err != nil {
			return mapNoRows(err, "hold")
		}
		if !h.Active() {
			return conflict("hold is no longer active")
		}
		bookID = h.BookID
		if err := setHoldStatus(ctx, tx, holdID, HoldCancelled); err != nil {
			return err
		}
		if h.Status == HoldReady {
			if _, err := promoteNextHold(ctx, tx, h.BookID, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, "hold", bookID)
	return nil
}

// GetHold returns one hold.
func (s *Service) GetHold(ctx context.Context, holdID string) (*Hold, error) {
	h, err := getHold(ctx, s.db, holdID)
	return h, mapNoRows(err, "hold")
}

// ListHolds is every active hold, optionally for one book, in queue order.
func (s *Service) ListHolds(ctx context.Context, bookID string) ([]Hold, error) {
	if bookID != "" {
		return s.BookHolds(ctx, bookID)
	}
	return queryHolds(ctx, s.db, `WHERE h.status IN ('waiting','ready') ORDER BY h.book_id, h.placed_at, h.id`)
}

// UserHolds is a member's active holds, oldest first.
func (s *Service) UserHolds(ctx context.Context, userID string) ([]Hold, error) {
	return queryHolds(ctx, s.db, `WHERE h.user_id = ? AND h.status IN ('waiting','ready') ORDER BY h.placed_at, h.id`, userID)
}

// BookHolds is a book's active queue in position order.
func (s *Service) BookHolds(ctx context.Context, bookID string) ([]Hold, error) {
	return queryHolds(ctx, s.db, `WHERE h.book_id = ? AND h.status IN ('waiting','ready') ORDER BY h.placed_at, h.id`, bookID)
}

// LapseHolds expires ready holds that were not picked up within
// HoldPickupDays of becoming ready and passes each freed copy to the next
// member waiting. It returns how many holds lapsed. Run it periodically.
func (s *Service) LapseHolds(ctx context.Context, now time.Time) (int, error) {
	now = now.UTC().Truncate(time.Millisecond)
	cutoff := now.AddDate(0, 0, -s.opts.HoldPickupDays)
	var books []string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		books, err = lapseReadyHolds(ctx, tx, cutoff)
		if err != nil {
			return err
		}
		for _, bookID := range books {
			if _, err := promoteNextHold(ctx, tx, bookID, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, bookID := range slices.Compact(slices.Sorted(slices.Values(books))) {
		s.publish(ctx, "hold", bookID)
	}
	return len(books), nil
}

// ---- dashboard -----------------------------------------------------------

// Dashboard summarises the library for librarians: counts, the ten most
// recent loans, every overdue loan and every hold waiting for pickup.
func (s *Service) Dashboard(ctx context.Context) (*Dashboard, error) {
	now := s.now()
	d, err := dashboardCounts(ctx, s.db, now)
	if err != nil {
		return nil, err
	}
	if d.RecentLoans, err = queryLoans(ctx, s.db, now, `ORDER BY l.checked_out_at DESC, l.id LIMIT 10`); err != nil {
		return nil, err
	}
	if d.OverdueLoans, err = queryLoans(ctx, s.db, now, `WHERE l.returned_at IS NULL AND l.due_at < ? ORDER BY l.due_at, l.id`, fmtTime(now)); err != nil {
		return nil, err
	}
	if d.ReadyHolds, err = queryHolds(ctx, s.db, `WHERE h.status = 'ready' ORDER BY h.ready_at, h.id`); err != nil {
		return nil, err
	}
	return d, nil
}
