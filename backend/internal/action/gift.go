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

// GiftFee is the conversion fee charged by the ギフト屋 (レガシー gifutoya.cgi
// の $koukan_money)。
const GiftFee int64 = 50000

// Gift is one wrapped present the player holds. Gifts cannot be used by their
// owner — they exist to be mailed to someone else (レガシー「ギフトは贈り物専用の
// 商品です。自分で使用することはできません。」)。
type Gift struct {
	ID     int64  `json:"id"`
	ItemID int64  `json:"item_id"`
	Name   string `json:"name"`
	Uses   int    `json:"uses"`
}

// GiftConvertible is an inventory item that can be turned into a gift.
type GiftConvertible struct {
	ItemID int64  `json:"item_id"`
	Name   string `json:"name"`
	Uses   int    `json:"uses"`
}

// GiftShopState is the ギフト屋 view.
type GiftShopState struct {
	Fee          int64             `json:"fee"`
	Gifts        []Gift            `json:"gifts"`
	Convertibles []GiftConvertible `json:"convertibles"`
}

// GiftShopState returns the player's gifts plus what they could wrap.
func (s *Service) GiftShopState(ctx context.Context, playerID int64) (*GiftShopState, error) {
	out := &GiftShopState{Fee: GiftFee, Gifts: []Gift{}, Convertibles: []GiftConvertible{}}

	gifts, err := s.pool.Query(ctx,
		`SELECT g.id, g.item_id, ci.name, g.uses
		 FROM player_gifts g JOIN content_items ci ON ci.id = g.item_id
		 WHERE g.owner_id = $1 ORDER BY g.id`, playerID)
	if err != nil {
		return nil, fmt.Errorf("list gifts: %w", err)
	}
	defer gifts.Close()
	for gifts.Next() {
		var g Gift
		if err := gifts.Scan(&g.ID, &g.ItemID, &g.Name, &g.Uses); err != nil {
			return nil, fmt.Errorf("scan gift: %w", err)
		}
		out.Gifts = append(out.Gifts, g)
	}
	if err := gifts.Err(); err != nil {
		return nil, err
	}

	items, err := s.pool.Query(ctx,
		`SELECT ci.id, ci.name, pi.remaining_uses
		 FROM player_items pi JOIN content_items ci ON ci.id = pi.item_id
		 WHERE pi.player_id = $1 AND pi.remaining_uses > 0
		 ORDER BY ci.id`, playerID)
	if err != nil {
		return nil, fmt.Errorf("list convertibles: %w", err)
	}
	defer items.Close()
	for items.Next() {
		var c GiftConvertible
		if err := items.Scan(&c.ItemID, &c.Name, &c.Uses); err != nil {
			return nil, fmt.Errorf("scan convertible: %w", err)
		}
		out.Convertibles = append(out.Convertibles, c)
	}
	return out, items.Err()
}

// DoGiftConvert wraps `uses` units of a held item into a gift for the fee.
// uses <= 0 or >= the held amount wraps the whole stack (レガシー gifutoya.cgi
// の henkansu)。
func (s *Service) DoGiftConvert(ctx context.Context, playerID, itemID int64, uses int, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "gift_convert", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if state.Money < GiftFee {
			return &ConditionError{Message: fmt.Sprintf("手数料%d円が足りません。", GiftFee)}
		}
		var name string
		var held int
		err := tx.QueryRow(ctx,
			`SELECT ci.name, pi.remaining_uses
			 FROM player_items pi JOIN content_items ci ON ci.id = pi.item_id
			 WHERE pi.player_id = $1 AND pi.item_id = $2 FOR UPDATE OF pi`,
			playerID, itemID).Scan(&name, &held)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "そのアイテムを持っていません。"}
		}
		if err != nil {
			return fmt.Errorf("load item: %w", err)
		}
		if held <= 0 {
			return &ConditionError{Message: "そのアイテムを持っていません。"}
		}
		if uses <= 0 || uses > held {
			uses = held
		}

		if err := s.ledger.PostTx(ctx, tx, "gift_convert", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -GiftFee},
			{Account: ledger.SystemAccount("gift_fee"), Delta: GiftFee},
		}); err != nil {
			return fmt.Errorf("pay fee: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE player_items SET remaining_uses = remaining_uses - $3, updated_at = now()
			 WHERE player_id = $1 AND item_id = $2`, playerID, itemID, uses); err != nil {
			return fmt.Errorf("take from inventory: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_gifts (owner_id, item_id, uses) VALUES ($1, $2, $3)`,
			playerID, itemID, uses); err != nil {
			return fmt.Errorf("insert gift: %w", err)
		}
		return nil
	})
}

// GrantGift moves one unit of the sender's gift into the recipient's inventory
// and returns the item name for the mail record. It runs inside the mail
// transaction (レガシー command.pl:3575-3610: 送ると耐久が1減り、相手には
// 「ギフト商品」として1個入る=相手は普通に使える)。
func (s *Service) GrantGift(ctx context.Context, tx pgx.Tx, senderID, recipientID, giftID int64) (string, error) {
	var itemID int64
	var uses int
	var name string
	err := tx.QueryRow(ctx,
		`SELECT g.item_id, g.uses, ci.name FROM player_gifts g
		 JOIN content_items ci ON ci.id = g.item_id
		 WHERE g.id = $1 AND g.owner_id = $2 FOR UPDATE OF g`,
		giftID, senderID).Scan(&itemID, &uses, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &ConditionError{Message: "その贈り物を持っていません。"}
	}
	if err != nil {
		return "", fmt.Errorf("load gift: %w", err)
	}
	// 1個渡す。残りが無くなったら行ごと消す。
	if uses <= 1 {
		if _, err := tx.Exec(ctx, `DELETE FROM player_gifts WHERE id = $1`, giftID); err != nil {
			return "", fmt.Errorf("consume gift: %w", err)
		}
	} else if _, err := tx.Exec(ctx,
		`UPDATE player_gifts SET uses = uses - 1 WHERE id = $1`, giftID); err != nil {
		return "", fmt.Errorf("consume gift: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses)
		 VALUES ($1, $2, 1, 1)
		 ON CONFLICT (player_id, item_id)
		 DO UPDATE SET quantity = player_items.quantity + 1,
		               remaining_uses = player_items.remaining_uses + 1,
		               updated_at = now()`,
		recipientID, itemID); err != nil {
		return "", fmt.Errorf("grant gift: %w", err)
	}
	return name, nil
}
