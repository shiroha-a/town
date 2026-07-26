package action

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/player"
)

// yamiMaxItems caps the listings of a 持ち物販売店 (レガシー$mothi_jougen=25).
const yamiMaxItems = 25

// yamiBuybackFee is the cash fee when the owner takes back their own listing
// (レガシーmothi_do: 自分の店では売上が入らず500円かかる).
const yamiBuybackFee = 500

// DoYamiList moves one unit of the player's own item onto their 持ち物販売店
// (闇市) shelf (レガシーmothi_baikyaku_do). warehouse=true stores it as 倉庫品
// (訪問者に非表示). price=0 uses the default price (単価×残耐久).
func (s *Service) DoYamiList(ctx context.Context, playerID, houseID, itemID, price int64, warehouse bool, idempotencyKey string) (*player.Player, error) {
	if price < 0 {
		return nil, &ConditionError{Message: "マイナス設定は出来ません。"}
	}
	return s.runAction(ctx, playerID, "yami_list", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		if err := loadYamiHouse(ctx, tx, houseID, playerID, true); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM yami_items WHERE house_id = $1`, houseID).Scan(&count); err != nil {
			return fmt.Errorf("count listings: %w", err)
		}
		if count >= yamiMaxItems {
			return &ConditionError{Message: fmt.Sprintf("お店に置けるのは%d品までです。", yamiMaxItems)}
		}
		var (
			qty, remaining, durability int
			masterPrice                int64
		)
		err := tx.QueryRow(ctx,
			`SELECT pi.quantity, pi.remaining_uses, GREATEST(ci.durability, 1), ci.price
			 FROM player_items pi JOIN content_items ci ON ci.id = pi.item_id
			 WHERE pi.player_id = $1 AND pi.item_id = $2`, playerID, itemID).
			Scan(&qty, &remaining, &durability, &masterPrice)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "そのアイテムを持っていません。"}
		}
		if err != nil {
			return fmt.Errorf("load item: %w", err)
		}
		if qty <= 0 || remaining <= 0 {
			return &ConditionError{Message: "そのアイテムを持っていません。"}
		}
		// 出品する1個の残り耐久。新品があれば新品(耐久フル)から出す。
		uses := remaining
		if uses > durability {
			uses = durability
		}
		if price == 0 {
			// 既定価格 = 単価(マスタ価格/耐久)×残り耐久。
			price = masterPrice / int64(durability) * int64(uses)
			if price <= 0 {
				price = masterPrice
			}
		}
		// 持ち物から1個分を減らす。
		if qty == 1 || remaining-uses <= 0 {
			if _, err := tx.Exec(ctx,
				`DELETE FROM player_items WHERE player_id = $1 AND item_id = $2`, playerID, itemID); err != nil {
				return fmt.Errorf("remove item: %w", err)
			}
		} else {
			if _, err := tx.Exec(ctx,
				`UPDATE player_items SET quantity = quantity - 1, remaining_uses = remaining_uses - $3, updated_at = now()
				 WHERE player_id = $1 AND item_id = $2`, playerID, itemID, uses); err != nil {
				return fmt.Errorf("reduce item: %w", err)
			}
		}
		zokusei := 0
		if warehouse {
			zokusei = 1
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO yami_items (house_id, item_id, price, uses, zokusei) VALUES ($1, $2, $3, $4, $5)`,
			houseID, itemID, price, uses, zokusei); err != nil {
			return fmt.Errorf("insert listing: %w", err)
		}
		return nil
	})
}

// YamiBuyResult summarizes a 闇市 purchase for the result toast.
type YamiBuyResult struct {
	Name   string `json:"name"`
	Paid   int64  `json:"paid"`
	Method string `json:"method"`
	Own    bool   `json:"own"` // 自分の店からの回収(手数料500円)
}

