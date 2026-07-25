package worker

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/ledger"
)

// RepayLoans deducts one day's payment from each active loan's savings account
// and decrements its remaining count, deleting fully-repaid loans. Legacy:
// 住宅ローン支払い. Savings may go negative (no penalty), matching legacy behavior.
// It returns the number of loans charged.
func RepayLoans(ctx context.Context, tx pgx.Tx, led *ledger.Repo) (int, error) {
	// 先に全ローンを読み切ってから台帳へ書く(同一tx上でrowsを開いたまま
	// 別クエリを発行できないため)。
	rows, err := tx.Query(ctx, `SELECT player_id, nitigaku FROM player_loans WHERE kaisuu > 0`)
	if err != nil {
		return 0, err
	}
	type loan struct {
		playerID int64
		nitigaku int64
	}
	var loans []loan
	for rows.Next() {
		var l loan
		if err := rows.Scan(&l.playerID, &l.nitigaku); err != nil {
			rows.Close()
			return 0, err
		}
		loans = append(loans, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, l := range loans {
		if err := led.PostTx(ctx, tx, "loan_repay", "", []ledger.Entry{
			{Account: ledger.SavingsAccount(l.playerID), Delta: -l.nitigaku},
			{Account: ledger.SystemAccount("loan_sink"), Delta: l.nitigaku},
		}); err != nil {
			return 0, err
		}
	}
	// 残回数を1減らし、完済(0回)になったローンを削除する。
	if _, err := tx.Exec(ctx, `UPDATE player_loans SET kaisuu = kaisuu - 1`); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM player_loans WHERE kaisuu <= 0`); err != nil {
		return 0, err
	}
	return len(loans), nil
}
