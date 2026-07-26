package action

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/player"
)

// 運営/株式会社の社員教育定数(レガシーunei_2.pl/kaishiya.pl)。
const (
	companyEduEfficiency  = 10    // 与えたパラの1/10が社員に入る
	companyEduFeePerPoint = 20000 // 上がった1ポイントあたりの養育費(円)
	companyEduIntervalMin = 60    // 社員ごとの教育間隔(分) = レガシー1時間
	uneiStaffMax          = 5     // 運営の社員上限
)

// companyParamKeys are the 16 educatable parameters (player_statusの列名)。
var companyParamKeys = map[string]string{
	"kokugo": "国語", "suugaku": "数学", "rika": "理科", "syakai": "社会",
	"eigo": "英語", "ongaku": "音楽", "bijutsu": "美術", "looks": "ルックス",
	"tairyoku": "体力", "kenkou": "健康", "speed": "スピード", "power": "パワー",
	"wanryoku": "腕力", "kyakuryoku": "脚力", "love": "LOVE", "omoshirosa": "面白さ",
}

// loadCompanyHouse validates a 運営(tuika=1)/株式会社(tuika=2) house and returns
// its owner and kind.
func loadCompanyHouse(ctx context.Context, tx pgx.Tx, houseID int64) (ownerID int64, tuika int, err error) {
	err = tx.QueryRow(ctx,
		`SELECT owner_id, tuika FROM player_houses WHERE id = $1`, houseID).Scan(&ownerID, &tuika)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, &ConditionError{Message: "その家は存在しません。"}
	}
	if err != nil {
		return 0, 0, fmt.Errorf("load house: %w", err)
	}
	if tuika != 1 && tuika != 2 {
		return 0, 0, &ConditionError{Message: "この家は運営・株式会社ではありません。"}
	}
	return ownerID, tuika, nil
}

// DoStaffAdd adds an empty-parameter employee to a 運営/株式会社
// (レガシーsyain_up)。運営は家主のみ・上限5人。株式会社はオーナーのみ・
// 上限3×役員数。
func (s *Service) DoStaffAdd(ctx context.Context, playerID, houseID int64, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "staff_add", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		ownerID, tuika, err := loadCompanyHouse(ctx, tx, houseID)
		if err != nil {
			return err
		}
		if ownerID != playerID {
			return &ConditionError{Message: "社員を増やせるのはオーナーだけです。"}
		}
		limit, err := companyStaffLimit(ctx, tx, houseID, tuika)
		if err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM company_staff WHERE house_id = $1`, houseID).Scan(&count); err != nil {
			return fmt.Errorf("count staff: %w", err)
		}
		if count >= limit {
			return &ConditionError{Message: fmt.Sprintf("社員は%d人までです。", limit)}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO company_staff (house_id, idx) VALUES ($1, COALESCE((SELECT MAX(idx) FROM company_staff WHERE house_id = $1), 0) + 1)`,
			houseID); err != nil {
			return fmt.Errorf("insert staff: %w", err)
		}
		return nil
	})
}

// companyStaffLimit returns the staff cap: 運営=5, 株式会社=3×役員数(オーナー含む).
func companyStaffLimit(ctx context.Context, tx pgx.Tx, houseID int64, tuika int) (int, error) {
	if tuika == 1 {
		return uneiStaffMax, nil
	}
	var officers int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM company_officers WHERE house_id = $1`, houseID).Scan(&officers); err != nil {
		return 0, fmt.Errorf("count officers: %w", err)
	}
	return 3 * (officers + 1), nil
}

// isCompanyOfficer reports whether the player may educate staff of the house:
// 運営は家主のみ、株式会社はオーナー+役員。
func isCompanyOfficer(ctx context.Context, tx pgx.Tx, houseID, ownerID, playerID int64, tuika int) (bool, error) {
	if playerID == ownerID {
		return true, nil
	}
	if tuika != 2 {
		return false, nil
	}
	var ok bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM company_officers WHERE house_id = $1 AND player_id = $2)`,
		houseID, playerID).Scan(&ok); err != nil {
		return false, fmt.Errorf("check officer: %w", err)
	}
	return ok, nil
}

// StaffEduResult summarizes a 社員教育 for the result toast.
type StaffEduResult struct {
	ParamName string `json:"param_name"`
	Gained    int    `json:"gained"`
	Fee       int64  `json:"fee"`
}

