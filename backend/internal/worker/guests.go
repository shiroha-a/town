package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PurgeGuests deletes お試しプレイ(ゲスト)の住民と、その住民が残したものを
// まとめて消す。作成から lifetimeMin 分で期限切れとする(最終操作ではなく作成
// 起点にするのは、「お試しは1時間」と説明どおりに動く方が分かりやすいため)。
//
// players を消すだけでは足りない: player_houses と saisen_log は NO ACTION で
// 削除を拒み、town_news などは SET NULL で本文だけが残る。ゲストはこれらの操作を
// authGuard で止めているので通常は空だが、制限を緩めたときに取りこぼさないよう
// ここでも明示的に消しておく。
func PurgeGuests(ctx context.Context, pool *pgxpool.Pool, lifetimeMin int) (int64, error) {
	if lifetimeMin <= 0 {
		lifetimeMin = 60
	}
	var deleted int64
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id FROM players
			  WHERE is_guest AND created_at < now() - make_interval(mins => $1)`, lifetimeMin)
		if err != nil {
			return fmt.Errorf("list expired guests: %w", err)
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		// 参照を先に落としてから本体を消す(CASCADEでないものへの対応)。
		for _, q := range []string{
			`DELETE FROM saisen_log WHERE from_id = ANY($1) OR to_id = ANY($1)`,
			`DELETE FROM player_houses WHERE owner_id = ANY($1)`,
			`DELETE FROM town_news WHERE actor_id = ANY($1)`,
			`DELETE FROM greetings WHERE user_id = ANY($1)`,
			`DELETE FROM house_bbs WHERE author_id = ANY($1)`,
			`DELETE FROM messages WHERE owner_id = ANY($1) OR counterpart_id = ANY($1)`,
		} {
			if _, err := tx.Exec(ctx, q, ids); err != nil {
				return fmt.Errorf("purge guest refs: %w", err)
			}
		}
		tag, err := tx.Exec(ctx, `DELETE FROM players WHERE id = ANY($1)`, ids)
		if err != nil {
			return fmt.Errorf("delete guests: %w", err)
		}
		deleted = tag.RowsAffected()
		return nil
	})
	return deleted, err
}
