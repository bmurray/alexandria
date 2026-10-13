package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const olISBN = `{"ISBN:9780441013593": {"title": "Dune", "authors": [{"name": "Frank Herbert"}], "publish_date": "1965",
  "publishers": [{"name": "Chilton"}], "subjects": [{"name": "Science fiction"}, {"name": "Deserts"}], "cover": {"large": "https://covers.openlibrary.org/b/id/1-L.jpg"}}}`

const olSearch = `{"docs": [{"title": "Emma", "author_name": ["Jane Austen"], "first_publish_year": 1815, "isbn": ["9780141439587"], "cover_i": 7}]}`

func withOpenLibrary(t *testing.T, svc *Service, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	svc.ol.BaseURL = srv.URL
	svc.ol.CoversURL = "https://covers.test"
}

func olHandler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/books":
		if strings.Contains(r.URL.Query().Get("bibkeys"), "9780441013593") {
			w.Write([]byte(olISBN))
			return
		}
		w.Write([]byte(`{}`))
	case "/search.json":
		if r.URL.Query().Get("title") == "Emma" {
			w.Write([]byte(olSearch))
			return
		}
		w.Write([]byte(`{"docs": []}`))
	default:
		http.NotFound(w, r)
	}
}

func TestEnrichBoth(t *testing.T) {
	ctx := context.Background()
	p := newFake("opus", text(`{"genre": "Science fiction", "description": "A desert planet. A boy becomes a legend.", "tags": ["Desert", "politics", "desert", " ecology "]}`, 50, 30))
	svc := newTestService(t, Config{}, nil, p)
	withOpenLibrary(t, svc, olHandler)

	res, err := svc.Enrich(ctx, EnrichInput{Title: "dune", ISBN: "978-0441013593"}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	s := res.Suggestion
	if res.Source != SourceBoth || *s.Author != "Frank Herbert" || *s.Year != 1965 || *s.Publisher != "Chilton" || *s.ISBN != "9780441013593" {
		t.Fatalf("%+v %+v", res.Source, s)
	}
	if *s.Genre != "Science fiction" || !strings.Contains(*s.Description, "legend") || strings.Join(s.Tags, ",") != "desert,politics,ecology" {
		t.Fatalf("model part: %+v", s)
	}
	if *s.CoverURL != "https://covers.openlibrary.org/b/id/1-L.jpg" {
		t.Fatal(*s.CoverURL)
	}
	req := p.Requests[0]
	if req.OutputSchema == nil || !strings.Contains(req.Messages[0].Text, "Frank Herbert") || !strings.Contains(req.Messages[0].Text, "subjects: Science fiction; Deserts") {
		t.Fatalf("prompt: %+v", req)
	}
	report, _ := svc.Usage(ctx)
	if report.ByPurpose[0].Purpose != PurposeEnrich || report.ByPurpose[0].Calls != 1 {
		t.Fatalf("ledger: %+v", report.ByPurpose)
	}
}

func TestEnrichTitleSearchAndModelOnly(t *testing.T) {
	ctx := context.Background()
	p := newFake("opus",
		text("Here you go:\n```json\n{\"genre\":\"Classics\",\"description\":\"Matchmaking.\",\"tags\":[\"regency\"]}\n```", 1, 1),
		text(`{"genre":"Mystery","description":"Unknown book.","tags":[]}`, 1, 1),
	)
	svc := newTestService(t, Config{}, nil, p)
	withOpenLibrary(t, svc, olHandler)

	res, err := svc.Enrich(ctx, EnrichInput{Title: "Emma", Author: "Austen"}, "")
	if err != nil || res.Source != SourceBoth || *res.Suggestion.Author != "Jane Austen" || *res.Suggestion.Genre != "Classics" || *res.Suggestion.CoverURL != "https://covers.test/b/id/7-L.jpg" {
		t.Fatalf("%+v %v", res, err)
	}
	// Open Library has nothing: model only.
	res, err = svc.Enrich(ctx, EnrichInput{Title: "Obscure"}, "")
	if err != nil || res.Source != SourceModel || *res.Suggestion.Genre != "Mystery" || res.Suggestion.Author != nil || res.Suggestion.Tags != nil {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := svc.Enrich(ctx, EnrichInput{}, ""); statusOf(t, err) != http.StatusBadRequest {
		t.Fatal("empty input")
	}
}

func TestEnrichWithoutProvider(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, Config{}, nil, newFake("opus"))
	withOpenLibrary(t, svc, olHandler)
	svc.UpdateProvider(ctx, "opus", bp(false), nil, nil, false)

	res, err := svc.Enrich(ctx, EnrichInput{ISBN: "9780441013593"}, "")
	if err != nil || res.Source != SourceOpenLibrary || *res.Suggestion.Author != "Frank Herbert" || res.Suggestion.Genre != nil {
		t.Fatalf("%+v %v", res, err)
	}
	if strings.Join(res.Suggestion.Tags, ",") != "science fiction,deserts" {
		t.Fatalf("subjects should seed tags: %v", res.Suggestion.Tags)
	}
	if _, err := svc.Enrich(ctx, EnrichInput{Title: "Nothing"}, ""); statusOf(t, err) != http.StatusNotFound {
		t.Fatalf("got %v", err)
	}
	// Provider errors and junk output degrade to the Open Library part.
	svc = newTestService(t, Config{}, nil, newFake("opus", failing("down"), text("not json at all", 1, 1), failing("down")))
	withOpenLibrary(t, svc, olHandler)
	if res, err := svc.Enrich(ctx, EnrichInput{ISBN: "9780441013593"}, ""); err != nil || res.Source != SourceOpenLibrary {
		t.Fatalf("%+v %v", res, err)
	}
	if res, err := svc.Enrich(ctx, EnrichInput{ISBN: "9780441013593"}, ""); err != nil || res.Source != SourceOpenLibrary {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := svc.Enrich(ctx, EnrichInput{Title: "Nothing"}, ""); err == nil {
		t.Fatal("no OL and provider down must error")
	}
}

func TestLookup(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, Config{}, nil, newFake("opus"))
	withOpenLibrary(t, svc, olHandler)
	sug, source, err := svc.Lookup(ctx, "9780441013593", "", "")
	if err != nil || source != SourceOpenLibrary || *sug.Author != "Frank Herbert" {
		t.Fatal(sug, source, err)
	}
	if _, _, err := svc.Lookup(ctx, "", "Nothing", ""); statusOf(t, err) != http.StatusNotFound {
		t.Fatal(err)
	}
	if _, _, err := svc.Lookup(ctx, "", "", ""); statusOf(t, err) != http.StatusBadRequest {
		t.Fatal(err)
	}
	// ISBN miss falls back to a title search.
	sug, _, err = svc.Lookup(ctx, "9780000000000", "Emma", "")
	if err != nil || *sug.Author != "Jane Austen" {
		t.Fatal(sug, err)
	}
}

func TestParseJSONObject(t *testing.T) {
	var v struct{ A int }
	if err := parseJSONObject("prose {\"A\": 1} trailing", &v); err != nil || v.A != 1 {
		t.Fatal(err, v)
	}
	if err := parseJSONObject("no braces", &v); err == nil {
		t.Fatal("want error")
	}
}
