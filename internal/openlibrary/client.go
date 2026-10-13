// Package openlibrary is a small read-only client for the Open Library book
// and search APIs. It is used by Enrich and the ISBN lookup; there is no
// authentication and no write path.
package openlibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Defaults for the public service.
const (
	DefaultBaseURL   = "https://openlibrary.org"
	DefaultCoversURL = "https://covers.openlibrary.org"
	defaultTimeout   = 10 * time.Second
	maxSubjects      = 10
	maxBody          = 4 << 20
)

// Record is what we keep from an Open Library hit. Every field may be empty;
// callers treat it as a set of hints, not as truth.
type Record struct {
	Title      string
	Authors    []string
	Year       *int
	Publishers []string
	ISBN13     string
	Subjects   []string
	CoverURL   string
}

// Client talks to one Open Library deployment. BaseURL and CoversURL are
// exported so tests can point it at an httptest server.
type Client struct {
	hc        *http.Client
	BaseURL   string
	CoversURL string
}

// NewClient returns a client. A nil hc gets a 10 second timeout; a caller
// supplied client is used as is (set its Timeout).
func NewClient(hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{hc: hc, BaseURL: DefaultBaseURL, CoversURL: DefaultCoversURL}
}

// ByISBN looks a book up by ISBN-10 or ISBN-13 through the books API with
// jscmd=data. It returns (nil, nil) when Open Library has no record.
func (c *Client) ByISBN(ctx context.Context, isbn string) (*Record, error) {
	isbn = NormalizeISBN(isbn)
	if isbn == "" {
		return nil, nil
	}
	key := "ISBN:" + isbn
	q := url.Values{"bibkeys": {key}, "format": {"json"}, "jscmd": {"data"}}
	var payload map[string]struct {
		Title       string                  `json:"title"`
		Authors     []struct{ Name string } `json:"authors"`
		PublishDate string                  `json:"publish_date"`
		Publishers  []struct{ Name string } `json:"publishers"`
		Identifiers struct {
			ISBN13 []string `json:"isbn_13"`
		} `json:"identifiers"`
		Subjects []struct{ Name string } `json:"subjects"`
		Cover    struct {
			Large string `json:"large"`
		} `json:"cover"`
	}
	if err := c.getJSON(ctx, "/api/books?"+q.Encode(), &payload); err != nil {
		return nil, err
	}
	hit, ok := payload[key]
	if !ok || hit.Title == "" {
		return nil, nil
	}
	rec := &Record{Title: hit.Title, Year: yearFrom(hit.PublishDate)}
	for _, a := range hit.Authors {
		rec.Authors = appendNonEmpty(rec.Authors, a.Name)
	}
	for _, p := range hit.Publishers {
		rec.Publishers = appendNonEmpty(rec.Publishers, p.Name)
	}
	for _, s := range hit.Subjects {
		if len(rec.Subjects) >= maxSubjects {
			break
		}
		rec.Subjects = appendNonEmpty(rec.Subjects, s.Name)
	}
	rec.ISBN13 = firstISBN13(hit.Identifiers.ISBN13)
	if rec.ISBN13 == "" && len(isbn) == 13 {
		rec.ISBN13 = isbn
	}
	switch {
	case hit.Cover.Large != "":
		rec.CoverURL = hit.Cover.Large
	default:
		rec.CoverURL = c.CoversURL + "/b/isbn/" + isbn + "-L.jpg?default=false"
	}
	return rec, nil
}

// Search runs a title/author search and returns the best hit (the first
// document with a title, preferring one that carries an ISBN). It returns
// (nil, nil) when nothing matches.
func (c *Client) Search(ctx context.Context, title, author string) (*Record, error) {
	title, author = strings.TrimSpace(title), strings.TrimSpace(author)
	if title == "" && author == "" {
		return nil, nil
	}
	q := url.Values{"limit": {"5"}}
	if title != "" {
		q.Set("title", title)
	}
	if author != "" {
		q.Set("author", author)
	}
	var payload struct {
		Docs []struct {
			Title            string   `json:"title"`
			AuthorName       []string `json:"author_name"`
			FirstPublishYear *int     `json:"first_publish_year"`
			ISBN             []string `json:"isbn"`
			Publisher        []string `json:"publisher"`
			Subject          []string `json:"subject"`
			CoverID          *int64   `json:"cover_i"`
		} `json:"docs"`
	}
	if err := c.getJSON(ctx, "/search.json?"+q.Encode(), &payload); err != nil {
		return nil, err
	}
	best := -1
	for i, d := range payload.Docs {
		if d.Title == "" {
			continue
		}
		if best < 0 {
			best = i
		}
		if firstISBN13(d.ISBN) != "" {
			best = i
			break
		}
	}
	if best < 0 {
		return nil, nil
	}
	d := payload.Docs[best]
	rec := &Record{Title: d.Title, Year: d.FirstPublishYear, ISBN13: firstISBN13(d.ISBN)}
	for _, a := range d.AuthorName {
		rec.Authors = appendNonEmpty(rec.Authors, a)
	}
	for _, p := range d.Publisher {
		if len(rec.Publishers) >= 3 {
			break
		}
		rec.Publishers = appendNonEmpty(rec.Publishers, p)
	}
	for _, s := range d.Subject {
		if len(rec.Subjects) >= maxSubjects {
			break
		}
		rec.Subjects = appendNonEmpty(rec.Subjects, s)
	}
	switch {
	case d.CoverID != nil && *d.CoverID > 0:
		rec.CoverURL = fmt.Sprintf("%s/b/id/%d-L.jpg?default=false", c.CoversURL, *d.CoverID)
	case rec.ISBN13 != "":
		rec.CoverURL = c.CoversURL + "/b/isbn/" + rec.ISBN13 + "-L.jpg?default=false"
	}
	return rec, nil
}

func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "alexandria/1.0 (https://github.com/bmurray/alexandria)")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("openlibrary: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openlibrary: %s returned %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v); err != nil {
		return fmt.Errorf("openlibrary: decode: %w", err)
	}
	return nil
}

var yearRe = regexp.MustCompile(`\b(1[5-9]\d{2}|20\d{2})\b`)

// yearFrom pulls a four digit year out of free-form publish dates such as
// "May 1, 1999" or "1999".
func yearFrom(s string) *int {
	m := yearRe.FindString(s)
	if m == "" {
		return nil
	}
	var y int
	fmt.Sscanf(m, "%d", &y)
	return &y
}

// NormalizeISBN strips hyphens and spaces and upper-cases a trailing X. It
// returns "" when the result is not 10 or 13 characters.
func NormalizeISBN(s string) string {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
	if len(s) != 10 && len(s) != 13 {
		return ""
	}
	return s
}

func firstISBN13(list []string) string {
	for _, s := range list {
		if n := NormalizeISBN(s); len(n) == 13 {
			return n
		}
	}
	return ""
}

func appendNonEmpty(list []string, s string) []string {
	if s = strings.TrimSpace(s); s != "" {
		list = append(list, s)
	}
	return list
}
