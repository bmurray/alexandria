package ai

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/bmurray/alexandria/internal/handler"
	"github.com/bmurray/alexandria/internal/openlibrary"
)

// Purposes recorded in the ledger.
const (
	PurposeChat   = "chat"
	PurposeEnrich = "enrich"
)

// Errors the handlers map to statuses.
var (
	ErrNoProvider  = handler.NewStatusError(http.StatusServiceUnavailable, "the librarian is away (no AI provider available)")
	ErrDailyBudget = handler.NewStatusError(http.StatusTooManyRequests, "daily AI budget reached")
)

// Service is the AI feature set: chat with tools, enrich, usage and provider
// administration. One instance serves the whole process.
type Service struct {
	cfg       Config
	providers map[string]Provider
	pricing   map[string]Pricing
	ledger    *ledger
	store     *store
	catalog   Catalog
	ol        *openlibrary.Client
	metrics   metrics
	now       func() time.Time
}

type metrics struct {
	tokens metric.Int64Counter
	cost   metric.Float64Counter
	calls  metric.Int64Counter
}

// NewService builds providers from cfg (type "anthropic"), seeds the
// ai_providers table and wires the Open Library client. hc may be nil, in
// which case the SDK and Open Library use their own defaults.
func NewService(ctx context.Context, db *sql.DB, cfg Config, catalog Catalog, hc *http.Client) (*Service, error) {
	if cfg.MaxToolRounds <= 0 {
		cfg.MaxToolRounds = 6
	}
	if cfg.HistoryTurns <= 0 {
		cfg.HistoryTurns = 20
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = defaultSystemPrompt
	}
	s := &Service{
		cfg:       cfg,
		providers: map[string]Provider{},
		pricing:   map[string]Pricing{},
		ledger:    newLedger(db, cfg.Providers),
		store:     &store{db: db, now: time.Now},
		catalog:   catalog,
		ol:        openlibrary.NewClient(hc),
		now:       time.Now,
	}
	for _, pc := range cfg.Providers {
		if _, dup := s.providers[pc.Name]; dup {
			return nil, fmt.Errorf("ai: duplicate provider name %q", pc.Name)
		}
		p, err := NewProvider(pc, hc)
		if err != nil {
			return nil, err
		}
		s.providers[pc.Name] = p
		s.pricing[pc.Name] = Pricing{InputPerM: pc.InputPerMillionUSD, OutputPerM: pc.OutputPerMillionUSD, CacheReadPerM: pc.CacheReadPerMillionUSD}
	}
	if err := s.ledger.seed(ctx, cfg.Providers); err != nil {
		return nil, err
	}
	if err := s.initMetrics(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) initMetrics() error {
	m := otel.Meter("github.com/bmurray/alexandria/internal/ai")
	var err error
	if s.metrics.tokens, err = m.Int64Counter("alexandria.ai.tokens", metric.WithDescription("Tokens billed per provider, purpose and kind")); err != nil {
		return err
	}
	if s.metrics.cost, err = m.Float64Counter("alexandria.ai.cost_usd", metric.WithDescription("Model spend in US dollars"), metric.WithUnit("USD")); err != nil {
		return err
	}
	if s.metrics.calls, err = m.Int64Counter("alexandria.ai.calls", metric.WithDescription("Model calls by result")); err != nil {
		return err
	}
	return nil
}

// Enabled reports whether at least one provider is configured.
func (s *Service) Enabled() bool { return s.cfg.Enabled && len(s.providers) > 0 }

// complete runs one priced model call: it checks the daily budget, walks the
// eligible providers by priority, falls through on error, records the call
// in the ledger and emits metrics. It returns the response and the provider
// that answered.
func (s *Service) complete(ctx context.Context, purpose, userID string, req Request) (*Response, Provider, error) {
	if !s.cfg.Enabled {
		return nil, nil, ErrNoProvider
	}
	if err := s.checkDailyBudget(ctx); err != nil {
		return nil, nil, err
	}
	states, err := s.ledger.states(ctx)
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]*ProviderState{}
	for i := range states {
		byName[states[i].Name] = &states[i]
	}
	var lastErr error
	for _, name := range Order(states, nil) {
		p := s.providers[name]
		resp, err := p.Complete(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, err
			}
			lastErr = err
			s.metrics.calls.Add(ctx, 1, metric.WithAttributes(attribute.String("provider", name), attribute.String("purpose", purpose), attribute.String("result", "error")))
			slog.WarnContext(ctx, "ai provider failed, falling through", slog.String("provider", name), slog.String("error", err.Error()))
			continue
		}
		cost := s.pricing[name].Cost(resp.Usage)
		if err := s.ledger.record(ctx, usageRecord{Provider: name, Model: p.Model(), Purpose: purpose, Usage: resp.Usage, CostUSD: cost, UserID: userID}); err != nil {
			return nil, nil, err
		}
		if st := byName[name]; st != nil && Charge(st, cost) {
			slog.WarnContext(ctx, "ai provider reached its spend cap", slog.String("provider", name), slog.Float64("spent_usd", st.SpentUSD), slog.Float64("limit_usd", *st.SpendLimitUSD))
		}
		s.emit(ctx, name, purpose, resp.Usage, cost, resp.StopReason)
		slog.InfoContext(ctx, "ai call", slog.String("provider", name), slog.String("model", p.Model()), slog.String("purpose", purpose),
			slog.Int("input_tokens", resp.Usage.InputTokens), slog.Int("output_tokens", resp.Usage.OutputTokens), slog.Float64("cost_usd", cost), slog.String("stop", resp.StopReason))
		return resp, p, nil
	}
	if lastErr != nil {
		return nil, nil, fmt.Errorf("every AI provider failed: %w", lastErr)
	}
	return nil, nil, ErrNoProvider
}

