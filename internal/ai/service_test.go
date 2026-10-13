package ai

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bmurray/alexandria/internal/db"
	"github.com/bmurray/alexandria/internal/handler"
)

// newTestService builds a Service over an in-memory database with the given
// fake providers swapped in for the Anthropic ones.
func newTestService(t *testing.T, cfg Config, catalog Catalog, fakes ...*fakeProvider) *Service {
	t.Helper()
	ctx := context.Background()
	database, err := db.OpenTest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	cfg.Enabled = true
	for _, f := range fakes {
		found := false
		for _, pc := range cfg.Providers {
			if pc.Name == f.name {
				found = true
			}
		}
		if !found {
			cfg.Providers = append(cfg.Providers, ProviderConfig{Name: f.name, Type: "anthropic", Model: f.model, Enabled: true, Priority: 100,
				InputPerMillionUSD: 1, OutputPerMillionUSD: 10})
		}
	}
	if catalog == nil {
		catalog = NewFakeCatalog()
	}
	svc, err := NewService(ctx, database, cfg, catalog, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fakes {
		svc.providers[f.name] = f
	}
	return svc
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	var se *handler.StatusError
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.As(err, &se) {
		t.Fatalf("not a StatusError: %v", err)
	}
	return se.Status
}

func TestNewServiceSeedsAndRejectsBadConfig(t *testing.T) {
	ctx := context.Background()
	database, _ := db.OpenTest(ctx)
	defer database.Close()
	cfg := Config{Enabled: true, Providers: []ProviderConfig{
		{Name: "opus", Type: "anthropic", Model: "claude-opus-5-5", Enabled: true, Priority: 10, SpendLimitUSD: f(5)},
		{Name: "haiku", Type: "anthropic", Model: "claude-haiku-5-5", Enabled: true, Priority: 20},
	}}
	svc, err := NewService(ctx, database, cfg, NewFakeCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Providers) != 2 || report.Providers[0].Name != "opus" || *report.Providers[0].SpendLimitUSD != 5 || report.Providers[1].SpendLimitUSD != nil {
		t.Fatalf("seeded providers: %+v", report.Providers)
	}
	if report.Providers[0].Model != "claude-opus-5-5" {
		t.Fatal("model not joined from config")
	}
	// Seeding again keeps runtime edits.
	if _, err := svc.UpdateProvider(ctx, "opus", nil, nil, f(1), false); err != nil {
		t.Fatal(err)
	}
	svc2, err := NewService(ctx, database, cfg, NewFakeCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := svc2.ledger.state(ctx, "opus")
	if *st.SpendLimitUSD != 1 {
		t.Fatal("INSERT OR IGNORE should keep the runtime cap")
	}

	if _, err := NewService(ctx, database, Config{Providers: []ProviderConfig{{Name: "x", Type: "nope"}}}, nil, nil); err == nil {
		t.Fatal("unknown provider type must fail")
	}
	if _, err := NewService(ctx, database, Config{Providers: []ProviderConfig{{Name: "x", Type: "anthropic"}, {Name: "x", Type: "anthropic"}}}, nil, nil); err == nil {
		t.Fatal("duplicate names must fail")
	}
}

func TestUpdateProvider(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, Config{}, nil, newFake("a"))
	st, err := svc.UpdateProvider(ctx, "a", bp(false), ip(3), f(2.5), false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || st.Priority != 3 || *st.SpendLimitUSD != 2.5 {
		t.Fatalf("state: %+v", st)
	}
	st, _ = svc.UpdateProvider(ctx, "a", nil, nil, nil, true)
	if st.SpendLimitUSD != nil || st.Priority != 3 {
		t.Fatalf("clear limit: %+v", st)
	}
	if _, err := svc.UpdateProvider(ctx, "nope", nil, nil, nil, false); statusOf(t, err) != 404 {
		t.Fatal("unknown provider should 404")
	}
	if _, err := svc.UpdateProvider(ctx, "a", nil, nil, f(-1), false); statusOf(t, err) != 400 {
		t.Fatal("negative limit should 400")
	}
}

func TestLedgerRecordChargeAndCapCrossing(t *testing.T) {
	ctx := context.Background()
	a := newFake("a", text("hi", 1_000_000, 0), text("again", 1_000_000, 0), text("never", 0, 0))
	b := newFake("b", text("from b", 10, 10))
	cfg := Config{Providers: []ProviderConfig{
		{Name: "a", Type: "anthropic", Model: "m", Enabled: true, Priority: 1, InputPerMillionUSD: 1, SpendLimitUSD: f(1.5)},
		{Name: "b", Type: "anthropic", Model: "m", Enabled: true, Priority: 2, InputPerMillionUSD: 1, OutputPerMillionUSD: 1},
	}}
	svc := newTestService(t, cfg, nil, a, b)

	resp, p, err := svc.complete(ctx, PurposeChat, "user1", Request{})
	if err != nil || p.Name() != "a" || resp.Text != "hi" {
		t.Fatal(resp, p, err)
	}
	st, _ := svc.ledger.state(ctx, "a")
	if st.SpentUSD != 1 || st.Exceeded() {
		t.Fatalf("after first call: %+v", st)
	}
	// Second call crosses the 1.5 cap (spent becomes 2.0) but still goes to a.
	if _, p, err = svc.complete(ctx, PurposeChat, "", Request{}); err != nil || p.Name() != "a" {
		t.Fatal(p, err)
	}
	st, _ = svc.ledger.state(ctx, "a")
	if !st.Exceeded() {
		t.Fatalf("a should be capped: %+v", st)
	}
	// Third call falls through to b.
	resp, p, err = svc.complete(ctx, PurposeEnrich, "user1", Request{})
	if err != nil || p.Name() != "b" || resp.Text != "from b" {
		t.Fatal(resp, p, err)
	}
	if a.calls() != 2 {
		t.Fatalf("capped provider was called %d times", a.calls())
	}

	report, err := svc.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals.Calls != 3 || report.Totals.AllTimeUSD != 2.00002 || report.Totals.TodayUSD != 2.00002 {
		t.Fatalf("totals: %+v", report.Totals)
	}
	if len(report.ByPurpose) != 2 || report.ByPurpose[0].Purpose != "chat" || report.ByPurpose[0].Calls != 2 || report.ByPurpose[0].InputTokens != 2_000_000 {
		t.Fatalf("by purpose: %+v", report.ByPurpose)
	}
	if report.ByPurpose[1].Purpose != "enrich" || report.ByPurpose[1].CostUSD != 0.00002 || report.ByPurpose[1].OutputTokens != 10 {
		t.Fatalf("by purpose: %+v", report.ByPurpose)
	}
	// user_id is nullable.
	var nulls int
	svc.ledger.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_usage WHERE user_id IS NULL`).Scan(&nulls)
	if nulls != 1 {
		t.Fatalf("null user rows = %d", nulls)
	}
}

func TestLedgerDailyTotalAndBudget(t *testing.T) {
	ctx := context.Background()
	a := newFake("a", text("one", 1_000_000, 0), text("two", 1_000_000, 0), text("three", 0, 0))
	cfg := Config{DailySpendLimitUSD: f(2), Providers: []ProviderConfig{
		{Name: "a", Type: "anthropic", Model: "m", Enabled: true, Priority: 1, InputPerMillionUSD: 1},
	}}
	svc := newTestService(t, cfg, nil, a)
	// Yesterday's spend does not count.
	svc.ledger.now = func() time.Time { return time.Now().Add(-48 * time.Hour) }
	if _, _, err := svc.complete(ctx, PurposeChat, "", Request{}); err != nil {
		t.Fatal(err)
	}
	svc.ledger.now = time.Now
	today, _ := svc.ledger.todayUSD(ctx)
	if today != 0 {
		t.Fatalf("today = %v", today)
	}
	if _, _, err := svc.complete(ctx, PurposeChat, "", Request{}); err != nil {
		t.Fatal(err)
	}
	// 1 USD today, under the 2 USD limit; next call brings it to 2 and the one after is refused.
	if _, _, err := svc.complete(ctx, PurposeChat, "", Request{}); err != nil {
		t.Fatal(err)
	}
	// The third scripted response cost 0 so today is 1; lower the limit to prove the refusal path.
	svc.cfg.DailySpendLimitUSD = f(1)
	_, _, err := svc.complete(ctx, PurposeChat, "", Request{})
	if statusOf(t, err) != http.StatusTooManyRequests || !strings.Contains(err.Error(), "daily AI budget") {
		t.Fatalf("want 429, got %v", err)
	}
}

func TestCompleteFallsThroughOnError(t *testing.T) {
	ctx := context.Background()
	a := newFake("a", failing("boom"))
	b := newFake("b", text("ok", 1, 1))
	cfg := Config{Providers: []ProviderConfig{
		{Name: "a", Type: "anthropic", Model: "m", Enabled: true, Priority: 1},
		{Name: "b", Type: "anthropic", Model: "m", Enabled: true, Priority: 2},
	}}
	svc := newTestService(t, cfg, nil, a, b)
	resp, p, err := svc.complete(ctx, PurposeChat, "", Request{})
	if err != nil || p.Name() != "b" || resp.Text != "ok" {
		t.Fatal(resp, p, err)
	}
	// Everything failing surfaces the last error.
	a2 := newFake("a", failing("boom"))
	b2 := newFake("b", failing("bang"))
	svc = newTestService(t, cfg, nil, a2, b2)
	if _, _, err := svc.complete(ctx, PurposeChat, "", Request{}); err == nil || !strings.Contains(err.Error(), "bang") {
		t.Fatalf("got %v", err)
	}
	// Nothing enabled is a 503.
	svc.UpdateProvider(ctx, "a", bp(false), nil, nil, false)
	svc.UpdateProvider(ctx, "b", bp(false), nil, nil, false)
	if _, _, err := svc.complete(ctx, PurposeChat, "", Request{}); statusOf(t, err) != http.StatusServiceUnavailable {
		t.Fatalf("got %v", err)
	}
	// A cancelled context stops the walk immediately.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	svc = newTestService(t, cfg, nil, newFake("a", failing("ctx")), newFake("b", text("no", 0, 0)))
	if _, _, err := svc.complete(cctx, PurposeChat, "", Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	// Disabled feature flag.
	svc.cfg.Enabled = false
	if _, _, err := svc.complete(ctx, PurposeChat, "", Request{}); err != ErrNoProvider {
		t.Fatalf("got %v", err)
	}
	if ok, _ := svc.hasEligibleProvider(ctx); ok {
		t.Fatal("disabled service has no eligible provider")
	}
}

func bp(b bool) *bool { return &b }
func ip(i int) *int   { return &i }

func TestEnabledAndTitleTruncation(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, Config{}, nil, newFake("a", text("ok", 1, 1)))
	if !svc.Enabled() {
		t.Fatal("configured service should be enabled")
	}
	long := strings.Repeat("é", 70)
	res, err := svc.Chat(ctx, "u", nil, "", long)
	if err != nil {
		t.Fatal(err)
	}
	conv, _, _ := svc.GetConversation(ctx, "u", res.ConversationID)
	if conv.Title != strings.Repeat("é", 60) {
		t.Fatalf("title: %q", conv.Title)
	}
}
