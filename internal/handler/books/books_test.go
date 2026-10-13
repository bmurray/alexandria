package books

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/bmurray/alexandria/internal/auth"
	"github.com/bmurray/alexandria/internal/db"
	"github.com/bmurray/alexandria/internal/library"
	"github.com/bmurray/alexandria/internal/middleware"
	"github.com/bmurray/alexandria/internal/realtime"
)

var (
	alice = &auth.User{ID: "u_alice", Email: "alice@example.com", Name: "Alice", Role: auth.RoleMember}
	bob   = &auth.User{ID: "u_bob", Email: "bob@example.com", Name: "Bob", Role: auth.RoleMember}
	libby = &auth.User{ID: "u_libby", Email: "libby@example.com", Name: "Libby", Role: auth.RoleLibrarian}
	admin = &auth.User{ID: "u_admin", Email: "admin@example.com", Name: "Admin", Role: auth.RoleAdmin}
)

type env struct {
	mux *http.ServeMux
	svc *library.Service
	hub *realtime.Hub
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	d, err := db.OpenTest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, u := range []*auth.User{alice, bob, libby, admin} {
		insertUser(t, d, u)
	}
	hub := &realtime.Hub{}
	svc := library.NewService(d, hub, library.Options{LoanDays: 21, HoldPickupDays: 7})
	mux := http.NewServeMux()
	New(svc, hub).Register(mux)
	return &env{mux: mux, svc: svc, hub: hub}
}

func insertUser(t *testing.T, d *sql.DB, u *auth.User) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO users (id, email, name, role, created_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.Name, string(u.Role), now, now); err != nil {
		t.Fatal(err)
	}
}

// do performs a request as user (nil = anonymous) and decodes the JSON body
// into a generic map (nil for 204).
func (e *env) do(t *testing.T, user *auth.User, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		var buf bytes.Buffer
		switch b := body.(type) {
		case string:
			buf.WriteString(b)
		default:
			_ = json.NewEncoder(&buf).Encode(b)
		}
		rd = &buf
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != nil {
		req = req.WithContext(middleware.WithUser(req.Context(), user))
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: bad JSON %q", method, path, rec.Body.String())
		}
	}
	return rec.Code, out
}

func (e *env) want(t *testing.T, user *auth.User, method, path string, body any, status int) map[string]any {
	t.Helper()
	code, out := e.do(t, user, method, path, body)
	if code != status {
		t.Fatalf("%s %s as %v: status %d want %d: %v", method, path, userName(user), code, status, out)
	}
	return out
}

func userName(u *auth.User) string {
	if u == nil {
		return "anonymous"
	}
	return u.Name
}

func (e *env) addBook(t *testing.T, title string, copies int) string {
	t.Helper()
	out := e.want(t, libby, "POST", "/api/books", map[string]any{"title": title, "author": "Author", "copies": copies}, http.StatusCreated)
	return out["book"].(map[string]any)["id"].(string)
}

func sub(out map[string]any, key string) map[string]any {
	m, _ := out[key].(map[string]any)
	return m
}

func list(out map[string]any, key string) []any {
	l, _ := out[key].([]any)
	return l
}

// ---- books --------------------------------------------------------------

