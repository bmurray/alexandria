package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bmurray/alexandria/internal/db"
	"github.com/bmurray/alexandria/internal/handler"
	"github.com/bmurray/alexandria/internal/realtime"
)

// Test users inserted by newTestService.
const (
	alice = "user_alice"
	bob   = "user_bob"
	carol = "user_carol"
	libby = "user_libby" // librarian
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// fixture is a service on a fresh in-memory database with a movable clock.
type fixture struct {
	*Service
	db  *sql.DB
	now time.Time
}

func newTestService(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	d, err := db.OpenTest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	f := &fixture{db: d, now: t0}
	f.Service = NewService(d, nil, Options{LoanDays: 21, HoldPickupDays: 7})
	f.Service.clock = func() time.Time { return f.now }
	for _, u := range []struct{ id, name, role string }{
		{alice, "Alice", "member"}, {bob, "Bob", "member"}, {carol, "Carol", "member"}, {libby, "Libby", "librarian"},
	} {
		insertUser(t, d, u.id, u.name, u.role)
	}
	return f
}

// insertUser adds a user row directly; auth owns that table.
func insertUser(t *testing.T, d *sql.DB, id, name, role string) {
	t.Helper()
	_, err := d.ExecContext(context.Background(),
		`INSERT INTO users (id, email, name, role, created_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, id+"@example.com", name, role, fmtTime(t0), fmtTime(t0))
	if err != nil {
		t.Fatal(err)
	}
}

// advance moves the clock forward.
func (f *fixture) advance(d time.Duration) { f.now = f.now.Add(d) }

func (f *fixture) addBook(t *testing.T, title, author string, copies int, mod ...func(*BookInput)) *Book {
	t.Helper()
	in := BookInput{Title: title, Author: author, Copies: copies}
	for _, m := range mod {
		m(&in)
	}
	b, err := f.CreateBook(context.Background(), in, libby)
	if err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return b
}

func (f *fixture) checkout(t *testing.T, bookID, userID string) *Loan {
	t.Helper()
	l, err := f.Checkout(context.Background(), bookID, userID, userID)
	if err != nil {
		t.Fatalf("checkout %s for %s: %v", bookID, userID, err)
	}
	return l
}

func (f *fixture) hold(t *testing.T, bookID, userID string) *Hold {
	t.Helper()
	h, err := f.PlaceHold(context.Background(), bookID, userID)
	if err != nil {
		t.Fatalf("hold %s for %s: %v", bookID, userID, err)
	}
	return h
}

func (f *fixture) book(t *testing.T, id string) *Book {
	t.Helper()
	b, err := f.GetBook(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (f *fixture) holdByID(t *testing.T, id string) *Hold {
	t.Helper()
	h, err := f.GetHold(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// wantStatus asserts err is a StatusError with the given status and that its
// message contains contains.
func wantStatus(t *testing.T, err error, status int, contains string) {
	t.Helper()
	var se *handler.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("want status %d error, got %v", status, err)
	}
	if se.Status != status {
		t.Fatalf("want status %d, got %d (%s)", status, se.Status, se.Msg)
	}
	if !strings.Contains(se.Msg, contains) {
		t.Fatalf("want message containing %q, got %q", contains, se.Msg)
	}
}

func wantProblems(t *testing.T, err error, contains ...string) {
	t.Helper()
	var ve ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	for _, c := range contains {
		found := false
		for _, p := range ve.Problems {
			if strings.Contains(p, c) {
				found = true
			}
		}
		if !found {
			t.Fatalf("want problem containing %q in %v", c, ve.Problems)
		}
	}
}

func ptr[T any](v T) *T { return &v }

// ---- books --------------------------------------------------------------

func TestCreateBookNormalises(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	b, err := f.CreateBook(ctx, BookInput{
		Title: "  Dune ", Author: " Frank Herbert ", ISBN: "978-0-441-01359-3", Year: ptr(1965),
		Genre: "Science Fiction", Copies: 2, Tags: []string{" Space Opera", "ecology", "SPACE OPERA", ""},
		CoverURL: "https://covers.openlibrary.org/b/isbn/9780441013593-M.jpg",
	}, libby)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Dune" || b.Author != "Frank Herbert" {
		t.Errorf("not trimmed: %q %q", b.Title, b.Author)
	}
	if b.ISBN != "9780441013593" {
		t.Errorf("isbn = %q", b.ISBN)
	}
	if got := strings.Join(b.Tags, ","); got != "space opera,ecology" {
		t.Errorf("tags = %q", got)
	}
	if b.Year == nil || *b.Year != 1965 {
		t.Errorf("year = %v", b.Year)
	}
	if b.Copies != 2 || b.Available != 2 || b.OnLoan != 0 || b.HoldsWaiting != 0 {
		t.Errorf("availability: %+v", b)
	}
	if !b.CreatedAt.Equal(t0) || !b.UpdatedAt.Equal(t0) {
		t.Errorf("times: %v %v", b.CreatedAt, b.UpdatedAt)
	}
	if len(b.ID) != 20 {
		t.Errorf("id = %q", b.ID)
	}
	var createdBy sql.NullString
	if err := f.db.QueryRowContext(ctx, `SELECT created_by FROM books WHERE id = ?`, b.ID).Scan(&createdBy); err != nil {
		t.Fatal(err)
	}
	if createdBy.String != libby {
		t.Errorf("created_by = %v", createdBy)
	}

	// Tags are never nil in JSON, and the JSON shape matches the contract.
	plain := f.addBook(t, "Plain", "Nobody", 1)
	out, _ := json.Marshal(plain)
	for _, want := range []string{`"tags":[]`, `"year":null`, `"coverUrl":""`, `"available":1`, `"onLoan":0`, `"holdsWaiting":0`, `"createdAt":"2026-10-01T12:00:00Z"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("json missing %s: %s", want, out)
		}
	}
	if _, err := f.GetBook(ctx, "missing"); err == nil {
		t.Fatal("want 404")
	} else {
		wantStatus(t, err, http.StatusNotFound, "book not found")
	}
}

