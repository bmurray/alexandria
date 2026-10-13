package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bmurray/alexandria/internal/auth"
)

func TestRequireRole(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
	h := RequireRole(auth.RoleLibrarian, ok)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h(rec, req.WithContext(WithUser(req.Context(), &auth.User{Role: auth.RoleMember})))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h(rec, req.WithContext(WithUser(req.Context(), &auth.User{Role: auth.RoleAdmin})))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin: %d", rec.Code)
	}
}

func TestJSONOnly(t *testing.T) {
	h := JSONOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/x", strings.NewReader("a=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/x", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("json: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/api/x", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site: %d", rec.Code)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	if ClientIP(r) != "10.0.0.1" {
		t.Fatal(ClientIP(r))
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	if ClientIP(r) != "203.0.113.5" {
		t.Fatal(ClientIP(r))
	}
}
