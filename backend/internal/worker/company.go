package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/ledger"
)

// companyIncomeSQL sums the daily income of every 運営(tuika=1)/株式会社(tuika=2)
// per house. Per staff: 職判定(最高給の有資格職、無資格=給与0)と
// 収入=(基本給×10+総合能力値×10)/4。総合能力値=Σパラ×235/20(身長170+体重65固定)。
const companyIncomeSQL = `
	SELECT h.id, h.owner_id, h.tuika,
	       COALESCE(SUM((COALESCE(best.salary, 0)*10 + (ps.param_sum*235/20)*10) / 4), 0)
	FROM player_houses h
	JOIN company_staff cs ON cs.house_id = h.id
	CROSS JOIN LATERAL (
	  SELECT (SELECT COALESCE(SUM(v.value::int), 0) FROM jsonb_each_text(cs.params) v) AS param_sum
	) ps
	LEFT JOIN LATERAL (
	  SELECT j.salary FROM content_jobs j
	  WHERE j.enabled AND COALESCE(j.require_master, '') = ''
	    AND (j.bmi_min IS NULL OR j.bmi_min <= 22.5)
	    AND (j.bmi_max IS NULL OR j.bmi_max >= 22.5)
	    AND (j.height_min IS NULL OR j.height_min <= 170)
	    AND NOT EXISTS (
	      SELECT 1 FROM jsonb_array_elements(j.requirements) r
	      WHERE r->>'pred' = 'param_gte'
	        AND COALESCE((cs.params->>(r->>'param'))::int, 0) < (r->>'value')::int)
	  ORDER BY j.salary DESC LIMIT 1
	) best ON true
	WHERE h.tuika IN (1, 2)
	GROUP BY h.id, h.owner_id, h.tuika`

// PayCompanyIncome pays the daily 運営/株式会社 income (レガシーtown_makerの
// unei_siokuri2/3): 運営はオーナーの普通口座へ、株式会社はオーナーに加えて
// 各役員にも同額を普通口座へ振り込む。Returns the number of paid houses.
func PayCompanyIncome(ctx context.Context, tx pgx.Tx, led *ledger.Repo) (int, error) {
	rows, err := tx.Query(ctx, companyIncomeSQL)
	if err != nil {
		return 0, fmt.Errorf("company income: %w", err)
	}
	type payout struct {
		houseID, ownerID, income int64
		tuika                    int
	}
	var payouts []payout
	for rows.Next() {
		var p payout
		if err := rows.Scan(&p.houseID, &p.ownerID, &p.tuika, &p.income); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan company income: %w", err)
		}
		if p.income > 0 {
			payouts = append(payouts, p)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	paid := 0
	for _, p := range payouts {
		entries := []ledger.Entry{
			{Account: ledger.SavingsAccount(p.ownerID), Delta: p.income},
			{Account: ledger.SystemAccount("company_income"), Delta: -p.income},
		}
		if p.tuika == 2 {
			// 株式会社は各役員にも同額(レガシーunei_siokuri3)。
			orows, err := tx.Query(ctx,
				`SELECT player_id FROM company_officers WHERE house_id = $1`, p.houseID)
			if err != nil {
				return paid, fmt.Errorf("officers: %w", err)
			}
			var officerIDs []int64
			for orows.Next() {
				var oid int64
				if err := orows.Scan(&oid); err != nil {
					orows.Close()
					return paid, err
				}
				officerIDs = append(officerIDs, oid)
			}
			orows.Close()
			if err := orows.Err(); err != nil {
				return paid, err
			}
			for _, oid := range officerIDs {
				entries = append(entries,
					ledger.Entry{Account: ledger.SavingsAccount(oid), Delta: p.income},
					ledger.Entry{Account: ledger.SystemAccount("company_income"), Delta: -p.income})
			}
		}
		if err := led.PostTx(ctx, tx, "company_income", "", entries); err != nil {
			return paid, fmt.Errorf("pay company income: %w", err)
		}
		paid++
	}
	return paid, nil
}
