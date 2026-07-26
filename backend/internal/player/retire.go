package player

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/ledger"
)

// Retire deletes a resident at their own request: 所持金と貯金を街へ返し、
// その人が街に残したもの(家・店・掲示板・メール・あいさつ等)を消してから
// 住民の行を消す。
//
// お金を先に0へ寄せるのは、台帳が「世界のお金の合計は動かない」ことを
// 不変条件にしているため。行だけ消すと合計が合わなくなる(台帳の行は
// 監査のために残す)。
//
// 家や投稿は player_houses / saisen_log が NO ACTION、掲示板やニュースは
// SET NULL で、players を消すだけでは残ってしまうので明示的に消す。
func (s *Service) Retire(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var guest bool
		if err := tx.QueryRow(ctx,
			`SELECT is_guest FROM players WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&guest); err != nil {
			if err == pgx.ErrNoRows {
				return ErrNotFound
			}
			return fmt.Errorf("load player: %w", err)
		}

		// 残っているお金(現金・普通預金・スーパー定期)を街へ返す。
		for _, acct := range []string{
			ledger.PlayerAccount(id), ledger.SavingsAccount(id), ledger.SuperSavingsAccount(id),
		} {
			var bal int64
			if err := tx.QueryRow(ctx,
				`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`, acct).Scan(&bal); err != nil {
				return fmt.Errorf("read balance: %w", err)
			}
			if bal == 0 {
				continue
			}
			if err := s.ledger.PostTx(ctx, tx, "retire", fmt.Sprintf("retire:%d", id), []ledger.Entry{
				{Account: acct, Delta: -bal},
				{Account: ledger.SystemAccount("retire"), Delta: bal},
			}); err != nil {
				return fmt.Errorf("return money: %w", err)
			}
		}

		// 街に残したものを消す(CASCADEで消えないもの)。
		for _, q := range []string{
			`DELETE FROM saisen_log WHERE from_id = $1 OR to_id = $1`,
			`DELETE FROM house_shop_stock WHERE house_id IN (SELECT id FROM player_houses WHERE owner_id = $1)`,
			`DELETE FROM house_shops WHERE house_id IN (SELECT id FROM player_houses WHERE owner_id = $1)`,
			`DELETE FROM house_contents WHERE house_id IN (SELECT id FROM player_houses WHERE owner_id = $1)`,
			`DELETE FROM house_bbs WHERE house_id IN (SELECT id FROM player_houses WHERE owner_id = $1)`,
			`DELETE FROM yami_items WHERE house_id IN (SELECT id FROM player_houses WHERE owner_id = $1)`,
			`DELETE FROM company_bbs WHERE house_id IN (SELECT id FROM player_houses WHERE owner_id = $1)`,
			`DELETE FROM company_staff WHERE house_id IN (SELECT id FROM player_houses WHERE owner_id = $1)`,
			`DELETE FROM company_materials WHERE house_id IN (SELECT id FROM player_houses WHERE owner_id = $1)`,
			`DELETE FROM player_houses WHERE owner_id = $1`,
			`DELETE FROM house_bbs WHERE author_id = $1`,
			`DELETE FROM greetings WHERE user_id = $1`,
			`DELETE FROM messages WHERE owner_id = $1 OR counterpart_id = $1`,
			`DELETE FROM town_news WHERE actor_id = $1`,
		} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return fmt.Errorf("purge resident data: %w", err)
			}
		}

		// 住民の行を消す(持ち物・ステータス・セッション等はCASCADEで消える)。
		if _, err := tx.Exec(ctx, `DELETE FROM players WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete player: %w", err)
		}
		return nil
	})
}
