package content

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/building"
	"github.com/shiroha-a/town/internal/townmap"
)

// BuildingState is everything the build screen (建設会社/KentikuView) needs to
// render the town grids, the exterior/interior catalog, and the player's houses.
type BuildingState struct {
	Towns      []building.Town     `json:"towns"`
	Exteriors  []building.Exterior `json:"exteriors"`
	Interiors  []building.Interior `json:"interiors"`
	Tuikas     []building.Tuika    `json:"tuikas"`      // 2軒目以降の追加種別
	ShinsaOK   bool                `json:"shinsa_ok"`   // 能力審査(総資産1億+全パラ1万)合格か
	Plots      []PlotCell          `json:"plots"`       // 管理者が指定した空地マス
	Houses     []HouseCell         `json:"houses"`      // 全プレイヤーの家(グリッド描画用)
	MyHouses   []MyHouse           `json:"my_houses"`   // 自分の家(一覧)
	Permits    int                 `json:"permits"`     // 所持している建築許可証の枚数
	PermitSpan int                 `json:"permit_span"` // 許可証で建てられる最大の横幅(0=許可証なし)
	CostFactor int                 `json:"cost_factor"` // 2マス以上の家にかかる建築費の倍率
	ShopKinds  []string            `json:"shop_kinds"`  // 店の種類の選択肢
	HouseCount int                 `json:"house_count"` // 自分の所有軒数
	MochiieMax int                 `json:"mochiie_max"`
	Cols       int                 `json:"cols"`
	Rows       int                 `json:"rows"`
}

// HouseContent is one configured in-house content slot (コンテンツ枠)。訪問者には
// 設定された枠のコンテンツだけが枠順に表示される(一番上の枠が入室時の初期表示)。
type HouseContent struct {
	Slot    int    `json:"slot"`
	Kind    string `json:"kind"` // 'bbs'=通常掲示板 / 'shop'=お店 / 'nushi'=家主板 / 'url'=独自URL
	Title   string `json:"title"`
	URL     string `json:"url"`     // kind='url' の埋め込みURL
	Comment string `json:"comment"` // タイトル下コメント(リード文)
}

// HouseCell is a house on the map (any owner), used for grid rendering and
// visiting. Setumei is the owner's mouse-over comment (フェーズ3a).
type HouseCell struct {
	ID        int64          `json:"id"`
	Town      int            `json:"town"`
	Row       int            `json:"row"`
	Col       int            `json:"col"`
	Exterior  string         `json:"exterior"`
	Setumei   string         `json:"setumei"`
	SpanW     int            `json:"span_w"` // 横のマス数(1 or 2)
	OwnerName string         `json:"owner_name"`
	Own       bool           `json:"own"`
	Tuika     int            `json:"tuika"` // 0=家のみ/1=運営/2=株式会社/3=持ち物販売店
	Contents  []HouseContent `json:"contents"`
}

// MyHouse is one of the player's own houses (for the owned-houses list).
type MyHouse struct {
	ID           int64          `json:"id"`
	Town         int            `json:"town"`
	Row          int            `json:"row"`
	Col          int            `json:"col"`
	Exterior     string         `json:"exterior"`
	Setumei      string         `json:"setumei"`
	SpanW        int            `json:"span_w"` // 横のマス数(1 or 2)
	InteriorRank int            `json:"interior_rank"`
	Tuika        int            `json:"tuika"`    // 0=家のみ/1=運営/2=株式会社/3=持ち物販売店
	Slots        int            `json:"slots"`    // 内装ランクで決まるコンテンツ枠数
	BuiltAt      string         `json:"built_at"` // RFC3339
	HasShop      bool           `json:"has_shop"`
	ShopTitle    string         `json:"shop_title"`
	ShopKind     string         `json:"shop_kind"`
	ShopMarkup   float64        `json:"shop_markup"`
	Contents     []HouseContent `json:"contents"`
}

// PlotCell is an admin-designated empty plot on which a house may be built.
type PlotCell struct {
	Town int `json:"town"`
	Row  int `json:"row"`
	Col  int `json:"col"`
}

