// Package books serves the catalog, loan and hold endpoints under /api.
// Rules live in internal/library; this package only parses requests, applies
// role checks that depend on the caller, and shapes responses.
package books

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/bmurray/alexandria/internal/auth"
	"github.com/bmurray/alexandria/internal/handler"
	"github.com/bmurray/alexandria/internal/library"
	"github.com/bmurray/alexandria/internal/middleware"
	"github.com/bmurray/alexandria/internal/realtime"
)

// Page size bounds for the two paged listings.
const (
	defaultBookLimit = 24
	maxBookLimit     = 100
	defaultLoanLimit = 50
	maxLoanLimit     = 200
)

// Handler holds the dependencies of the catalog endpoints.
type Handler struct {
	svc *library.Service
	hub *realtime.Hub // nil disables streaming; GET /api/books is then plain JSON
}

// New builds the handler.
func New(svc *library.Service, hub *realtime.Hub) *Handler {
	return &Handler{svc: svc, hub: hub}
}

// Register mounts every route on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	librarian := func(fn http.HandlerFunc) http.HandlerFunc { return middleware.RequireRole(auth.RoleLibrarian, fn) }
	member := middleware.RequireUser

	mux.HandleFunc("GET /api/books", h.listBooks)
	mux.HandleFunc("GET /api/books/{id}", h.getBook)
	mux.HandleFunc("POST /api/books", librarian(h.createBook))
	mux.HandleFunc("PATCH /api/books/{id}", librarian(h.updateBook))
	mux.HandleFunc("DELETE /api/books/{id}", librarian(h.deleteBook))
	mux.HandleFunc("GET /api/genres", h.genres)

	mux.HandleFunc("POST /api/books/{id}/checkout", member(h.checkout))
	mux.HandleFunc("POST /api/loans/{id}/return", member(h.returnLoan))
	mux.HandleFunc("GET /api/loans", member(h.listLoans))
	mux.HandleFunc("GET /api/me/loans", member(h.myLoans))
	mux.HandleFunc("GET /api/me/holds", member(h.myHolds))

	mux.HandleFunc("POST /api/books/{id}/holds", member(h.placeHold))
	mux.HandleFunc("DELETE /api/holds/{id}", member(h.cancelHold))
	mux.HandleFunc("GET /api/holds", librarian(h.listHolds))
	mux.HandleFunc("GET /api/dashboard", librarian(h.dashboard))
}

// writeErr maps a service error: validation problems become a 400 with the
// list, StatusErrors keep their status, anything else is a logged 500.
func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var ve library.ValidationError
	if errors.As(err, &ve) {
		handler.WriteProblems(w, ve.Problems)
		return
	}
	handler.WriteServiceError(w, r, err)
}

// decode reads a JSON body. With allowEmpty an absent body is fine.
func decode(w http.ResponseWriter, r *http.Request, v any, allowEmpty bool) bool {
	err := handler.Decode(r, v)
	if err == nil || (allowEmpty && errors.Is(err, io.EOF)) {
		return true
	}
	handler.WriteError(w, http.StatusBadRequest, "invalid JSON body")
	return false
}

func isLibrarian(u *auth.User) bool { return u != nil && u.Role.AtLeast(auth.RoleLibrarian) }

// ---- books --------------------------------------------------------------

// booksPage is the GET /api/books snapshot.
type booksPage struct {
	Books []library.Book `json:"books"`
	Total int            `json:"total"`
}

func queryFrom(r *http.Request) library.Query {
	v := r.URL.Query()
	available, _ := strconv.ParseBool(v.Get("available"))
	return library.Query{
		Text:          v.Get("q"),
		Genre:         v.Get("genre"),
		AvailableOnly: available,
		Sort:          v.Get("sort"),
		Limit:         handler.QueryInt(r, "limit", defaultBookLimit, 1, maxBookLimit),
		Offset:        handler.QueryInt(r, "offset", 0, 0, 1<<30),
	}
}

// listBooks answers a page of the catalog; with Upgrade: websocket it streams
// the page followed by a "books" event per change.
func (h *Handler) listBooks(w http.ResponseWriter, r *http.Request) {
	q := queryFrom(r)
	snapshot := func(ctx context.Context) (any, error) {
		page, err := h.svc.ListBooks(ctx, q)
		if err != nil {
			return nil, err
		}
		return booksPage{Books: page.Items, Total: page.Total}, nil
	}
	if h.hub == nil {
		data, err := snapshot(r.Context())
		if err != nil {
			writeErr(w, r, err)
			return
		}
		handler.WriteJSON(w, http.StatusOK, data)
		return
	}
	h.hub.Serve(w, r, realtime.Options{Topics: []string{library.Topic}, Snapshot: snapshot, OnError: writeErr})
}

// bookDetail is the GET /api/books/{id} shape.
type bookDetail struct {
	Book  *library.Book  `json:"book"`
	Loans []library.Loan `json:"loans"`
	Holds []library.Hold `json:"holds"`
}