func TestBookCRUDAndRoles(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"title": " Dune ", "author": "Frank Herbert", "copies": 2, "isbn": "978-0-441-01359-3", "tags": []string{"SF"}}

	e.want(t, nil, "POST", "/api/books", body, http.StatusUnauthorized)
	e.want(t, alice, "POST", "/api/books", body, http.StatusForbidden)
	out := e.want(t, libby, "POST", "/api/books", body, http.StatusCreated)
	book := sub(out, "book")
	if book["title"] != "Dune" || book["isbn"] != "9780441013593" || book["available"] != float64(2) {
		t.Errorf("created = %v", book)
	}
	id := book["id"].(string)

	// Validation problems and malformed bodies.
	out = e.want(t, libby, "POST", "/api/books", map[string]any{"title": "", "copies": 0}, http.StatusBadRequest)
	if out["error"] != "validation failed" || len(list(out, "problems")) < 2 {
		t.Errorf("validation = %v", out)
	}
	out = e.want(t, libby, "POST", "/api/books", `{"title":"x","author":"y","copies":1,"bogus":1}`, http.StatusBadRequest)
	if out["error"] != "invalid JSON body" {
		t.Errorf("unknown field = %v", out)
	}
	e.want(t, libby, "POST", "/api/books", `{not json`, http.StatusBadRequest)

	// Public reads.
	out = e.want(t, nil, "GET", "/api/books?q=herb", nil, http.StatusOK)
	if out["total"] != float64(1) || len(list(out, "books")) != 1 {
		t.Errorf("list = %v", out)
	}
	out = e.want(t, nil, "GET", "/api/books/"+id, nil, http.StatusOK)
	if sub(out, "book")["id"] != id || len(list(out, "loans")) != 0 || len(list(out, "holds")) != 0 {
		t.Errorf("get = %v", out)
	}
	e.want(t, nil, "GET", "/api/books/nope", nil, http.StatusNotFound)
	out = e.want(t, nil, "GET", "/api/genres", nil, http.StatusOK)
	if _, ok := out["genres"].([]any); !ok {
		t.Errorf("genres = %v", out)
	}

	// Patch: admin counts as librarian; year null clears.
	e.want(t, alice, "PATCH", "/api/books/"+id, map[string]any{"year": 1965}, http.StatusForbidden)
	out = e.want(t, admin, "PATCH", "/api/books/"+id, map[string]any{"year": 1965, "genre": "SF"}, http.StatusOK)
	if sub(out, "book")["year"] != float64(1965) {
		t.Errorf("patched = %v", out)
	}
	out = e.want(t, libby, "PATCH", "/api/books/"+id, `{"year":null}`, http.StatusOK)
	if sub(out, "book")["year"] != nil || sub(out, "book")["genre"] != "SF" {
		t.Errorf("year clear = %v", out)
	}
	e.want(t, libby, "PATCH", "/api/books/"+id, map[string]any{"copies": 0}, http.StatusBadRequest)
	e.want(t, libby, "PATCH", "/api/books/nope", map[string]any{"title": "x"}, http.StatusNotFound)

	// Delete blocked by an open loan.
	e.want(t, alice, "POST", "/api/books/"+id+"/checkout", nil, http.StatusCreated)
	e.want(t, alice, "DELETE", "/api/books/"+id, nil, http.StatusForbidden)
	out = e.want(t, libby, "DELETE", "/api/books/"+id, nil, http.StatusConflict)
	if !strings.Contains(out["error"].(string), "open loan") {
		t.Errorf("conflict = %v", out)
	}
	other := e.addBook(t, "Other", 1)
	e.want(t, libby, "DELETE", "/api/books/"+other, nil, http.StatusNoContent)
	e.want(t, libby, "DELETE", "/api/books/"+other, nil, http.StatusNotFound)
}

