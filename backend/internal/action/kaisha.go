package action

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/player"
)

// 株式会社の定数(レガシーkaishiya.pl)。
const (
	kaishaOfficerMax  = 10 // 役員上限(オーナー除く)
	kaishaBbsMax      = 30 // 会社BBSの板ごとの保持件数
	kaishaBbsBodyMax  = 500
	seizouParamMax    = 500  // 商品1つのup値上限
	seizouCalMax      = 9999 // 商品1つのカロリー上限
	seizouDefaultUnit = 1000 // 既定価格 = max_taiku × 1000円
)

// loadKaishaHouse validates a 株式会社(tuika=2) house and returns its owner.
func loadKaishaHouse(ctx context.Context, tx pgx.Tx, houseID int64) (int64, error) {
	var ownerID int64
	var tuika int
	err := tx.QueryRow(ctx,
		`SELECT owner_id, tuika FROM player_houses WHERE id = $1`, houseID).Scan(&ownerID, &tuika)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, &ConditionError{Message: "その家は存在しません。"}
	}
	if err != nil {
		return 0, fmt.Errorf("load house: %w", err)
	}
	if tuika != 2 {
		return 0, &ConditionError{Message: "この家は株式会社ではありません。"}
	}
	return ownerID, nil
}

// DoCompanyBbsPost writes to the 会社BBS (レガシーkaisya_bbs_do)。
// board='open'は誰でも書け、wantJoin=trueで入会希望を付ける。board='member'は
// オーナー/役員のみで、wantLeave=trueで退会希望を付ける。各板30件保持。
func (s *Service) DoCompanyBbsPost(ctx context.Context, playerID, houseID int64, board, body string, wantJoin, wantLeave bool, idempotencyKey string) (*player.Player, error) {
	body = strings.TrimSpace(body)
	if board != "open" && board != "member" {
		return nil, &ConditionError{Message: "掲示板の指定が正しくありません。"}
	}
	if body == "" && !wantJoin && !wantLeave {
		return nil, &ConditionError{Message: "メッセージが入力されていません。"}
	}
	if utf8.RuneCountInString(body) > kaishaBbsBodyMax {
		return nil, &ConditionError{Message: fmt.Sprintf("メッセージは%d字以内です。", kaishaBbsBodyMax)}
	}
	return s.runAction(ctx, playerID, "company_bbs", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		ownerID, err := loadKaishaHouse(ctx, tx, houseID)
		if err != nil {
			return err
		}
		isOfficer, err := isCompanyOfficer(ctx, tx, houseID, ownerID, playerID, 2)
		if err != nil {
			return err
		}
		status := ""
		if board == "member" {
			if !isOfficer {
				return &ConditionError{Message: "メンバー掲示板は役員のみ書き込めます。"}
			}
			if wantLeave && playerID != ownerID {
				status = "out"
			}
		} else {
			// オーナー/役員の入会希望は無効(レガシー同様)。
			if wantJoin && !isOfficer {
				status = "in"
			}
		}
		var name string
		if err := tx.QueryRow(ctx, `SELECT display_name FROM players WHERE id = $1`, playerID).Scan(&name); err != nil {
			return fmt.Errorf("load name: %w", err)
		}
		if err := insertCompanyBbs(ctx, tx, houseID, board, playerID, name, body, status); err != nil {
			return err
		}
		return nil
	})
}

// insertCompanyBbs appends a BBS post and trims the board to 30 rows.
func insertCompanyBbs(ctx context.Context, tx pgx.Tx, houseID int64, board string, authorID int64, authorName, body, status string) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO company_bbs (house_id, board, no, author_id, author_name, body, status)
		 VALUES ($1, $2, COALESCE((SELECT MAX(no) FROM company_bbs WHERE house_id = $1 AND board = $2), 0) + 1, $3, $4, $5, $6)`,
		houseID, board, authorID, authorName, body, status); err != nil {
		return fmt.Errorf("insert bbs: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM company_bbs WHERE house_id = $1 AND board = $2 AND id NOT IN (
		   SELECT id FROM company_bbs WHERE house_id = $1 AND board = $2 ORDER BY id DESC LIMIT $3)`,
		houseID, board, kaishaBbsMax); err != nil {
		return fmt.Errorf("trim bbs: %w", err)
	}
	return nil
}

