package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bmurray/alexandria/internal/ai"
	"github.com/bmurray/alexandria/internal/auth"
	"github.com/bmurray/alexandria/internal/credential"
	aianthropic "github.com/bmurray/alexandria/internal/gen/ai/provider/anthropic"
	"github.com/bmurray/alexandria/internal/gen/appconfig"
	authdev "github.com/bmurray/alexandria/internal/gen/auth/provider/dev"
	authoidc "github.com/bmurray/alexandria/internal/gen/auth/provider/oidc"
	"github.com/bmurray/alexandria/internal/handler/assistant"
	"github.com/bmurray/alexandria/internal/handler/authn"
	"github.com/bmurray/alexandria/internal/handler/books"
	"github.com/bmurray/alexandria/internal/library"
	"github.com/bmurray/alexandria/internal/middleware"
	"github.com/bmurray/alexandria/internal/realtime"
)

type services struct {
	auth     middleware.Authenticator
	register func(api *http.ServeMux)
}

func buildServices(ctx context.Context, cfg *appconfig.AppConfig, database *sql.DB, creds *credential.Resolver, hub *realtime.Hub) (*services, error) {
	lib := library.NewService(database, hub, library.Options{
		Name:           cfg.Library.Name,
		LoanDays:       int(cfg.Library.LoanDays),
		HoldPickupDays: int(cfg.Library.HoldPickupDays),
		SeedOnEmpty:    cfg.Library.SeedOnEmpty,
	})
	if err := lib.Start(ctx); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}

	authCfg, err := authConfig(ctx, cfg, creds)
	if err != nil {
		return nil, err
	}
	authSvc, err := auth.NewService(ctx, database, authCfg)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	go authSvc.RunSweeper(ctx, 10*time.Minute)
	go runEvery(ctx, time.Hour, func(ctx context.Context) {
		if n, err := lib.LapseHolds(ctx, time.Now()); err != nil {
			slog.WarnContext(ctx, "lapse holds", slog.String("error", err.Error()))
		} else if n > 0 {
			slog.InfoContext(ctx, "holds lapsed", slog.Int("count", n))
		}
	})

	var aiSvc *ai.Service
	if cfg.Ai.Enabled {
		aiCfg, err := aiConfig(ctx, cfg, creds)
		if err != nil {
			return nil, err
		}
		aiSvc, err = ai.NewService(ctx, database, aiCfg, catalogAdapter{lib}, nil)
		if err != nil {
			return nil, fmt.Errorf("ai: %w", err)
		}
		slog.Info("ai configured", slog.Int("providers", len(aiCfg.Providers)))
	}
	slog.Info("auth configured", slog.Any("providers", authSvc.Providers()))

	return &services{
		auth: authSvc,
		register: func(api *http.ServeMux) {
			authn.New(authSvc, cfg.Library.Name, aiSvc != nil, int(cfg.Library.LoanDays)).Register(api)
			books.New(lib, hub).Register(api)
			if aiSvc != nil {
				assistant.New(aiSvc).Register(api)
			}
		},
	}, nil
}

func runEvery(ctx context.Context, every time.Duration, fn func(context.Context)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn(ctx)
		}
	}
}

func authConfig(ctx context.Context, cfg *appconfig.AppConfig, creds *credential.Resolver) (auth.Config, error) {
	out := auth.Config{
		PublicURL:           strings.TrimRight(cfg.Server.PublicUrl, "/"),
		CookieName:          cfg.Auth.Session.CookieName,
		SessionDuration:     time.Duration(cfg.Auth.Session.DurationHours) * time.Hour,
		SecureCookie:        cfg.Auth.Session.SecureCookie,
		Admins:              cfg.Auth.Admins,
		AllowedEmailDomains: cfg.Auth.AllowedEmailDomains,
	}
	for _, p := range cfg.Auth.Providers {
		if p == nil || !p.GetEnabled() {
			continue
		}
		switch v := p.(type) {
		case authoidc.Oidc:
			secret, err := creds.Resolve(ctx, v.GetClientSecret())
			if err != nil {
				return out, fmt.Errorf("auth.providers[%s].clientSecret: %w", v.GetName(), err)
			}
			out.OIDC = append(out.OIDC, auth.OIDCProvider{
				Name: v.GetName(), DisplayName: v.GetDisplayName(), Issuer: v.GetIssuer(),
				ClientID: v.GetClientId(), ClientSecret: secret, Scopes: v.GetScopes(), ExtraAuthParams: v.GetExtraAuthParams(),
			})
		case authdev.Dev:
			public := !strings.Contains(out.PublicURL, "localhost") && !strings.Contains(out.PublicURL, "127.0.0.1")
			if public && !v.GetAllowPublic() {
				return out, fmt.Errorf("auth.providers: the dev provider is enabled on a public hostname (%s); set allowPublic to confirm", out.PublicURL)
			}
			out.DevLogin = true
			out.DevDisplayName = v.GetDisplayName()
		default:
			return out, fmt.Errorf("auth.providers: unknown type %q", p.GetType())
		}
	}
	return out, nil
}

