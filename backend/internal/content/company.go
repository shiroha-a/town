package content

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CompanyStaff is one employee of a 運営/株式会社 with the derived job and
// income (レガシー: 総合能力値=Σパラ×(170+65)/20、収入=(基本給×10+総合×10)/4)。
type CompanyStaff struct {
	ID       int64          `json:"id"`
	Idx      int            `json:"idx"`
	Params   map[string]int `json:"params"`
	Job      string         `json:"job"`
	Sougou   int            `json:"sougou"`
	Income   int64          `json:"income"`
	EduLog   string         `json:"edu_log"`
	CanEduAt string         `json:"can_edu_at"` // 次に教育できる時刻(RFC3339、空=今すぐ可)
}

// CompanyOfficer is one 役員 of a 株式会社.
type CompanyOfficer struct {
	PlayerID int64  `json:"player_id"`
	Name     string `json:"name"`
}

// CompanyBbsPost is one 会社BBS post. Statusは入退会ワークフロー
// (in=入会希望/out=退会希望/m_ryoukai=受領/taikai=退会指定)。
type CompanyBbsPost struct {
	ID         int64  `json:"id"`
	No         int    `json:"no"`
	AuthorID   int64  `json:"author_id"`
	AuthorName string `json:"author_name"`
	Body       string `json:"body"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
}

// CompanyMaterials is the 製造 form inputs: 原料max(社員パラ最大値/10)と食料。
type CompanyMaterials struct {
	Maxima      map[string]int `json:"maxima"`
	Syoku       int            `json:"syoku"`
	StaffCount  int            `json:"staff_count"`
	MadeToday   bool           `json:"made_today"`
	HasShop     bool           `json:"has_shop"`
	ShopSyubetu string         `json:"shop_syubetu"`
}

// CompanyView is a 運営(tuika=1)/株式会社(tuika=2) house as seen by a viewer.
type CompanyView struct {
	IsCompany   bool             `json:"is_company"`
	Kind        int              `json:"kind"` // 1=運営 2=株式会社
	OwnerName   string           `json:"owner_name"`
	Own         bool             `json:"own"`
	Officer     bool             `json:"officer"` // 閲覧者が役員(株式会社)か
	Officers    []CompanyOfficer `json:"officers"`
	StaffMax    int              `json:"staff_max"`
	TotalIncome int64            `json:"total_income"`
	Staff       []CompanyStaff   `json:"staff"`
	// 株式会社のみ: 会社BBS(メンバー板は役員/オーナーだけ)と製造原料(オーナーだけ)。
	BbsOpen   []CompanyBbsPost  `json:"bbs_open"`
	BbsMember []CompanyBbsPost  `json:"bbs_member"`
	Materials *CompanyMaterials `json:"materials"`
	// 教育UI用の定数(レガシー: 効率1/10、1pt=2万円、間隔1時間)。
	EduEfficiency int `json:"edu_efficiency"`
	EduFeePoint   int `json:"edu_fee_point"`
	EduIntervalMi int `json:"edu_interval_min"`
}

// staffJobSQL derives the best-paying job a staff qualifies for. 社員は身長170cm
// /体重65kg固定なのでBMI≒22.5として絞る。無資格は浮浪者(給与0)。
const staffJobSQL = `
	SELECT cs.id, cs.idx, cs.params, cs.edu_log, cs.last_edu_at,
	       COALESCE(best.name, '浮浪者'), COALESCE(best.salary, 0),
	       (SELECT COALESCE(SUM(v.value::int), 0) FROM jsonb_each_text(cs.params) v)
	FROM company_staff cs
	LEFT JOIN LATERAL (
	  SELECT j.name, j.salary FROM content_jobs j
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
	WHERE cs.house_id = $1
	ORDER BY cs.idx`

// StaffIncome computes the per-staff income from salary and total params
// (レガシーjyob_machi2: (基本給×10 + 総合能力値×10)/4)。
func StaffIncome(salary int64, paramSum int) (sougou int, income int64) {
	// 総合能力値 = Σパラ × (身長170+体重65)/20 = Σ×11.75。
	sougou = paramSum * 235 / 20
	income = (salary*10 + int64(sougou)*10) / 4
	return sougou, income
}

// Company returns the 運営/株式会社 screen state of a house.
func (s *Service) Company(ctx context.Context, viewerID, houseID int64) (*CompanyView, error) {
	var (
		ownerID   int64
		tuika     int
		ownerName string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT h.owner_id, h.tuika, COALESCE(p.display_name, '')
		 FROM player_houses h LEFT JOIN players p ON p.id = h.owner_id
		 WHERE h.id = $1`, houseID).Scan(&ownerID, &tuika, &ownerName)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && tuika != 1 && tuika != 2) {
		return &CompanyView{IsCompany: false, Staff: []CompanyStaff{}, Officers: []CompanyOfficer{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load house: %w", err)
	}
	view := &CompanyView{
		IsCompany:     true,
		Kind:          tuika,
		OwnerName:     ownerName,
		Own:           viewerID == ownerID,
		Staff:         []CompanyStaff{},
		Officers:      []CompanyOfficer{},
		BbsOpen:       []CompanyBbsPost{},
		BbsMember:     []CompanyBbsPost{},
		EduEfficiency: 10,
		EduFeePoint:   20000,
		EduIntervalMi: 60,
	}
	view.StaffMax = 5
	if tuika == 2 {
		orows, err := s.pool.Query(ctx,
			`SELECT o.player_id, COALESCE(p.display_name, '') FROM company_officers o
			 LEFT JOIN players p ON p.id = o.player_id WHERE o.house_id = $1 ORDER BY o.joined_at`, houseID)
		if err != nil {
			return nil, fmt.Errorf("list officers: %w", err)
		}
		defer orows.Close()
		for orows.Next() {
			var o CompanyOfficer
			if err := orows.Scan(&o.PlayerID, &o.Name); err != nil {
				return nil, fmt.Errorf("scan officer: %w", err)
			}
			if o.PlayerID == viewerID {
				view.Officer = true
			}
			view.Officers = append(view.Officers, o)
		}
		if err := orows.Err(); err != nil {
			return nil, err
		}
		view.StaffMax = 3 * (len(view.Officers) + 1)
		if err := s.loadCompanyBbs(ctx, view, houseID); err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, staffJobSQL, houseID)
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			st       CompanyStaff
			lastEdu  *time.Time
			salary   int64
			paramSum int
		)
		if err := rows.Scan(&st.ID, &st.Idx, &st.Params, &st.EduLog, &lastEdu, &st.Job, &salary, &paramSum); err != nil {
			return nil, fmt.Errorf("scan staff: %w", err)
		}
		st.Sougou, st.Income = StaffIncome(salary, paramSum)
		if lastEdu != nil {
			next := lastEdu.Add(time.Hour)
			if time.Now().Before(next) {
				st.CanEduAt = next.Format(time.RFC3339)
			}
		}
		view.TotalIncome += st.Income
		view.Staff = append(view.Staff, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 製造フォーム(株式会社のオーナーのみ): 原料=社員パラ最大値/10と食料。
	if tuika == 2 && view.Own {
		if err := s.loadCompanyMaterials(ctx, view, houseID, ownerID); err != nil {
			return nil, err
		}
	}
	return view, nil
}

// loadCompanyBbs fills both 会社BBS boards (最新30件、新しい順)。
func (s *Service) loadCompanyBbs(ctx context.Context, view *CompanyView, houseID int64) error {
	rows, err := s.pool.Query(ctx,
		`SELECT id, board, no, COALESCE(author_id, 0), author_name, body, status, created_at
		 FROM company_bbs WHERE house_id = $1 ORDER BY id DESC`, houseID)
	if err != nil {
		return fmt.Errorf("list company bbs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			p       CompanyBbsPost
			board   string
			created time.Time
		)
		if err := rows.Scan(&p.ID, &board, &p.No, &p.AuthorID, &p.AuthorName, &p.Body, &p.Status, &created); err != nil {
			return fmt.Errorf("scan company bbs: %w", err)
		}
		p.CreatedAt = created.Format(time.RFC3339)
		if board == "member" {
			// メンバー板は役員/オーナーにだけ返す。
			if view.Own || view.Officer {
				view.BbsMember = append(view.BbsMember, p)
			}
		} else {
			view.BbsOpen = append(view.BbsOpen, p)
		}
	}
	return rows.Err()
}

// loadCompanyMaterials fills the 製造 form: 原料max(社員パラ最大値/10)・食料・
// 1日1回の生産可否・売り先の店。
func (s *Service) loadCompanyMaterials(ctx context.Context, view *CompanyView, houseID, ownerID int64) error {
	m := &CompanyMaterials{Maxima: map[string]int{}, StaffCount: len(view.Staff)}
	for _, st := range view.Staff {
		for k, v := range st.Params {
			if v/10 > m.Maxima[k] {
				m.Maxima[k] = v / 10
			}
		}
	}
	var (
		lastMade *time.Time
		syoku    float64
	)
	err := s.pool.QueryRow(ctx,
		`SELECT last_made_on, syoku::float8 FROM company_materials WHERE house_id = $1`, houseID).
		Scan(&lastMade, &syoku)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load materials: %w", err)
	}
	m.Syoku = int(syoku)
	if lastMade != nil && lastMade.Format("2006-01-02") == time.Now().Format("2006-01-02") {
		m.MadeToday = true
	}
	err = s.pool.QueryRow(ctx,
		`SELECT hs.syubetu FROM house_shops hs JOIN player_houses h ON h.id = hs.house_id
		 WHERE h.owner_id = $1 ORDER BY hs.house_id LIMIT 1`, ownerID).Scan(&m.ShopSyubetu)
	if err == nil {
		m.HasShop = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load shop: %w", err)
	}
	view.Materials = m
	return nil
}