// DoStaffEducate spends the educator's own parameter to raise an employee's
// (レガシーdo_unei): 社員は与えた値の1/10だけ上がり、上がった1ポイントにつき
// 20000円の養育費(現金/クレジット→普通口座)。社員ごとに1時間間隔。
func (s *Service) DoStaffEducate(ctx context.Context, playerID, houseID, staffID int64, paramKey string, amount int, payMethod, idempotencyKey string) (*player.Player, *StaffEduResult, error) {
	// 食材購入(株式会社のみ): パラメータを消費せずお金だけで会社の食料原料を買う。
	isSyoku := paramKey == "syoku"
	paramName := "食材購入"
	if !isSyoku {
		var ok bool
		paramName, ok = companyParamKeys[paramKey]
		if !ok {
			return nil, nil, &ConditionError{Message: "能力の指定が正しくありません。"}
		}
	}
	if amount < 1 || amount > 1000 {
		return nil, nil, &ConditionError{Message: "値に誤りがあります。"}
	}
	if payMethod == "" {
		payMethod = "cash"
	}
	if payMethod != "cash" && payMethod != "credit" {
		return nil, nil, &ConditionError{Message: "支払い方法が正しくありません。"}
	}
	result := &StaffEduResult{ParamName: paramName}
	p, err := s.runAction(ctx, playerID, "staff_edu", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		ownerID, tuika, err := loadCompanyHouse(ctx, tx, houseID)
		if err != nil {
			return err
		}
		allowed, err := isCompanyOfficer(ctx, tx, houseID, ownerID, playerID, tuika)
		if err != nil {
			return err
		}
		if !allowed {
			return &ConditionError{Message: "社員教育ができるのはオーナー(株式会社は役員も)だけです。"}
		}
		var lastEdu *time.Time
		err = tx.QueryRow(ctx,
			`SELECT last_edu_at FROM company_staff WHERE id = $1 AND house_id = $2`, staffID, houseID).Scan(&lastEdu)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "その社員はいません。"}
		}
		if err != nil {
			return fmt.Errorf("load staff: %w", err)
		}
		if lastEdu != nil && time.Since(*lastEdu) < companyEduIntervalMin*time.Minute {
			return &ConditionError{Message: "まだできません。"}
		}
		gained := amount / companyEduEfficiency
		fee := int64(gained) * companyEduFeePerPoint
		if isSyoku {
			if tuika != 2 {
				return &ConditionError{Message: "食材購入は株式会社だけができます。"}
			}
		} else {
			// 教育者自身のパラメータを消費(負になる教育は不可)。
			tag, err := tx.Exec(ctx,
				fmt.Sprintf(`UPDATE player_status SET %s = %s - $2 WHERE player_id = $1 AND %s >= $2`,
					paramKey, paramKey, paramKey),
				playerID, amount)
			if err != nil {
				return fmt.Errorf("spend param: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return &ConditionError{Message: "パラメータが足りません。教育する側のパラメータが必要です。"}
			}
		}
		// 養育費の支払い。
		if fee > 0 {
			payer := ledger.PlayerAccount(playerID)
			if payMethod == "credit" {
				hasCard, err := hasCreditCard(ctx, tx, playerID)
				if err != nil {
					return err
				}
				if !hasCard {
					return &ConditionError{Message: "クレジットカードを持っていません。"}
				}
				var savings int64
				if err := tx.QueryRow(ctx,
					`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
					ledger.SavingsAccount(playerID)).Scan(&savings); err != nil {
					return fmt.Errorf("read savings: %w", err)
				}
				if savings < fee {
					return &ConditionError{Message: "貯金がありません。"}
				}
				payer = ledger.SavingsAccount(playerID)
			} else if state.Money < fee {
				return &ConditionError{Message: "お金が足りません"}
			}
			if err := s.ledger.PostTx(ctx, tx, "staff_edu_fee", "", []ledger.Entry{
				{Account: payer, Delta: -fee},
				{Account: ledger.SystemAccount("company_edu"), Delta: fee},
			}); err != nil {
				return fmt.Errorf("pay fee: %w", err)
			}
		}
		var eduName string
		if err := tx.QueryRow(ctx, `SELECT display_name FROM players WHERE id = $1`, playerID).Scan(&eduName); err != nil {
			return fmt.Errorf("load name: %w", err)
		}
		if isSyoku {
			// 食料原料 += 0.1×上昇分(レガシー: $unei_syoku += 0.1*$konoagatta_suuti)。
			if _, err := tx.Exec(ctx,
				`INSERT INTO company_materials (house_id, syoku) VALUES ($1, $2 * 0.1)
				 ON CONFLICT (house_id) DO UPDATE SET syoku = company_materials.syoku + $2 * 0.1`,
				houseID, gained); err != nil {
				return fmt.Errorf("add syoku: %w", err)
			}
			log := fmt.Sprintf("食材を%.1fキロカロリー購入しました（%s） by %s",
				float64(gained)*0.1, time.Now().Format("2006/01/02 15:04"), eduName)
			if _, err := tx.Exec(ctx,
				`UPDATE company_staff SET edu_log = $2, last_edu_at = now() WHERE id = $1`, staffID, log); err != nil {
				return fmt.Errorf("update staff: %w", err)
			}
		} else {
			log := fmt.Sprintf("社員教育で%sパラメータを%dあげました（%s） by %s",
				paramName, gained, time.Now().Format("2006/01/02 15:04"), eduName)
			if _, err := tx.Exec(ctx,
				`UPDATE company_staff SET params = jsonb_set(params, ARRAY[$2],
				   to_jsonb(COALESCE((params->>$2)::int, 0) + $3), true),
				   edu_log = $4, last_edu_at = now()
				 WHERE id = $1`, staffID, paramKey, gained, log); err != nil {
				return fmt.Errorf("update staff: %w", err)
			}
		}
		result.Gained = gained
		result.Fee = fee
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return p, result, nil
}