func TestListBooksParams(t *testing.T) {
	e := newEnv(t)
	for i, title := range []string{"Beta", "Alpha", "Gamma"} {
		genre := "Y"
		if i == 0 {
			genre = "X"
		}
		body := map[string]any{"title": title, "author": "A", "copies": 1, "genre": genre, "year": 1950 + 20*i}
		e.want(t, libby, "POST", "/api/books", body, http.StatusCreated)
	}
	names := func(out map[string]any) string {
		var ts []string
		for _, b := range list(out, "books") {
			ts = append(ts, b.(map[string]any)["title"].(string))
		}
		return strings.Join(ts, ",")
	}
	if out := e.want(t, nil, "GET", "/api/books", nil, 200); names(out) != "Alpha,Beta,Gamma" {
		t.Errorf("default = %s", names(out))
	}
	if out := e.want(t, nil, "GET", "/api/books?limit=1&offset=1", nil, 200); names(out) != "Beta" || out["total"] != float64(3) {
		t.Errorf("paged = %s %v", names(out), out["total"])
	}
	if out := e.want(t, nil, "GET", "/api/books?genre=x", nil, 200); names(out) != "Beta" {
		t.Errorf("genre = %s", names(out))
	}
	id := list(e.want(t, nil, "GET", "/api/books?q=alpha", nil, 200), "books")[0].(map[string]any)["id"].(string)
	e.want(t, alice, "POST", "/api/books/"+id+"/checkout", nil, http.StatusCreated)
	if out := e.want(t, nil, "GET", "/api/books?available=true", nil, 200); names(out) != "Beta,Gamma" {
		t.Errorf("available = %s", names(out))
	}
	// Years are distinct, so sort=year is deterministic (newest first).
	if out := e.want(t, nil, "GET", "/api/books?sort=year", nil, 200); names(out) != "Gamma,Alpha,Beta" {
		t.Errorf("sort year = %s", names(out))
	}
}

func TestBookDetailVisibility(t *testing.T) {
	e := newEnv(t)
	id := e.addBook(t, "Shared", 3)
	e.want(t, alice, "POST", "/api/books/"+id+"/checkout", nil, http.StatusCreated)
	e.want(t, bob, "POST", "/api/books/"+id+"/checkout", nil, http.StatusCreated)

	out := e.want(t, nil, "GET", "/api/books/"+id, nil, 200)
	if len(list(out, "loans")) != 0 {
		t.Errorf("anonymous sees loans: %v", out)
	}
	out = e.want(t, alice, "GET", "/api/books/"+id, nil, 200)
	if l := list(out, "loans"); len(l) != 1 || l[0].(map[string]any)["userId"] != alice.ID {
		t.Errorf("member sees %v", l)
	}
	out = e.want(t, libby, "GET", "/api/books/"+id, nil, 200)
	if len(list(out, "loans")) != 2 {
		t.Errorf("librarian sees %v", list(out, "loans"))
	}
}

// ---- loans ---------------------------------------------------------------

func TestCheckoutReturnRoles(t *testing.T) {
	e := newEnv(t)
	id := e.addBook(t, "Loanable", 2)

	e.want(t, nil, "POST", "/api/books/"+id+"/checkout", nil, http.StatusUnauthorized)
	// A member may not check out for someone else.
	e.want(t, alice, "POST", "/api/books/"+id+"/checkout", map[string]any{"userId": bob.ID}, http.StatusForbidden)
	// Naming yourself is fine; an empty body is fine.
	out := e.want(t, alice, "POST", "/api/books/"+id+"/checkout", map[string]any{"userId": alice.ID}, http.StatusCreated)
	loan := sub(out, "loan")
	if loan["userId"] != alice.ID || loan["bookId"] != id || loan["returnedAt"] != nil || loan["overdue"] != false {
		t.Errorf("loan = %v", loan)
	}
	if sub(loan, "book")["title"] != "Loanable" || sub(loan, "user")["name"] != "Alice" {
		t.Errorf("refs = %v", loan)
	}
	aliceLoan := loan["id"].(string)
	// Twice: 409.
	e.want(t, alice, "POST", "/api/books/"+id+"/checkout", nil, http.StatusConflict)
	// Librarian checks out for Bob.
	out = e.want(t, libby, "POST", "/api/books/"+id+"/checkout", map[string]any{"userId": bob.ID}, http.StatusCreated)
	bobLoan := sub(out, "loan")["id"].(string)
	// No copies left for the librarian herself.
	e.want(t, libby, "POST", "/api/books/"+id+"/checkout", nil, http.StatusConflict)
	e.want(t, libby, "POST", "/api/books/nope/checkout", nil, http.StatusNotFound)
	e.want(t, libby, "POST", "/api/books/"+id+"/checkout", map[string]any{"userId": "ghost"}, http.StatusNotFound)
	e.want(t, libby, "POST", "/api/books/"+id+"/checkout", `{"userId":1}`, http.StatusBadRequest)

	// Returns: own, not others', librarian any.
	e.want(t, bob, "POST", "/api/loans/"+aliceLoan+"/return", nil, http.StatusForbidden)
	out = e.want(t, alice, "POST", "/api/loans/"+aliceLoan+"/return", nil, http.StatusOK)
	if sub(out, "loan")["returnedAt"] == nil {
		t.Errorf("not returned: %v", out)
	}
	e.want(t, alice, "POST", "/api/loans/"+aliceLoan+"/return", nil, http.StatusConflict)
	e.want(t, libby, "POST", "/api/loans/"+bobLoan+"/return", nil, http.StatusOK)
	e.want(t, libby, "POST", "/api/loans/nope/return", nil, http.StatusNotFound)
}

