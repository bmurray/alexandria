package library

import (
	"encoding/json"
	"strings"
	"time"
)

// Book is a catalog record with its derived availability. Times are UTC.
type Book struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	ISBN        string    `json:"isbn"`
	Year        *int      `json:"year"`
	Genre       string    `json:"genre"`
	Publisher   string    `json:"publisher"`
	Description string    `json:"description"`
	CoverURL    string    `json:"coverUrl"`
	Copies      int       `json:"copies"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`

	// Derived on read, never stored.
	Available    int `json:"available"`    // copies minus open loans (never negative)
	OnLoan       int `json:"onLoan"`       // open loans
	HoldsWaiting int `json:"holdsWaiting"` // holds in the waiting queue
}

// BookInput is the body of POST /api/books.
type BookInput struct {
	Title       string   `json:"title"`
	Author      string   `json:"author"`
	ISBN        string   `json:"isbn"`
	Year        *int     `json:"year"`
	Genre       string   `json:"genre"`
	Publisher   string   `json:"publisher"`
	Description string   `json:"description"`
	CoverURL    string   `json:"coverUrl"`
	Copies      int      `json:"copies"`
	Tags        []string `json:"tags"`
}

// OptionalInt is a nullable integer that also remembers whether the JSON
// field was present at all, so a PATCH can tell "clear the year" (null) from
// "leave it alone" (absent).
type OptionalInt struct {
	Set   bool // the field appeared in the JSON body
	Value *int // nil when the field was null
}

// UnmarshalJSON records presence and accepts null or an integer.
func (o *OptionalInt) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	o.Value = &n
	return nil
}

// MarshalJSON writes the value or null.
func (o OptionalInt) MarshalJSON() ([]byte, error) {
	if o.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*o.Value)
}

// BookPatch is the body of PATCH /api/books/{id}: every field is optional and
// only the fields present are changed.
type BookPatch struct {
	Title       *string     `json:"title"`
	Author      *string     `json:"author"`
	ISBN        *string     `json:"isbn"`
	Year        OptionalInt `json:"year"`
	Genre       *string     `json:"genre"`
	Publisher   *string     `json:"publisher"`
	Description *string     `json:"description"`
	CoverURL    *string     `json:"coverUrl"`
	Copies      *int        `json:"copies"`
	Tags        *[]string   `json:"tags"`
}

// UserRef is the user summary embedded in loans and holds.
type UserRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// BookRef is the book summary embedded in loans and holds.
type BookRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	CoverURL string `json:"coverUrl"`
}

// Loan is one check-out of one copy by one member.
type Loan struct {
	ID           string     `json:"id"`
	BookID       string     `json:"bookId"`
	UserID       string     `json:"userId"`
	CheckedOutAt time.Time  `json:"checkedOutAt"`
	DueAt        time.Time  `json:"dueAt"`
	ReturnedAt   *time.Time `json:"returnedAt"`
	Overdue      bool       `json:"overdue"` // derived: open and past due
	Book         BookRef    `json:"book"`
	User         UserRef    `json:"user"`
}

// Open reports whether the loan has not been returned.
func (l *Loan) Open() bool { return l.ReturnedAt == nil }

// Hold statuses.
const (
	HoldWaiting   = "waiting"   // queued behind other holds or open loans
	HoldReady     = "ready"     // a copy is reserved for this member
	HoldFulfilled = "fulfilled" // the member checked the book out
	HoldCancelled = "cancelled" // the member (or a librarian) cancelled it
	HoldLapsed    = "lapsed"    // the member did not pick it up in time
)

// Hold is a member's place in the queue for a book.
type Hold struct {
	ID       string     `json:"id"`
	BookID   string     `json:"bookId"`
	UserID   string     `json:"userId"`
	PlacedAt time.Time  `json:"placedAt"`
	ReadyAt  *time.Time `json:"readyAt"`
	Status   string     `json:"status"`
	Position int        `json:"position"` // 1-based rank among active holds; 0 once inactive
	Book     BookRef    `json:"book"`
	User     UserRef    `json:"user"`
}

// Active reports whether the hold still occupies a place in the queue.
func (h *Hold) Active() bool { return h.Status == HoldWaiting || h.Status == HoldReady }

// Sort keys for Query.Sort.
const (
	SortTitle  = "title"
	SortAuthor = "author"
	SortYear   = "year"  // newest first, unknown years last
	SortAdded  = "added" // most recently added first
)

// Query describes a catalog listing.
type Query struct {
	Text          string // free text over title, author, ISBN, genre, tags, description
	Genre         string // exact genre (case-insensitive)
	AvailableOnly bool
	Sort          string // title (default) | author | year | added; ignored while searching (rank wins)
	Limit, Offset int
}

// Page is one page of a listing with the unpaged total.
type Page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// LoanFilter narrows ListLoans.
type LoanFilter struct {
	Status        string // open | overdue | returned | "" (all)
	UserID        string
	Limit, Offset int
}

// Loan filter statuses.
const (
	LoanOpen     = "open"
	LoanOverdue  = "overdue"
	LoanReturned = "returned"
)

// Dashboard is the librarian overview.
type Dashboard struct {
	Books        int    `json:"books"`
	Copies       int    `json:"copies"`
	OpenLoans    int    `json:"openLoans"`
	Overdue      int    `json:"overdue"`
	HoldsWaiting int    `json:"holdsWaiting"`
	Members      int    `json:"members"`
	RecentLoans  []Loan `json:"recentLoans"`
	OverdueLoans []Loan `json:"overdueLoans"`
	ReadyHolds   []Hold `json:"readyHolds"`
}

// Genre is a genre name with the number of books in it.
type Genre struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ValidationError lists what is wrong with an input. Handlers map it to a
// 400 with the problems.
type ValidationError struct {
	Problems []string
}

func (e ValidationError) Error() string {
	return "validation failed: " + strings.Join(e.Problems, "; ")
}
