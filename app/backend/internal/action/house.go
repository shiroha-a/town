package action

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/building"
	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/news"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/townmap"
)

// recordHouseNews appends a 家 article to the town news (legacy command.pl /
// original_house.cgi の news_kiroku("家", ...)).
func recordHouseNews(ctx context.Context, tx pgx.Tx, playerID int64, town int, what string) error {
	name, err := news.ActorName(ctx, tx, playerID)
	if err != nil {
		return err
	}
	return news.RecordFor(ctx, tx, news.KindHouse, playerID, name,
		fmt.Sprintf("%sさんが「%s」%s", name, building.TownName(town), what), nil, true)
}

// DoBuildHouse builds a house on an empty plot for the player (建設会社 フェーズ2a).
// The build fee is drawn from the player's bank savings (普通口座). It enforces
// the 4-house limit, empty-plot validation (no facility, no existing house), and
// the legacy cost formula (1軒目=(地価+外装)×内装倍率, 2軒目以降=地価+外装×2+tuika費).
// tuika selects the 2nd+ house type (0=家のみ/1=運営/2=株式会社/3=持ち物販売店);
// 株式会社/持ち物販売店には能力審査(総資産1億+全パラ1万)がかかる。
func (s *Service) DoBuildHouse(ctx context.Context, playerID int64, town, row, col int, exterior string, interiorRank, tuika int, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "build_house", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		if town < 0 || town >= building.TownCount() || col < 1 || col > townmap.Cols || row < 0 || row >= townmap.Rows {
			return &ConditionError{Message: "建築場所の指定が正しくありません。"}
		}
		// 隠し町は建設会社の対象外だが、その街に現在いる場合だけは建てられる
		// (街マップの空き地クリック導線。隠し町へは徒歩/バスでしか入れない)。
		if building.IsHidden(town) {
			var cur int
			if err := tx.QueryRow(ctx, `SELECT current_town FROM players WHERE id = $1`, playerID).Scan(&cur); err != nil {
				return fmt.Errorf("read current town: %w", err)
			}
			if cur != town {
				return &ConditionError{Message: "その街には家を建てられません。"}
			}
		}
		// 所有軒数(上限)。建てる前の軒数で1軒目/2軒目以降の費用式を分ける。
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM player_houses WHERE owner_id = $1`, playerID).Scan(&count); err != nil {
			return fmt.Errorf("count houses: %w", err)
		}
		if count >= building.MochiieMax {
			return &ConditionError{Message: fmt.Sprintf("家は%d軒までしか持てません。", building.MochiieMax)}
		}
		// 追加種別(tuika)は2軒目以降のみ。1軒目は常に家のみ(0)。
		if count == 0 {
			tuika = 0
		}
		tk, ok := building.TuikaByNo(tuika)
		if !ok {
			return &ConditionError{Message: "追加種別の指定が正しくありません。"}
		}
		if tuika != 0 {
			// 同種の追加家は1人1軒まで(レガシー: 同種別サフィックス検出で非表示)。
			var sameCnt int
			if err := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM player_houses WHERE owner_id = $1 AND tuika = $2`,
				playerID, tuika).Scan(&sameCnt); err != nil {
				return fmt.Errorf("count same tuika: %w", err)
			}
			if sameCnt > 0 {
				return &ConditionError{Message: fmt.Sprintf("%sはすでに持っています。", tk.Name)}
			}
			// 能力審査(株式会社/持ち物販売店): 総資産1億円+全パラメータ1万以上。
			if tk.Shinsa {
				ok, err := s.checkShinsa(ctx, tx, playerID)
				if err != nil {
					return err
				}
				if !ok {
					return &ConditionError{Message: fmt.Sprintf("設営能力不足です。%sには総資産%d円と全パラメータ%d以上が必要です。", tk.Name, building.ShinsaAsset, building.ShinsaParam)}
				}
			}
		}
		// 建築可能マス = key='akichi' の空き地施設があるマスのみ(空き地は施設に統合済み)。
		// 通常施設のあるマスには akichi が無い(1セル1施設)ので、自動的に建築不可になる。
		var isPlot bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(
			   SELECT 1 FROM town_map, jsonb_array_elements(facilities) f
			   WHERE id = 1 AND f->>'key' = 'akichi'
			     AND COALESCE((f->>'town')::int, 0) = $1
			     AND (f->>'row')::int = $2 AND (f->>'col')::int = $3)`,
			town, row, col).Scan(&isPlot); err != nil {
			return fmt.Errorf("check plot: %w", err)
		}
		if !isPlot {
			return &ConditionError{Message: "そこは空地に指定されていません。空地に設定された場所にのみ家を建てられます。"}
		}
		cost, err := building.BuildCost(town, exterior, interiorRank, count, tuika)
		if err != nil {
			return &ConditionError{Message: "外装または内装の指定が正しくありません。"}
		}
		// 空地判定(同一マスに既存の家が無いこと)。UNIQUE制約も二重の保険になる。
		var occupied bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM player_houses WHERE town = $1 AND grid_row = $2 AND grid_col = $3)`,
			town, row, col).Scan(&occupied); err != nil {
			return fmt.Errorf("check plot: %w", err)
		}
		if occupied {
			return &ConditionError{Message: "その場所にはすでに家が建っています。"}
		}
		// 建築費は普通口座(savings)から引き落とす。
		var savings int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
			ledger.SavingsAccount(playerID)).Scan(&savings); err != nil {
			return fmt.Errorf("read savings: %w", err)
		}
		if savings < cost {
			return &ConditionError{Message: fmt.Sprintf("普通口座の残高が足りません。建築費%d円が必要です。", cost)}
		}
		if err := s.ledger.PostTx(ctx, tx, "build_house", "", []ledger.Entry{
			{Account: ledger.SavingsAccount(playerID), Delta: -cost},
			{Account: ledger.SystemAccount("build_house"), Delta: cost},
		}); err != nil {
			return fmt.Errorf("charge build fee: %w", err)
		}
		// 2軒目以降は内装を選ばない(家のみ)。interior_rankは0を格納。
		ir := interiorRank
		if count > 0 {
			ir = 0
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_houses (owner_id, town, grid_row, grid_col, exterior, interior_rank, tuika)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			playerID, town, row, col, exterior, ir, tuika); err != nil {
			return fmt.Errorf("insert house: %w", err)
		}
		return recordHouseNews(ctx, tx, playerID, town, "に家を建築しました。")
	})
}

