package content

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// YamiItem is one listing on a 持ち物販売店 (闇市) shelf. Each row is a single
// unit with its own remaining durability (レガシー3_log.cgiの1行=1品).
type YamiItem struct {
	ListingID int64  `json:"listing_id"`
	ItemID    int64  `json:"item_id"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	Price     int64  `json:"price"`
	Uses      int    `json:"uses"`    // この1品の残り耐久
	Zokusei   int    `json:"zokusei"` // 1=倉庫品(家主のみ表示)
	// 商品詳細(レガシー闇市表示の全カラム相当)。
	Money          int64          `json:"money"`
	Params         map[string]int `json:"params"`
	CalorieG       int            `json:"calorie_g"`
	Special        string         `json:"special"` // 特殊効果の説明(体重/身長/病気)
	DurabilityUnit string         `json:"durability_unit"`
	IntervalMin    int            `json:"interval_min"`
	BodyCost       int            `json:"body_cost"`
	NouCost        int            `json:"nou_cost"`
}

// YamiView is a 持ち物販売店 as seen by a viewer. 倉庫品(zokusei=1)は家主にだけ
// 含まれる。
type YamiView struct {
	IsYami    bool       `json:"is_yami"`
	OwnerName string     `json:"owner_name"`
	Own       bool       `json:"own"`
	MaxItems  int        `json:"max_items"`
	Items     []YamiItem `json:"items"`
}

// Yami returns the 闇市 shelf of a 持ち物販売店 house.
func (s *Service) Yami(ctx context.Context, viewerID, houseID int64) (*YamiView, error) {
	var (
		ownerID   int64
		tuika     int
		ownerName string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT h.owner_id, h.tuika, COALESCE(p.display_name, '')
		 FROM player_houses h LEFT JOIN players p ON p.id = h.owner_id
		 WHERE h.id = $1`, houseID).Scan(&ownerID, &tuika, &ownerName)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && tuika != 3) {
		return &YamiView{IsYami: false, Items: []YamiItem{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load house: %w", err)
	}
	view := &YamiView{IsYami: true, OwnerName: ownerName, Own: viewerID == ownerID, MaxItems: 25, Items: []YamiItem{}}
	rows, err := s.pool.Query(ctx,
		`SELECT yi.id, yi.item_id, yi.price, yi.uses, yi.zokusei,
		        ci.name, ci.category, ci.effect, ci.calorie_g, ci.durability_unit,
		        ci.use_interval_min, ci.body_cost, ci.nou_cost
		 FROM yami_items yi JOIN content_items ci ON ci.id = yi.item_id
		 WHERE yi.house_id = $1 AND ($2 OR yi.zokusei = 0)
		 ORDER BY ci.category, yi.id`, houseID, view.Own)
	if err != nil {
		return nil, fmt.Errorf("list yami: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			it      YamiItem
			effJSON []byte
		)
		if err := rows.Scan(&it.ListingID, &it.ItemID, &it.Price, &it.Uses, &it.Zokusei,
			&it.Name, &it.Category, &effJSON, &it.CalorieG, &it.DurabilityUnit,
			&it.IntervalMin, &it.BodyCost, &it.NouCost); err != nil {
			return nil, fmt.Errorf("scan yami: %w", err)
		}
		it.Money, it.Params, it.Special = effectSummary(effJSON)
		view.Items = append(view.Items, it)
	}
	return view, rows.Err()
}

// YamiInventoryItem is one of the owner's own items, for the listing screen
// (レガシーmothi_baikyaku). Uses is the durability the listed unit would carry;
// DefaultPrice is 単価×残耐久.
type YamiInventoryItem struct {
	ItemID         int64  `json:"item_id"`
	Name           string `json:"name"`
	Category       string `json:"category"`
	Quantity       int    `json:"quantity"`
	Uses           int    `json:"uses"`
	DurabilityUnit string `json:"durability_unit"`
	DefaultPrice   int64  `json:"default_price"`
}

// YamiInventory lists the player's own items for listing onto their 闇市.
func (s *Service) YamiInventory(ctx context.Context, playerID int64) ([]YamiInventoryItem, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT pi.item_id, ci.name, ci.category, pi.quantity, pi.remaining_uses,
		        GREATEST(ci.durability, 1), ci.durability_unit, ci.price
		 FROM player_items pi JOIN content_items ci ON ci.id = pi.item_id
		 WHERE pi.player_id = $1 AND pi.quantity > 0 AND pi.remaining_uses > 0
		 ORDER BY ci.category, ci.name`, playerID)
	if err != nil {
		return nil, fmt.Errorf("list inventory: %w", err)
	}
	defer rows.Close()
	out := []YamiInventoryItem{}
	for rows.Next() {
		var (
			it                    YamiInventoryItem
			remaining, durability int
			masterPrice           int64
		)
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Category, &it.Quantity, &remaining,
			&durability, &it.DurabilityUnit, &masterPrice); err != nil {
			return nil, fmt.Errorf("scan inventory: %w", err)
		}
		it.Uses = remaining
		if it.Uses > durability {
			it.Uses = durability
		}
		it.DefaultPrice = masterPrice / int64(durability) * int64(it.Uses)
		if it.DefaultPrice <= 0 {
			it.DefaultPrice = masterPrice
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
