package openlibrary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const isbnJSON = `{"ISBN:9780140328721": {
  "title": "Fantastic Mr. Fox",
  "authors": [{"name": "Roald Dahl"}],
  "publish_date": "October 1, 1988",
  "publishers": [{"name": "Puffin"}],
  "identifiers": {"isbn_13": ["9780140328721"]},
  "subjects": [{"name": "Foxes"}, {"name": "Juvenile fiction"}],
  "cover": {"large": "https://covers.openlibrary.org/b/id/8739161-L.jpg"}
}}`

const searchJSON = `{"docs": [
  {"title": "Dune (no isbn)", "author_name": ["Frank Herbert"]},
  {"title": "Dune", "author_name": ["Frank Herbert"], "first_publish_year": 1965,
   "isbn": ["0441013597", "978-0441013593"], "publisher": ["Ace"], "subject": ["Science fiction"], "cover_i": 12345}
]}`

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(srv.Client())
	c.BaseURL = srv.URL
	c.CoversURL = "https://covers.test"
	return c
}

func TestByISBN(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/books" || r.URL.Query().Get("bibkeys") != "ISBN:9780140328721" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(isbnJSON))
	})
	rec, err := c.ByISBN(context.Background(), "978-0-14-032872-1")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || rec.Title != "Fantastic Mr. Fox" || rec.Authors[0] != "Roald Dahl" {
		t.Fatalf("bad record: %+v", rec)
	}
	if rec.Year == nil || *rec.Year != 1988 {
		t.Fatalf("year: %v", rec.Year)
	}
	if rec.Publishers[0] != "Puffin" || rec.ISBN13 != "9780140328721" || len(rec.Subjects) != 2 {
		t.Fatalf("fields: %+v", rec)
	}
	if rec.CoverURL != "https://covers.openlibrary.org/b/id/8739161-L.jpg" {
		t.Fatalf("cover: %s", rec.CoverURL)
	}
}

func TestByISBNNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	rec, err := c.ByISBN(context.Background(), "9780000000000")
	if err != nil || rec != nil {
		t.Fatalf("want nil,nil got %v,%v", rec, err)
	}
	if rec, err := c.ByISBN(context.Background(), "nope"); rec != nil || err != nil {
		t.Fatalf("invalid isbn should be nil,nil")
	}
}

func TestByISBNCoverFallback(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ISBN:0441013597": {"title": "Dune"}}`))
	})
	rec, err := c.ByISBN(context.Background(), "0441013597")
	if err != nil || rec == nil {
		t.Fatal(rec, err)
	}
	if rec.CoverURL != "https://covers.test/b/isbn/0441013597-L.jpg?default=false" {
		t.Fatalf("cover: %s", rec.CoverURL)
	}
}

func TestSearch(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" || r.URL.Query().Get("title") != "dune" || r.URL.Query().Get("limit") != "5" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(searchJSON))
	})
	rec, err := c.Search(context.Background(), "dune", "herbert")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || rec.Title != "Dune" || rec.ISBN13 != "9780441013593" {
		t.Fatalf("bad record: %+v", rec)
	}
	if rec.Year == nil || *rec.Year != 1965 || rec.Publishers[0] != "Ace" || rec.Subjects[0] != "Science fiction" {
		t.Fatalf("fields: %+v", rec)
	}
	if rec.CoverURL != "https://covers.test/b/id/12345-L.jpg?default=false" {
		t.Fatalf("cover: %s", rec.CoverURL)
	}
}

func TestSearchEmptyAndErrors(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"docs": []}`)) })
	if rec, err := c.Search(context.Background(), "nothing", ""); rec != nil || err != nil {
		t.Fatalf("want nil,nil got %v,%v", rec, err)
	}
	if rec, err := c.Search(context.Background(), "", ""); rec != nil || err != nil {
		t.Fatalf("blank query should be nil,nil")
	}
	bad := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	if _, err := bad.Search(context.Background(), "x", ""); err == nil {
		t.Fatal("want error on 500")
	}
	if _, err := bad.ByISBN(context.Background(), "0441013597"); err == nil {
		t.Fatal("want error on 500")
	}
}

func TestYearFrom(t *testing.T) {
	if y := yearFrom("May 1, 1999"); y == nil || *y != 1999 {
		t.Fatal(y)
	}
	if y := yearFrom("unknown"); y != nil {
		t.Fatal(y)
	}
}