// getBook returns the book with the loans and holds the caller may see:
// every open loan and active hold for librarians, only the caller's own for
// members, none for anonymous visitors.
func (h *Handler) getBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := h.svc.GetBook(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := bookDetail{Book: b, Loans: []library.Loan{}, Holds: []library.Hold{}}
	u := middleware.UserFrom(ctx)
	switch {
	case isLibrarian(u):
		if out.Loans, err = h.svc.BookLoans(ctx, b.ID); err != nil {
			writeErr(w, r, err)
			return
		}
		if out.Holds, err = h.svc.BookHolds(ctx, b.ID); err != nil {
			writeErr(w, r, err)
			return
		}
	case u != nil:
		loans, err := h.svc.UserLoans(ctx, u.ID)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		for _, l := range loans {
			if l.BookID == b.ID {
				out.Loans = append(out.Loans, l)
			}
		}
		holds, err := h.svc.UserHolds(ctx, u.ID)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		for _, hold := range holds {
			if hold.BookID == b.ID {
				out.Holds = append(out.Holds, hold)
			}
		}
	}
	handler.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createBook(w http.ResponseWriter, r *http.Request) {
	var in library.BookInput
	if !decode(w, r, &in, false) {
		return
	}
	b, err := h.svc.CreateBook(r.Context(), in, middleware.UserFrom(r.Context()).ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusCreated, map[string]any{"book": b})
}

func (h *Handler) updateBook(w http.ResponseWriter, r *http.Request) {
	var p library.BookPatch
	if !decode(w, r, &p, false) {
		return
	}
	b, err := h.svc.UpdateBook(r.Context(), r.PathValue("id"), p)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"book": b})
}

func (h *Handler) deleteBook(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteBook(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) genres(w http.ResponseWriter, r *http.Request) {
	g, err := h.svc.Genres(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"genres": g})
}

// ---- loans ---------------------------------------------------------------

// checkout lends the book to the caller, or to body.userId when a librarian
// asks for another member.
func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID string `json:"userId"`
	}
	if !decode(w, r, &body, true) {
		return
	}
	u := middleware.UserFrom(r.Context())
	userID := body.UserID
	if userID == "" {
		userID = u.ID
	}
	if userID != u.ID && !isLibrarian(u) {
		handler.WriteError(w, http.StatusForbidden, "only librarians may check out for another member")
		return
	}
	l, err := h.svc.Checkout(r.Context(), r.PathValue("id"), userID, u.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusCreated, map[string]any{"loan": l})
}

// returnLoan closes the caller's own loan, or any loan for a librarian.
func (h *Handler) returnLoan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := middleware.UserFrom(ctx)
	l, err := h.svc.GetLoan(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if l.UserID != u.ID && !isLibrarian(u) {
		handler.WriteError(w, http.StatusForbidden, "not your loan")
		return
	}
	if l, err = h.svc.Return(ctx, l.ID, u.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"loan": l})
}

// listLoans pages loans. Librarians may filter by status and userId; members
// always get their own.
func (h *Handler) listLoans(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r.Context())
	f := library.LoanFilter{
		Status: r.URL.Query().Get("status"),
		UserID: r.URL.Query().Get("userId"),
		Limit:  handler.QueryInt(r, "limit", defaultLoanLimit, 1, maxLoanLimit),
		Offset: handler.QueryInt(r, "offset", 0, 0, 1<<30),
	}
	if !isLibrarian(u) {
		f.UserID = u.ID
	}
	page, err := h.svc.ListLoans(r.Context(), f)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"loans": page.Items, "total": page.Total})
}

func (h *Handler) myLoans(w http.ResponseWriter, r *http.Request) {
	loans, err := h.svc.UserLoans(r.Context(), middleware.UserFrom(r.Context()).ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"loans": loans})
}

func (h *Handler) myHolds(w http.ResponseWriter, r *http.Request) {
	holds, err := h.svc.UserHolds(r.Context(), middleware.UserFrom(r.Context()).ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"holds": holds})
}

// ---- holds ---------------------------------------------------------------

func (h *Handler) placeHold(w http.ResponseWriter, r *http.Request) {
	hold, err := h.svc.PlaceHold(r.Context(), r.PathValue("id"), middleware.UserFrom(r.Context()).ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusCreated, map[string]any{"hold": hold})
}

// cancelHold withdraws the caller's own hold, or any hold for a librarian.
func (h *Handler) cancelHold(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := middleware.UserFrom(ctx)
	hold, err := h.svc.GetHold(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if hold.UserID != u.ID && !isLibrarian(u) {
		handler.WriteError(w, http.StatusForbidden, "not your hold")
		return
	}
	if err := h.svc.CancelHold(ctx, hold.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listHolds(w http.ResponseWriter, r *http.Request) {
	holds, err := h.svc.ListHolds(r.Context(), r.URL.Query().Get("bookId"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"holds": holds})
}

// ---- dashboard -----------------------------------------------------------

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Dashboard(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, d)
}