// DoYamiBuy buys one listing from a 持ち物販売店 (レガシーmothi_do). Visitors pay
// the listed price (現金/クレジット→普通口座) to the owner; the owner may take
// back any listing (倉庫品含む) for a 500円 cash fee with no sale proceeds.
func (s *Service) DoYamiBuy(ctx context.Context, buyerID, houseID, listingID int64, payMethod, idempotencyKey string) (*player.Player, *YamiBuyResult, error) {
	if payMethod == "" {
		payMethod = "cash"
	}
	if payMethod != "cash" && payMethod != "credit" {
		return nil, nil, &ConditionError{Message: "支払い方法が正しくありません。"}
	}
	result := &YamiBuyResult{Method: payMethod}
	p, err := s.runAction(ctx, buyerID, "yami_buy", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var ownerID int64
		err := tx.QueryRow(ctx,
			`SELECT owner_id FROM player_houses WHERE id = $1 AND tuika = 3`, houseID).Scan(&ownerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その持ち物販売店はありません。"}
		}
		if err != nil {
			return fmt.Errorf("load house: %w", err)
		}
		var (
			itemID  int64
			price   int64
			uses    int
			zokusei int
			name    string
		)
		err = tx.QueryRow(ctx,
			`SELECT yi.item_id, yi.price, yi.uses, yi.zokusei, ci.name
			 FROM yami_items yi JOIN content_items ci ON ci.id = yi.item_id
			 WHERE yi.id = $1 AND yi.house_id = $2`, listingID, houseID).
			Scan(&itemID, &price, &uses, &zokusei, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その商品はありません。"}
		}
		if err != nil {
			return fmt.Errorf("load listing: %w", err)
		}
		result.Name = name
		result.Own = buyerID == ownerID
		// 所持上限(最大セット)チェック。
		var durability, maxSets int
		if err := tx.QueryRow(ctx,
			`SELECT GREATEST(durability, 1), max_sets FROM content_items WHERE id = $1`, itemID).
			Scan(&durability, &maxSets); err != nil {
			return fmt.Errorf("item: %w", err)
		}
		var current int
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(remaining_uses, 0) FROM player_items WHERE player_id = $1 AND item_id = $2`,
			buyerID, itemID).Scan(&current); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if maxSets > 0 && current+uses > maxSets*durability {
			return &ConditionError{Message: fmt.Sprintf("これ以上は持てません(最大%dセット)。", maxSets)}
		}
		if buyerID == ownerID {
			// 自分の店からの回収: 手数料500円(現金)。売上は入らない。
			if state.Money < yamiBuybackFee {
				return &ConditionError{Message: "お金が足りません"}
			}
			if err := s.ledger.PostTx(ctx, tx, "yami_buyback", "", []ledger.Entry{
				{Account: ledger.PlayerAccount(buyerID), Delta: -yamiBuybackFee},
				{Account: ledger.SystemAccount("yami_fee"), Delta: yamiBuybackFee},
			}); err != nil {
				return fmt.Errorf("fee: %w", err)
			}
			result.Paid = yamiBuybackFee
		} else {
			if zokusei != 0 {
				return &ConditionError{Message: "その商品はありません。"}
			}
			buyerAccount := ledger.PlayerAccount(buyerID)
			if payMethod == "credit" {
				ok, err := hasCreditCard(ctx, tx, buyerID)
				if err != nil {
					return err
				}
				if !ok {
					return &ConditionError{Message: "クレジットカードを持っていません。"}
				}
				var savings int64
				if err := tx.QueryRow(ctx,
					`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
					ledger.SavingsAccount(buyerID)).Scan(&savings); err != nil {
					return fmt.Errorf("read savings: %w", err)
				}
				if savings < price {
					return &ConditionError{Message: "貯金がありません。"}
				}
				buyerAccount = ledger.SavingsAccount(buyerID)
			} else if state.Money < price {
				return &ConditionError{Message: "お金が足りません"}
			}
			if price > 0 {
				if err := s.ledger.PostTx(ctx, tx, "yami_buy", "", []ledger.Entry{
					{Account: buyerAccount, Delta: -price},
					{Account: ledger.SavingsAccount(ownerID), Delta: price},
				}); err != nil {
					return fmt.Errorf("pay: %w", err)
				}
			}
			result.Paid = price
		}
		if _, err := tx.Exec(ctx, `DELETE FROM yami_items WHERE id = $1`, listingID); err != nil {
			return fmt.Errorf("remove listing: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses) VALUES ($1, $2, 1, $3)
			 ON CONFLICT (player_id, item_id) DO UPDATE SET quantity = player_items.quantity + 1,
			   remaining_uses = player_items.remaining_uses + $3, updated_at = now()`,
			buyerID, itemID, uses); err != nil {
			return fmt.Errorf("grant item: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return p, result, nil
}

// loadYamiHouse validates a 持ち物販売店 house. requireOwner enforces ownership.
func loadYamiHouse(ctx context.Context, tx pgx.Tx, houseID, playerID int64, requireOwner bool) error {
	var ownerID int64
	var tuika int
	err := tx.QueryRow(ctx,
		`SELECT owner_id, tuika FROM player_houses WHERE id = $1`, houseID).Scan(&ownerID, &tuika)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ConditionError{Message: "その家は存在しません。"}
	}
	if err != nil {
		return fmt.Errorf("load house: %w", err)
	}
	if tuika != 3 {
		return &ConditionError{Message: "この家は持ち物販売店ではありません。"}
	}
	if requireOwner && ownerID != playerID {
		return &ConditionError{Message: "自分の店だけ操作できます。"}
	}
	return nil
}
