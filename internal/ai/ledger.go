package ai

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bmurray/alexandria/internal/id"
)

// ledger is the store for ai_providers (runtime state) and ai_usage (one
// row per model call). Spend is kept in two places on purpose: ai_usage is
// the audit trail, ai_providers.spent_usd is the fast running total the
// router reads.
type ledger struct {
	db     *sql.DB
	models map[string]string // provider name -> model, from config
	now    func() time.Time
}

// usageRecord is one priced call.
type usageRecord struct {
	Provider string
	Model    string
	Purpose  string
	Usage    Usage
	CostUSD  float64
	UserID   string // "" when anonymous
}

// PurposeTotals aggregates the ledger by purpose.
type PurposeTotals struct {
	Purpose      string  `json:"purpose"`
	Calls        int     `json:"calls"`
	CostUSD      float64 `json:"costUsd"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
}

// Totals is the overall spend.
type Totals struct {
	TodayUSD   float64 `json:"todayUsd"`
	AllTimeUSD float64 `json:"allTimeUsd"`
	Calls      int     `json:"calls"`
}

func newLedger(db *sql.DB, cfgs []ProviderConfig) *ledger {
	l := &ledger{db: db, models: map[string]string{}, now: time.Now}
	for _, c := range cfgs {
		l.models[c.Name] = c.Model
	}
	return l
}

// seed inserts a row per configured provider with its initial enabled,
// priority and cap. INSERT OR IGNORE keeps runtime edits across restarts.
func (l *ledger) seed(ctx context.Context, cfgs []ProviderConfig) error {
	for _, c := range cfgs {
		_, err := l.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO ai_providers (name, enabled, priority, spend_limit_usd, spent_usd) VALUES (?, ?, ?, ?, 0)`,
			c.Name, c.Enabled, c.Priority, nullFloat(c.SpendLimitUSD))
		if err != nil {
			return fmt.Errorf("seed provider %s: %w", c.Name, err)
		}
	}
	return nil
}

// states loads every configured provider's runtime row. Rows for providers
// no longer in config are ignored (they cannot be called anyway).
func (l *ledger) states(ctx context.Context) ([]ProviderState, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT name, enabled, priority, spend_limit_usd, spent_usd FROM ai_providers ORDER BY priority, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderState
	for rows.Next() {
		var s ProviderState
		var limit sql.NullFloat64
		if err := rows.Scan(&s.Name, &s.Enabled, &s.Priority, &limit, &s.SpentUSD); err != nil {
			return nil, err
		}
		model, known := l.models[s.Name]
		if !known {
			continue
		}
		s.Model = model
		if limit.Valid {
			v := limit.Float64
			s.SpendLimitUSD = &v
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// state loads one provider.
func (l *ledger) state(ctx context.Context, name string) (*ProviderState, error) {
	all, err := l.states(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, sql.ErrNoRows
}

// record writes the usage row and bumps the provider's running total in one
// transaction.
func (l *ledger) record(ctx context.Context, r usageRecord) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO ai_usage (id, provider, model, purpose, input_tokens, output_tokens, cache_read_tokens, cost_usd, user_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id.New(), r.Provider, r.Model, r.Purpose, r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.CacheReadTokens,
		r.CostUSD, nullString(r.UserID), l.now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_providers SET spent_usd = spent_usd + ? WHERE name = ?`, r.CostUSD, r.Provider); err != nil {
		return fmt.Errorf("charge provider: %w", err)
	}
	return tx.Commit()
}

// todayUSD sums today's spend (UTC day).
func (l *ledger) todayUSD(ctx context.Context) (float64, error) {
	start := l.now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
	var usd float64
	err := l.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(cost_usd), 0) FROM ai_usage WHERE created_at >= ?`, start).Scan(&usd)
	return RoundMicro(usd), err
}

// totals returns the overall figures.
func (l *ledger) totals(ctx context.Context) (Totals, error) {
	var t Totals
	var err error
	if t.TodayUSD, err = l.todayUSD(ctx); err != nil {
		return t, err
	}
	err = l.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(cost_usd), 0), COUNT(*) FROM ai_usage`).Scan(&t.AllTimeUSD, &t.Calls)
	t.AllTimeUSD = RoundMicro(t.AllTimeUSD)
	return t, err
}

// byPurpose groups the ledger by purpose.
func (l *ledger) byPurpose(ctx context.Context) ([]PurposeTotals, error) {
	rows, err := l.db.QueryContext(ctx,
		`SELECT purpose, COUNT(*), COALESCE(SUM(cost_usd), 0), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0)
		 FROM ai_usage GROUP BY purpose ORDER BY purpose`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PurposeTotals{}
	for rows.Next() {
		var p PurposeTotals
		if err := rows.Scan(&p.Purpose, &p.Calls, &p.CostUSD, &p.InputTokens, &p.OutputTokens); err != nil {
			return nil, err
		}
		p.CostUSD = RoundMicro(p.CostUSD)
		out = append(out, p)
	}
	return out, rows.Err()
}

// update applies an admin edit. Nil pointers leave a column alone;
// clearLimit removes the cap.
func (l *ledger) update(ctx context.Context, name string, enabled *bool, priority *int, limit *float64, clearLimit bool) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if enabled != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_providers SET enabled = ? WHERE name = ?`, *enabled, name); err != nil {
			return err
		}
	}
	if priority != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_providers SET priority = ? WHERE name = ?`, *priority, name); err != nil {
			return err
		}
	}
	switch {
	case clearLimit:
		if _, err := tx.ExecContext(ctx, `UPDATE ai_providers SET spend_limit_usd = NULL WHERE name = ?`, name); err != nil {
			return err
		}
	case limit != nil:
		if _, err := tx.ExecContext(ctx, `UPDATE ai_providers SET spend_limit_usd = ? WHERE name = ?`, *limit, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