func TestValidation(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	_, err := f.CreateBook(ctx, BookInput{Title: "  ", Author: "", ISBN: "12-34", Year: ptr(2101), Copies: 0, CoverURL: "ftp://x"}, libby)
	wantProblems(t, err, "title is required", "author is required", "isbn", "year must be", "copies must be at least 1", "coverUrl")

	_, err = f.CreateBook(ctx, BookInput{Title: strings.Repeat("x", 301), Author: "a", Copies: 1001, Year: ptr(-1)}, libby)
	wantProblems(t, err, "title must be at most", "copies must be at most", "year must be")

	_, err = f.CreateBook(ctx, BookInput{Title: "t", Author: "a", Copies: 1, Tags: []string{strings.Repeat("y", 51)}}, libby)
	wantProblems(t, err, "longer than")

	// ISBN-10 with check X is fine; lowercase x is upper-cased.
	b, err := f.CreateBook(ctx, BookInput{Title: "t", Author: "a", Copies: 1, ISBN: "0-8044-2957-x"}, libby)
	if err != nil {
		t.Fatal(err)
	}
	if b.ISBN != "080442957X" {
		t.Errorf("isbn = %q", b.ISBN)
	}
	if ve := (ValidationError{Problems: []string{"a", "b"}}); ve.Error() != "validation failed: a; b" {
		t.Errorf("Error() = %q", ve.Error())
	}
}

