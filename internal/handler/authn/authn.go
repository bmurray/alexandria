// Package authn serves the authentication endpoints: the public config,
// the current user, logout, the OIDC start/callback legs, the Dev sign-in and
// the admin-only user list and role changes. See docs/api/auth.md.
package authn

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/bmurray/alexandria/internal/auth"
	"github.com/bmurray/alexandria/internal/handler"
	"github.com/bmurray/alexandria/internal/middleware"
)

// callbackPath is the web app route that finishes a sign-in.
const callbackPath = "/auth/callback"

// Handler holds the auth service and the values GET /api/config exposes.
type Handler struct {
	svc         *auth.Service
	libraryName string
	aiEnabled   bool
	loanDays    int
}

// New builds the handler. libraryName, aiEnabled and loanDays are published
// by GET /api/config so the web app can render before anyone signs in.
func New(svc *auth.Service, libraryName string, aiEnabled bool, loanDays int) *Handler {
	return &Handler{svc: svc, libraryName: libraryName, aiEnabled: aiEnabled, loanDays: loanDays}
}

// Register mounts the routes on mux. The session middleware must already
// wrap mux so middleware.UserFrom works.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config", h.config)
	mux.HandleFunc("GET /api/auth/me", h.me)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("GET /api/auth/oidc/{provider}/start", h.oidcStart)
	mux.HandleFunc("GET /api/auth/oidc/{provider}/callback", h.oidcCallback)
	mux.HandleFunc("POST /api/auth/dev/login", h.devLogin)
	mux.HandleFunc("GET /api/users", middleware.RequireRole(auth.RoleAdmin, h.listUsers))
	mux.HandleFunc("PATCH /api/users/{id}", middleware.RequireRole(auth.RoleAdmin, h.setRole))
}

// configResponse is the body of GET /api/config.
type configResponse struct {
	LibraryName string              `json:"libraryName"`
	Providers   []auth.ProviderInfo `json:"providers"`
	AIEnabled   bool                `json:"aiEnabled"`
	LoanDays    int                 `json:"loanDays"`
}

func (h *Handler) config(w http.ResponseWriter, r *http.Request) {
	handler.WriteJSON(w, http.StatusOK, configResponse{
		LibraryName: h.libraryName,
		Providers:   h.svc.Providers(),
		AIEnabled:   h.aiEnabled,
		LoanDays:    h.loanDays,
	})
}

type userResponse struct {
	User *auth.User `json:"user"`
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r.Context())
	if u == nil {
		writeError(w, r, auth.ErrUnauthenticated)
		return
	}
	handler.WriteJSON(w, http.StatusOK, userResponse{User: u})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if token := middleware.TokenFrom(r.Context()); token != "" {
		if err := h.svc.Logout(r.Context(), token); err != nil {
			writeError(w, r, err)
			return
		}
	}
	h.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) oidcStart(w http.ResponseWriter, r *http.Request) {
	redirect, err := h.svc.OIDCStart(r.Context(), r.PathValue("provider"), r.URL.Query().Get("returnTo"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

// oidcCallback finishes the flow and always lands on the web app's
// /auth/callback route: with returnTo on success, with error otherwise. The
// browser arrived here from the IdP, so a JSON body would be a dead end.
func (h *Handler) oidcCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if msg := q.Get("error"); msg != "" {
		// The IdP declined (user cancelled, consent denied, ...).
		if d := q.Get("error_description"); d != "" {
			msg = d
		}
		h.redirectError(w, r, msg)
		return
	}
	u, returnTo, err := h.svc.OIDCCallback(r.Context(), r.PathValue("provider"), q.Get("state"), q.Get("code"))
	if err != nil {
		h.redirectError(w, r, publicMessage(r, err))
		return
	}
	if err := h.startSession(w, r, u); err != nil {
		h.redirectError(w, r, publicMessage(r, err))
		return
	}
	http.Redirect(w, r, callbackPath+"?returnTo="+url.QueryEscape(returnTo), http.StatusFound)
}

func (h *Handler) redirectError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, callbackPath+"?error="+url.QueryEscape(msg), http.StatusFound)
}

// devLoginRequest is the body of POST /api/auth/dev/login.
type devLoginRequest struct {
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Role  auth.Role `json:"role"`
}

