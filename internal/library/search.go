package library

import (
	"context"
	"strings"
	"unicode"
)

// ftsQuery turns free text into a safe FTS5 MATCH expression. Each
// whitespace-separated term is reduced to its letters and digits, quoted, and
// given a trailing * so "herb" matches "Herbert"; terms are implicitly ANDed.
// Quoting means user input can never reach the FTS5 query parser as syntax.
// An empty result means "no text filter".
func ftsQuery(text string) string {
	var terms []string
	for _, f := range strings.Fields(text) {
		ws := words(f)
		switch len(ws) {
		case 0:
		case 1:
			terms = append(terms, `"`+ws[0]+`"*`)
		default:
			// A field with separators ("978-0-441-01359-3", "Catch-22") must
			// match both the stored joined form (ISBNs are normalised) and the
			// tokenizer's split form.
			var split []string
			for _, w := range ws {
				split = append(split, `"`+w+`"*`)
			}
			terms = append(terms, `("`+strings.Join(ws, "")+`"* OR (`+strings.Join(split, " ")+`))`)
		}
	}
	return strings.Join(terms, " ")
}

// words splits text into its alphanumeric runs.
func words(text string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// likeTerms is the LIKE fallback used when FTS fails: one %term% per word.
func likeTerms(text string) []string {
	var out []string
	for _, w := range words(text) {
		out = append(out, "%"+w+"%")
	}
	return out
}

// orderBy maps a sort key to SQL. Ties break on title then id so paging is
// stable.
func orderBy(sort string) string {
	switch sort {
	case SortAuthor:
		return "b.author COLLATE NOCASE, b.title COLLATE NOCASE, b.id"
	case SortYear:
		return "b.year IS NULL, b.year DESC, b.title COLLATE NOCASE, b.id"
	case SortAdded:
		return "b.created_at DESC, b.id"
	default:
		return "b.title COLLATE NOCASE, b.id"
	}
}

// listBooks runs a catalog query. With useFTS the text (if any) goes through
// books_fts MATCH and results are ranked by bm25; otherwise title/author LIKE
// is used and results follow q.Sort.
func listBooks(ctx context.Context, db dbtx, q Query, useFTS bool) (Page[Book], error) {
	from := "FROM books b"
	conds := []string{"1 = 1"}
	var args []any
	searching := false

	if q.Text != "" {
		if useFTS {
			if match := ftsQuery(q.Text); match != "" {
				from += " JOIN books_fts ON books_fts.rowid = b.rowid"
				conds = append(conds, "books_fts MATCH ?")
				args = append(args, match)
				searching = true
			}
		} else {
			for _, t := range likeTerms(q.Text) {
				conds = append(conds, "(b.title LIKE ? OR b.author LIKE ? OR b.isbn LIKE ?)")
				args = append(args, t, t, t)
			}
		}
	}
	if q.Genre != "" {
		conds = append(conds, "b.genre = ? COLLATE NOCASE")
		args = append(args, q.Genre)
	}
	if q.AvailableOnly {
		conds = append(conds, "b.copies - "+openLoansOn+" > 0")
	}
	where := " WHERE " + strings.Join(conds, " AND ")

	var total int
	if err := db.QueryRowContext(ctx, "SELECT count(*) "+from+where, args...).Scan(&total); err != nil {
		return Page[Book]{}, err
	}

	order := " ORDER BY " + orderBy(q.Sort)
	if searching {
		order = " ORDER BY bm25(books_fts), b.title COLLATE NOCASE, b.id"
	}
	rows, err := db.QueryContext(ctx, "SELECT "+bookColumns+" "+from+where+order+" LIMIT ? OFFSET ?",
		append(args, q.Limit, q.Offset)...)
	if err != nil {
		return Page[Book]{}, err
	}
	defer rows.Close()
	items := []Book{}
	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return Page[Book]{}, err
		}
		items = append(items, *b)
	}
	if err := rows.Err(); err != nil {
		return Page[Book]{}, err
	}
	return Page[Book]{Items: items, Total: total}, nil
}
