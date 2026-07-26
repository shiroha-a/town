package action

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/serial"
)

// SerialRedeemResult describes what a redeemed code handed out, for the UI.
type SerialRedeemResult struct {
	Message  string `json:"message"`   // 管理者が設定した文言
	ItemName string `json:"item_name"` // もらったアイテム(無ければ空)
	ItemUses int    `json:"item_uses"`
}

// DoRedeemSerial redeems a serial code for the player: it applies the code's
// effect (パラメータ/お金/体重/身長/病気) and grants its reward item, recording
// who used it. Each player may redeem a given code once.
func (s *Service) DoRedeemSerial(ctx context.Context, playerID int64, code, idempotencyKey string) (*player.Player, *SerialRedeemResult, error) {
	norm := serial.Normalize(code)
	if norm == "" {
		return nil, nil, &ConditionError{Message: "コードを入力してください。"}
	}
	var res *SerialRedeemResult
	p, err := s.runAction(ctx, playerID, "serial_redeem", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var (
			id            int64
			message       string
			effJSON       []byte
			rewardItemID  *int64
			rewardUses    int
			maxUses, used int
			startsAt      *time.Time
			endsAt        *time.Time
			enabled       bool
		)
		err := tx.QueryRow(ctx,
			`SELECT id, message, effect, reward_item_id, reward_item_uses,
			        max_uses, used_count, starts_at, ends_at, enabled
			 FROM serial_codes WHERE code = $1 FOR UPDATE`, norm).
			Scan(&id, &message, &effJSON, &rewardItemID, &rewardUses,
				&maxUses, &used, &startsAt, &endsAt, &enabled)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "そのコードは使えません。"}
		}
		if err != nil {
			return fmt.Errorf("load code: %w", err)
		}
		if !enabled {
			return &ConditionError{Message: "そのコードは使えません。"}
		}
		now := time.Now()
		if startsAt != nil && now.Before(*startsAt) {
			return &ConditionError{Message: "そのコードはまだ使えません。"}
		}
		if endsAt != nil && now.After(*endsAt) {
			return &ConditionError{Message: "そのコードは期限切れです。"}
		}
		if maxUses > 0 && used >= maxUses {
			return &ConditionError{Message: "そのコードは上限まで使われています。"}
		}

		// 1人1回。UNIQUE(code_id, player_id)で二重取得を防ぐ。
		var name string
		if err := tx.QueryRow(ctx, `SELECT display_name FROM players WHERE id = $1`, playerID).Scan(&name); err != nil {
			return fmt.Errorf("read player: %w", err)
		}
		tag, err := tx.Exec(ctx,
			`INSERT INTO serial_code_uses (code_id, player_id, player_name) VALUES ($1, $2, $3)
			 ON CONFLICT (code_id, player_id) DO NOTHING`, id, playerID, name)
		if err != nil {
			return fmt.Errorf("record use: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return &ConditionError{Message: "そのコードはすでに使っています。"}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE serial_codes SET used_count = used_count + 1 WHERE id = $1`, id); err != nil {
			return fmt.Errorf("bump used_count: %w", err)
		}

		out := &SerialRedeemResult{Message: message}
		// 効果(パラメータ/お金/体重/身長/病気)。
		if eff, err := effects.ParseEffect(effJSON); err == nil && len(eff.Ops) > 0 {
			if err := s.applyEffect(ctx, tx, playerID, "serial_redeem", eff, state); err != nil {
				return err
			}
		}
		// 報酬アイテム。
		if rewardItemID != nil && rewardUses > 0 {
			var itemName string
			var isGift bool
			if err := tx.QueryRow(ctx,
				`SELECT name, is_gift FROM content_items WHERE id = $1`, *rewardItemID).
				Scan(&itemName, &isGift); err != nil {
				return fmt.Errorf("load reward item: %w", err)
			}
			// 贈答専用の品はギフト箱へ(購入時と同じ扱い)。
			if isGift {
				if _, err := tx.Exec(ctx,
					`INSERT INTO player_gifts (owner_id, item_id, uses) VALUES ($1, $2, $3)`,
					playerID, *rewardItemID, rewardUses); err != nil {
					return fmt.Errorf("grant reward gift: %w", err)
				}
			} else if _, err := tx.Exec(ctx,
				`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses)
				 VALUES ($1, $2, 1, $3)
				 ON CONFLICT (player_id, item_id)
				 DO UPDATE SET quantity = player_items.quantity + 1,
				               remaining_uses = player_items.remaining_uses + $3,
				               updated_at = now()`,
				playerID, *rewardItemID, rewardUses); err != nil {
				return fmt.Errorf("grant reward item: %w", err)
			}
			out.ItemName, out.ItemUses = itemName, rewardUses
		}
		res = out
		return nil
	})
	return p, res, err
}
