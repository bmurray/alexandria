// Package library is the domain: books, loans, holds, search and the
// librarian dashboard, backed by SQLite.
package library

// Options are the library settings as plain values.
type Options struct {
	Name           string
	LoanDays       int
	HoldPickupDays int
	SeedOnEmpty    bool
}
