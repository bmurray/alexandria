package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bmurray/alexandria/internal/handler"
	"github.com/bmurray/alexandria/internal/openlibrary"
)

// EnrichInput is what the librarian has typed so far.
type EnrichInput struct {
	Title       string `json:"title"`
	Author      string `json:"author"`
	ISBN        string `json:"isbn"`
	Description string `json:"description"`
}

// Suggestion is proposed metadata. Nothing is applied automatically; the
// librarian reviews it and saves with a normal PATCH.
type Suggestion struct {
	Author      *string  `json:"author,omitempty"`
	Year        *int     `json:"year,omitempty"`
	Genre       *string  `json:"genre,omitempty"`
	Publisher   *string  `json:"publisher,omitempty"`
	Description *string  `json:"description,omitempty"`
	CoverURL    *string  `json:"coverUrl,omitempty"`
	ISBN        *string  `json:"isbn,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// Sources of a suggestion.
const (
	SourceOpenLibrary = "openlibrary"
	SourceModel       = "model"
	SourceBoth        = "both"
)

// EnrichResult is the Enrich reply.
type EnrichResult struct {
	Suggestion Suggestion `json:"suggestion"`
	Source     string     `json:"source"`
}

var errNothingFound = handler.NewStatusError(http.StatusNotFound, "no metadata found for that book")

// enrichSchema is the structured output we ask the model for.
var enrichSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"genre":       map[string]any{"type": "string", "description": "One short genre label, e.g. 'Science fiction' or 'Memoir'."},
		"description": map[string]any{"type": "string", "description": "Two sentences describing the book for a library catalog card."},
		"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Three to six lowercase topic tags."},
	},
	"required":             []string{"genre", "description", "tags"},
	"additionalProperties": false,
}

// Enrich looks the book up on Open Library (ISBN first, then title and
// author), then asks the model for genre, description and tags given what
// was found. When no provider is eligible the Open Library part is still
// returned with source "openlibrary".
func (s *Service) Enrich(ctx context.Context, in EnrichInput, userID string) (*EnrichResult, error) {
	in.Title, in.Author, in.ISBN = strings.TrimSpace(in.Title), strings.TrimSpace(in.Author), strings.TrimSpace(in.ISBN)
	if in.Title == "" && in.ISBN == "" {
		return nil, handler.NewStatusError(http.StatusBadRequest, "title or isbn is required")
	}
	rec, err := s.lookup(ctx, in.ISBN, in.Title, in.Author)
	if err != nil {
		slog.WarnContext(ctx, "open library lookup failed", slog.String("error", err.Error()))
		rec = nil // the model can still help
	}
	result := &EnrichResult{}
	if rec != nil {
		result.Suggestion = suggestionFrom(rec)
		result.Source = SourceOpenLibrary
	}

	ok, err := s.hasEligibleProvider(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		if rec == nil {
			return nil, errNothingFound
		}
		return result, nil
	}
	if err := s.checkDailyBudget(ctx); err != nil {
		if rec == nil {
			return nil, err
		}
		return result, nil
	}

	resp, _, err := s.complete(ctx, PurposeEnrich, userID, Request{
		System:       "You write catalog metadata for a small library. Answer only with JSON matching the schema. Be factual; when unsure, keep the description general and never invent plot details.",
		Messages:     []Turn{{Role: RoleUser, Text: enrichPrompt(in, rec)}},
		OutputSchema: enrichSchema,
		MaxTokens:    1024,
	})
	if err != nil {
		if rec == nil {
			return nil, err
		}
		slog.WarnContext(ctx, "enrich model call failed; returning open library part", slog.String("error", err.Error()))
		return result, nil
	}
	if resp.StopReason == StopRefusal {
		if rec == nil {
			return nil, errNothingFound
		}
		return result, nil
	}
	var model struct {
		Genre       string   `json:"genre"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	}
	if err := parseJSONObject(resp.Text, &model); err != nil {
		slog.WarnContext(ctx, "enrich: model output was not JSON", slog.String("error", err.Error()))
		if rec == nil {
			return nil, errNothingFound
		}
		return result, nil
	}
	if g := strings.TrimSpace(model.Genre); g != "" {
		result.Suggestion.Genre = &g
	}
	if d := strings.TrimSpace(model.Description); d != "" {
		result.Suggestion.Description = &d
	}
	if tags := cleanTags(model.Tags); len(tags) > 0 {
		result.Suggestion.Tags = tags
	}
	switch {
	case rec != nil:
		result.Source = SourceBoth
	default:
		result.Source = SourceModel
	}
	return result, nil
}