// Building returns the full state of the construction screen for a player.
func (s *Service) Building(ctx context.Context, playerID int64) (*BuildingState, error) {
	st := &BuildingState{
		Towns:      building.Towns(),
		Exteriors:  building.Exteriors(),
		Interiors:  building.Interiors(),
		Tuikas:     building.Tuikas(),
		MochiieMax: building.MochiieMax,
		CostFactor: building.MulticellCostFactor,
		Cols:       townmap.Cols,
		Rows:       townmap.Rows,
		ShopKinds:  building.ShopKinds(),
		Houses:     []HouseCell{},
		MyHouses:   []MyHouse{},
	}
	// 能力審査(株式会社/持ち物販売店の選択条件): 総資産1億+全パラ1万。
	var assets int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account IN ($1, $2, $3)`,
		fmt.Sprintf("player:%d", playerID), fmt.Sprintf("savings:%d", playerID),
		fmt.Sprintf("super_savings:%d", playerID)).Scan(&assets); err != nil {
		return nil, fmt.Errorf("sum assets: %w", err)
	}
	var minParam int
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(LEAST(kokugo, suugaku, rika, syakai, eigo, ongaku, bijutsu, looks,
		              tairyoku, kenkou, speed, power, wanryoku, kyakuryoku, love, omoshirosa), 0)
		 FROM player_status WHERE player_id = $1`, playerID).Scan(&minParam); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read params: %w", err)
	}
	st.ShinsaOK = assets >= building.ShinsaAsset && minParam >= building.ShinsaParam

	// 建築許可証(content_items.build_span)の所持枚数と、建てられる最大の横幅。
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(pi.quantity), 0)::int, COALESCE(MAX(ci.build_span), 0)
		 FROM player_items pi JOIN content_items ci ON ci.id = pi.item_id
		 WHERE pi.player_id = $1 AND pi.quantity > 0 AND pi.remaining_uses > 0 AND ci.build_span > 1`,
		playerID).Scan(&st.Permits, &st.PermitSpan); err != nil {
		return nil, fmt.Errorf("count build permits: %w", err)
	}

	plots, err := s.ListPlots(ctx)
	if err != nil {
		return nil, err
	}
	st.Plots = plots

	houses, err := s.ListHouses(ctx, playerID)
	if err != nil {
		return nil, err
	}
	st.Houses = houses

	mrows, err := s.pool.Query(ctx,
		`SELECT h.id, h.town, h.grid_row, h.grid_col, h.exterior, h.setumei, h.span_w, h.interior_rank, h.tuika, h.built_at,
		        (hs.house_id IS NOT NULL), COALESCE(hs.title, ''), COALESCE(hs.syubetu, ''), COALESCE(hs.markup, 0)::float8
		 FROM player_houses h LEFT JOIN house_shops hs ON hs.house_id = h.id
		 WHERE h.owner_id = $1 ORDER BY h.built_at`, playerID)
	if err != nil {
		return nil, fmt.Errorf("list my houses: %w", err)
	}
	defer mrows.Close()
	for mrows.Next() {
		var (
			h     MyHouse
			built time.Time
		)
		if err := mrows.Scan(&h.ID, &h.Town, &h.Row, &h.Col, &h.Exterior, &h.Setumei, &h.SpanW, &h.InteriorRank, &h.Tuika, &built,
			&h.HasShop, &h.ShopTitle, &h.ShopKind, &h.ShopMarkup); err != nil {
			return nil, fmt.Errorf("scan my house: %w", err)
		}
		h.BuiltAt = built.Format(time.RFC3339)
		h.Slots = building.SlotsByRank(h.InteriorRank)
		st.MyHouses = append(st.MyHouses, h)
	}
	if err := mrows.Err(); err != nil {
		return nil, fmt.Errorf("iterate my houses: %w", err)
	}
	// 自分の家のコンテンツ枠は全家一覧(ListHouses)で取得済みのものを引き当てる。
	byID := map[int64][]HouseContent{}
	for _, h := range st.Houses {
		byID[h.ID] = h.Contents
	}
	for i := range st.MyHouses {
		st.MyHouses[i].Contents = byID[st.MyHouses[i].ID]
		if st.MyHouses[i].Contents == nil {
			st.MyHouses[i].Contents = []HouseContent{}
		}
	}
	st.HouseCount = len(st.MyHouses)
	return st, nil
}

// loadHouseContents returns every house's configured content slots, keyed by
// house id and ordered by slot.
func (s *Service) loadHouseContents(ctx context.Context) (map[int64][]HouseContent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT house_id, slot, kind, title, url, comment FROM house_contents ORDER BY house_id, slot`)
	if err != nil {
		return nil, fmt.Errorf("list house contents: %w", err)
	}
	defer rows.Close()
	out := map[int64][]HouseContent{}
	for rows.Next() {
		var (
			houseID int64
			c       HouseContent
		)
		if err := rows.Scan(&houseID, &c.Slot, &c.Kind, &c.Title, &c.URL, &c.Comment); err != nil {
			return nil, fmt.Errorf("scan house content: %w", err)
		}
		out[houseID] = append(out[houseID], c)
	}
	return out, rows.Err()
}

