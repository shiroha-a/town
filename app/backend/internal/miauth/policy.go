package miauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Policy decides which instances may take part.
type Policy string

const (
	// Blacklist (既定): block ルールが無いホストは通す。
	Blacklist Policy = "blacklist"
	// Whitelist: allow ルールが有るホストだけ通す。
	Whitelist Policy = "whitelist"
)

// Rule is one entry of the allow/block lists.
type Rule struct {
	Host      string    `json:"host"`
	Kind      string    `json:"kind"` // block | allow
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// Rules stores the per-instance allow/block lists. The two kinds are kept
// separately so switching policy does not destroy the other list.
type Rules struct {
	pool *pgxpool.Pool
}

// NewRules returns the rule store.
func NewRules(pool *pgxpool.Pool) *Rules {
	return &Rules{pool: pool}
}

// List returns every rule, newest first.
func (r *Rules) List(ctx context.Context) ([]Rule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT host, kind, note, created_at FROM instance_rules ORDER BY kind, host`)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var v Rule
		if err := rows.Scan(&v.Host, &v.Kind, &v.Note, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Put adds or replaces a rule.
func (r *Rules) Put(ctx context.Context, host, kind, note string) error {
	if kind != "block" && kind != "allow" {
		return fmt.Errorf("種別が正しくありません。")
	}
	h, err := NormalizeHost(host)
	if err != nil {
		return err
	}
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO instance_rules (host, kind, note) VALUES ($1, $2, $3)
		 ON CONFLICT (host) DO UPDATE SET kind = $2, note = $3`, h, kind, note); err != nil {
		return fmt.Errorf("put rule: %w", err)
	}
	return nil
}

// Delete removes a rule.
func (r *Rules) Delete(ctx context.Context, host string) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM instance_rules WHERE host = $1`, host); err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}
	return nil
}

// Allowed reports whether the host may take part under the given policy.
func (r *Rules) Allowed(ctx context.Context, policy Policy, host string) (bool, error) {
	var kind string
	err := r.pool.QueryRow(ctx, `SELECT kind FROM instance_rules WHERE host = $1`, host).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		// ルール無し: ブラックリスト方式なら通す、ホワイトリスト方式なら通さない。
		return policy != Whitelist, nil
	}
	if err != nil {
		return false, fmt.Errorf("check rule: %w", err)
	}
	if policy == Whitelist {
		return kind == "allow", nil
	}
	return kind != "block", nil
}