// Lookup is the Open Library only path (no model call). Not found is a 404.
func (s *Service) Lookup(ctx context.Context, isbn, title, author string) (*Suggestion, string, error) {
	isbn, title, author = strings.TrimSpace(isbn), strings.TrimSpace(title), strings.TrimSpace(author)
	if isbn == "" && title == "" {
		return nil, "", handler.NewStatusError(http.StatusBadRequest, "isbn or title is required")
	}
	rec, err := s.lookup(ctx, isbn, title, author)
	if err != nil {
		return nil, "", err
	}
	if rec == nil {
		return nil, "", errNothingFound
	}
	sug := suggestionFrom(rec)
	return &sug, SourceOpenLibrary, nil
}

func (s *Service) lookup(ctx context.Context, isbn, title, author string) (*openlibrary.Record, error) {
	if isbn != "" {
		rec, err := s.ol.ByISBN(ctx, isbn)
		if err != nil || rec != nil {
			return rec, err
		}
	}
	if title == "" {
		return nil, nil
	}
	return s.ol.Search(ctx, title, author)
}

// suggestionFrom maps an Open Library record to a Suggestion. Subjects become
// tags only until the model proposes better ones.
func suggestionFrom(r *openlibrary.Record) Suggestion {
	var sug Suggestion
	if len(r.Authors) > 0 {
		a := strings.Join(r.Authors, ", ")
		sug.Author = &a
	}
	if r.Year != nil {
		y := *r.Year
		sug.Year = &y
	}
	if len(r.Publishers) > 0 {
		p := r.Publishers[0]
		sug.Publisher = &p
	}
	if r.CoverURL != "" {
		c := r.CoverURL
		sug.CoverURL = &c
	}
	if r.ISBN13 != "" {
		i := r.ISBN13
		sug.ISBN = &i
	}
	sug.Tags = cleanTags(r.Subjects)
	return sug
}

func enrichPrompt(in EnrichInput, rec *openlibrary.Record) string {
	var b strings.Builder
	b.WriteString("Book as entered by the librarian:\n")
	fmt.Fprintf(&b, "- title: %s\n", in.Title)
	if in.Author != "" {
		fmt.Fprintf(&b, "- author: %s\n", in.Author)
	}
	if in.ISBN != "" {
		fmt.Fprintf(&b, "- isbn: %s\n", in.ISBN)
	}
	if d := strings.TrimSpace(in.Description); d != "" {
		fmt.Fprintf(&b, "- current description: %s\n", d)
	}
	if rec != nil {
		b.WriteString("\nOpen Library record:\n")
		fmt.Fprintf(&b, "- title: %s\n", rec.Title)
		if len(rec.Authors) > 0 {
			fmt.Fprintf(&b, "- authors: %s\n", strings.Join(rec.Authors, ", "))
		}
		if rec.Year != nil {
			fmt.Fprintf(&b, "- first published: %d\n", *rec.Year)
		}
		if len(rec.Publishers) > 0 {
			fmt.Fprintf(&b, "- publishers: %s\n", strings.Join(rec.Publishers, ", "))
		}
		if len(rec.Subjects) > 0 {
			fmt.Fprintf(&b, "- subjects: %s\n", strings.Join(rec.Subjects, "; "))
		}
	} else {
		b.WriteString("\nOpen Library had no record; rely on what you know about this title.\n")
	}
	b.WriteString("\nReturn JSON with genre, description (two sentences) and tags.")
	return b.String()
}

// parseJSONObject decodes text as a JSON object, tolerating prose or code
// fences around the first {...} block.
func parseJSONObject(text string, v any) error {
	text = strings.TrimSpace(text)
	if err := json.Unmarshal([]byte(text), v); err == nil {
		return nil
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return errors.New("no JSON object in model output")
	}
	return json.Unmarshal([]byte(text[start:end+1]), v)
}

// cleanTags lowercases, trims, dedupes and caps tags at eight.
func cleanTags(tags []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] || len(t) > 40 {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == 8 {
			break
		}
	}
	return out
}