// checkShinsa reports whether the player passes 能力審査: total assets
// (現金+普通口座+スーパー普通口座) >= ShinsaAsset and every parameter >= ShinsaParam.
func (s *Service) checkShinsa(ctx context.Context, tx pgx.Tx, playerID int64) (bool, error) {
	var assets int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account IN ($1, $2, $3)`,
		ledger.PlayerAccount(playerID), ledger.SavingsAccount(playerID), ledger.SuperSavingsAccount(playerID)).Scan(&assets); err != nil {
		return false, fmt.Errorf("sum assets: %w", err)
	}
	if assets < building.ShinsaAsset {
		return false, nil
	}
	var minParam int
	if err := tx.QueryRow(ctx,
		`SELECT LEAST(kokugo, suugaku, rika, syakai, eigo, ongaku, bijutsu, looks,
		              tairyoku, kenkou, speed, power, wanryoku, kyakuryoku, love, omoshirosa)
		 FROM player_status WHERE player_id = $1`, playerID).Scan(&minParam); err != nil {
		return false, fmt.Errorf("read params: %w", err)
	}
	return minParam >= building.ShinsaParam, nil
}

// DoSellHouse demolishes one of the player's houses and refunds the land price
// (地価×10000) in cash. The exterior/interior cost is not refunded, and admins
// receive no refund (レガシー忠実). The freed plot returns to buildable state.
func (s *Service) DoSellHouse(ctx context.Context, playerID, houseID int64, idempotencyKey string) (*player.Player, error) {
	isAdmin, err := s.players.HasRole(ctx, playerID, "admin")
	if err != nil {
		return nil, err
	}
	return s.runAction(ctx, playerID, "sell_house", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var town int
		err := tx.QueryRow(ctx,
			`SELECT town FROM player_houses WHERE id = $1 AND owner_id = $2`, houseID, playerID).Scan(&town)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その家は所有していません。"}
		}
		if err != nil {
			return fmt.Errorf("load house: %w", err)
		}
		refund, err := building.SellValue(town)
		if err != nil {
			return &ConditionError{Message: "家の街情報が不正です。"}
		}
		if isAdmin {
			refund = 0 // 管理者は返金なし
		}
		// 持ち物販売店(闇市)の売り場・倉庫の品は持ち主の持ち物へ戻す
		// (レガシーは運営ログを残す=消滅させない)。
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses)
			 SELECT $1, item_id, COUNT(*), SUM(uses) FROM yami_items WHERE house_id = $2 GROUP BY item_id
			 ON CONFLICT (player_id, item_id) DO UPDATE SET
			   quantity = player_items.quantity + EXCLUDED.quantity,
			   remaining_uses = player_items.remaining_uses + EXCLUDED.remaining_uses, updated_at = now()`,
			playerID, houseID); err != nil {
			return fmt.Errorf("return yami items: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM player_houses WHERE id = $1`, houseID); err != nil {
			return fmt.Errorf("delete house: %w", err)
		}
		if refund > 0 {
			// 返金は地価分のみを現金へ(外装・内装費は戻らない)。
			if err := s.ledger.PostTx(ctx, tx, "sell_house", "", []ledger.Entry{
				{Account: ledger.SystemAccount("house_sell"), Delta: -refund},
				{Account: ledger.PlayerAccount(playerID), Delta: refund},
			}); err != nil {
				return fmt.Errorf("refund: %w", err)
			}
		}
		return recordHouseNews(ctx, tx, playerID, town, "の家を売却しました。")
	})
}

// DoRebuildHouse rebuilds an existing house with a new exterior and interior
// rank. The cost (外装+内装)×10000 is charged in cash; the land price is not
// re-charged because it was already paid at build time.
func (s *Service) DoRebuildHouse(ctx context.Context, playerID, houseID int64, exterior string, interiorRank int, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "rebuild_house", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var town int
		err := tx.QueryRow(ctx,
			`SELECT town FROM player_houses WHERE id = $1 AND owner_id = $2`, houseID, playerID).Scan(&town)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その家は所有していません。"}
		}
		if err != nil {
			return fmt.Errorf("load house: %w", err)
		}
		cost, err := building.RebuildCost(exterior, interiorRank)
		if err != nil {
			return &ConditionError{Message: "外装または内装の指定が正しくありません。"}
		}
		if state.Money < cost {
			return &ConditionError{Message: fmt.Sprintf("現金が足りません。建て替え費用%d円が必要です。", cost)}
		}
		if err := s.ledger.PostTx(ctx, tx, "rebuild_house", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -cost},
			{Account: ledger.SystemAccount("house_rebuild"), Delta: cost},
		}); err != nil {
			return fmt.Errorf("charge rebuild: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE player_houses SET exterior = $1, interior_rank = $2 WHERE id = $3`,
			exterior, interiorRank, houseID); err != nil {
			return fmt.Errorf("update house: %w", err)
		}
		return recordHouseNews(ctx, tx, playerID, town, "の家を建て替えました。")
	})
}

// maxSetumeiLen is the mouse-over comment length limit (legacy 40字).
const maxSetumeiLen = 40