func TestLoanListings(t *testing.T) {
	e := newEnv(t)
	a := e.addBook(t, "A", 1)
	b := e.addBook(t, "B", 1)
	e.want(t, alice, "POST", "/api/books/"+a+"/checkout", nil, http.StatusCreated)
	e.want(t, bob, "POST", "/api/books/"+b+"/checkout", nil, http.StatusCreated)

	e.want(t, nil, "GET", "/api/loans", nil, http.StatusUnauthorized)
	// Members only ever see their own, whatever they ask for.
	out := e.want(t, alice, "GET", "/api/loans?userId="+bob.ID, nil, 200)
	if l := list(out, "loans"); len(l) != 1 || l[0].(map[string]any)["userId"] != alice.ID || out["total"] != float64(1) {
		t.Errorf("member loans = %v", out)
	}
	out = e.want(t, libby, "GET", "/api/loans", nil, 200)
	if out["total"] != float64(2) {
		t.Errorf("all loans = %v", out)
	}
	out = e.want(t, libby, "GET", "/api/loans?userId="+bob.ID+"&status=open", nil, 200)
	if l := list(out, "loans"); len(l) != 1 || l[0].(map[string]any)["userId"] != bob.ID {
		t.Errorf("filtered = %v", out)
	}
	e.want(t, libby, "GET", "/api/loans?status=weird", nil, http.StatusBadRequest)
	if out = e.want(t, libby, "GET", "/api/loans?status=overdue", nil, 200); out["total"] != float64(0) {
		t.Errorf("overdue = %v", out)
	}

	out = e.want(t, alice, "GET", "/api/me/loans", nil, 200)
	if len(list(out, "loans")) != 1 {
		t.Errorf("me/loans = %v", out)
	}
	e.want(t, nil, "GET", "/api/me/loans", nil, http.StatusUnauthorized)
}

// ---- holds ---------------------------------------------------------------

