package ai

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// FakeCatalog is an in-memory Catalog for tests (this package's and the
// integrator's). Search is a case-insensitive substring match over title,
// author, genre and tags.
type FakeCatalog struct {
	mu    sync.Mutex
	Books []BookDetail
	Loans map[string][]LoanSummary // by user id
	Holds []HoldSummary
	// Calls records every tool-facing call as "method:args" for assertions.
	Calls []string
}

// NewFakeCatalog returns a catalog seeded with books.
func NewFakeCatalog(books ...BookDetail) *FakeCatalog {
	return &FakeCatalog{Books: books, Loans: map[string][]LoanSummary{}}
}

func (f *FakeCatalog) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, s)
}

// SearchBooks implements Catalog.
func (f *FakeCatalog) SearchBooks(_ context.Context, text, genre string, availableOnly bool, limit int) ([]BookSummary, error) {
	f.record(fmt.Sprintf("search:%s|%s|%v|%d", text, genre, availableOnly, limit))
	text = strings.ToLower(strings.TrimSpace(text))
	var out []BookSummary
	for _, b := range f.Books {
		if genre != "" && !strings.EqualFold(b.Genre, genre) {
			continue
		}
		if availableOnly && b.Available <= 0 {
			continue
		}
		hay := strings.ToLower(strings.Join(append([]string{b.Title, b.Author, b.Genre, b.ISBN}, b.Tags...), " "))
		if text != "" && !anyWord(hay, text) {
			continue
		}
		out = append(out, b.BookSummary)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// anyWord reports whether any word of query appears in hay.
func anyWord(hay, query string) bool {
	for _, w := range strings.Fields(query) {
		if strings.Contains(hay, w) {
			return true
		}
	}
	return false
}

// BookDetail implements Catalog.
func (f *FakeCatalog) BookDetail(_ context.Context, id string) (*BookDetail, error) {
	f.record("get:" + id)
	for _, b := range f.Books {
		if b.ID == id {
			b := b
			return &b, nil
		}
	}
	return nil, fmt.Errorf("book %q not found", id)
}

// Genres implements Catalog.
func (f *FakeCatalog) Genres(context.Context) ([]GenreCount, error) {
	f.record("genres")
	counts := map[string]int{}
	for _, b := range f.Books {
		if b.Genre != "" {
			counts[b.Genre]++
		}
	}
	out := make([]GenreCount, 0, len(counts))
	for g, n := range counts {
		out = append(out, GenreCount{Name: g, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// UserLoans implements Catalog.
func (f *FakeCatalog) UserLoans(_ context.Context, userID string) ([]LoanSummary, error) {
	f.record("loans:" + userID)
	return f.Loans[userID], nil
}

// PlaceHold implements Catalog. It refuses when the book is available or
// missing, mirroring the library's rules.
func (f *FakeCatalog) PlaceHold(ctx context.Context, bookID, userID string) (*HoldSummary, error) {
	f.record("hold:" + bookID + ":" + userID)
	b, err := f.BookDetail(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if b.Available > 0 {
		return nil, fmt.Errorf("book is available; check it out instead")
	}
	h := HoldSummary{HoldID: "hold-" + bookID + "-" + userID, BookID: bookID, Position: b.HoldsWaiting + 1}
	f.mu.Lock()
	f.Holds = append(f.Holds, h)
	f.mu.Unlock()
	return &h, nil
}
