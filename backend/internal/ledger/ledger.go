// Package ledger implements the money system as an append-only double-entry
// ledger. Balances are derived from the entries; there is no mutable balance
// column. Every transaction's entries must sum to zero, which makes "total
// money in the world" an auditable invariant.
package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry is one leg of a ledger transaction. Delta is signed integer yen.
type Entry struct {
	Account string
	Delta   int64
}

// Repo is the ledger data-access layer.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// PlayerAccount returns the ledger account name for a player's cash.
func PlayerAccount(id int64) string { return fmt.Sprintf("player:%d", id) }

// SavingsAccount returns the ledger account name for a player's bank savings.
func SavingsAccount(id int64) string { return fmt.Sprintf("savings:%d", id) }

// SuperSavingsAccount returns the ledger account name for a player's super
// time-deposit (スーパー定期).
func SuperSavingsAccount(id int64) string { return fmt.Sprintf("super_savings:%d", id) }

// SystemAccount returns a system faucet/sink account name.
func SystemAccount(name string) string { return "system:" + name }

// ErrUnbalanced is returned when a transaction's entries do not sum to zero.
var ErrUnbalanced = errors.New("ledger transaction is not balanced")

// Post writes a balanced transaction atomically in its own database
// transaction. If ref is non-empty the post is idempotent: a second post with
// the same ref is a no-op.
func (r *Repo) Post(ctx context.Context, reason, ref string, entries []Entry) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return r.PostTx(ctx, tx, reason, ref, entries)
	})
}

// PostTx writes a balanced transaction using the caller's transaction, so it
// can be composed atomically with other writes (status updates, action logs).
// Semantics match Post.
func (r *Repo) PostTx(ctx context.Context, tx pgx.Tx, reason, ref string, entries []Entry) error {
	var sum int64
	for _, e := range entries {
		sum += e.Delta
	}
	if sum != 0 {
		return fmt.Errorf("%w: sum=%d", ErrUnbalanced, sum)
	}

	var txID int64
	if ref == "" {
		if err := tx.QueryRow(ctx,
			`INSERT INTO ledger_tx (reason) VALUES ($1) RETURNING id`,
			reason).Scan(&txID); err != nil {
			return fmt.Errorf("insert ledger_tx: %w", err)
		}
	} else {
		err := tx.QueryRow(ctx,
			`INSERT INTO ledger_tx (reason, ref) VALUES ($1, $2)
			 ON CONFLICT (ref) WHERE ref IS NOT NULL DO NOTHING
			 RETURNING id`, reason, ref).Scan(&txID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // 同一refの二重post: 冪等にno-op
		}
		if err != nil {
			return fmt.Errorf("insert ledger_tx: %w", err)
		}
	}
	for _, e := range entries {
		if _, err := tx.Exec(ctx,
			`INSERT INTO ledger_entry (tx_id, account, delta) VALUES ($1, $2, $3)`,
			txID, e.Account, e.Delta); err != nil {
			return fmt.Errorf("insert ledger_entry: %w", err)
		}
	}
	return nil
}

// Balance returns the current balance of an account.
func (r *Repo) Balance(ctx context.Context, account string) (int64, error) {
	var bal int64
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
		account).Scan(&bal); err != nil {
		return 0, fmt.Errorf("balance: %w", err)
	}
	return bal, nil
}

// TotalPlayerMoney returns the sum of all player balances (money in circulation).
func (r *Repo) TotalPlayerMoney(ctx context.Context) (int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account LIKE 'player:%'`).
		Scan(&total); err != nil {
		return 0, fmt.Errorf("total player money: %w", err)
	}
	return total, nil
}

// AuditZeroSum returns the sum of every ledger entry, which must always be 0 in
// a correct double-entry ledger. A non-zero result indicates corruption.
func (r *Repo) AuditZeroSum(ctx context.Context) (int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry`).Scan(&total); err != nil {
		return 0, fmt.Errorf("audit zero sum: %w", err)
	}
	return total, nil
}