func aiConfig(ctx context.Context, cfg *appconfig.AppConfig, creds *credential.Resolver) (ai.Config, error) {
	out := ai.Config{
		Enabled:            cfg.Ai.Enabled,
		SystemPrompt:       cfg.Ai.SystemPrompt,
		MaxToolRounds:      int(cfg.Ai.MaxToolRounds),
		HistoryTurns:       int(cfg.Ai.HistoryTurns),
		DailySpendLimitUSD: cfg.Ai.DailySpendLimitUsd,
	}
	for _, p := range cfg.Ai.Providers {
		if p == nil {
			continue
		}
		key, err := creds.Resolve(ctx, p.GetApiKey())
		if err != nil {
			return out, fmt.Errorf("ai.providers[%s].apiKey: %w", p.GetName(), err)
		}
		pc := ai.ProviderConfig{
			Name: p.GetName(), Type: p.GetType(), Model: p.GetModel(), APIKey: key,
			InputPerMillionUSD: p.GetInputCostPerMillionUsd(), OutputPerMillionUSD: p.GetOutputCostPerMillionUsd(),
			CacheReadPerMillionUSD: p.GetCacheReadCostPerMillionUsd(),
			MaxTokens:              int(p.GetMaxTokens()), Priority: int(p.GetPriority()), SpendLimitUSD: p.GetSpendLimitUsd(),
			Enabled: p.GetEnabled(), Timeout: time.Duration(p.GetTimeoutSeconds()) * time.Second,
		}
		if a, ok := p.(aianthropic.Anthropic); ok {
			if a.GetBaseUrl() != nil {
				pc.BaseURL = *a.GetBaseUrl()
			}
			pc.Effort = a.GetEffort()
		}
		out.Providers = append(out.Providers, pc)
	}
	return out, nil
}

// catalogAdapter exposes the library to the AI tools as plain values.
type catalogAdapter struct{ lib *library.Service }

func (c catalogAdapter) SearchBooks(ctx context.Context, text, genre string, availableOnly bool, limit int) ([]ai.BookSummary, error) {
	page, err := c.lib.ListBooks(ctx, library.Query{Text: text, Genre: genre, AvailableOnly: availableOnly, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]ai.BookSummary, 0, len(page.Items))
	for _, b := range page.Items {
		out = append(out, summary(b))
	}
	return out, nil
}

func summary(b library.Book) ai.BookSummary {
	return ai.BookSummary{ID: b.ID, Title: b.Title, Author: b.Author, Year: b.Year, Genre: b.Genre, Available: b.Available, Copies: b.Copies, Tags: b.Tags}
}

func (c catalogAdapter) BookDetail(ctx context.Context, id string) (*ai.BookDetail, error) {
	b, err := c.lib.GetBook(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ai.BookDetail{BookSummary: summary(*b), ISBN: b.ISBN, Publisher: b.Publisher, Description: b.Description, CoverURL: b.CoverURL, HoldsWaiting: b.HoldsWaiting}, nil
}

func (c catalogAdapter) Genres(ctx context.Context) ([]ai.GenreCount, error) {
	gs, err := c.lib.Genres(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ai.GenreCount, 0, len(gs))
	for _, g := range gs {
		out = append(out, ai.GenreCount{Name: g.Name, Count: g.Count})
	}
	return out, nil
}

func (c catalogAdapter) UserLoans(ctx context.Context, userID string) ([]ai.LoanSummary, error) {
	loans, err := c.lib.UserLoans(ctx, userID)
	if err != nil {
		return nil, err
	}
	var out []ai.LoanSummary
	for _, l := range loans {
		if l.ReturnedAt != nil {
			continue
		}
		out = append(out, ai.LoanSummary{LoanID: l.ID, BookID: l.BookID, Title: l.Book.Title, Author: l.Book.Author, DueAt: l.DueAt, Overdue: l.Overdue})
	}
	return out, nil
}

func (c catalogAdapter) PlaceHold(ctx context.Context, bookID, userID string) (*ai.HoldSummary, error) {
	h, err := c.lib.PlaceHold(ctx, bookID, userID)
	if err != nil {
		return nil, err
	}
	return &ai.HoldSummary{HoldID: h.ID, BookID: h.BookID, Position: h.Position}, nil
}
