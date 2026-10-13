// Package server assembles the HTTP mux: API routes, the embedded SPA (or the
// Vite dev proxy) and the middleware chain, then runs it with graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/bmurray/alexandria/internal/middleware"
)

// Config is the server block as plain values.
type Config struct {
	Host              string
	Port              int
	DevProxy          string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
}

// Server is the configured HTTP server.
type Server struct {
	cfg  Config
	http *http.Server
}

// New builds the handler chain. register mounts the API routes on a mux that
// is served under /api with the JSON-only guard. auth may be nil (no sessions).
// staticFS is the built web app or nil in dev when DevProxy is set.
func New(cfg Config, auth middleware.Authenticator, register func(api *http.ServeMux), staticFS fs.FS) (*Server, error) {
	api := http.NewServeMux()
	register(api)
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})

	root := http.NewServeMux()
	root.Handle("/api/", middleware.JSONOnly(api))

	if cfg.DevProxy != "" {
		target, err := url.Parse(cfg.DevProxy)
		if err != nil {
			return nil, fmt.Errorf("server.devProxy: %w", err)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "dev server unavailable: "+err.Error()+"\nrun `make web-dev`", http.StatusBadGateway)
		}
		root.Handle("/", proxy)
		slog.Info("proxying frontend to dev server", slog.String("target", target.String()))
	} else {
		if staticFS == nil {
			return nil, errors.New("no embedded web assets and no server.devProxy configured (build without -tags dev)")
		}
		root.Handle("/", spaHandler(staticFS))
	}

	var h http.Handler = root
	if auth != nil {
		h = middleware.Session(auth)(h)
	}
	h = middleware.Log(h)
	h = middleware.Recover(h)
	h = otelhttp.NewHandler(h, "http.server", otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			return r.Method + " " + r.URL.Path
		}
		return r.Method + " static"
	}))

	return &Server{
		cfg: cfg,
		http: &http.Server{
			Addr:              net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port)),
			Handler:           h,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		},
	}, nil
}

// Handler exposes the full chain (tests).
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Run serves until ctx is cancelled, then drains within ShutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
	s.http.BaseContext = func(net.Listener) context.Context { return ctx }
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", slog.String("addr", s.http.Addr))
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancel()
	return s.http.Shutdown(shutdownCtx)
}

// spaHandler serves files from the built app and falls back to index.html for
// client-side routes. Hashed assets are cached for a year; the shell is not.
func spaHandler(staticFS fs.FS) http.Handler {
	fileServer := http.FileServerFS(staticFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := staticFS.Open(p); err == nil {
			st, serr := f.Stat()
			f.Close()
			if serr == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, staticFS, "index.html")
	})
}
