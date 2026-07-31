package player

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AdminHeldItem is one stack in the admin's view of a player's inventory. It
// carries the master's durability so the admin can tell 残量 from セット数.
type AdminHeldItem struct {
	ItemID          int64
	Name            string
	Category        string
	Quantity        int
	RemainingUses   int
	Sets            int
	Durability      int
	DurabilityUnit  string // 'use'(回) or 'day'(日)
	UseIntervalMin  int
	Usable          bool
	NextAvailableAt *time.Time // クールタイム中の再使用可能時刻
}

// ErrItemNotFound means the item master row does not exist.
var ErrItemNotFound = errors.New("item not found")

// AdminListItems returns everything a player holds.
func (s *Service) AdminListItems(ctx context.Context, playerID int64) ([]AdminHeldItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ci.id, ci.name, COALESCE(ci.category, ''), pi.quantity, pi.remaining_uses,
		       CEIL(pi.remaining_uses::numeric / GREATEST(ci.durability, 1))::int,
		       GREATEST(ci.durability, 1), ci.durability_unit, ci.use_interval_min, ci.usable,
		       CASE WHEN pi.last_used_at IS NOT NULL
		                 AND pi.last_used_at + make_interval(mins => ci.use_interval_min) > now()
		            THEN pi.last_used_at + make_interval(mins => ci.use_interval_min)
		            ELSE NULL END
		FROM player_items pi JOIN content_items ci ON ci.id = pi.item_id
		WHERE pi.player_id = $1
		ORDER BY ci.category, ci.id`, playerID)
	if err != nil {
		return nil, fmt.Errorf("admin list items: %w", err)
	}
	defer rows.Close()
	out := []AdminHeldItem{}
	for rows.Next() {
		var it AdminHeldItem
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Category, &it.Quantity, &it.RemainingUses,
			&it.Sets, &it.Durability, &it.DurabilityUnit, &it.UseIntervalMin, &it.Usable,
			&it.NextAvailableAt); err != nil {
			return nil, fmt.Errorf("scan held item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// AdminSetItem sets how much of an item a player holds, creating the stack if
// they had none and removing it once the remaining uses reach zero.
//
// 数えるのはサーバー側。quantity は remaining_uses から導く決まりで、画面から
// 送らせると必ずずれる(持っているのに「0個」と出る、没収が効かない等)。
// 購入時の上限(max_sets・持てる種類数)はここでは見ない。救済や検証で上限を
// 超えて持たせたいことがあり、そこで弾かれると手段が無くなるため。
func (s *Service) AdminSetItem(ctx context.Context, playerID, itemID int64, remainingUses int, clearCooldown bool) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM players WHERE id = $1 AND deleted_at IS NULL)`, playerID).Scan(&exists); err != nil {
			return fmt.Errorf("check player: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
		var durability int
		err := tx.QueryRow(ctx,
			`SELECT GREATEST(durability, 1) FROM content_items WHERE id = $1`, itemID).Scan(&durability)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrItemNotFound
		}
		if err != nil {
			return fmt.Errorf("load item: %w", err)
		}
		if remainingUses <= 0 {
			return adminDropItem(ctx, tx, playerID, itemID)
		}
		qty := (remainingUses + durability - 1) / durability // ceil
		if _, err := tx.Exec(ctx, `
			INSERT INTO player_items (player_id, item_id, quantity, remaining_uses)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (player_id, item_id)
			DO UPDATE SET quantity = $3, remaining_uses = $4, updated_at = now()`,
			playerID, itemID, qty, remainingUses); err != nil {
			return fmt.Errorf("set item: %w", err)
		}
		if clearCooldown {
			if _, err := tx.Exec(ctx,
				`UPDATE player_items SET last_used_at = NULL, updated_at = now()
				 WHERE player_id = $1 AND item_id = $2`, playerID, itemID); err != nil {
				return fmt.Errorf("clear cooldown: %w", err)
			}
		}
		return nil
	})
}

// AdminDeleteItem removes a stack from a player's inventory.
func (s *Service) AdminDeleteItem(ctx context.Context, playerID, itemID int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return adminDropItem(ctx, tx, playerID, itemID)
	})
}

func adminDropItem(ctx context.Context, tx pgx.Tx, playerID, itemID int64) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM player_items WHERE player_id = $1 AND item_id = $2`, playerID, itemID); err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	return nil
}

// ItemTotal is how much of one item the whole town holds.
type ItemTotal struct {
	ItemID        int64
	Name          string
	Category      string
	Holders       int   // 持っている人数
	Sets          int64 // 合計セット数
	RemainingUses int64 // 合計残量
}

// AdminItemTotals sums every player's holdings per item, most held first.
// 何がどれだけ世に出回っているかを見るためのもの。
func (s *Service) AdminItemTotals(ctx context.Context) ([]ItemTotal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ci.id, ci.name, COALESCE(ci.category, ''), count(*),
		       SUM(CEIL(pi.remaining_uses::numeric / GREATEST(ci.durability, 1))::int),
		       SUM(pi.remaining_uses)
		FROM player_items pi
		JOIN content_items ci ON ci.id = pi.item_id
		JOIN players p ON p.id = pi.player_id AND p.deleted_at IS NULL
		GROUP BY ci.id, ci.name, ci.category
		ORDER BY SUM(pi.remaining_uses) DESC, ci.id`)
	if err != nil {
		return nil, fmt.Errorf("item totals: %w", err)
	}
	defer rows.Close()
	out := []ItemTotal{}
	for rows.Next() {
		var t ItemTotal
		if err := rows.Scan(&t.ItemID, &t.Name, &t.Category, &t.Holders, &t.Sets, &t.RemainingUses); err != nil {
			return nil, fmt.Errorf("scan item total: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
