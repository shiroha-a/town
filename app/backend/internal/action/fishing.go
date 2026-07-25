package action

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/fishing"
	"github.com/shiroha-a/town/internal/player"
)

// FishingState is the 釣りゲーム view: either the bait to choose from (no
// session) or the current round (session in progress).
type FishingState struct {
	Active  bool          `json:"active"`
	Cards   int           `json:"cards"`    // 場に出ているカード枚数(activeのとき)
	Baits   []FishingBait `json:"baits"`    // 使える釣り餌(未開始のとき)
	AtLimit bool          `json:"at_limit"` // 持ち物が上限で釣りができない
}

// FishingBait is one usable bait item.
type FishingBait struct {
	ItemID int64  `json:"item_id"`
	Name   string `json:"name"`
	Uses   int    `json:"uses"`
}

// FishingResult is the outcome of picking one card.
type FishingResult struct {
	Outcome string `json:"outcome"` // win / continue / lose
	Fish    string `json:"fish"`    // 釣れた魚(winのみ)
	Cards   int    `json:"cards"`   // 次のラウンドの枚数(continueのみ)
}

// baitCategory is the item category that can be used as bait (レガシーの種別「釣り」)。
const baitCategory = "釣り"

// isBaitName reports whether a 釣り item can be used as bait: the fish
// themselves (特/幻) are not bait (レガシー tsuri.cgi:63)。
func isBaitName(name string) bool {
	return !strings.Contains(name, "特") && !strings.Contains(name, "幻")
}

// FishingState returns the current 釣り view for the player.
func (s *Service) FishingState(ctx context.Context, playerID int64) (*FishingState, error) {
	out := &FishingState{Baits: []FishingBait{}}
	var cards int
	err := s.pool.QueryRow(ctx,
		`SELECT cards FROM player_fishing WHERE player_id = $1`, playerID).Scan(&cards)
	if err == nil {
		out.Active, out.Cards = true, cards
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read fishing: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT ci.id, ci.name, pi.remaining_uses
		 FROM player_items pi JOIN content_items ci ON ci.id = pi.item_id
		 WHERE pi.player_id = $1 AND pi.remaining_uses > 0 AND ci.category = $2
		 ORDER BY ci.id`, playerID, baitCategory)
	if err != nil {
		return nil, fmt.Errorf("list bait: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b FishingBait
		if err := rows.Scan(&b.ItemID, &b.Name, &b.Uses); err != nil {
			return nil, fmt.Errorf("scan bait: %w", err)
		}
		if isBaitName(b.Name) {
			out.Baits = append(out.Baits, b)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 持ち物の種類が上限なら釣った魚を持てない(レガシー tsuri.cgi:66)。
	if limit := s.settings.Get().ItemKindLimit; limit > 0 {
		var kinds int
		if err := s.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM player_items WHERE player_id = $1 AND remaining_uses > 0`,
			playerID).Scan(&kinds); err != nil {
			return nil, fmt.Errorf("count kinds: %w", err)
		}
		out.AtLimit = kinds >= limit
	}
	return out, nil
}

