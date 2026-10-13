package library

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/bmurray/alexandria/internal/id"
)

// seedJSON is a starter catalog of classics so a fresh install is not empty.
//
//go:embed seed_data.json
var seedJSON []byte

// SeedBooks returns the built-in starter catalog, validated.
func SeedBooks() ([]BookInput, error) {
	var in []BookInput
	if err := json.Unmarshal(seedJSON, &in); err != nil {
		return nil, fmt.Errorf("seed_data.json: %w", err)
	}
	for i := range in {
		if err := normalizeInput(&in[i]); err != nil {
			return nil, fmt.Errorf("seed_data.json[%d] %q: %w", i, in[i].Title, err)
		}
	}
	return in, nil
}

// seedIfEmpty inserts the starter catalog when there are no books. The check
// and the inserts share one transaction, so it is safe to call repeatedly.
func (s *Service) seedIfEmpty(ctx context.Context) (int, error) {
	books, err := SeedBooks()
	if err != nil {
		return 0, err
	}
	inserted := 0
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		n, err := countBooks(ctx, tx)
		if err != nil || n > 0 {
			return err
		}
		now := s.now()
		for _, in := range books {
			b := &Book{
				ID: id.New(), Title: in.Title, Author: in.Author, ISBN: in.ISBN, Year: in.Year, Genre: in.Genre,
				Publisher: in.Publisher, Description: in.Description, CoverURL: in.CoverURL, Copies: in.Copies, Tags: in.Tags,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := insertBook(ctx, tx, b, ""); err != nil {
				return fmt.Errorf("insert %q: %w", in.Title, err)
			}
			inserted++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return inserted, nil
}