func TestNormalizeISBN(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"", "", true},
		{"978-0-14-143951-8", "9780141439518", true},
		{"0 14 143951 8", "0141439518", true},
		{"080442957x", "080442957X", true},
		{"X804429570", "X804429570", false}, // X only allowed last
		{"12345", "12345", false},
		{"97801414395181", "97801414395181", false},
		{"978014143951A", "978014143951A", false},
	}
	for _, c := range cases {
		got, ok := NormalizeISBN(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizeISBN(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestUpdateBook(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	b := f.addBook(t, "Dune", "Frank Herbert", 2, func(in *BookInput) { in.Year = ptr(1965); in.Genre = "SF" })
	f.advance(time.Hour)

	// A patch without year leaves it alone; title changes.
	var p BookPatch
	if err := json.Unmarshal([]byte(`{"title":"Dune Messiah","tags":["Sequel"]}`), &p); err != nil {
		t.Fatal(err)
	}
	u, err := f.UpdateBook(ctx, b.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if u.Title != "Dune Messiah" || u.Year == nil || *u.Year != 1965 || u.Genre != "SF" || strings.Join(u.Tags, ",") != "sequel" {
		t.Errorf("patched = %+v", u)
	}
	if !u.UpdatedAt.After(u.CreatedAt) {
		t.Errorf("updated_at not bumped: %v %v", u.CreatedAt, u.UpdatedAt)
	}

	// Explicit null clears the year.
	p = BookPatch{}
	if err := json.Unmarshal([]byte(`{"year":null}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Year.Set || p.Year.Value != nil {
		t.Fatalf("OptionalInt from null = %+v", p.Year)
	}
	if u, err = f.UpdateBook(ctx, b.ID, p); err != nil {
		t.Fatal(err)
	}
	if u.Year != nil {
		t.Errorf("year not cleared: %v", *u.Year)
	}
	if u, err = f.UpdateBook(ctx, b.ID, BookPatch{Year: OptionalInt{Set: true, Value: ptr(1969)}}); err != nil {
		t.Fatal(err)
	}
	if u.Year == nil || *u.Year != 1969 {
		t.Errorf("year = %v", u.Year)
	}

	// Validation applies to the merged record.
	_, err = f.UpdateBook(ctx, b.ID, BookPatch{Title: ptr("   ")})
	wantProblems(t, err, "title is required")

	// Copies cannot drop below open loans.
	f.checkout(t, b.ID, alice)
	f.checkout(t, b.ID, bob)
	_, err = f.UpdateBook(ctx, b.ID, BookPatch{Copies: ptr(1)})
	wantProblems(t, err, "fewer than the 2 currently on loan")
	if u, err = f.UpdateBook(ctx, b.ID, BookPatch{Copies: ptr(3)}); err != nil {
		t.Fatal(err)
	}
	if u.Copies != 3 || u.Available != 1 || u.OnLoan != 2 {
		t.Errorf("after copies=3: %+v", u)
	}

	_, err = f.UpdateBook(ctx, "nope", BookPatch{})
	wantStatus(t, err, http.StatusNotFound, "book")
}

func TestUpdateBookMoreCopiesPromotesHold(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	b := f.addBook(t, "One", "A", 1)
	f.checkout(t, b.ID, alice)
	h := f.hold(t, b.ID, bob)
	if _, err := f.UpdateBook(ctx, b.ID, BookPatch{Copies: ptr(2)}); err != nil {
		t.Fatal(err)
	}
	if got := f.holdByID(t, h.ID); got.Status != HoldReady {
		t.Errorf("hold after adding a copy = %s", got.Status)
	}
}

func TestDeleteBook(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	b := f.addBook(t, "Gone", "Soon", 1)
	l := f.checkout(t, b.ID, alice)
	h := f.hold(t, b.ID, bob)

	err := f.DeleteBook(ctx, b.ID)
	wantStatus(t, err, http.StatusConflict, "open loan")

	if _, err := f.Return(ctx, l.ID, alice); err != nil {
		t.Fatal(err)
	}
	// Returned history and holds go with the book.
	if err := f.DeleteBook(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	_, err = f.GetBook(ctx, b.ID)
	wantStatus(t, err, http.StatusNotFound, "book")
	_, err = f.GetHold(ctx, h.ID)
	wantStatus(t, err, http.StatusNotFound, "hold")
	_, err = f.GetLoan(ctx, l.ID)
	wantStatus(t, err, http.StatusNotFound, "loan")
	if ul, _ := f.UserLoans(ctx, alice); len(ul) != 0 {
		t.Errorf("history survived: %+v", ul)
	}
	err = f.DeleteBook(ctx, "missing")
	wantStatus(t, err, http.StatusNotFound, "book")
	// Gone from the index too.
	if p, _ := f.ListBooks(ctx, Query{Text: "gone"}); p.Total != 0 {
		t.Errorf("deleted book still indexed")
	}
}

func TestGenres(t *testing.T) {
	f := newTestService(t)
	f.addBook(t, "a", "x", 1, func(in *BookInput) { in.Genre = "Science" })
	f.addBook(t, "b", "x", 1, func(in *BookInput) { in.Genre = "Fiction" })
	f.addBook(t, "c", "x", 1, func(in *BookInput) { in.Genre = "Fiction" })
	f.addBook(t, "d", "x", 1)
	g, err := f.Genres(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 2 || g[0] != (Genre{"Fiction", 2}) || g[1] != (Genre{"Science", 1}) {
		t.Errorf("genres = %+v", g)
	}
}

// ---- search --------------------------------------------------------------

func TestFTSQuery(t *testing.T) {
	cases := map[string]string{
		"dune":                       `"dune"*`,
		"  Frank   herb ":            `"Frank"* "herb"*`,
		`"drop table" OR (x) NEAR/2`: `"drop"* "table"* "OR"* "x"* ("NEAR2"* OR ("NEAR"* "2"*))`,
		"978-0-441-01359-3":          `("9780441013593"* OR ("978"* "0"* "441"* "01359"* "3"*))`,
		"Catch-22":                   `("Catch22"* OR ("Catch"* "22"*))`,
		"!!! ---":                    ``,
		"Brontë":                     `"Brontë"*`,
	}
	for in, want := range cases {
		if got := ftsQuery(in); got != strings.Join(strings.Fields(want), " ") {
			t.Errorf("ftsQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func seedSearch(t *testing.T, f *fixture) (dune, hobbit, fellowship, frank *Book) {
	t.Helper()
	dune = f.addBook(t, "Dune", "Frank Herbert", 2, func(in *BookInput) {
		in.ISBN = "978-0-441-01359-3"
		in.Genre = "Science Fiction"
		in.Tags = []string{"space opera", "ecology"}
		in.Year = ptr(1965)
		in.Description = "The desert planet Arrakis."
	})
	hobbit = f.addBook(t, "The Hobbit", "J. R. R. Tolkien", 1, func(in *BookInput) {
		in.Genre = "Fantasy"
		in.Tags = []string{"middle-earth"}
		in.Year = ptr(1937)
	})
	fellowship = f.addBook(t, "The Fellowship of the Ring", "J. R. R. Tolkien", 1, func(in *BookInput) {
		in.Genre = "Fantasy"
		in.Tags = []string{"middle-earth", "epic"}
		in.Year = ptr(1954)
	})
	frank = f.addBook(t, "Frankenstein", "Mary Shelley", 1, func(in *BookInput) {
		in.Genre = "Science Fiction"
		in.Tags = []string{"gothic"}
	})
	return
}

func titles(p Page[Book]) string {
	var ts []string
	for _, b := range p.Items {
		ts = append(ts, b.Title)
	}
	return strings.Join(ts, "|")
}

func TestSearch(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	_, hobbit, _, _ := seedSearch(t, f)

	cases := []struct {
		name string
		q    Query
		want string
	}{
		{"all by title", Query{}, "Dune|Frankenstein|The Fellowship of the Ring|The Hobbit"},
		{"title", Query{Text: "hobbit"}, "The Hobbit"},
		{"author", Query{Text: "tolkien"}, "The Hobbit|The Fellowship of the Ring"},
		{"author prefix", Query{Text: "herb"}, "Dune"},
		{"two terms AND", Query{Text: "tolkien fellowship"}, "The Fellowship of the Ring"},
		{"isbn with dashes", Query{Text: "978-0-441-01359-3"}, "Dune"},
		{"isbn prefix", Query{Text: "97804410"}, "Dune"},
		{"tag", Query{Text: "ecology"}, "Dune"},
		{"tag shared", Query{Text: "middle"}, "The Hobbit|The Fellowship of the Ring"},
		{"description", Query{Text: "arrakis"}, "Dune"},
		{"genre word", Query{Text: "fantasy"}, "The Hobbit|The Fellowship of the Ring"},
		{"no match", Query{Text: "zzzz"}, ""},
		{"punctuation only means no filter", Query{Text: "!!!"}, "Dune|Frankenstein|The Fellowship of the Ring|The Hobbit"},
		{"genre filter", Query{Genre: "science fiction"}, "Dune|Frankenstein"},
		{"genre + text", Query{Genre: "Fantasy", Text: "the"}, "The Fellowship of the Ring|The Hobbit"},
		{"sort author", Query{Sort: SortAuthor}, "Dune|The Fellowship of the Ring|The Hobbit|Frankenstein"},
		{"sort year newest first, unknown last", Query{Sort: SortYear}, "Dune|The Fellowship of the Ring|The Hobbit|Frankenstein"},
		{"page 1", Query{Limit: 2}, "Dune|Frankenstein"},
		{"page 2", Query{Limit: 2, Offset: 2}, "The Fellowship of the Ring|The Hobbit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := f.ListBooks(ctx, c.q)
			if err != nil {
				t.Fatal(err)
			}
			if got := titles(p); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}

	// Total counts the unpaged set.
	p, err := f.ListBooks(ctx, Query{Limit: 1, Text: "tolkien"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 2 || len(p.Items) != 1 {
		t.Errorf("total = %d items = %d", p.Total, len(p.Items))
	}

	// Sort by added puts the newest first.
	f.advance(time.Minute)
	f.addBook(t, "Newest", "Z", 1)
	p, _ = f.ListBooks(ctx, Query{Sort: SortAdded, Limit: 1})
	if titles(p) != "Newest" {
		t.Errorf("sort added = %q", titles(p))
	}

	// Available filter and availability on listed rows.
	f.checkout(t, hobbit.ID, alice)
	p, _ = f.ListBooks(ctx, Query{AvailableOnly: true, Text: "tolkien"})
	if titles(p) != "The Fellowship of the Ring" || p.Total != 1 {
		t.Errorf("available only = %q total %d", titles(p), p.Total)
	}
	p, _ = f.ListBooks(ctx, Query{Text: "hobbit"})
	if b := p.Items[0]; b.Available != 0 || b.OnLoan != 1 {
		t.Errorf("listed availability = %+v", b)
	}

	// Updates reach the index (title changes).
	if _, err := f.UpdateBook(ctx, hobbit.ID, BookPatch{Title: ptr("There and Back Again")}); err != nil {
		t.Fatal(err)
	}
	if p, _ = f.ListBooks(ctx, Query{Text: "hobbit"}); p.Total != 0 {
		t.Errorf("stale index: %q", titles(p))
	}
	if p, _ = f.ListBooks(ctx, Query{Text: "back again"}); titles(p) != "There and Back Again" {
		t.Errorf("new title not indexed: %q", titles(p))
	}

	// Limit is clamped.
	p, _ = f.ListBooks(ctx, Query{Limit: 1000})
	if len(p.Items) != 5 {
		t.Errorf("items = %d", len(p.Items))
	}
}

func TestSearchLikeFallback(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	seedSearch(t, f)
	p, err := listBooks(ctx, f.db, Query{Text: "tolk hobb", Limit: 10}, false)
	if err != nil {
		t.Fatal(err)
	}
	if titles(p) != "The Hobbit" || p.Total != 1 {
		t.Errorf("LIKE fallback = %q total %d", titles(p), p.Total)
	}
	p, err = listBooks(ctx, f.db, Query{Text: "9780441", Limit: 10, Genre: "science fiction"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if titles(p) != "Dune" {
		t.Errorf("LIKE isbn = %q", titles(p))
	}
}

// ---- loans ---------------------------------------------------------------

func TestCheckoutAndReturn(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	b := f.addBook(t, "Dune", "Frank Herbert", 2)

	l, err := f.Checkout(ctx, b.ID, alice, libby)
	if err != nil {
		t.Fatal(err)
	}
	if l.BookID != b.ID || l.UserID != alice || !l.CheckedOutAt.Equal(t0) || !l.DueAt.Equal(t0.AddDate(0, 0, 21)) {
		t.Errorf("loan = %+v", l)
	}
	if l.ReturnedAt != nil || l.Overdue {
		t.Errorf("new loan state: %+v", l)
	}
	if l.Book.Title != "Dune" || l.Book.ID != b.ID || l.User.Name != "Alice" || l.User.Email != "user_alice@example.com" {
		t.Errorf("refs = %+v %+v", l.Book, l.User)
	}
	var by string
	if err := f.db.QueryRowContext(ctx, `SELECT checked_out_by FROM loans WHERE id = ?`, l.ID).Scan(&by); err != nil || by != libby {
		t.Errorf("checked_out_by = %q %v", by, err)
	}
	if got := f.book(t, b.ID); got.Available != 1 || got.OnLoan != 1 {
		t.Errorf("after checkout: available %d onLoan %d", got.Available, got.OnLoan)
	}

	// Same member again: 409.
	_, err = f.Checkout(ctx, b.ID, alice, alice)
	wantStatus(t, err, http.StatusConflict, "already has this book")

	// Last copy goes to Bob; Carol gets 409.
	f.checkout(t, b.ID, bob)
	_, err = f.Checkout(ctx, b.ID, carol, carol)
	wantStatus(t, err, http.StatusConflict, "no copies available")
	if got := f.book(t, b.ID); got.Available != 0 {
		t.Errorf("available = %d", got.Available)
	}

	// Return by a librarian records who did it and frees a copy.
	f.advance(48 * time.Hour)
	r, err := f.Return(ctx, l.ID, libby)
	if err != nil {
		t.Fatal(err)
	}
	if r.ReturnedAt == nil || !r.ReturnedAt.Equal(f.now) || r.Overdue {
		t.Errorf("returned = %+v", r)
	}
	if err := f.db.QueryRowContext(ctx, `SELECT returned_by FROM loans WHERE id = ?`, l.ID).Scan(&by); err != nil || by != libby {
		t.Errorf("returned_by = %q %v", by, err)
	}
	if got := f.book(t, b.ID); got.Available != 1 {
		t.Errorf("available after return = %d", got.Available)
	}
	_, err = f.Return(ctx, l.ID, libby)
	wantStatus(t, err, http.StatusConflict, "already returned")
	_, err = f.Return(ctx, "nope", libby)
	wantStatus(t, err, http.StatusNotFound, "loan")

	// Unknown book / user.
	_, err = f.Checkout(ctx, "nope", alice, alice)
	wantStatus(t, err, http.StatusNotFound, "book")
	_, err = f.Checkout(ctx, b.ID, "ghost", "ghost")
	wantStatus(t, err, http.StatusNotFound, "user")
}

func TestOverdueAndLoanListing(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	a := f.addBook(t, "A", "x", 1)
	b := f.addBook(t, "B", "x", 1)
	c := f.addBook(t, "C", "x", 1)

	la := f.checkout(t, a.ID, alice) // will be overdue
	f.advance(time.Hour)
	lb := f.checkout(t, b.ID, alice) // returned
	f.advance(time.Hour)
	lc := f.checkout(t, c.ID, bob) // open, not overdue
	if _, err := f.Return(ctx, lb.ID, alice); err != nil {
		t.Fatal(err)
	}
	f.advance(21*24*time.Hour - time.Hour) // la (due t0+21d) is past due, lc (due t0+2h+21d) is not

	ids := func(ls []Loan) string {
		var s []string
		for _, l := range ls {
			s = append(s, l.Book.Title)
		}
		return strings.Join(s, "")
	}
	cases := []struct {
		f     LoanFilter
		want  string
		total int
	}{
		{LoanFilter{}, "CAB", 3},
		{LoanFilter{Status: LoanOpen}, "CA", 2},
		{LoanFilter{Status: LoanOverdue}, "A", 1},
		{LoanFilter{Status: LoanReturned}, "B", 1},
		{LoanFilter{UserID: alice}, "AB", 2},
		{LoanFilter{UserID: alice, Status: LoanOpen}, "A", 1},
		{LoanFilter{Limit: 1, Offset: 1}, "A", 3},
	}
	for _, c := range cases {
		p, err := f.ListLoans(ctx, c.f)
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(p.Items); got != c.want || p.Total != c.total {
			t.Errorf("ListLoans(%+v) = %q/%d want %q/%d", c.f, got, p.Total, c.want, c.total)
		}
	}
	if _, err := f.ListLoans(ctx, LoanFilter{Status: "bogus"}); err == nil {
		t.Error("bogus status accepted")
	}

	got, err := f.GetLoan(ctx, la.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Overdue {
		t.Error("la should be overdue")
	}
	if got, _ := f.GetLoan(ctx, lc.ID); got.Overdue {
		t.Error("lc should not be overdue")
	}

	// UserLoans: open first, then history, newest first.
	ul, err := f.UserLoans(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if ids(ul) != "AB" || !ul[0].Overdue {
		t.Errorf("UserLoans = %q", ids(ul))
	}
	bl, err := f.BookLoans(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 1 || bl[0].ID != la.ID {
		t.Errorf("BookLoans = %+v", bl)
	}
	if bl, _ = f.BookLoans(ctx, b.ID); len(bl) != 0 {
		t.Errorf("returned loan listed as open: %+v", bl)
	}
}

// ---- holds ---------------------------------------------------------------

func TestHoldRules(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	b := f.addBook(t, "Dune", "Frank Herbert", 1)

	_, err := f.PlaceHold(ctx, b.ID, bob)
	wantStatus(t, err, http.StatusConflict, "book is available; check it out instead")

	f.checkout(t, b.ID, alice)
	_, err = f.PlaceHold(ctx, b.ID, alice)
	wantStatus(t, err, http.StatusConflict, "already has this book on loan")

	h1 := f.hold(t, b.ID, bob)
	if h1.Status != HoldWaiting || h1.Position != 1 || h1.ReadyAt != nil || !h1.PlacedAt.Equal(t0) {
		t.Errorf("h1 = %+v", h1)
	}
	if h1.Book.Title != "Dune" || h1.User.Name != "Bob" {
		t.Errorf("refs = %+v %+v", h1.Book, h1.User)
	}
	_, err = f.PlaceHold(ctx, b.ID, bob)
	wantStatus(t, err, http.StatusConflict, "already has a hold")

	f.advance(time.Minute)
	h2 := f.hold(t, b.ID, carol)
	if h2.Position != 2 {
		t.Errorf("h2 position = %d", h2.Position)
	}
	if got := f.book(t, b.ID); got.HoldsWaiting != 2 {
		t.Errorf("holdsWaiting = %d", got.HoldsWaiting)
	}

	// Queue views.
	bh, _ := f.BookHolds(ctx, b.ID)
	if len(bh) != 2 || bh[0].ID != h1.ID || bh[1].ID != h2.ID || bh[1].Position != 2 {
		t.Errorf("BookHolds = %+v", bh)
	}
	uh, _ := f.UserHolds(ctx, carol)
	if len(uh) != 1 || uh[0].ID != h2.ID {
		t.Errorf("UserHolds = %+v", uh)
	}
	all, _ := f.ListHolds(ctx, "")
	if len(all) != 2 {
		t.Errorf("ListHolds = %d", len(all))
	}

	// Cancel the first: Carol moves up.
	if err := f.CancelHold(ctx, h1.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.holdByID(t, h1.ID); got.Status != HoldCancelled || got.Position != 0 {
		t.Errorf("cancelled = %+v", got)
	}
	if got := f.holdByID(t, h2.ID); got.Position != 1 {
		t.Errorf("after cancel position = %d", got.Position)
	}
	err = f.CancelHold(ctx, h1.ID)
	wantStatus(t, err, http.StatusConflict, "no longer active")
	err = f.CancelHold(ctx, "nope")
	wantStatus(t, err, http.StatusNotFound, "hold")
	_, err = f.PlaceHold(ctx, "nope", bob)
	wantStatus(t, err, http.StatusNotFound, "book")
	_, err = f.PlaceHold(ctx, b.ID, "ghost")
	wantStatus(t, err, http.StatusNotFound, "user")

	// Bob may hold again after cancelling (the partial unique index allows it).
	f.hold(t, b.ID, bob)
}

func TestReturnPromotesHoldAndCheckoutHonoursIt(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	b := f.addBook(t, "Dune", "Frank Herbert", 1)
	l := f.checkout(t, b.ID, alice)
	hb := f.hold(t, b.ID, bob)
	f.advance(time.Minute)
	hc := f.hold(t, b.ID, carol)

	f.advance(time.Hour)
	if _, err := f.Return(ctx, l.ID, alice); err != nil {
		t.Fatal(err)
	}
	got := f.holdByID(t, hb.ID)
	if got.Status != HoldReady || got.ReadyAt == nil || !got.ReadyAt.Equal(f.now) || got.Position != 1 {
		t.Errorf("bob's hold after return = %+v", got)
	}
	if got := f.holdByID(t, hc.ID); got.Status != HoldWaiting || got.Position != 2 {
		t.Errorf("carol's hold = %+v", got)
	}
	// The copy is physically available but reserved.
	book := f.book(t, b.ID)
	if book.Available != 1 || book.HoldsWaiting != 1 {
		t.Errorf("book = available %d waiting %d", book.Available, book.HoldsWaiting)
	}
	_, err := f.Checkout(ctx, b.ID, alice, alice)
	wantStatus(t, err, http.StatusConflict, "reserved")
	// Someone else can still queue up behind the ready hold.
	f.hold(t, b.ID, alice)

	// Bob checks out: his hold is fulfilled.
	lb := f.checkout(t, b.ID, bob)
	if got := f.holdByID(t, hb.ID); got.Status != HoldFulfilled || got.Position != 0 {
		t.Errorf("fulfilled = %+v", got)
	}
	d, _ := f.Dashboard(ctx)
	if len(d.ReadyHolds) != 0 {
		t.Errorf("ready holds = %+v", d.ReadyHolds)
	}

	// Cancelling a ready hold passes the copy on.
	if _, err := f.Return(ctx, lb.ID, bob); err != nil {
		t.Fatal(err)
	}
	if got := f.holdByID(t, hc.ID); got.Status != HoldReady {
		t.Errorf("carol should be ready: %+v", got)
	}
	if err := f.CancelHold(ctx, hc.ID); err != nil {
		t.Fatal(err)
	}
	ah, _ := f.UserHolds(ctx, alice)
	if len(ah) != 1 || ah[0].Status != HoldReady || ah[0].Position != 1 {
		t.Errorf("alice after carol cancels = %+v", ah)
	}
}

func TestMultipleCopiesPromoteOnePerReturn(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	b := f.addBook(t, "Two", "Copies", 2)
	la := f.checkout(t, b.ID, alice)
	lb := f.checkout(t, b.ID, bob)
	hc := f.hold(t, b.ID, carol)
	f.advance(time.Second)
	hl := f.hold(t, b.ID, libby)

	if _, err := f.Return(ctx, la.ID, alice); err != nil {
		t.Fatal(err)
	}
	if f.holdByID(t, hc.ID).Status != HoldReady || f.holdByID(t, hl.ID).Status != HoldWaiting {
		t.Error("exactly one hold should be ready after one return")
	}
	if _, err := f.Return(ctx, lb.ID, bob); err != nil {
		t.Fatal(err)
	}
	if f.holdByID(t, hl.ID).Status != HoldReady {
		t.Error("second return should ready the second hold")
	}
	// Both copies reserved: a third member can neither check out nor is told the book is available.
	_, err := f.Checkout(ctx, b.ID, alice, alice)
	wantStatus(t, err, http.StatusConflict, "reserved")
	f.hold(t, b.ID, alice)
}

func TestLapseHolds(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	b := f.addBook(t, "Dune", "Frank Herbert", 1)
	l := f.checkout(t, b.ID, alice)
	hb := f.hold(t, b.ID, bob)
	f.advance(time.Second)
	hc := f.hold(t, b.ID, carol)
	if _, err := f.Return(ctx, l.ID, alice); err != nil {
		t.Fatal(err)
	}
	readyAt := f.now

	// Nothing lapses before the pickup window closes.
	n, err := f.LapseHolds(ctx, readyAt.AddDate(0, 0, 7))
	if err != nil || n != 0 {
		t.Fatalf("early lapse = %d, %v", n, err)
	}
	if f.holdByID(t, hb.ID).Status != HoldReady {
		t.Error("bob lapsed too early")
	}

	// After HoldPickupDays, Bob lapses and Carol becomes ready.
	later := readyAt.AddDate(0, 0, 7).Add(time.Second)
	n, err = f.LapseHolds(ctx, later)
	if err != nil || n != 1 {
		t.Fatalf("lapse = %d, %v", n, err)
	}
	if got := f.holdByID(t, hb.ID); got.Status != HoldLapsed || got.Position != 0 {
		t.Errorf("bob = %+v", got)
	}
	if got := f.holdByID(t, hc.ID); got.Status != HoldReady || got.ReadyAt == nil || !got.ReadyAt.Equal(later.UTC()) || got.Position != 1 {
		t.Errorf("carol = %+v", got)
	}
	// Idempotent.
	if n, _ = f.LapseHolds(ctx, later); n != 0 {
		t.Errorf("second run lapsed %d", n)
	}
	// Carol lapses too; nobody is waiting, so the copy is simply free.
	if n, _ = f.LapseHolds(ctx, later.AddDate(0, 0, 8)); n != 1 {
		t.Errorf("carol lapse = %d", n)
	}
	f.checkout(t, b.ID, alice)
}

// ---- dashboard -----------------------------------------------------------

func TestDashboard(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	a := f.addBook(t, "A", "x", 1)
	b := f.addBook(t, "B", "x", 3)
	f.checkout(t, a.ID, alice)
	f.advance(time.Hour)
	f.checkout(t, b.ID, bob)
	f.hold(t, a.ID, carol)

	d, err := f.Dashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d.Books != 2 || d.Copies != 4 || d.OpenLoans != 2 || d.Overdue != 0 || d.HoldsWaiting != 1 || d.Members != 4 {
		t.Errorf("counts = %+v", d)
	}
	if len(d.RecentLoans) != 2 || d.RecentLoans[0].Book.Title != "B" {
		t.Errorf("recent = %+v", d.RecentLoans)
	}
	if len(d.OverdueLoans) != 0 || len(d.ReadyHolds) != 0 {
		t.Errorf("overdue/ready = %+v %+v", d.OverdueLoans, d.ReadyHolds)
	}

	f.advance(30 * 24 * time.Hour)
	d, _ = f.Dashboard(ctx)
	if d.Overdue != 2 || len(d.OverdueLoans) != 2 || !d.OverdueLoans[0].Overdue || d.OverdueLoans[0].Book.Title != "A" {
		t.Errorf("overdue = %d %+v", d.Overdue, d.OverdueLoans)
	}
}

func TestDashboardHoldCounts(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	a := f.addBook(t, "A", "x", 1)
	l := f.checkout(t, a.ID, alice)
	f.hold(t, a.ID, bob)
	f.advance(time.Second)
	f.hold(t, a.ID, carol)
	d, _ := f.Dashboard(ctx)
	if d.HoldsWaiting != 2 {
		t.Errorf("holdsWaiting = %d", d.HoldsWaiting)
	}
	if _, err := f.Return(ctx, l.ID, alice); err != nil {
		t.Fatal(err)
	}
	d, _ = f.Dashboard(ctx)
	if d.HoldsWaiting != 1 || len(d.ReadyHolds) != 1 || d.ReadyHolds[0].UserID != bob {
		t.Errorf("after return: waiting %d ready %+v", d.HoldsWaiting, d.ReadyHolds)
	}
}

// ---- seed ----------------------------------------------------------------

func TestSeed(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	seed, err := SeedBooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) < 40 {
		t.Fatalf("only %d seed books", len(seed))
	}
	for _, b := range seed {
		if b.CoverURL != "" && !strings.Contains(b.CoverURL, b.ISBN) {
			t.Errorf("%q cover does not use its isbn", b.Title)
		}
		if b.Description == "" || len(b.Tags) == 0 || b.Copies < 1 || b.Copies > 3 {
			t.Errorf("%q is thin: %+v", b.Title, b)
		}
	}

	// Off by default.
	if err := f.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := countBooks(ctx, f.db); n != 0 {
		t.Fatalf("seeded without SeedOnEmpty: %d", n)
	}

	f.opts.SeedOnEmpty = true
	if err := f.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := countBooks(ctx, f.db); n != len(seed) {
		t.Fatalf("seeded %d want %d", n, len(seed))
	}
	// Idempotent.
	if err := f.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := countBooks(ctx, f.db); n != len(seed) {
		t.Fatalf("second start changed count to %d", n)
	}
	// Not re-seeded after the catalog shrinks to non-empty either.
	p, _ := f.ListBooks(ctx, Query{Text: "dune"})
	if p.Total != 1 {
		t.Fatalf("dune in seed = %d", p.Total)
	}
	g, _ := f.Genres(ctx)
	if len(g) < 8 {
		t.Errorf("seed genres = %+v", g)
	}
}

// ---- realtime --------------------------------------------------------------

func TestPublishesChanges(t *testing.T) {
	f := newTestService(t)
	ctx := context.Background()
	hub := &realtime.Hub{}
	f.hub = hub
	msgs, cancel := hub.Subscribe(Topic)
	defer cancel()

	b := f.addBook(t, "Live", "Wire", 1)
	l := f.checkout(t, b.ID, alice)
	h := f.hold(t, b.ID, bob)
	if _, err := f.UpdateBook(ctx, b.ID, BookPatch{Genre: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Return(ctx, l.ID, alice); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.LapseHolds(ctx, f.now.AddDate(0, 0, 8)); n != 1 {
		t.Fatalf("lapsed %d", n)
	}
	_ = h
	if err := f.DeleteBook(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	want := []string{"created", "checkout", "hold", "updated", "return", "hold", "deleted"}
	for _, action := range want {
		select {
		case m := <-msgs:
			var ev struct{ Action, ID string }
			if err := json.Unmarshal(m.Payload, &ev); err != nil {
				t.Fatal(err)
			}
			if m.Topic != Topic || ev.Action != action || ev.ID != b.ID {
				t.Errorf("event = %s %s/%s want %s", m.Topic, ev.Action, ev.ID, action)
			}
		default:
			t.Fatalf("missing event %q", action)
		}
	}
	select {
	case m := <-msgs:
		t.Errorf("unexpected event %s", m.Payload)
	default:
	}
}

func TestOptionalIntJSON(t *testing.T) {
	var p struct {
		Y OptionalInt `json:"y"`
	}
	for in, want := range map[string]string{`{}`: "unset", `{"y":null}`: "null", `{"y":7}`: "7"} {
		p.Y = OptionalInt{}
		if err := json.Unmarshal([]byte(in), &p); err != nil {
			t.Fatal(err)
		}
		got := "unset"
		if p.Y.Set {
			got = "null"
			if p.Y.Value != nil {
				got = "7"
			}
		}
		if got != want {
			t.Errorf("%s -> %s want %s", in, got, want)
		}
	}
	if err := json.Unmarshal([]byte(`{"y":"x"}`), &p); err == nil {
		t.Error("string accepted as year")
	}
	p.Y = OptionalInt{Set: true, Value: ptr(7)}
	out, _ := json.Marshal(p)
	if string(out) != `{"y":7}` {
		t.Errorf("marshal = %s", out)
	}
}
