package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/casino"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/news"
	"github.com/shiroha-a/town/internal/rng"
)

// DrawLoto6 settles every un-drawn loto6 day before today: it draws the winning
// numbers, credits each ticket's prize (by match count) to the buyer's savings
// account, records the draw, and clears the day's tickets. Returns the number of
// days drawn.
func DrawLoto6(ctx context.Context, tx pgx.Tx, led *ledger.Repo, r *rng.Rand, today time.Time) (int, error) {
	rows, err := tx.Query(ctx,
		`SELECT DISTINCT game_date FROM loto6_tickets
		 WHERE game_date < $1 AND game_date NOT IN (SELECT game_date FROM loto6_draws)
		 ORDER BY game_date`, today)
	if err != nil {
		return 0, err
	}
	var dates []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return 0, err
		}
		dates = append(dates, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	drawn := 0
	for _, date := range dates {
		winning := casino.GenLoto6(r)
		win32 := make([]int32, len(winning))
		for i, w := range winning {
			win32[i] = int32(w)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO loto6_draws (game_date, winning) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			date, win32); err != nil {
			return drawn, err
		}
		// その日の全券を読み切る(rowsを開いたまま台帳へ書けないため)。
		tRows, err := tx.Query(ctx, `SELECT player_id, numbers FROM loto6_tickets WHERE game_date=$1`, date)
		if err != nil {
			return drawn, err
		}
		type ticket struct {
			pid  int64
			nums []int
		}
		var tickets []ticket
		for tRows.Next() {
			var pid int64
			var n32 []int32
			if err := tRows.Scan(&pid, &n32); err != nil {
				tRows.Close()
				return drawn, err
			}
			nums := make([]int, len(n32))
			for i, x := range n32 {
				nums[i] = int(x)
			}
			tickets = append(tickets, ticket{pid, nums})
		}
		tRows.Close()
		if err := tRows.Err(); err != nil {
			return drawn, err
		}
		// プレイヤーごとに賞金を合算する。
		payouts := map[int64]int64{}
		for _, t := range tickets {
			m := casino.Loto6Matches(t.nums, winning)
			payouts[t.pid] += casino.Loto6Prizes[m]
		}
		for pid, amt := range payouts {
			if amt <= 0 {
				continue
			}
			if err := led.PostTx(ctx, tx, "loto6_prize", "", []ledger.Entry{
				{Account: ledger.SystemAccount("loto"), Delta: -amt},
				{Account: ledger.SavingsAccount(pid), Delta: amt},
			}); err != nil {
				return drawn, err
			}
			// 高額当選は街のニュースにする(レガシーには無いが、抽選が
			// 深夜のバッチで進むため役場で結果が分かるようにする)。
			if amt >= news.TownWideMoneyThreshold {
				name, err := news.ActorName(ctx, tx, pid)
				if err != nil {
					return drawn, err
				}
				if err := news.RecordFor(ctx, tx, news.KindPrize, pid, name,
					fmt.Sprintf("%sさんがロト6で%d円当選しました。", name, amt), nil, true); err != nil {
					return drawn, err
				}
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM loto6_tickets WHERE game_date=$1`, date); err != nil {
			return drawn, err
		}
		drawn++
	}
	return drawn, nil
}