// DoSetHouseComment sets the mouse-over comment (setumei) of the player's house
// (マイホーム設定 フェーズ3a).
func (s *Service) DoSetHouseComment(ctx context.Context, playerID, houseID int64, setumei, idempotencyKey string) (*player.Player, error) {
	setumei = strings.TrimSpace(setumei)
	if utf8.RuneCountInString(setumei) > maxSetumeiLen {
		return nil, &ConditionError{Message: fmt.Sprintf("コメントは%d文字以内で入力してください。", maxSetumeiLen)}
	}
	return s.runAction(ctx, playerID, "house_comment", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		ct, err := tx.Exec(ctx,
			`UPDATE player_houses SET setumei = $1 WHERE id = $2 AND owner_id = $3`,
			setumei, houseID, playerID)
		if err != nil {
			return fmt.Errorf("update setumei: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return &ConditionError{Message: "その家は所有していません。"}
		}
		return nil
	})
}

// Allowed offering amounts and daily caps for さい銭 (レガシー忠実).
var saisenAmounts = map[int64]bool{100: true, 500: true, 1000: true, 2000: true, 5000: true, 10000: true}

const (
	saisenPerTargetDaily   = 20000  // 同一相手への1日上限(円)
	saisenTargetTotalDaily = 100000 // 相手が1日に受け取れる総額(円)
)

// DoSaisen offers money at a house's offering box: it moves the amount from the
// visitor's cash to the owner's bank savings, subject to daily caps (同一相手
// 20000円/日、相手受取総額100000円/日). A player cannot offer at their own house.
func (s *Service) DoSaisen(ctx context.Context, playerID, houseID, amount int64, idempotencyKey string) (*player.Player, error) {
	if !saisenAmounts[amount] {
		return nil, &ConditionError{Message: "さい銭の金額が正しくありません。"}
	}
	date := s.gameDate(time.Now())
	return s.runAction(ctx, playerID, "saisen", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var ownerID int64
		err := tx.QueryRow(ctx, `SELECT owner_id FROM player_houses WHERE id = $1`, houseID).Scan(&ownerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その家は存在しません。"}
		}
		if err != nil {
			return fmt.Errorf("load house owner: %w", err)
		}
		// レガシー(saisensuru)は自分の家へのさい銭も許す(現金→自分の普通口座)。
		// 日次上限(相手別2万/受取10万)は自分の家にも適用される。
		if state.Money < amount {
			return &ConditionError{Message: "所持金が足りません。"}
		}
		var toPair int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(amount), 0) FROM saisen_log WHERE from_id = $1 AND to_id = $2 AND game_date = $3`,
			playerID, ownerID, date).Scan(&toPair); err != nil {
			return fmt.Errorf("sum pair saisen: %w", err)
		}
		if toPair+amount > saisenPerTargetDaily {
			return &ConditionError{Message: fmt.Sprintf("同じ相手へのさい銭は1日%d円までです。", saisenPerTargetDaily)}
		}
		var toTotal int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(amount), 0) FROM saisen_log WHERE to_id = $1 AND game_date = $2`,
			ownerID, date).Scan(&toTotal); err != nil {
			return fmt.Errorf("sum total saisen: %w", err)
		}
		if toTotal+amount > saisenTargetTotalDaily {
			return &ConditionError{Message: "この家は今日のさい銭受け取り上限に達しています。"}
		}
		// 送金は現金→相手の普通口座。
		if err := s.ledger.PostTx(ctx, tx, "saisen", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -amount},
			{Account: ledger.SavingsAccount(ownerID), Delta: amount},
		}); err != nil {
			return fmt.Errorf("saisen transfer: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO saisen_log (from_id, to_id, amount, game_date) VALUES ($1, $2, $3, $4)`,
			playerID, ownerID, amount, date); err != nil {
			return fmt.Errorf("insert saisen log: %w", err)
		}
		return nil
	})
}

// maxShopTitleLen caps a shop title length.
const maxShopTitleLen = 50

// DoOpenHouseShop opens (or reconfigures) the shop attached to the player's
// house: its title, category (syubetu), and base markup (掛け率, 0.3<率<=3).
// Changing the category clears the existing stock (店の種類変更で在庫全消去).
func (s *Service) DoOpenHouseShop(ctx context.Context, playerID, houseID int64, title, syubetu string, markup float64, idempotencyKey string) (*player.Player, error) {
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) > maxShopTitleLen {
		return nil, &ConditionError{Message: fmt.Sprintf("店名は%d文字以内で入力してください。", maxShopTitleLen)}
	}
	if !building.IsShopKind(syubetu) {
		return nil, &ConditionError{Message: "店の種類が正しくありません。"}
	}
	if markup <= building.ShopMarkupMin || markup > building.ShopMarkupMax {
		return nil, &ConditionError{Message: fmt.Sprintf("販売掛け率は%gより大きく%g以下にしてください。", building.ShopMarkupMin, building.ShopMarkupMax)}
	}
	return s.runAction(ctx, playerID, "open_house_shop", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM player_houses WHERE id = $1 AND owner_id = $2)`,
			houseID, playerID).Scan(&exists); err != nil {
			return fmt.Errorf("check house: %w", err)
		}
		if !exists {
			return &ConditionError{Message: "その家は所有していません。"}
		}
		// 店の種類を変更した場合は在庫を全消去する(レガシー忠実)。
		var oldSyubetu string
		err := tx.QueryRow(ctx, `SELECT syubetu FROM house_shops WHERE house_id = $1`, houseID).Scan(&oldSyubetu)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("load shop: %w", err)
		}
		if err == nil && oldSyubetu != syubetu {
			if _, err := tx.Exec(ctx, `DELETE FROM house_shop_stock WHERE house_id = $1`, houseID); err != nil {
				return fmt.Errorf("clear stock: %w", err)
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO house_shops (house_id, title, syubetu, markup) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (house_id) DO UPDATE SET title = $2, syubetu = $3, markup = $4`,
			houseID, title, syubetu, markup); err != nil {
			return fmt.Errorf("upsert shop: %w", err)
		}
		return nil
	})
}