// hasEligibleProvider reports whether any provider could take a call now.
func (s *Service) hasEligibleProvider(ctx context.Context) (bool, error) {
	if !s.cfg.Enabled {
		return false, nil
	}
	states, err := s.ledger.states(ctx)
	if err != nil {
		return false, err
	}
	return len(Order(states, nil)) > 0, nil
}

func (s *Service) checkDailyBudget(ctx context.Context) error {
	if s.cfg.DailySpendLimitUSD == nil {
		return nil
	}
	today, err := s.ledger.todayUSD(ctx)
	if err != nil {
		return err
	}
	if today >= *s.cfg.DailySpendLimitUSD {
		return ErrDailyBudget
	}
	return nil
}

func (s *Service) emit(ctx context.Context, provider, purpose string, u Usage, cost float64, stop string) {
	base := []attribute.KeyValue{attribute.String("provider", provider), attribute.String("purpose", purpose)}
	kind := func(k string) metric.AddOption {
		return metric.WithAttributes(append(base, attribute.String("kind", k))...)
	}
	s.metrics.tokens.Add(ctx, int64(u.InputTokens), kind("input"))
	s.metrics.tokens.Add(ctx, int64(u.OutputTokens), kind("output"))
	s.metrics.tokens.Add(ctx, int64(u.CacheReadTokens), kind("cache_read"))
	s.metrics.cost.Add(ctx, cost, metric.WithAttributes(base...))
	result := "ok"
	if stop == StopRefusal {
		result = "refusal"
	}
	s.metrics.calls.Add(ctx, 1, metric.WithAttributes(append(base, attribute.String("result", result))...))
}

// UsageReport is the admin spend view.
type UsageReport struct {
	Providers []ProviderState `json:"providers"`
	Totals    Totals          `json:"totals"`
	ByPurpose []PurposeTotals `json:"byPurpose"`
}

// Usage returns provider state, totals and the per-purpose breakdown.
func (s *Service) Usage(ctx context.Context) (*UsageReport, error) {
	states, err := s.ledger.states(ctx)
	if err != nil {
		return nil, err
	}
	totals, err := s.ledger.totals(ctx)
	if err != nil {
		return nil, err
	}
	byPurpose, err := s.ledger.byPurpose(ctx)
	if err != nil {
		return nil, err
	}
	if states == nil {
		states = []ProviderState{}
	}
	return &UsageReport{Providers: states, Totals: totals, ByPurpose: byPurpose}, nil
}

// UpdateProvider applies an admin edit and returns the new state. Nil
// pointers leave a field alone; clearLimit removes the spend cap.
func (s *Service) UpdateProvider(ctx context.Context, name string, enabled *bool, priority *int, spendLimitUSD *float64, clearLimit bool) (*ProviderState, error) {
	if _, ok := s.providers[name]; !ok {
		return nil, handler.NewStatusError(http.StatusNotFound, "provider not found")
	}
	if spendLimitUSD != nil && *spendLimitUSD < 0 {
		return nil, handler.NewStatusError(http.StatusBadRequest, "spendLimitUsd must be >= 0")
	}
	if err := s.ledger.update(ctx, name, enabled, priority, spendLimitUSD, clearLimit); err != nil {
		return nil, err
	}
	st, err := s.ledger.state(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, handler.NewStatusError(http.StatusNotFound, "provider not found")
	}
	return st, err
}
