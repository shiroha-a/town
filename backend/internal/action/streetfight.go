package action

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/streetfight"
)

// DoStreetFight walks the player into a random monster and fights it to the end
// (legacy game.cgi mode=battle). The power spent in the fight is what the player
// is left with, so the fight is its own rate limit — there is no cooldown, same
// as the legacy game.
func (s *Service) DoStreetFight(ctx context.Context, playerID int64, idempotencyKey string) (*player.Player, *streetfight.Result, error) {
	var result *streetfight.Result
	p, err := s.runAction(ctx, playerID, "street_fight", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		m, err := streetfight.PickRandom(ctx, tx)
		if err == streetfight.ErrNoMonsters {
			return &ConditionError{Message: "今日は誰にも出会いませんでした。"}
		}
		if err != nil {
			return err
		}
		params := map[string]int{}
		for _, k := range streetfight.Abilities {
			params[k] = state.Params[k].Value
		}
		res := streetfight.Fight(params, state.Params["energy"].Value, state.Params["nou_energy"].Value, m, s.rng)

		// 削られたパワーをそのまま残す(レガシーも戦闘後の値を保存していた)。
		if _, err := tx.Exec(ctx,
			`UPDATE player_status SET energy = $2, nou_energy = $3, updated_at = now()
			 WHERE player_id = $1`, playerID, res.Energy, res.Nou); err != nil {
			return fmt.Errorf("save power: %w", err)
		}

		// 取り分。負けは持ち金までしか奪われない(所持金をマイナスにすると
		// 台帳の集計が壊れる)。
		if res.Money < 0 && -res.Money > state.Money {
			res.Money = -state.Money
		}
		if res.Money != 0 {
			if err := s.ledger.PostTx(ctx, tx, "street_fight", "", []ledger.Entry{
				{Account: ledger.PlayerAccount(playerID), Delta: res.Money},
				{Account: ledger.SystemAccount("street_fight"), Delta: -res.Money},
			}); err != nil {
				return fmt.Errorf("street fight money: %w", err)
			}
		}

		// 景品。贈答専用の品はギフト箱へ(シリアルコードの受け取りと同じ扱い)。
		if res.RewardItemID != nil {
			var name string
			var isGift bool
			var durability int
			if err := tx.QueryRow(ctx,
				`SELECT name, is_gift, GREATEST(durability, 1) FROM content_items WHERE id = $1`,
				*res.RewardItemID).Scan(&name, &isGift, &durability); err != nil {
				return fmt.Errorf("load reward item: %w", err)
			}
			res.RewardName = name
			if isGift {
				if _, err := tx.Exec(ctx,
					`INSERT INTO player_gifts (owner_id, item_id, uses) VALUES ($1, $2, $3)`,
					playerID, *res.RewardItemID, durability); err != nil {
					return fmt.Errorf("grant reward gift: %w", err)
				}
			} else if _, err := tx.Exec(ctx,
				`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses)
				 VALUES ($1, $2, 1, $3)
				 ON CONFLICT (player_id, item_id)
				 DO UPDATE SET quantity = player_items.quantity + 1,
				               remaining_uses = player_items.remaining_uses + $3,
				               updated_at = now()`,
				playerID, *res.RewardItemID, durability); err != nil {
				return fmt.Errorf("grant reward item: %w", err)
			}
		}
		result = &res
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return p, result, nil
}