func TestHoldsAndRoles(t *testing.T) {
	e := newEnv(t)
	id := e.addBook(t, "Wanted", 1)

	e.want(t, nil, "POST", "/api/books/"+id+"/holds", nil, http.StatusUnauthorized)
	out := e.want(t, bob, "POST", "/api/books/"+id+"/holds", nil, http.StatusConflict)
	if out["error"] != "book is available; check it out instead" {
		t.Errorf("available hold = %v", out)
	}
	e.want(t, alice, "POST", "/api/books/"+id+"/checkout", nil, http.StatusCreated)
	out = e.want(t, bob, "POST", "/api/books/"+id+"/holds", nil, http.StatusCreated)
	hold := sub(out, "hold")
	if hold["status"] != "waiting" || hold["position"] != float64(1) || hold["userId"] != bob.ID {
		t.Errorf("hold = %v", hold)
	}
	holdID := hold["id"].(string)
	e.want(t, bob, "POST", "/api/books/"+id+"/holds", nil, http.StatusConflict)
	e.want(t, bob, "POST", "/api/books/nope/holds", nil, http.StatusNotFound)

	out = e.want(t, bob, "GET", "/api/me/holds", nil, 200)
	if len(list(out, "holds")) != 1 {
		t.Errorf("me/holds = %v", out)
	}
	out = e.want(t, bob, "GET", "/api/books/"+id, nil, 200)
	if len(list(out, "holds")) != 1 || len(list(out, "loans")) != 0 {
		t.Errorf("bob's detail = %v", out)
	}

	e.want(t, alice, "GET", "/api/holds?bookId="+id, nil, http.StatusForbidden)
	out = e.want(t, libby, "GET", "/api/holds?bookId="+id, nil, 200)
	if len(list(out, "holds")) != 1 {
		t.Errorf("holds queue = %v", out)
	}
	if out = e.want(t, libby, "GET", "/api/holds", nil, 200); len(list(out, "holds")) != 1 {
		t.Errorf("all holds = %v", out)
	}

	// Cancel: not Alice's, Bob's own, then gone.
	e.want(t, alice, "DELETE", "/api/holds/"+holdID, nil, http.StatusForbidden)
	e.want(t, bob, "DELETE", "/api/holds/"+holdID, nil, http.StatusNoContent)
	e.want(t, bob, "DELETE", "/api/holds/"+holdID, nil, http.StatusConflict)
	e.want(t, libby, "DELETE", "/api/holds/nope", nil, http.StatusNotFound)

	// Librarian can cancel anyone's.
	out = e.want(t, bob, "POST", "/api/books/"+id+"/holds", nil, http.StatusCreated)
	e.want(t, libby, "DELETE", "/api/holds/"+sub(out, "hold")["id"].(string), nil, http.StatusNoContent)
}

func TestDashboard(t *testing.T) {
	e := newEnv(t)
	id := e.addBook(t, "D", 2)
	e.want(t, alice, "POST", "/api/books/"+id+"/checkout", nil, http.StatusCreated)
	e.want(t, nil, "GET", "/api/dashboard", nil, http.StatusUnauthorized)
	e.want(t, alice, "GET", "/api/dashboard", nil, http.StatusForbidden)
	out := e.want(t, libby, "GET", "/api/dashboard", nil, 200)
	if out["books"] != float64(1) || out["copies"] != float64(2) || out["openLoans"] != float64(1) || out["members"] != float64(4) {
		t.Errorf("dashboard = %v", out)
	}
	for _, k := range []string{"recentLoans", "overdueLoans", "readyHolds"} {
		if _, ok := out[k].([]any); !ok {
			t.Errorf("%s missing or not an array: %v", k, out[k])
		}
	}
}

// ---- streaming -------------------------------------------------------------

func TestListBooksWebSocket(t *testing.T) {
	e := newEnv(t)
	e.addBook(t, "First", 1)
	srv := httptest.NewServer(e.mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/books?sort=added", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	read := func() realtime.Frame {
		t.Helper()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var f realtime.Frame
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	snap := read()
	var page booksPage
	if snap.Type != "snapshot" || json.Unmarshal(snap.Data, &page) != nil || page.Total != 1 || page.Books[0].Title != "First" {
		t.Fatalf("snapshot = %+v", snap)
	}
	id := e.addBook(t, "Second", 1)
	ev := read()
	var data map[string]string
	_ = json.Unmarshal(ev.Data, &data)
	if ev.Type != "event" || ev.Topic != "books" || data["action"] != "created" || data["id"] != id {
		t.Fatalf("event = %+v %v", ev, data)
	}
}

func TestNilHubServesPlainJSON(t *testing.T) {
	e := newEnv(t)
	mux := http.NewServeMux()
	New(e.svc, nil).Register(mux)
	e.mux = mux
	e.addBook(t, "Plain", 1)
	out := e.want(t, nil, "GET", "/api/books", nil, 200)
	if out["total"] != float64(1) {
		t.Errorf("plain list = %v", out)
	}
}