// ListHouses returns every house across all towns (for map rendering). Own is
// set relative to playerID (pass 0 for none).
func (s *Service) ListHouses(ctx context.Context, playerID int64) ([]HouseCell, error) {
	contents, err := s.loadHouseContents(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT h.id, h.town, h.grid_row, h.grid_col, h.exterior, h.setumei, h.span_w, h.tuika, h.owner_id, COALESCE(p.display_name, '')
		 FROM player_houses h LEFT JOIN players p ON p.id = h.owner_id
		 ORDER BY h.town, h.grid_row, h.grid_col`)
	if err != nil {
		return nil, fmt.Errorf("list houses: %w", err)
	}
	defer rows.Close()
	out := []HouseCell{}
	for rows.Next() {
		var (
			c       HouseCell
			ownerID int64
		)
		if err := rows.Scan(&c.ID, &c.Town, &c.Row, &c.Col, &c.Exterior, &c.Setumei, &c.SpanW, &c.Tuika, &ownerID, &c.OwnerName); err != nil {
			return nil, fmt.Errorf("scan house: %w", err)
		}
		c.Own = ownerID == playerID
		c.Contents = contents[c.ID]
		if c.Contents == nil {
			c.Contents = []HouseContent{}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListHouseCells returns every cell that currently has a house (any owner),
// across all towns. 施設編集で家のあるマスをロックするために使う。
// 2マスの家は覆っている全マスを返す(占有マスの実体テーブルから読む)。
func (s *Service) ListHouseCells(ctx context.Context) ([]PlotCell, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT town, grid_row, grid_col FROM player_house_cells ORDER BY town, grid_row, grid_col`)
	if err != nil {
		return nil, fmt.Errorf("list house cells: %w", err)
	}
	defer rows.Close()
	out := []PlotCell{}
	for rows.Next() {
		var c PlotCell
		if err := rows.Scan(&c.Town, &c.Row, &c.Col); err != nil {
			return nil, fmt.Errorf("scan house cell: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListPlots returns every buildable empty plot across all towns. 空き地は施設に
// 統合済みなので、key='akichi' の施設マスを空地として返す。
func (s *Service) ListPlots(ctx context.Context) ([]PlotCell, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT COALESCE((f->>'town')::int, 0), (f->>'row')::int, (f->>'col')::int
		 FROM town_map, jsonb_array_elements(facilities) f
		 WHERE id = 1 AND f->>'key' = 'akichi'
		 ORDER BY 1, 2, 3`)
	if err != nil {
		return nil, fmt.Errorf("list plots: %w", err)
	}
	defer rows.Close()
	out := []PlotCell{}
	for rows.Next() {
		var c PlotCell
		if err := rows.Scan(&c.Town, &c.Row, &c.Col); err != nil {
			return nil, fmt.Errorf("scan plot: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// OrosiItem is one item available at the wholesaler for the shop's category.
type OrosiItem struct {
	ItemID   int64  `json:"item_id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	BuyPrice int64  `json:"buy_price"` // 仕入れ値(スーパーは1.5倍込み)
	InStock  int    `json:"in_stock"`  // 現在の店在庫
}

// OrosiState is the wholesaler screen for a house shop (卸問屋 フェーズ4b).
type OrosiState struct {
	Syubetu    string      `json:"syubetu"`
	Markup     float64     `json:"markup"`
	Savings    int64       `json:"savings"`     // 普通口座残高
	StockKinds int         `json:"stock_kinds"` // 現在の在庫種類数
	MaxKinds   int         `json:"max_kinds"`
	MaxStock   int         `json:"max_stock"`
	Items      []OrosiItem `json:"items"`
}

// Orosi returns the wholesaler catalog for the player's house shop: the items
// the shop's category can stock, their purchase price (スーパーは1.5倍), and the
// shop's current per-item stock.
func (s *Service) Orosi(ctx context.Context, playerID, houseID int64) (*OrosiState, error) {
	var (
		syubetu string
		markup  float64
	)
	err := s.pool.QueryRow(ctx,
		`SELECT hs.syubetu, hs.markup
		 FROM house_shops hs JOIN player_houses h ON h.id = hs.house_id
		 WHERE hs.house_id = $1 AND h.owner_id = $2`, houseID, playerID).Scan(&syubetu, &markup)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &ValidationError{Message: "その家に自分の店がありません。"}
	}
	if err != nil {
		return nil, fmt.Errorf("load shop: %w", err)
	}

	super := syubetu == building.SuperMarketKind
	items := []OrosiItem{}
	var rows pgx.Rows
	if super {
		rows, err = s.pool.Query(ctx,
			`SELECT id, name, category, price FROM content_items
			 WHERE enabled AND shop_listed AND facility = '' AND category = ANY($1) AND category <> $2
			 ORDER BY category, name`, building.ShopKinds(), building.SuperMarketKind)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, name, category, price FROM content_items
			 WHERE enabled AND shop_listed AND facility = '' AND category = $1
			 ORDER BY name`, syubetu)
	}
	if err != nil {
		return nil, fmt.Errorf("list orosi: %w", err)
	}
	for rows.Next() {
		var it OrosiItem
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Category, &it.BuyPrice); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan orosi: %w", err)
		}
		if super {
			it.BuyPrice = it.BuyPrice * 3 / 2 // スーパーは1.5倍
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orosi: %w", err)
	}

	// 現在の店在庫を先に読み切ってからマップ化する。
	stockMap := map[int64]int{}
	srows, err := s.pool.Query(ctx, `SELECT item_id, stock FROM house_shop_stock WHERE house_id = $1`, houseID)
	if err != nil {
		return nil, fmt.Errorf("load stock: %w", err)
	}
	for srows.Next() {
		var (
			id  int64
			stk int
		)
		if err := srows.Scan(&id, &stk); err != nil {
			srows.Close()
			return nil, fmt.Errorf("scan stock: %w", err)
		}
		stockMap[id] = stk
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stock: %w", err)
	}

	var savings int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
		"savings:"+strconv.FormatInt(playerID, 10)).Scan(&savings); err != nil {
		return nil, fmt.Errorf("load savings: %w", err)
	}
	for i := range items {
		items[i].InStock = stockMap[items[i].ItemID]
	}
	return &OrosiState{
		Syubetu:    syubetu,
		Markup:     markup,
		Savings:    savings,
		StockKinds: len(stockMap),
		MaxKinds:   building.ShopMaxKinds,
		MaxStock:   building.ShopMaxStock,
		Items:      items,
	}, nil
}

// HouseShopItem is one item on sale at a house shop (訪問時の店表示).
type HouseShopItem struct {
	ItemID   int64  `json:"item_id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Price    int64  `json:"price"` // 店頭価格(個別価格 or 仕入れ値×掛け率)
	Stock    int    `json:"stock"`
	// 商品詳細(レガシー店表示の全カラム相当)。
	Money          int64          `json:"money"`           // 使用時のお金増減
	Params         map[string]int `json:"params"`          // 使用時の上昇パラメータ
	CalorieG       int            `json:"calorie_g"`       // カロリー(g換算)
	Special        string         `json:"special"`         // 特殊効果の説明(体重/身長/病気)
	Durability     int            `json:"durability"`      // 耐久
	DurabilityUnit string         `json:"durability_unit"` // 'use'(回)/'day'(日)
	IntervalMin    int            `json:"interval_min"`    // 使用間隔(分)
	BodyCost       int            `json:"body_cost"`       // 身体消費
	NouCost        int            `json:"nou_cost"`        // 頭脳消費
	Owned          int            `json:"owned"`           // 閲覧者の所持残数(未所持は0)
}

// HouseShopView is a house shop as seen by a visitor (店表示 フェーズ4c).
type HouseShopView struct {
	HasShop   bool            `json:"has_shop"`
	Title     string          `json:"title"`
	Syubetu   string          `json:"syubetu"`
	OwnerName string          `json:"owner_name"`
	Own       bool            `json:"own"`
	Items     []HouseShopItem `json:"items"`
}

// HouseShop returns a house shop's on-sale items for a visitor. The shelf price
// is the per-item price if set, otherwise 仕入れ値×掛け率. Sold-out items are
// omitted.
func (s *Service) HouseShop(ctx context.Context, viewerID, houseID int64) (*HouseShopView, error) {
	var (
		ownerID   int64
		ownerName string
		title     string
		syubetu   string
		markup    float64
	)
	err := s.pool.QueryRow(ctx,
		`SELECT h.owner_id, COALESCE(p.display_name, ''), hs.title, hs.syubetu, hs.markup
		 FROM house_shops hs
		 JOIN player_houses h ON h.id = hs.house_id
		 LEFT JOIN players p ON p.id = h.owner_id
		 WHERE hs.house_id = $1`, houseID).Scan(&ownerID, &ownerName, &title, &syubetu, &markup)
	if errors.Is(err, pgx.ErrNoRows) {
		return &HouseShopView{HasShop: false, Items: []HouseShopItem{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load shop: %w", err)
	}
	view := &HouseShopView{
		HasShop:   true,
		Title:     title,
		Syubetu:   syubetu,
		OwnerName: ownerName,
		Own:       ownerID == viewerID,
		Items:     []HouseShopItem{},
	}
	rows, err := s.pool.Query(ctx,
		`SELECT ss.item_id, ci.name, ci.category, ss.buy_price, ss.sell_price, ss.stock
		 FROM house_shop_stock ss JOIN content_items ci ON ci.id = ss.item_id
		 WHERE ss.house_id = $1 AND ss.stock > 0
		 ORDER BY ci.category, ci.name`, houseID)
	if err != nil {
		return nil, fmt.Errorf("list shop stock: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			it        HouseShopItem
			buyPrice  int64
			sellPrice *int64
		)
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Category, &buyPrice, &sellPrice, &it.Stock); err != nil {
			return nil, fmt.Errorf("scan shop stock: %w", err)
		}
		if sellPrice != nil {
			it.Price = *sellPrice
		} else {
			it.Price = int64(float64(buyPrice) * markup)
		}
		view.Items = append(view.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shop stock: %w", err)
	}
	rows.Close()
	// 商品詳細(効果/カロリー/耐久/間隔/消費)と閲覧者の所持残数を付与する
	// (レガシー店表示の全カラム+所持中表示)。
	for i := range view.Items {
		it := &view.Items[i]
		var (
			effJSON []byte
			owned   *int
		)
		if err := s.pool.QueryRow(ctx,
			`SELECT ci.effect, ci.calorie_g, GREATEST(ci.durability, 1), ci.durability_unit,
			        ci.use_interval_min, ci.body_cost, ci.nou_cost,
			        (SELECT remaining_uses FROM player_items WHERE player_id = $2 AND item_id = ci.id)
			 FROM content_items ci WHERE ci.id = $1`, it.ItemID, viewerID).
			Scan(&effJSON, &it.CalorieG, &it.Durability, &it.DurabilityUnit,
				&it.IntervalMin, &it.BodyCost, &it.NouCost, &owned); err != nil {
			return nil, fmt.Errorf("item detail: %w", err)
		}
		it.Money, it.Params, it.Special = effectSummary(effJSON)
		if owned != nil {
			it.Owned = *owned
		}
	}
	return view, nil
}

// BbsPost is one bulletin-board message on a house board (フェーズ3b).
// Normal-board posts are threaded: parents carry ThreadNo (NO.x), replies carry
// ParentNo referencing their parent's ThreadNo.
type BbsPost struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"` // normal / nushi
	AuthorID   int64  `json:"author_id"`
	AuthorName string `json:"author_name"`
	AuthorJob  string `json:"author_job"` // 投稿時の職業(（職業）表示用)
	Title      string `json:"title"`      // 家主板(nushi)の記事タイトル
	Body       string `json:"body"`
	ThreadNo   int    `json:"thread_no"`  // 親記事のNO.x(レスは0)
	ParentNo   int    `json:"parent_no"`  // レス先スレッドNO(親記事は0)
	CreatedAt  string `json:"created_at"` // RFC3339
}

// HouseBbs returns a house's bulletin-board posts (newest first, both boards).
func (s *Service) HouseBbs(ctx context.Context, houseID int64) ([]BbsPost, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, kind, COALESCE(author_id, 0), author_name, COALESCE(author_job, ''), title, body,
		        COALESCE(thread_no, 0), COALESCE(parent_no, 0), created_at
		 FROM house_bbs WHERE house_id = $1 ORDER BY id DESC LIMIT 300`, houseID)
	if err != nil {
		return nil, fmt.Errorf("list bbs: %w", err)
	}
	defer rows.Close()
	out := []BbsPost{}
	for rows.Next() {
		var (
			p       BbsPost
			created time.Time
		)
		if err := rows.Scan(&p.ID, &p.Kind, &p.AuthorID, &p.AuthorName, &p.AuthorJob, &p.Title, &p.Body,
			&p.ThreadNo, &p.ParentNo, &created); err != nil {
			return nil, fmt.Errorf("scan bbs: %w", err)
		}
		p.CreatedAt = created.Format(time.RFC3339)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ShopStockItem is one item in the owner's shop stock, for price setting.
type ShopStockItem struct {
	ItemID    int64  `json:"item_id"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	BuyPrice  int64  `json:"buy_price"`
	SellPrice *int64 `json:"sell_price"` // NULL=掛け率で自動計算
	Shelf     int64  `json:"shelf"`      // 現在の店頭価格
	Stock     int    `json:"stock"`
	MaxPrice  int64  `json:"max_price"` // 仕入れ値×3(上限)
}

// ShopStockView is the owner's shop stock for the price-setting screen (my_syouhin).
type ShopStockView struct {
	HasShop bool            `json:"has_shop"`
	Markup  float64         `json:"markup"`
	Items   []ShopStockItem `json:"items"`
}

// ShopStock returns the player's own shop stock with buy price, current shelf
// price, and the max allowed price (仕入れ値×3), for per-item price setting.
func (s *Service) ShopStock(ctx context.Context, playerID, houseID int64) (*ShopStockView, error) {
	var markup float64
	err := s.pool.QueryRow(ctx,
		`SELECT hs.markup FROM house_shops hs JOIN player_houses h ON h.id = hs.house_id
		 WHERE hs.house_id = $1 AND h.owner_id = $2`, houseID, playerID).Scan(&markup)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ShopStockView{HasShop: false, Items: []ShopStockItem{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load shop: %w", err)
	}
	view := &ShopStockView{HasShop: true, Markup: markup, Items: []ShopStockItem{}}
	rows, err := s.pool.Query(ctx,
		`SELECT ss.item_id, ci.name, ci.category, ss.buy_price, ss.sell_price, ss.stock
		 FROM house_shop_stock ss JOIN content_items ci ON ci.id = ss.item_id
		 WHERE ss.house_id = $1 ORDER BY ci.category, ci.name`, houseID)
	if err != nil {
		return nil, fmt.Errorf("list stock: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it ShopStockItem
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Category, &it.BuyPrice, &it.SellPrice, &it.Stock); err != nil {
			return nil, fmt.Errorf("scan stock: %w", err)
		}
		if it.SellPrice != nil {
			it.Shelf = *it.SellPrice
		} else {
			it.Shelf = int64(float64(it.BuyPrice) * markup)
		}
		it.MaxPrice = it.BuyPrice * 3
		view.Items = append(view.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stock: %w", err)
	}
	return view, nil
}