// DoFishingStart spends one bait and deals the first round of cards.
func (s *Service) DoFishingStart(ctx context.Context, playerID, itemID int64, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "fishing_start", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM player_fishing WHERE player_id = $1)`, playerID).Scan(&exists); err != nil {
			return fmt.Errorf("check session: %w", err)
		}
		if exists {
			return &ConditionError{Message: "すでに釣りを始めています。"}
		}
		if err := s.checkFishingRoom(ctx, tx, playerID); err != nil {
			return err
		}

		var name, category string
		var uses int
		err := tx.QueryRow(ctx,
			`SELECT ci.name, COALESCE(ci.category, ''), pi.remaining_uses
			 FROM player_items pi JOIN content_items ci ON ci.id = pi.item_id
			 WHERE pi.player_id = $1 AND pi.item_id = $2`, playerID, itemID).Scan(&name, &category, &uses)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その餌を持っていません。"}
		}
		if err != nil {
			return fmt.Errorf("load bait: %w", err)
		}
		if category != baitCategory || !isBaitName(name) || uses <= 0 {
			return &ConditionError{Message: "それは釣り餌ではありません。"}
		}
		// 餌を1消費する(レガシー tsuri.cgi:349-358)。
		if _, err := tx.Exec(ctx,
			`UPDATE player_items SET remaining_uses = remaining_uses - 1, updated_at = now()
			 WHERE player_id = $1 AND item_id = $2`, playerID, itemID); err != nil {
			return fmt.Errorf("consume bait: %w", err)
		}

		rankA := fishing.BaitRank(name)
		l := fishing.Deal(s.rng, fishing.InitialCards)
		rankB := fishing.InitialRankB(l.Cards, rankA)
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_fishing (player_id, rank_a, rank_b, cards, win_card, cont_card1, cont_card2)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			playerID, rankA, rankB, l.Cards, l.Win, l.Cont1, l.Cont2); err != nil {
			return fmt.Errorf("insert fishing: %w", err)
		}
		return nil
	})
}

// DoFishingPick resolves one card pick, returning the outcome.
func (s *Service) DoFishingPick(ctx context.Context, playerID int64, card int, idempotencyKey string) (*player.Player, *FishingResult, error) {
	var res *FishingResult
	p, err := s.runAction(ctx, playerID, "fishing_pick", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var rankA, rankB int
		var l fishing.Layout
		err := tx.QueryRow(ctx,
			`SELECT rank_a, rank_b, cards, win_card, cont_card1, cont_card2
			 FROM player_fishing WHERE player_id = $1 FOR UPDATE`, playerID).
			Scan(&rankA, &rankB, &l.Cards, &l.Win, &l.Cont1, &l.Cont2)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "釣りを始めていません。"}
		}
		if err != nil {
			return fmt.Errorf("load fishing: %w", err)
		}
		if card < 1 || card > l.Cards {
			return &ConditionError{Message: "そのカードはありません。"}
		}

		switch l.Pick(card) {
		case fishing.Continue:
			// 引いたカード番号がランクになる(レガシー tsuri.cgi:160-168)。
			if card == l.Cont1 {
				rankA = card
			} else {
				rankB = card
			}
			next := fishing.NextCards(s.rng)
			nl := fishing.Deal(s.rng, next)
			if rb := fishing.InitialRankB(nl.Cards, rankA); rb > rankB {
				rankB = rb
			}
			if _, err := tx.Exec(ctx,
				`UPDATE player_fishing SET rank_a = $2, rank_b = $3, cards = $4,
				        win_card = $5, cont_card1 = $6, cont_card2 = $7, updated_at = now()
				 WHERE player_id = $1`,
				playerID, rankA, rankB, nl.Cards, nl.Win, nl.Cont1, nl.Cont2); err != nil {
				return fmt.Errorf("update fishing: %w", err)
			}
			res = &FishingResult{Outcome: string(fishing.Continue), Cards: nl.Cards}
			return nil

		case fishing.Win:
			fish := fishing.FishName(rankA + rankB)
			if err := s.grantFish(ctx, tx, playerID, fish); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM player_fishing WHERE player_id = $1`, playerID); err != nil {
				return fmt.Errorf("clear fishing: %w", err)
			}
			res = &FishingResult{Outcome: string(fishing.Win), Fish: fish}
			return nil

		default:
			if _, err := tx.Exec(ctx, `DELETE FROM player_fishing WHERE player_id = $1`, playerID); err != nil {
				return fmt.Errorf("clear fishing: %w", err)
			}
			res = &FishingResult{Outcome: string(fishing.Lose)}
			return nil
		}
	})
	return p, res, err
}

// checkFishingRoom rejects fishing when the player already holds the maximum
// number of item kinds (they would have nowhere to put the fish).
func (s *Service) checkFishingRoom(ctx context.Context, tx pgx.Tx, playerID int64) error {
	limit := s.settings.Get().ItemKindLimit
	if limit <= 0 {
		return nil
	}
	var kinds int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM player_items WHERE player_id = $1 AND remaining_uses > 0`,
		playerID).Scan(&kinds); err != nil {
		return fmt.Errorf("count kinds: %w", err)
	}
	if kinds >= limit {
		return &ConditionError{Message: fmt.Sprintf("持ち物が上限なので釣りができません(%d/%d)。", kinds, limit)}
	}
	return nil
}

// catchUses is how much durability one landed fish is worth. The legacy prize
// table (dat_dir/tsuri.cgi) gives every fish 耐久1 — the larger durability on
// the shop rows is for the store-bought copies, so a catch must not inherit it
// (5回分もらえると釣りが儲かりすぎる)。
const catchUses = 1

// grantFish adds one landed fish to the player's inventory.
func (s *Service) grantFish(ctx context.Context, tx pgx.Tx, playerID int64, name string) error {
	var itemID int64
	err := tx.QueryRow(ctx,
		`SELECT id FROM content_items WHERE name = $1 ORDER BY id LIMIT 1`, name).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("fish item %q not found", name)
	}
	if err != nil {
		return fmt.Errorf("load fish: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses)
		 VALUES ($1, $2, 1, $3)
		 ON CONFLICT (player_id, item_id)
		 DO UPDATE SET quantity = player_items.quantity + 1,
		               remaining_uses = player_items.remaining_uses + $3,
		               updated_at = now()`,
		playerID, itemID, catchUses); err != nil {
		return fmt.Errorf("grant fish: %w", err)
	}
	return nil
}