func (h *Handler) devLogin(w http.ResponseWriter, r *http.Request) {
	if !h.svc.DevLoginEnabled() {
		// Indistinguishable from an unknown route on purpose.
		writeError(w, r, auth.ErrDevLoginDisabled)
		return
	}
	var in devLoginRequest
	if err := handler.Decode(r, &in); err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	u, err := h.svc.DevLogin(r.Context(), in.Email, in.Name, in.Role)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.startSession(w, r, u); err != nil {
		writeError(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, userResponse{User: u})
}

type usersResponse struct {
	Users []auth.User `json:"users"`
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.ListUsers(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, usersResponse{Users: users})
}

// setRoleRequest is the body of PATCH /api/users/{id}.
type setRoleRequest struct {
	Role auth.Role `json:"role"`
}

func (h *Handler) setRole(w http.ResponseWriter, r *http.Request) {
	var in setRoleRequest
	if err := handler.Decode(r, &in); err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	u, err := h.svc.SetRole(r.Context(), middleware.UserFrom(r.Context()), r.PathValue("id"), in.Role)
	if err != nil {
		writeError(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, userResponse{User: u})
}

// --- cookies ---------------------------------------------------------------

// startSession creates a session for u and sets the cookie.
func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	token, _, err := h.svc.CreateSession(r.Context(), u, r.UserAgent(), middleware.ClientIP(r))
	if err != nil {
		return err
	}
	http.SetCookie(w, h.cookie(token, int(h.svc.SessionDuration().Seconds())))
	return nil
}

func (h *Handler) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, h.cookie("", -1))
}

// cookie builds the session cookie. HttpOnly keeps scripts away from the
// token, SameSite=Lax plus the JSON-only guard covers CSRF, and Secure is on
// whenever the public URL is https.
func (h *Handler) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     h.svc.CookieName(),
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.svc.CookieSecure(),
		SameSite: http.SameSiteLaxMode,
	}
}

// --- errors ----------------------------------------------------------------

// statusOf maps the auth sentinel errors to HTTP statuses. Unknown errors
// are 500 and their detail stays in the log.
func statusOf(err error) (int, bool) {
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		return http.StatusUnauthorized, true
	case errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrEmailNotAllowed):
		return http.StatusForbidden, true
	case errors.Is(err, auth.ErrNotFound), errors.Is(err, auth.ErrUnknownProvider), errors.Is(err, auth.ErrDevLoginDisabled):
		return http.StatusNotFound, true
	case errors.Is(err, auth.ErrInvalidEmail), errors.Is(err, auth.ErrInvalidRole), errors.Is(err, auth.ErrInvalidReturnTo):
		return http.StatusBadRequest, true
	case errors.Is(err, auth.ErrSelfDemotion):
		return http.StatusConflict, true
	case errors.Is(err, auth.ErrChallengeExpired):
		return http.StatusGone, true
	case errors.Is(err, auth.ErrExchangeFailed), errors.Is(err, auth.ErrProviderUnavailable):
		return http.StatusBadGateway, true
	}
	var se *handler.StatusError
	if errors.As(err, &se) {
		return se.Status, true
	}
	return http.StatusInternalServerError, false
}

// writeError answers err as JSON with the mapped status. Unknown errors are
// logged and answered as a bare 500.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, known := statusOf(err)
	if !known {
		logUnknown(r, err)
		handler.WriteError(w, status, "internal error")
		return
	}
	handler.WriteError(w, status, sentinelText(err))
}

// publicMessage is the error text safe to show a person: the sentinel's
// message for known errors, a generic line (with the detail logged) otherwise.
func publicMessage(r *http.Request, err error) string {
	if _, known := statusOf(err); known {
		return sentinelText(err)
	}
	logUnknown(r, err)
	return "sign-in failed"
}

// sentinelText returns the message of the outermost auth sentinel in err's
// chain, so wrapped detail (token endpoint bodies, SQL errors) never leaks.
func sentinelText(err error) string {
	for _, s := range []error{
		auth.ErrUnauthenticated, auth.ErrForbidden, auth.ErrEmailNotAllowed, auth.ErrNotFound,
		auth.ErrUnknownProvider, auth.ErrDevLoginDisabled, auth.ErrInvalidEmail, auth.ErrInvalidRole,
		auth.ErrInvalidReturnTo, auth.ErrSelfDemotion, auth.ErrChallengeExpired, auth.ErrExchangeFailed,
		auth.ErrProviderUnavailable,
	} {
		if errors.Is(err, s) {
			return s.Error()
		}
	}
	var se *handler.StatusError
	if errors.As(err, &se) {
		return se.Msg
	}
	return strings.TrimSpace(err.Error())
}

func logUnknown(r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "auth request failed",
		slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("error", err.Error()))
}
