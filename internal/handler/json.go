// Package handler contains the HTTP API handlers, one file per area.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
)

const maxBody = 1 << 20

// Decode reads a JSON body into v, rejecting unknown fields and oversized bodies.
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// WriteJSON writes v as JSON with status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// ErrorBody is the JSON error shape.
type ErrorBody struct {
	Error    string   `json:"error"`
	Problems []string `json:"problems,omitempty"`
}

// WriteError writes {"error": msg}.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, ErrorBody{Error: msg})
}

// WriteProblems writes a 400 with per-field validation messages.
func WriteProblems(w http.ResponseWriter, problems []string) {
	WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "validation failed", Problems: problems})
}

// StatusError carries an HTTP status with an error; services return these for
// client mistakes (404, 409, 403) and the handlers map them.
type StatusError struct {
	Status int
	Msg    string
}

func (e *StatusError) Error() string { return e.Msg }

// NewStatusError builds a StatusError.
func NewStatusError(status int, msg string) error { return &StatusError{Status: status, Msg: msg} }

// WriteServiceError maps a StatusError to its status; anything else is a 500
// with the detail logged, never shown.
func WriteServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var se *StatusError
	if errors.As(err, &se) {
		WriteError(w, se.Status, se.Msg)
		return
	}
	slog.ErrorContext(r.Context(), "request failed", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("error", err.Error()))
	WriteError(w, http.StatusInternalServerError, "internal error")
}

// QueryInt reads an integer query parameter with a default and bounds.
func QueryInt(r *http.Request, name string, def, min, max int) int {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
