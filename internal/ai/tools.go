package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Caller is the signed-in person on whose behalf tools run; nil when the
// chat is anonymous.
type Caller struct {
	ID   string
	Name string
}

const (
	searchLimitDefault = 8
	searchLimitMax     = 20
	recommendMax       = 8
	signInMessage      = "You need to sign in for that."
)

// toolset builds the librarian's tools for one chat call. Tools that act for
// a member check caller themselves and answer with a friendly message rather
// than an error so the model can relay it.
func toolset(catalog Catalog, caller *Caller) []ToolDef {
	return []ToolDef{
		{
			Name:        "search_books",
			Description: "Search the library catalog by title, author, ISBN, genre or tag words. Returns up to `limit` books with availability. Use it before answering any question about what the library has.",
			InputSchema: schema(map[string]any{
				"query":         prop("string", "Free text to search for: title, author, ISBN, genre or tag words. May be empty when filtering by genre only."),
				"genre":         prop("string", "Restrict to this genre (exact name from list_genres)."),
				"availableOnly": prop("boolean", "Only books with a copy on the shelf right now."),
				"limit":         prop("integer", "Maximum results, 1-20 (default 8)."),
			}, "query"),
			Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					Query         string `json:"query"`
					Genre         string `json:"genre"`
					AvailableOnly bool   `json:"availableOnly"`
					Limit         int    `json:"limit"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", err
				}
				limit := in.Limit
				if limit <= 0 {
					limit = searchLimitDefault
				}
				if limit > searchLimitMax {
					limit = searchLimitMax
				}
				books, err := catalog.SearchBooks(ctx, in.Query, in.Genre, in.AvailableOnly, limit)
				if err != nil {
					return "", err
				}
				return compact(map[string]any{"books": nonNil(books), "count": len(books)})
			},
		},
		{
			Name:        "get_book",
			Description: "Fetch one book's full record by its id: description, ISBN, publisher, copies, how many are available and how many holds are waiting.",
			InputSchema: schema(map[string]any{"id": prop("string", "The book id from search_books.")}, "id"),
			Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", err
				}
				b, err := catalog.BookDetail(ctx, in.ID)
				if err != nil {
					return compact(map[string]any{"error": err.Error()})
				}
				return compact(b)
			},
		},
		{
			Name:        "list_genres",
			Description: "List every genre in the catalog with how many books it has.",
			InputSchema: schema(map[string]any{}),
			Run: func(ctx context.Context, _ json.RawMessage) (string, error) {
				g, err := catalog.Genres(ctx)
				if err != nil {
					return "", err
				}
				return compact(map[string]any{"genres": nonNil(g)})
			},
		},
		{
			Name:        "recommend",
			Description: "Find candidate books to recommend, similar to a book the person liked and/or matching their stated interests. Returns up to 8 catalog books; explain in your own words why each fits.",
			InputSchema: schema(map[string]any{
				"likeBookId": prop("string", "Id of a book the person enjoyed; its genre, tags and author seed the search."),
				"interests":  prop("string", "What the person is in the mood for, in their words."),
			}),
			Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					LikeBookID string `json:"likeBookId"`
					Interests  string `json:"interests"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", err
				}
				return recommend(ctx, catalog, in.LikeBookID, in.Interests)
			},
		},
		{
			Name:        "my_loans",
			Description: "List the signed-in person's open loans with due dates and whether they are overdue.",
			InputSchema: schema(map[string]any{}),
			Run: func(ctx context.Context, _ json.RawMessage) (string, error) {
				if caller == nil {
					return signInMessage, nil
				}
				loans, err := catalog.UserLoans(ctx, caller.ID)
				if err != nil {
					return "", err
				}
				return compact(map[string]any{"loans": nonNil(loans)})
			},
		},
		{
			Name:        "place_hold",
			Description: "Place a hold for the signed-in person on a book that has no copies available. Only call this when they clearly ask to reserve or hold a specific book.",
			InputSchema: schema(map[string]any{"bookId": prop("string", "The book id.")}, "bookId"),
			Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
				if caller == nil {
					return signInMessage, nil
				}
				var in struct {
					BookID string `json:"bookId"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", err
				}
				h, err := catalog.PlaceHold(ctx, in.BookID, caller.ID)
				if err != nil {
					return compact(map[string]any{"placed": false, "error": err.Error()})
				}
				return compact(map[string]any{"placed": true, "hold": h})
			},
		},
	}
}

// recommend seeds a few catalog searches from the liked book and the
// interests, merges and dedupes them, and leaves the reasoning to the model.
func recommend(ctx context.Context, catalog Catalog, likeID, interests string) (string, error) {
	var seed *BookDetail
	var queries []struct{ text, genre string }
	if likeID != "" {
		b, err := catalog.BookDetail(ctx, likeID)
		if err != nil {
			return compact(map[string]any{"error": err.Error()})
		}
		seed = b
		if b.Genre != "" {
			queries = append(queries, struct{ text, genre string }{"", b.Genre})
		}
		if len(b.Tags) > 0 {
			queries = append(queries, struct{ text, genre string }{strings.Join(b.Tags, " "), ""})
		}
		if b.Author != "" {
			queries = append(queries, struct{ text, genre string }{b.Author, ""})
		}
	}
	if strings.TrimSpace(interests) != "" {
		queries = append(queries, struct{ text, genre string }{interests, ""})
	}
	seen := map[string]bool{}
	if seed != nil {
		seen[seed.ID] = true
	}
	var picks []BookSummary
	for _, q := range queries {
		if len(picks) >= recommendMax {
			break
		}
		hits, err := catalog.SearchBooks(ctx, q.text, q.genre, false, recommendMax)
		if err != nil {
			return "", err
		}
		for _, h := range hits {
			if seen[h.ID] || len(picks) >= recommendMax {
				continue
			}
			seen[h.ID] = true
			picks = append(picks, h)
		}
	}
	out := map[string]any{"candidates": nonNil(picks)}
	if seed != nil {
		out["seed"] = seed.BookSummary
	}
	return compact(out)
}

// schema builds a strict-friendly object schema.
func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// compact renders a tool result as single-line JSON.
func compact(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode tool result: %w", err)
	}
	return string(b), nil
}

// nonNil keeps empty results as [] rather than null in tool output.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