// DoShiire purchases qty of an item from the wholesaler into the player's house
// shop stock (卸問屋での仕入れ フェーズ4b). The cost is drawn from the bank
// savings. スーパーは店の全種類を1.5倍で扱える。
func (s *Service) DoShiire(ctx context.Context, playerID, houseID, itemID int64, qty int, idempotencyKey string) (*player.Player, error) {
	if qty <= 0 {
		return nil, &ConditionError{Message: "数量が正しくありません。"}
	}
	return s.runAction(ctx, playerID, "shiire", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var syubetu string
		err := tx.QueryRow(ctx,
			`SELECT hs.syubetu FROM house_shops hs JOIN player_houses h ON h.id = hs.house_id
			 WHERE hs.house_id = $1 AND h.owner_id = $2`, houseID, playerID).Scan(&syubetu)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その家に自分の店がありません。"}
		}
		if err != nil {
			return fmt.Errorf("load shop: %w", err)
		}
		var (
			category string
			price    int64
			facility string
		)
		err = tx.QueryRow(ctx,
			`SELECT category, price, facility FROM content_items WHERE id = $1 AND enabled`, itemID).
			Scan(&category, &price, &facility)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その商品は仕入れられません。"}
		}
		if err != nil {
			return fmt.Errorf("load item: %w", err)
		}
		if facility != "" {
			return &ConditionError{Message: "その商品は仕入れられません。"}
		}
		super := syubetu == building.SuperMarketKind
		if super {
			if !building.IsShopKind(category) || category == building.SuperMarketKind {
				return &ConditionError{Message: "その商品は扱えません。"}
			}
		} else if category != syubetu {
			return &ConditionError{Message: "この店ではその商品を扱えません。"}
		}
		buyPrice := price
		if super {
			buyPrice = price * 3 / 2 // スーパーは1.5倍
		}
		total := buyPrice * int64(qty)

		var curStock int
		exists := true
		err = tx.QueryRow(ctx,
			`SELECT stock FROM house_shop_stock WHERE house_id = $1 AND item_id = $2`, houseID, itemID).Scan(&curStock)
		if errors.Is(err, pgx.ErrNoRows) {
			exists = false
		} else if err != nil {
			return fmt.Errorf("load stock: %w", err)
		}
		if curStock+qty > building.ShopMaxStock {
			return &ConditionError{Message: fmt.Sprintf("在庫は1商品%d個までです(現在%d個)。", building.ShopMaxStock, curStock)}
		}
		if !exists {
			var kinds int
			if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM house_shop_stock WHERE house_id = $1`, houseID).Scan(&kinds); err != nil {
				return fmt.Errorf("count kinds: %w", err)
			}
			if kinds >= building.ShopMaxKinds {
				return &ConditionError{Message: fmt.Sprintf("店に置ける商品は%d種類までです。", building.ShopMaxKinds)}
			}
		}
		var savings int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
			ledger.SavingsAccount(playerID)).Scan(&savings); err != nil {
			return fmt.Errorf("read savings: %w", err)
		}
		if savings < total {
			return &ConditionError{Message: fmt.Sprintf("普通口座の残高が足りません(仕入れ額%d円)。", total)}
		}
		if err := s.ledger.PostTx(ctx, tx, "shiire", "", []ledger.Entry{
			{Account: ledger.SavingsAccount(playerID), Delta: -total},
			{Account: ledger.SystemAccount("shiire"), Delta: total},
		}); err != nil {
			return fmt.Errorf("charge shiire: %w", err)
		}
		if exists {
			if _, err := tx.Exec(ctx,
				`UPDATE house_shop_stock SET stock = stock + $1 WHERE house_id = $2 AND item_id = $3`,
				qty, houseID, itemID); err != nil {
				return fmt.Errorf("update stock: %w", err)
			}
		} else {
			if _, err := tx.Exec(ctx,
				`INSERT INTO house_shop_stock (house_id, item_id, buy_price, stock) VALUES ($1, $2, $3, $4)`,
				houseID, itemID, buyPrice, qty); err != nil {
				return fmt.Errorf("insert stock: %w", err)
			}
		}
		return nil
	})
}

// DoBuyFromHouseShop buys qty of an item from a house shop (訪問販売 フェーズ4c).
// The shelf price is the per-item price if set, otherwise 仕入れ値×掛け率. Payment
// goes to the owner's bank savings and the item transfers to the buyer. A player
// cannot buy from their own shop.
// maxBuyQty is the legacy per-purchase quantity limit (item_kosuuseigen).
const maxBuyQty = 4

// hasCreditCard reports whether the player holds a usable credit card item.
func hasCreditCard(ctx context.Context, tx pgx.Tx, playerID int64) (bool, error) {
	var hasCard bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM player_items pi JOIN content_items ci ON ci.id = pi.item_id
		   WHERE pi.player_id = $1 AND pi.remaining_uses > 0 AND ci.name = ANY($2))`,
		playerID, creditCardNames).Scan(&hasCard); err != nil {
		return false, fmt.Errorf("check credit card: %w", err)
	}
	return hasCard, nil
}

// creditCardNames are the items that enable credit (bank) payment at shops
// (レガシーのクレジット系アイテム)。
var creditCardNames = []string{"クレジットカード", "ゴールドクレジットカード", "スペシャルクレジットカード"}

// HouseBuyResult summarizes a shop purchase for the result toast.
type HouseBuyResult struct {
	Total    int64  // 割引前の合計(店頭価格×個数)
	Cashback int64  // ご近所キャッシュバック(単価10%×個数)
	Paid     int64  // 実際に支払った額
	Method   string // "cash"/"credit"
}

