package ai

import (
	"context"
	"time"
)

// Catalog is what the librarian's tools need from the library. It is a narrow
// view over plain values so this package does not depend on internal/library;
// internal/app adapts *library.Service to it.
type Catalog interface {
	// SearchBooks runs the catalog search (FTS over title, author, ISBN,
	// genre and tags). An empty text matches everything, filtered by genre
	// and availability. limit is already bounded by the caller.
	SearchBooks(ctx context.Context, text, genre string, availableOnly bool, limit int) ([]BookSummary, error)
	// BookDetail returns one book or a not-found error.
	BookDetail(ctx context.Context, id string) (*BookDetail, error)
	// Genres lists genres with their book counts.
	Genres(ctx context.Context) ([]GenreCount, error)
	// UserLoans lists a member's open loans.
	UserLoans(ctx context.Context, userID string) ([]LoanSummary, error)
	// PlaceHold places a hold for userID on bookID. Domain refusals (book
	// available, duplicate hold) come back as errors whose message is shown
	// to the model.
	PlaceHold(ctx context.Context, bookID, userID string) (*HoldSummary, error)
}

// BookSummary is a search hit.
type BookSummary struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Author    string   `json:"author"`
	Year      *int     `json:"year,omitempty"`
	Genre     string   `json:"genre,omitempty"`
	Available int      `json:"available"`
	Copies    int      `json:"copies"`
	Tags      []string `json:"tags,omitempty"`
}

// BookDetail is the full record.
type BookDetail struct {
	BookSummary
	ISBN         string `json:"isbn,omitempty"`
	Publisher    string `json:"publisher,omitempty"`
	Description  string `json:"description,omitempty"`
	CoverURL     string `json:"coverUrl,omitempty"`
	HoldsWaiting int    `json:"holdsWaiting"`
}

// GenreCount is one genre and how many books carry it.
type GenreCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// LoanSummary is one open loan of the signed-in member.
type LoanSummary struct {
	LoanID  string    `json:"loanId"`
	BookID  string    `json:"bookId"`
	Title   string    `json:"title"`
	Author  string    `json:"author"`
	DueAt   time.Time `json:"dueAt"`
	Overdue bool      `json:"overdue"`
}

// HoldSummary is a placed hold.
type HoldSummary struct {
	HoldID   string `json:"holdId"`
	BookID   string `json:"bookId"`
	Position int    `json:"position"`
}