// DoCompanyApprove lets the owner approve a pending 入会(in)/退会(out) request
// post (レガシー: オーナーの入会/退会ボタン)。
func (s *Service) DoCompanyApprove(ctx context.Context, playerID, houseID, postID int64, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "company_approve", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		ownerID, err := loadKaishaHouse(ctx, tx, houseID)
		if err != nil {
			return err
		}
		if playerID != ownerID {
			return &ConditionError{Message: "入退会の許可はオーナーだけができます。"}
		}
		var (
			authorID *int64
			status   string
		)
		err = tx.QueryRow(ctx,
			`SELECT author_id, status FROM company_bbs WHERE id = $1 AND house_id = $2`, postID, houseID).
			Scan(&authorID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その記事はありません。"}
		}
		if err != nil {
			return fmt.Errorf("load post: %w", err)
		}
		if authorID == nil || (status != "in" && status != "out") {
			return &ConditionError{Message: "その記事は入退会の申請ではありません。"}
		}
		if status == "in" {
			if *authorID == ownerID {
				return &ConditionError{Message: "オーナーは役員になれません。"}
			}
			var count int
			if err := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM company_officers WHERE house_id = $1`, houseID).Scan(&count); err != nil {
				return fmt.Errorf("count officers: %w", err)
			}
			if count >= kaishaOfficerMax {
				return &ConditionError{Message: fmt.Sprintf("役員は%d人までです。", kaishaOfficerMax)}
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO company_officers (house_id, player_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				houseID, *authorID); err != nil {
				return fmt.Errorf("add officer: %w", err)
			}
		} else {
			if _, err := tx.Exec(ctx,
				`DELETE FROM company_officers WHERE house_id = $1 AND player_id = $2`, houseID, *authorID); err != nil {
				return fmt.Errorf("remove officer: %w", err)
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE company_bbs SET status = 'm_ryoukai' WHERE id = $1`, postID); err != nil {
			return fmt.Errorf("update post: %w", err)
		}
		return nil
	})
}

