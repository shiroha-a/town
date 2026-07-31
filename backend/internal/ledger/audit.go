package ledger

import (
	"context"
	"fmt"
	"time"
)

// Movement is one line of money history: a single ledger entry with the reason
// of the transaction it belongs to.
type Movement struct {
	TxID       int64
	PlayerID   int64  // 0 = システム勘定
	Kind       string // cash / savings / super_savings / system
	Account    string
	Delta      int64
	Reason     string
	CreatedAt  time.Time
	Reversed   bool // この取引は取り消し済み
	IsReversal bool // この行自体が取り消しの記帳
}

// accountKindSQL splits an account name into its kind and player id.
// 勘定名は "player:12" のような形なので、":" の前後で分ける。
const accountKindSQL = `
	CASE split_part(e.account, ':', 1)
	     WHEN 'player' THEN 'cash'
	     ELSE split_part(e.account, ':', 1) END AS kind,
	CASE WHEN split_part(e.account, ':', 1) = 'system' THEN 0
	     ELSE COALESCE(NULLIF(split_part(e.account, ':', 2), '')::bigint, 0) END AS player_id`

// History returns the most recent money movements, newest first. playerID 0
// means every player (街全体の流れ).
func (r *Repo) History(ctx context.Context, playerID int64, limit int) ([]Movement, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// プレイヤーを指定したときは現金・普通口座・スーパー定期の3つを束ねて出す。
	// 別々に見ても「いつ何でお金が動いたか」は追えないため。
	// システム勘定(街側)の脚は出さない。複式なので1つの取引に必ず相手側の脚が
	// あり、そのまま並べると同じ取引が2行になって「誰がいくら得たか」が読めない。
	// 街の出入りは別に SystemTotals でまとめて出している。
	q := `SELECT e.tx_id, ` + accountKindSQL + `, e.account, e.delta, t.reason, t.created_at,
	             EXISTS(SELECT 1 FROM ledger_tx x WHERE x.ref = 'reverse:' || t.id::text),
	             COALESCE(t.ref, '') LIKE 'reverse:%'
	      FROM ledger_entry e JOIN ledger_tx t ON t.id = e.tx_id
	      WHERE e.account NOT LIKE 'system:%'
	        AND ($1 = 0 OR e.account IN ('player:' || $1::bigint::text,
	                                     'savings:' || $1::bigint::text,
	                                     'super_savings:' || $1::bigint::text))
	      ORDER BY t.created_at DESC, e.id DESC
	      LIMIT $2`
	rows, err := r.pool.Query(ctx, q, playerID, limit)
	if err != nil {
		return nil, fmt.Errorf("money history: %w", err)
	}
	defer rows.Close()
	out := []Movement{}
	for rows.Next() {
		var m Movement
		if err := rows.Scan(&m.TxID, &m.Kind, &m.PlayerID, &m.Account, &m.Delta, &m.Reason,
			&m.CreatedAt, &m.Reversed, &m.IsReversal); err != nil {
			return nil, fmt.Errorf("scan movement: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReasonTotal is how much money moved into (or out of) players for one reason.
type ReasonTotal struct {
	Reason string
	Count  int64 // 件数
	In     int64 // プレイヤー側に入った額(正のぶんの合計)
	Out    int64 // プレイヤー側から出た額(負のぶんの合計。正の数で返す)
	Net    int64 // In - Out
}

// ReasonTotals aggregates every player-side movement by reason, biggest first.
// 何でお金が生まれて何で消えているかを1枚で見るためのもの。
func (r *Repo) ReasonTotals(ctx context.Context, playerID int64) ([]ReasonTotal, error) {
	// 理由の末尾の ":数字" は落としてまとめる。利息は "interest:<player_id>" の
	// 形で記帳されるため、そのままだと人数ぶんの行に散ってしまう。
	// "casino:donuts" のように意味のある接尾辞は残る。
	rows, err := r.pool.Query(ctx, `
		SELECT regexp_replace(t.reason, ':[0-9]+$', ''), count(*),
		       COALESCE(SUM(e.delta) FILTER (WHERE e.delta > 0), 0),
		       COALESCE(-SUM(e.delta) FILTER (WHERE e.delta < 0), 0),
		       COALESCE(SUM(e.delta), 0)
		FROM ledger_entry e JOIN ledger_tx t ON t.id = e.tx_id
		WHERE e.account NOT LIKE 'system:%'
		  AND ($1 = 0 OR e.account IN ('player:' || $1::bigint::text,
		                               'savings:' || $1::bigint::text,
		                               'super_savings:' || $1::bigint::text))
		GROUP BY 1
		ORDER BY count(*) DESC, 1`, playerID)
	if err != nil {
		return nil, fmt.Errorf("reason totals: %w", err)
	}
	defer rows.Close()
	out := []ReasonTotal{}
	for rows.Next() {
		var t ReasonTotal
		if err := rows.Scan(&t.Reason, &t.Count, &t.In, &t.Out, &t.Net); err != nil {
			return nil, fmt.Errorf("scan reason total: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AccountTotal is a balance grouped by account kind or a single system account.
type AccountTotal struct {
	Account string
	Balance int64
}

// SystemTotals returns each system faucet/sink balance. 原資がどれだけ出て
// どのシンクにどれだけ吸われたかが分かる。
func (r *Repo) SystemTotals(ctx context.Context) ([]AccountTotal, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT account, COALESCE(SUM(delta), 0) AS bal
		FROM ledger_entry WHERE account LIKE 'system:%'
		GROUP BY account ORDER BY bal`)
	if err != nil {
		return nil, fmt.Errorf("system totals: %w", err)
	}
	defer rows.Close()
	out := []AccountTotal{}
	for rows.Next() {
		var a AccountTotal
		if err := rows.Scan(&a.Account, &a.Balance); err != nil {
			return nil, fmt.Errorf("scan system total: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PlayerTotals is the money held by players, split by where it sits.
type PlayerTotals struct {
	Cash         int64
	Savings      int64
	SuperSavings int64
	LoanRemain   int64 // ローン残高(日額×残回数)。資産からは引く
}

// Totals sums every player's money. playerID 0 means all players.
func (r *Repo) Totals(ctx context.Context, playerID int64) (PlayerTotals, error) {
	var t PlayerTotals
	err := r.pool.QueryRow(ctx, `
		SELECT
		  COALESCE((SELECT SUM(delta) FROM ledger_entry
		            WHERE account LIKE 'player:%'
		              AND ($1 = 0 OR account = 'player:' || $1::bigint::text)), 0),
		  COALESCE((SELECT SUM(delta) FROM ledger_entry
		            WHERE account LIKE 'savings:%'
		              AND ($1 = 0 OR account = 'savings:' || $1::bigint::text)), 0),
		  COALESCE((SELECT SUM(delta) FROM ledger_entry
		            WHERE account LIKE 'super_savings:%'
		              AND ($1 = 0 OR account = 'super_savings:' || $1::bigint::text)), 0),
		  COALESCE((SELECT SUM(nitigaku * kaisuu) FROM player_loans
		            WHERE ($1 = 0 OR player_id = $1::bigint)), 0)`, playerID).
		Scan(&t.Cash, &t.Savings, &t.SuperSavings, &t.LoanRemain)
	if err != nil {
		return PlayerTotals{}, fmt.Errorf("player totals: %w", err)
	}
	return t, nil
}
