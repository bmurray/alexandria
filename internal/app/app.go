// Package app wires configuration to services and runs them until the root
// context ends. It is the only package that imports internal/gen: every other
// package takes plain Go values, so a schema change surfaces here only.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"

	"github.com/bmurray/alexandria/internal/config"
	"github.com/bmurray/alexandria/internal/credential"
	"github.com/bmurray/alexandria/internal/db"
	"github.com/bmurray/alexandria/internal/gen/appconfig"
	"github.com/bmurray/alexandria/internal/handler"
	"github.com/bmurray/alexandria/internal/logging"
	"github.com/bmurray/alexandria/internal/otel"
	"github.com/bmurray/alexandria/internal/realtime"
	"github.com/bmurray/alexandria/internal/server"
	"github.com/bmurray/alexandria/web"
)

// Run loads CONFIG_FILE, builds every service and serves until ctx ends.
func Run(ctx context.Context, version string) error {
	slog.SetDefault(logging.New(slog.LevelInfo))
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(ctx, path)
	if err != nil {
		return err
	}
	level := logging.ParseLevel(cfg.LogLevel)
	slog.SetDefault(logging.New(level))

	tel, otelShutdown, err := otel.Setup(ctx, otelConfig(cfg), version)
	if err != nil {
		return err
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := otelShutdown(sctx); err != nil {
			slog.Warn("otel shutdown", slog.String("error", err.Error()))
		}
	}()
	if tel != nil {
		slog.SetDefault(logging.New(level, otelslog.NewHandler(cfg.Otel.ServiceName, otelslog.WithLoggerProvider(tel.LoggerProvider))))
		slog.Info("opentelemetry enabled", slog.String("endpoint", cfg.Otel.Endpoint))
	}
	slog.Info("starting alexandria", slog.String("version", version), slog.String("config", path))

	creds := credential.NewResolver()
	database, err := db.Open(ctx, cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer database.Close()
	if cfg.Database.MigrateOnStart {
		if err := db.Migrate(ctx, database); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	hub := &realtime.Hub{Origins: origins(cfg)}

	deps, err := buildServices(ctx, cfg, database, creds, hub)
	if err != nil {
		return err
	}

	srv, err := server.New(serverConfig(cfg), deps.auth, func(api *http.ServeMux) {
		api.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
			handler.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
		})
		api.HandleFunc("GET /api/readyz", func(w http.ResponseWriter, r *http.Request) {
			if err := database.PingContext(r.Context()); err != nil {
				handler.WriteError(w, http.StatusServiceUnavailable, "database unavailable")
				return
			}
			handler.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		})
		deps.register(api)
	}, web.FS())
	if err != nil {
		return err
	}
	return srv.Run(ctx)
}

func serverConfig(cfg *appconfig.AppConfig) server.Config {
	sc := server.Config{
		Host:              cfg.Server.Host,
		Port:              int(cfg.Server.Port),
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeoutSeconds) * time.Second,
		ShutdownTimeout:   time.Duration(cfg.Server.ShutdownTimeoutSeconds) * time.Second,
	}
	if cfg.Server.DevProxy != nil {
		sc.DevProxy = *cfg.Server.DevProxy
	}
	return sc
}

func otelConfig(cfg *appconfig.AppConfig) otel.Config {
	return otel.Config{
		Enabled:          cfg.Otel.Enabled,
		Endpoint:         cfg.Otel.Endpoint,
		Insecure:         cfg.Otel.Insecure,
		ServiceName:      cfg.Otel.ServiceName,
		Environment:      cfg.Otel.Environment,
		TraceSampleRatio: cfg.Otel.TraceSampleRatio,
		MetricInterval:   time.Duration(cfg.Otel.MetricIntervalSeconds) * time.Second,
	}
}

// origins are the browser origins allowed to open WebSockets: the public URL
// plus the Vite dev server when proxying.
func origins(cfg *appconfig.AppConfig) []string {
	out := []string{strings.TrimRight(cfg.Server.PublicUrl, "/")}
	if cfg.Server.DevProxy != nil && *cfg.Server.DevProxy != "" {
		out = append(out, *cfg.Server.DevProxy)
	}
	for _, h := range cfg.Server.Hostnames {
		out = append(out, "https://"+h)
	}
	return out
}
