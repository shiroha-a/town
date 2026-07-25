// Package bank implements savings interest accrual. Interest is a money faucet:
// it credits savings accounts from system:interest_faucet, keeping the ledger
// balanced. The amount is floored to an integer, matching the legacy game.
package bank

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/ledger"
)

// AccrueInterest credits interest to every account matching accountPrefix (e.g.
// "savings:") that has a positive balance, using the caller's transaction.
// permille is the per-mille daily rate (5 = 0.5%); reasonPrefix labels each
// ledger tx (e.g. "interest:"). It returns the number of accounts credited.
//
// Idempotency for "once per game day" is provided by the caller (the worker's
// worker_jobs claim runs this in the same transaction), so no per-post ref is
// needed here.
func AccrueInterest(ctx context.Context, tx pgx.Tx, led *ledger.Repo, accountPrefix, reasonPrefix string, permille int) (int, error) {
	if permille <= 0 {
		return 0, nil
	}

	// 先に全残高を読み切ってから台帳へ書く(同一tx上でrowsを開いたまま
	// 別クエリを発行できないため)。
	rows, err := tx.Query(ctx,
		`SELECT account, SUM(delta) AS balance
		 FROM ledger_entry
		 WHERE account LIKE $1
		 GROUP BY account
		 HAVING SUM(delta) > 0`, accountPrefix+"%")
	if err != nil {
		return 0, fmt.Errorf("query savings: %w", err)
	}
	type saving struct {
		account string
		balance int64
	}
	var savings []saving
	for rows.Next() {
		var s saving
		if err := rows.Scan(&s.account, &s.balance); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan savings: %w", err)
		}
		savings = append(savings, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("savings rows: %w", err)
	}

	applied := 0
	for _, s := range savings {
		interest := s.balance * int64(permille) / 1000 // 整数切り捨て
		if interest <= 0 {
			continue
		}
		reason := reasonPrefix + accountID(s.account)
		if err := led.PostTx(ctx, tx, reason, "", []ledger.Entry{
			{Account: ledger.SystemAccount("interest_faucet"), Delta: -interest},
			{Account: s.account, Delta: interest},
		}); err != nil {
			return applied, fmt.Errorf("post interest for %s: %w", s.account, err)
		}
		applied++
	}
	return applied, nil
}

// accountID extracts the numeric id from a "savings:<id>" account name.
func accountID(account string) string {
	_, id, _ := strings.Cut(account, ":")
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return "?"
	}
	return id
}