// DoCompanyKick lets the owner remove an officer directly (レガシー退会指定)。
// The removal is announced on the open board.
func (s *Service) DoCompanyKick(ctx context.Context, playerID, houseID, officerID int64, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "company_kick", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		ownerID, err := loadKaishaHouse(ctx, tx, houseID)
		if err != nil {
			return err
		}
		if playerID != ownerID {
			return &ConditionError{Message: "退会指定はオーナーだけができます。"}
		}
		tag, err := tx.Exec(ctx,
			`DELETE FROM company_officers WHERE house_id = $1 AND player_id = $2`, houseID, officerID)
		if err != nil {
			return fmt.Errorf("remove officer: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return &ConditionError{Message: "その役員はいません。"}
		}
		var target, name string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(display_name, '') FROM players WHERE id = $1`, officerID).Scan(&target); err != nil {
			return fmt.Errorf("load target: %w", err)
		}
		if err := tx.QueryRow(ctx, `SELECT display_name FROM players WHERE id = $1`, playerID).Scan(&name); err != nil {
			return fmt.Errorf("load name: %w", err)
		}
		return insertCompanyBbs(ctx, tx, houseID, "open", playerID, name, "退会者："+target, "taikai")
	})
}

// DoCompanyBbsDelete lets the owner delete a BBS post by number (レガシー削除)。
func (s *Service) DoCompanyBbsDelete(ctx context.Context, playerID, houseID int64, board string, no int, idempotencyKey string) (*player.Player, error) {
	if board != "open" && board != "member" {
		return nil, &ConditionError{Message: "掲示板の指定が正しくありません。"}
	}
	return s.runAction(ctx, playerID, "company_bbs_del", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		ownerID, err := loadKaishaHouse(ctx, tx, houseID)
		if err != nil {
			return err
		}
		if playerID != ownerID {
			return &ConditionError{Message: "記事削除はオーナーだけができます。"}
		}
		tag, err := tx.Exec(ctx,
			`DELETE FROM company_bbs WHERE house_id = $1 AND board = $2 AND no = $3`, houseID, board, no)
		if err != nil {
			return fmt.Errorf("delete post: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return &ConditionError{Message: "該当する記事no.が見つかりません。"}
		}
		return nil
	})
}

// SeizouInput is the product design form of 製造 (レガシーseizou)。
type SeizouInput struct {
	Name    string         `json:"name"`
	Params  map[string]int `json:"params"` // 16パラのup値(0..500かつ原料max以内)
	Cal     int            `json:"cal"`
	Kankaku int            `json:"kankaku"` // 使用間隔(分)
	Zaiko   int            `json:"zaiko"`
	Taikyuu int            `json:"taikyuu"`
	Price   int64          `json:"price"` // 0=既定(max_taiku×1000円)
}

// SeizouResult summarizes a production run.
type SeizouResult struct {
	Name    string `json:"name"`
	Zaiko   int    `json:"zaiko"`
	Taikyuu int    `json:"taikyuu"`
	Price   int64  `json:"price"`
}

// DoSeizou manufactures an original product from the company's materials and
// puts it on the owner's shop shelf (レガシーseizou: 1日1回、up値は原料max以内、
// 在庫×耐久は社員数×min(原料/設定値)以内、価格は総資産以内)。
func (s *Service) DoSeizou(ctx context.Context, playerID, houseID int64, in SeizouInput, idempotencyKey string) (*player.Player, *SeizouResult, error) {
	result := &SeizouResult{}
	p, err := s.runAction(ctx, playerID, "seizou", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		ownerID, err := loadKaishaHouse(ctx, tx, houseID)
		if err != nil {
			return err
		}
		if playerID != ownerID {
			return &ConditionError{Message: "製造はオーナーだけができます。"}
		}
		// 売り先: オーナーの家の店(house_shops)。無ければ製造できない。
		var shopHouseID int64
		var shopSyubetu string
		err = tx.QueryRow(ctx,
			`SELECT hs.house_id, hs.syubetu FROM house_shops hs
			 JOIN player_houses h ON h.id = hs.house_id
			 WHERE h.owner_id = $1 ORDER BY hs.house_id LIMIT 1`, playerID).Scan(&shopHouseID, &shopSyubetu)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "商品を並べるお店がありません。先に家の店を開いてください。"}
		}
		if err != nil {
			return fmt.Errorf("load shop: %w", err)
		}
		// 1日1回。
		var lastMade *time.Time
		var seq int
		var syoku float64
		err = tx.QueryRow(ctx,
			`SELECT last_made_on, product_seq, syoku::float8 FROM company_materials WHERE house_id = $1`, houseID).
			Scan(&lastMade, &seq, &syoku)
		if errors.Is(err, pgx.ErrNoRows) {
			seq, syoku = 0, 0
		} else if err != nil {
			return fmt.Errorf("load materials: %w", err)
		}
		today := time.Now().Format("2006-01-02")
		if lastMade != nil && lastMade.Format("2006-01-02") == today {
			return &ConditionError{Message: "本日の生産は完了しました。"}
		}
		// 原料 = 社員ごとの各パラ最大値/10。社員数も取得。
		maxima := map[string]int{}
		var staffCount int
		rows, err := tx.Query(ctx, `SELECT params FROM company_staff WHERE house_id = $1`, houseID)
		if err != nil {
			return fmt.Errorf("list staff: %w", err)
		}
		for rows.Next() {
			var params map[string]int
			if err := rows.Scan(&params); err != nil {
				rows.Close()
				return fmt.Errorf("scan staff: %w", err)
			}
			staffCount++
			for k, v := range params {
				if v/10 > maxima[k] {
					maxima[k] = v / 10
				}
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if staffCount == 0 {
			return &ConditionError{Message: "社員がいないので製造できません。"}
		}
		// up値を検証しつつ効果JSONを構築。b_i=原料max/設定値の最小がロット数。
		maxMini := 1 << 30
		effOps := []map[string]any{}
		for key, v := range in.Params {
			if _, ok := companyParamKeys[key]; !ok {
				return &ConditionError{Message: "能力の指定が正しくありません。"}
			}
			if v < 0 {
				v = 0
			}
			if v == 0 {
				continue
			}
			if v > seizouParamMax {
				v = seizouParamMax
			}
			if v > maxima[key] {
				return &ConditionError{Message: fmt.Sprintf("%sの原料が足りません(最大%d)。", companyParamKeys[key], maxima[key])}
			}
			if b := maxima[key] / v; b < maxMini {
				maxMini = b
			}
			effOps = append(effOps, map[string]any{"op": "add_param", "param": key, "amount": v})
		}
		cal := in.Cal
		if cal < 0 {
			cal = 0
		}
		if cal > seizouCalMax {
			cal = seizouCalMax
		}
		if cal > 0 {
			if float64(cal) > syoku {
				return &ConditionError{Message: fmt.Sprintf("食料原料が足りません(最大%d)。", int(syoku))}
			}
			if b := int(syoku) / cal; b < maxMini {
				maxMini = b
			}
		}
		if len(effOps) == 0 && cal == 0 {
			return &ConditionError{Message: "効果が1つもない商品は作れません。"}
		}
		if maxMini == 1<<30 || maxMini < 1 {
			maxMini = 1
		}
		maxTaiku := staffCount * maxMini
		// 在庫×耐久の割付(レガシーの優先規則)。
		zaiko, taikyuu := in.Zaiko, in.Taikyuu
		switch {
		case zaiko <= 0 && taikyuu <= 0:
			zaiko, taikyuu = 1, maxTaiku
		case zaiko > maxTaiku:
			zaiko, taikyuu = maxTaiku, 1
		case zaiko < 1:
			zaiko, taikyuu = 1, maxTaiku
		case taikyuu < 1:
			zaiko, taikyuu = maxTaiku, 1
		case taikyuu > maxTaiku:
			zaiko, taikyuu = 1, maxTaiku
		case zaiko+taikyuu-1 > maxTaiku:
			zaiko = 1
		}
		// 価格: 既定=max_taiku×1000円。上限=総資産。
		price := in.Price
		if price <= 0 {
			price = int64(maxTaiku) * seizouDefaultUnit
		}
		var assets int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account IN ($1, $2, $3)`,
			fmt.Sprintf("player:%d", playerID), fmt.Sprintf("savings:%d", playerID),
			fmt.Sprintf("super_savings:%d", playerID)).Scan(&assets); err != nil {
			return fmt.Errorf("sum assets: %w", err)
		}
		if price > assets {
			price = assets
		}
		if price < 1 {
			price = 1
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			var pname string
			if err := tx.QueryRow(ctx, `SELECT display_name FROM players WHERE id = $1`, playerID).Scan(&pname); err != nil {
				return fmt.Errorf("load name: %w", err)
			}
			name = pname + "の商品"
		}
		if utf8.RuneCountInString(name) > 50 {
			return &ConditionError{Message: "品名は50字以内です。"}
		}
		seq++
		name = fmt.Sprintf("%s %d", name, seq)
		kankaku := in.Kankaku
		if kankaku <= 0 {
			kankaku = 10
		}
		effJSON, err := json.Marshal(effOps)
		if err != nil {
			return fmt.Errorf("marshal effect: %w", err)
		}
		// 商品はcontent_itemsに登録(facility='company'で問屋/自販機の品揃えから除外)。
		var itemID int64
		if err := tx.QueryRow(ctx,
			`INSERT INTO content_items (name, category, price, effect, enabled, facility,
			   durability, durability_unit, use_interval_min, calorie_g, stock_master, max_sets)
			 VALUES ($1, $2, $3, $4, true, 'company', $5, 'use', $6, $7, 0, 0)
			 RETURNING id`,
			name, shopSyubetu, price, effJSON, taikyuu, kankaku, cal).Scan(&itemID); err != nil {
			return fmt.Errorf("insert product: %w", err)
		}
		// オーナーの家の店の棚へ(個別価格=設定価格)。
		if _, err := tx.Exec(ctx,
			`INSERT INTO house_shop_stock (house_id, item_id, buy_price, sell_price, stock)
			 VALUES ($1, $2, $3, $3, $4)`,
			shopHouseID, itemID, price, zaiko); err != nil {
			return fmt.Errorf("stock product: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO company_materials (house_id, syoku, last_made_on, product_seq)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (house_id) DO UPDATE SET last_made_on = $3, product_seq = $4`,
			houseID, syoku, today, seq); err != nil {
			return fmt.Errorf("update materials: %w", err)
		}
		result.Name = name
		result.Zaiko = zaiko
		result.Taikyuu = taikyuu
		result.Price = price
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return p, result, nil
}