func (s *Service) DoBuyFromHouseShop(ctx context.Context, buyerID, houseID, itemID int64, qty int, payMethod, idempotencyKey string) (*player.Player, *HouseBuyResult, error) {
	if qty <= 0 {
		qty = 1
	}
	if qty > maxBuyQty {
		return nil, nil, &ConditionError{Message: fmt.Sprintf("一度に買えるのは%d個までです。", maxBuyQty)}
	}
	if payMethod == "" {
		payMethod = "cash"
	}
	if payMethod != "cash" && payMethod != "credit" {
		return nil, nil, &ConditionError{Message: "支払い方法が正しくありません。"}
	}
	result := &HouseBuyResult{Method: payMethod}
	p, err := s.runAction(ctx, buyerID, "house_shop_buy", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var (
			ownerID  int64
			markup   float64
			shopTown int
		)
		err := tx.QueryRow(ctx,
			`SELECT h.owner_id, hs.markup, h.town FROM house_shops hs JOIN player_houses h ON h.id = hs.house_id
			 WHERE hs.house_id = $1`, houseID).Scan(&ownerID, &markup, &shopTown)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その家に店がありません。"}
		}
		if err != nil {
			return fmt.Errorf("load shop: %w", err)
		}
		if ownerID == buyerID {
			return &ConditionError{Message: "自分の店では買えません。"}
		}
		// コンテンツ枠に「お店」が設定されていない家では買えない(店が非公開)。
		if ok, err := hasHouseContent(ctx, tx, houseID, "shop"); err != nil {
			return err
		} else if !ok {
			return &ConditionError{Message: "この家の店は公開されていません。"}
		}
		var (
			buyPrice  int64
			sellPrice *int64
			stock     int
		)
		err = tx.QueryRow(ctx,
			`SELECT buy_price, sell_price, stock FROM house_shop_stock WHERE house_id = $1 AND item_id = $2`,
			houseID, itemID).Scan(&buyPrice, &sellPrice, &stock)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その商品はありません。"}
		}
		if err != nil {
			return fmt.Errorf("load stock: %w", err)
		}
		if stock < qty {
			return &ConditionError{Message: "在庫が足りません。"}
		}
		var price int64
		if sellPrice != nil {
			price = *sellPrice
		} else {
			price = int64(float64(buyPrice) * markup)
		}
		cost := price * int64(qty)
		// ご近所キャッシュバック(レガシー buy_syouhin): 買い手の居住街(最初に建てた家の街)
		// が店の街と同じなら、単価の10%×個数を割引。店主の売上も同額減る。
		var cashback int64
		var residenceTown *int
		if err := tx.QueryRow(ctx,
			`SELECT town FROM player_houses WHERE owner_id = $1 ORDER BY built_at, id LIMIT 1`,
			buyerID).Scan(&residenceTown); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("residence town: %w", err)
		}
		if residenceTown != nil && *residenceTown == shopTown {
			cashback = (price / 10) * int64(qty)
		}
		paid := cost - cashback
		// 支払い方法: 現金 or クレジット(クレジットカード所持で普通口座から)。
		buyerAccount := ledger.PlayerAccount(buyerID)
		if payMethod == "credit" {
			hasCard, err := hasCreditCard(ctx, tx, buyerID)
			if err != nil {
				return err
			}
			if !hasCard {
				return &ConditionError{Message: "クレジットカードを持っていません。"}
			}
			var savings int64
			if err := tx.QueryRow(ctx,
				`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
				ledger.SavingsAccount(buyerID)).Scan(&savings); err != nil {
				return fmt.Errorf("read savings: %w", err)
			}
			if savings < paid {
				return &ConditionError{Message: "普通口座の残高が足りません。"}
			}
			buyerAccount = ledger.SavingsAccount(buyerID)
		} else if state.Money < paid {
			return &ConditionError{Message: "お金が足りません。"}
		}
		var durability, maxSets int
		if err := tx.QueryRow(ctx,
			`SELECT GREATEST(1, durability), max_sets FROM content_items WHERE id = $1`, itemID).
			Scan(&durability, &maxSets); err != nil {
			return fmt.Errorf("item: %w", err)
		}
		add := durability * qty
		var current int
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(remaining_uses, 0) FROM player_items WHERE player_id = $1 AND item_id = $2`,
			buyerID, itemID).Scan(&current); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if maxSets > 0 && current+add > maxSets*durability {
			return &ConditionError{Message: fmt.Sprintf("これ以上は持てません(最大%dセット)。", maxSets)}
		}
		// 代金(キャッシュバック控除後)は店主の普通口座へ。
		if paid > 0 {
			if err := s.ledger.PostTx(ctx, tx, "house_shop_buy", "", []ledger.Entry{
				{Account: buyerAccount, Delta: -paid},
				{Account: ledger.SavingsAccount(ownerID), Delta: paid},
			}); err != nil {
				return fmt.Errorf("pay: %w", err)
			}
		}
		result.Total = cost
		result.Cashback = cashback
		result.Paid = paid
		if _, err := tx.Exec(ctx,
			`UPDATE house_shop_stock SET stock = stock - $3 WHERE house_id = $1 AND item_id = $2`,
			houseID, itemID, qty); err != nil {
			return fmt.Errorf("reduce stock: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (player_id, item_id) DO UPDATE SET quantity = player_items.quantity + $3,
			   remaining_uses = player_items.remaining_uses + $4, updated_at = now()`,
			buyerID, itemID, qty, add); err != nil {
			return fmt.Errorf("grant item: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return p, result, nil
}

// maxBbsBodyWeight caps a normal-board post body (レガシー: 全角100字半角200字。
// ASCII=1、それ以外=2の重み付き長で数える)。
const maxBbsBodyWeight = 200

// maxNormalBbsPosts caps stored normal-board posts per house (レガシー
// $bbs_kizi_max相当。超過時は最古の記事から削除される)。
const maxNormalBbsPosts = 200

// maxNushiBbsPosts caps stored 家主板 articles per house (レガシーgentei_registは
// 最新50件まで保持する)。
const maxNushiBbsPosts = 50

// bbsWeightedLen returns the legacy Shift_JIS-style length: ASCII counts 1,
// everything else counts 2.
func bbsWeightedLen(s string) int {
	n := 0
	for _, r := range s {
		if r <= 0x7f {
			n++
		} else {
			n += 2
		}
	}
	return n
}

// BbsPostResult carries the money reward granted for a normal-board post.
type BbsPostResult struct {
	Reward int64 `json:"reward"`
	Bonus  bool  `json:"bonus"`
}

// DoPostBbs posts a message to a house's bulletin board (レガシーbbs_regist/
// gentei_regist). kind is "normal" (anyone, threaded, rewards money) or "nushi"
// (家主板, owner only, posted from 家の設定). parentNo(>0) replies to the normal
// board thread NO.parentNo and floats the thread to the top.
func (s *Service) DoPostBbs(ctx context.Context, playerID, houseID int64, kind, title, body string, parentNo int, idempotencyKey string) (*player.Player, *BbsPostResult, error) {
	body = strings.TrimSpace(body)
	title = strings.TrimSpace(title)
	if body == "" {
		return nil, nil, &ConditionError{Message: "コメントが入力されていません"}
	}
	if kind == "normal" && bbsWeightedLen(body) > maxBbsBodyWeight {
		// レガシーbbs_registのエラーメッセージそのまま(挨拶と共通の文言)。
		return nil, nil, &ConditionError{Message: "挨拶は全角100字半角200字以内です"}
	}
	if kind == "nushi" && utf8.RuneCountInString(body) > 500 {
		return nil, nil, &ConditionError{Message: "本文は500文字以内で入力してください。"}
	}
	// 記事タイトル(家主板のブログ風記事用)。
	if utf8.RuneCountInString(title) > 40 {
		return nil, nil, &ConditionError{Message: "記事タイトルは40文字以内で入力してください。"}
	}
	if kind != "normal" && kind != "nushi" {
		return nil, nil, &ConditionError{Message: "掲示板の種類が正しくありません。"}
	}
	result := &BbsPostResult{}
	p, err := s.runAction(ctx, playerID, "house_bbs_post", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var ownerID int64
		err := tx.QueryRow(ctx, `SELECT owner_id FROM player_houses WHERE id = $1`, houseID).Scan(&ownerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その家は存在しません。"}
		}
		if err != nil {
			return fmt.Errorf("load house: %w", err)
		}
		if kind == "nushi" && ownerID != playerID {
			return &ConditionError{Message: "家主板には家主しか書き込めません。"}
		}
		// コンテンツ枠に設定されていない掲示板には書き込めない。
		// (掲示板のkind 'normal' はコンテンツ枠では 'bbs')
		contentKind := kind
		if kind == "normal" {
			contentKind = "bbs"
		}
		if ok, err := hasHouseContent(ctx, tx, houseID, contentKind); err != nil {
			return err
		} else if !ok {
			return &ConditionError{Message: "この家にはその掲示板がありません。"}
		}
		// 二重投稿チェック(直近の記事と同一人物・同一本文)。
		var lastAuthor int64
		var lastBody string
		err = tx.QueryRow(ctx,
			`SELECT COALESCE(author_id, 0), body FROM house_bbs WHERE house_id = $1 AND kind = $2 ORDER BY id DESC LIMIT 1`,
			houseID, kind).Scan(&lastAuthor, &lastBody)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("load last post: %w", err)
		}
		if err == nil && lastAuthor == playerID && lastBody == body {
			return &ConditionError{Message: "二重投稿です"}
		}
		var name, job string
		if err := tx.QueryRow(ctx,
			`SELECT p.display_name, COALESCE(ps.job, '') FROM players p
			 LEFT JOIN player_status ps ON ps.player_id = p.id WHERE p.id = $1`,
			playerID).Scan(&name, &job); err != nil {
			return fmt.Errorf("load name: %w", err)
		}
		var threadNo, parentRef *int
		if kind == "normal" {
			if parentNo > 0 {
				// レス: 親スレッドの存在確認。
				var ok bool
				if err := tx.QueryRow(ctx,
					`SELECT EXISTS(SELECT 1 FROM house_bbs WHERE house_id = $1 AND kind = 'normal' AND thread_no = $2)`,
					houseID, parentNo).Scan(&ok); err != nil {
					return fmt.Errorf("check parent: %w", err)
				}
				if !ok {
					return &ConditionError{Message: "該当する記事no.が見つかりません。"}
				}
				parentRef = &parentNo
			} else {
				// 新規スレッド: 家ごとの通し番号(NO.x)を採番。
				var next int
				if err := tx.QueryRow(ctx,
					`SELECT COALESCE(MAX(thread_no), 0) + 1 FROM house_bbs WHERE house_id = $1 AND kind = 'normal'`,
					houseID).Scan(&next); err != nil {
					return fmt.Errorf("next thread no: %w", err)
				}
				threadNo = &next
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO house_bbs (house_id, kind, author_id, author_name, author_job, title, body, thread_no, parent_no)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			houseID, kind, playerID, name, job, title, body, threadNo, parentRef); err != nil {
			return fmt.Errorf("insert bbs: %w", err)
		}
		// 保持上限を超えた分は最古の記事から削除(レガシーは末尾行をpop)。
		maxPosts := maxNormalBbsPosts
		if kind == "nushi" {
			maxPosts = maxNushiBbsPosts
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM house_bbs WHERE house_id = $1 AND kind = $2 AND id NOT IN (
			   SELECT id FROM house_bbs WHERE house_id = $1 AND kind = $2 ORDER BY id DESC LIMIT $3)`,
			houseID, kind, maxPosts); err != nil {
			return fmt.Errorf("trim bbs: %w", err)
		}
		// 通常掲示板への投稿はお金がもらえる(レガシー: 1/10でボーナス)。
		if kind == "normal" {
			base := rand.Intn(10) + 1
			if base == 7 {
				result.Reward = int64(base + rand.Intn(10000) + 5000)
				result.Bonus = true
			} else {
				result.Reward = int64(base + rand.Intn(2000) + 1000)
			}
			if err := s.ledger.PostTx(ctx, tx, "bbs_reward", "", []ledger.Entry{
				{Account: ledger.SystemAccount("bbs_reward"), Delta: -result.Reward},
				{Account: ledger.PlayerAccount(playerID), Delta: result.Reward},
			}); err != nil {
				return fmt.Errorf("bbs reward: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return p, result, nil
}

// HouseContentSlot is one requested content slot assignment (コンテンツ枠設定)。
type HouseContentSlot struct {
	Slot    int
	Kind    string // ""=非公開 / "bbs" / "shop" / "nushi" / "url"
	Title   string
	URL     string // kind="url" の埋め込みURL(http/httpsのみ)
	Comment string // タイトル下コメント(リード文)
}

const maxContentTitleLen = 20

// DoSetHouseContents replaces the house's content slots (レガシー my_house_settei)。
// 内装ランクで決まる枠数までしか設定できない。空kindの枠は非公開(未設定)扱い。
func (s *Service) DoSetHouseContents(ctx context.Context, playerID, houseID int64, contents []HouseContentSlot, idempotencyKey string) (*player.Player, error) {
	for _, c := range contents {
		if !building.IsHouseContentKind(c.Kind) {
			return nil, &ConditionError{Message: "コンテンツの種類が正しくありません。"}
		}
		if utf8.RuneCountInString(c.Title) > maxContentTitleLen {
			return nil, &ConditionError{Message: fmt.Sprintf("タイトルは%d文字以内で入力してください。", maxContentTitleLen)}
		}
		if utf8.RuneCountInString(c.Comment) > 100 {
			return nil, &ConditionError{Message: "コメントは100文字以内で入力してください。"}
		}
		// 独自URLはhttp/httpsのみ許可(javascript:等の混入防止)。
		if c.Kind == "url" {
			u := strings.TrimSpace(c.URL)
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				return nil, &ConditionError{Message: "独自URLはhttp://またはhttps://で始まるURLを入力してください。"}
			}
		}
	}
	return s.runAction(ctx, playerID, "house_contents", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var ownerID int64
		var rank, tuika int
		err := tx.QueryRow(ctx, `SELECT owner_id, interior_rank, tuika FROM player_houses WHERE id = $1`, houseID).
			Scan(&ownerID, &rank, &tuika)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その家は存在しません。"}
		}
		if err != nil {
			return fmt.Errorf("load house: %w", err)
		}
		if ownerID != playerID {
			return &ConditionError{Message: "自分の家のコンテンツだけ設定できます。"}
		}
		// 追加種別(運営/株式会社/持ち物販売店)の家は機能が固定でコンテンツ枠を持たない。
		if tuika != 0 {
			return &ConditionError{Message: "この家にはコンテンツ枠がありません。"}
		}
		slots := building.SlotsByRank(rank)
		seen := map[int]bool{}
		for _, c := range contents {
			if c.Slot < 0 || c.Slot >= slots {
				return &ConditionError{Message: fmt.Sprintf("この家のコンテンツ枠は%dつまでです。", slots)}
			}
			if seen[c.Slot] {
				return &ConditionError{Message: "同じ枠を複数回指定できません。"}
			}
			seen[c.Slot] = true
		}
		// 全置き換え: 一旦消して、公開する枠(kindあり)だけ入れ直す。
		if _, err := tx.Exec(ctx, `DELETE FROM house_contents WHERE house_id = $1`, houseID); err != nil {
			return fmt.Errorf("clear contents: %w", err)
		}
		for _, c := range contents {
			if c.Kind == "" {
				continue
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO house_contents (house_id, slot, kind, title, url, comment) VALUES ($1, $2, $3, $4, $5, $6)`,
				houseID, c.Slot, c.Kind, strings.TrimSpace(c.Title), strings.TrimSpace(c.URL), strings.TrimSpace(c.Comment)); err != nil {
				return fmt.Errorf("insert content: %w", err)
			}
		}
		return nil
	})
}

// hasHouseContent reports whether the house has a content slot of the kind.
func hasHouseContent(ctx context.Context, tx pgx.Tx, houseID int64, kind string) (bool, error) {
	var ok bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM house_contents WHERE house_id = $1 AND kind = $2)`,
		houseID, kind).Scan(&ok); err != nil {
		return false, fmt.Errorf("check house content: %w", err)
	}
	return ok, nil
}

// DoDeleteBbs deletes bulletin-board posts (レガシーbbs_delete/gentei_delete)。
// 通常掲示板(kind=normal)は家主かゲーム管理者のみが実行でき、articleNo(記事no.=
// 投稿ID)で1件、threadNo(親記事no.=NO.x)でスレッドごと、all=trueで全記事を削除する。
// レスが付いた親記事はarticleNo指定では削除できない。家主板(kind=nushi)は
// articleNoで1件のみ、書いた本人かゲーム管理者が削除できる。
func (s *Service) DoDeleteBbs(ctx context.Context, playerID, houseID int64, kind string, articleNo, threadNo int64, all bool, idempotencyKey string) (*player.Player, error) {
	if kind != "normal" && kind != "nushi" {
		return nil, &ConditionError{Message: "掲示板の種類が正しくありません。"}
	}
	if articleNo <= 0 && threadNo <= 0 && !all {
		return nil, &ConditionError{Message: "記事no.が指定されていません"}
	}
	return s.runAction(ctx, playerID, "house_bbs_delete", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var ownerID int64
		err := tx.QueryRow(ctx, `SELECT owner_id FROM player_houses WHERE id = $1`, houseID).Scan(&ownerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その家は存在しません。"}
		}
		if err != nil {
			return fmt.Errorf("load house: %w", err)
		}
		var isAdmin bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM player_roles WHERE player_id = $1 AND role = 'admin')`,
			playerID).Scan(&isAdmin); err != nil {
			return fmt.Errorf("check admin: %w", err)
		}
		if kind == "nushi" {
			if playerID != ownerID && !isAdmin {
				return &ConditionError{Message: "家主（配偶者）、ゲーム管理者以外は記事削除できません。"}
			}
			if articleNo <= 0 {
				return &ConditionError{Message: "記事no.が指定されていません"}
			}
			var authorID int64
			err := tx.QueryRow(ctx,
				`SELECT COALESCE(author_id, 0) FROM house_bbs WHERE id = $1 AND house_id = $2 AND kind = 'nushi'`,
				articleNo, houseID).Scan(&authorID)
			if errors.Is(err, pgx.ErrNoRows) {
				return &ConditionError{Message: "該当する記事no.が見つかりません。"}
			}
			if err != nil {
				return fmt.Errorf("load post: %w", err)
			}
			if authorID != playerID && !isAdmin {
				return &ConditionError{Message: "書いた本人以外は記事削除できません。"}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM house_bbs WHERE id = $1`, articleNo); err != nil {
				return fmt.Errorf("delete bbs: %w", err)
			}
			return nil
		}
		// 通常掲示板: 家主か管理者のみ(レガシーの画面注記は「管理者のみ」だが
		// 実装は自分の家なら家主も可)。
		if playerID != ownerID && !isAdmin {
			return &ConditionError{Message: "管理者以外は記事削除できません。"}
		}
		switch {
		case all:
			if _, err := tx.Exec(ctx,
				`DELETE FROM house_bbs WHERE house_id = $1 AND kind = 'normal'`, houseID); err != nil {
				return fmt.Errorf("delete all bbs: %w", err)
			}
		case articleNo > 0:
			var postThread *int
			err := tx.QueryRow(ctx,
				`SELECT thread_no FROM house_bbs WHERE id = $1 AND house_id = $2 AND kind = 'normal'`,
				articleNo, houseID).Scan(&postThread)
			if errors.Is(err, pgx.ErrNoRows) {
				return &ConditionError{Message: "該当する記事no.が見つかりません。"}
			}
			if err != nil {
				return fmt.Errorf("load post: %w", err)
			}
			if postThread != nil {
				var hasReply bool
				if err := tx.QueryRow(ctx,
					`SELECT EXISTS(SELECT 1 FROM house_bbs WHERE house_id = $1 AND kind = 'normal' AND parent_no = $2)`,
					houseID, *postThread).Scan(&hasReply); err != nil {
					return fmt.Errorf("check replies: %w", err)
				}
				if hasReply {
					return &ConditionError{Message: "子記事のついた親記事は削除できません。"}
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM house_bbs WHERE id = $1`, articleNo); err != nil {
				return fmt.Errorf("delete bbs: %w", err)
			}
		default:
			// 親記事no.(NO.x)指定: スレッドごと(親+レス)削除。
			tag, err := tx.Exec(ctx,
				`DELETE FROM house_bbs WHERE house_id = $1 AND kind = 'normal' AND (thread_no = $2 OR parent_no = $2)`,
				houseID, threadNo)
			if err != nil {
				return fmt.Errorf("delete thread: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return &ConditionError{Message: "該当する記事no.が見つかりません。"}
			}
		}
		return nil
	})
}

// DoSetShopPrice sets the per-item shelf price of a house shop item (my_syouhin,
// 個別価格設定). The price must be at most 仕入れ値×3. A price of 0 clears the
// override so the price falls back to 仕入れ値×掛け率.
func (s *Service) DoSetShopPrice(ctx context.Context, playerID, houseID, itemID, sellPrice int64, idempotencyKey string) (*player.Player, error) {
	if sellPrice < 0 {
		return nil, &ConditionError{Message: "販売価格が正しくありません。"}
	}
	return s.runAction(ctx, playerID, "shop_price", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var buyPrice int64
		err := tx.QueryRow(ctx,
			`SELECT ss.buy_price FROM house_shop_stock ss
			 JOIN player_houses h ON h.id = ss.house_id
			 WHERE ss.house_id = $1 AND ss.item_id = $2 AND h.owner_id = $3`,
			houseID, itemID, playerID).Scan(&buyPrice)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その商品は店にありません。"}
		}
		if err != nil {
			return fmt.Errorf("load stock: %w", err)
		}
		if sellPrice == 0 {
			// 0は個別価格の解除(掛け率に戻す)。
			if _, err := tx.Exec(ctx,
				`UPDATE house_shop_stock SET sell_price = NULL WHERE house_id = $1 AND item_id = $2`,
				houseID, itemID); err != nil {
				return fmt.Errorf("clear price: %w", err)
			}
			return nil
		}
		if sellPrice > buyPrice*3 {
			return &ConditionError{Message: "販売価格は仕入れ値の3倍以内にしてください。"}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE house_shop_stock SET sell_price = $1 WHERE house_id = $2 AND item_id = $3`,
			sellPrice, houseID, itemID); err != nil {
			return fmt.Errorf("update price: %w", err)
		}
		return nil
	})
}
