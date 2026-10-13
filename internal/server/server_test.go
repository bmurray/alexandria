package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestSPAFallbackAndAPI(t *testing.T) {
	static := fstest.MapFS{
		"index.html":      {Data: []byte("<html>shell</html>")},
		"assets/app.1.js": {Data: []byte("js")},
	}
	s, err := New(Config{Port: 0, ReadHeaderTimeout: time.Second, ShutdownTimeout: time.Second}, nil, func(api *http.ServeMux) {
		api.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) })
	}, static)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	for _, tc := range []struct {
		path, want string
		status     int
	}{
		{"/", "shell", 200},
		{"/books/abc", "shell", 200},
		{"/assets/app.1.js", "js", 200},
		{"/api/ping", `"ok":true`, 200},
		{"/api/nope", "not found", 404},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s: %d %q", tc.path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/assets/app.1.js", nil))
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatal("assets should be immutable")
	}
}
