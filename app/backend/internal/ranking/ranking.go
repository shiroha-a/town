// Package ranking implements the 役場 rankings (legacy yakuba.cgi sortid=23/49/6..22):
// 長者番付, 借金ワースト, 職業レベル and each of the 16 parameters. Everything is
// derived with SQL from existing tables — money from the ledger, parameters from
// player_status — so there is no ranking table to keep in sync.
package ranking

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Limit is the default (and maximum) number of ranked rows, matching the legacy
// $rankMax = 50.
const Limit = 50

// Key identifies one ranking. Keys are matched against this table before any
// column name reaches SQL, so no caller-supplied text is ever interpolated.
type Key struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
	// column is the player_status column to sort by; empty for the money-based
	// rankings, which are computed from the ledger instead.
	column string
	// asc sorts ascending (借金ワースト: the most negative first).
	asc bool
}

// Keys lists every selectable ranking in display order.
var Keys = []Key{
	{Key: "assets", Label: "長者番付", Unit: "円"},
	{Key: "debt", Label: "借金ワースト", Unit: "円", asc: true},
	{Key: "job_level", Label: "職業レベル", Unit: "Lv", column: "job_level"},
	{Key: "kokugo", Label: "国語", column: "kokugo"},
	{Key: "suugaku", Label: "数学", column: "suugaku"},
	{Key: "rika", Label: "理科", column: "rika"},
	{Key: "syakai", Label: "社会", column: "syakai"},
	{Key: "eigo", Label: "英語", column: "eigo"},
	{Key: "ongaku", Label: "音楽", column: "ongaku"},
	{Key: "bijutsu", Label: "美術", column: "bijutsu"},
	{Key: "looks", Label: "ルックス", column: "looks"},
	{Key: "tairyoku", Label: "体力", column: "tairyoku"},
	{Key: "kenkou", Label: "健康", column: "kenkou"},
	{Key: "speed", Label: "スピード", column: "speed"},
	{Key: "power", Label: "パワー", column: "power"},
	{Key: "wanryoku", Label: "腕力", column: "wanryoku"},
	{Key: "kyakuryoku", Label: "脚力", column: "kyakuryoku"},
	{Key: "love", Label: "LOVE", column: "love"},
	{Key: "omoshirosa", Label: "面白さ", column: "omoshirosa"},
}

func lookup(key string) (Key, bool) {
	for _, k := range Keys {
		if k.Key == key {
			return k, true
		}
	}
	return Key{}, false
}

// Entry is one ranked resident.
type Entry struct {
	Rank        int    `json:"rank"`
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
	Job         string `json:"job"`
	JobLevel    int    `json:"job_level"`
	Value       int64  `json:"value"`
}

// Result is one ranking with its heading metadata.
type Result struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	Unit    string  `json:"unit"`
	Entries []Entry `json:"entries"`
	// Self is the requesting player's own row even when out of the top N,
	// or nil when they are already listed / not ranked at all.
	Self *Entry `json:"self"`
}

// Service computes rankings.
type Service struct {
	pool *pgxpool.Pool
}

// New returns a ranking service backed by pool.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// ErrUnknownKey is returned for a ranking key that is not in Keys.
var ErrUnknownKey = fmt.Errorf("unknown ranking key")

// Rank computes one ranking, capped at limit rows. selfID (0 = none) asks for
// the caller's own row to be reported when it falls outside the top rows.
func (s *Service) Rank(ctx context.Context, key string, limit int, selfID int64) (*Result, error) {
	k, ok := lookup(key)
	if !ok {
		return nil, ErrUnknownKey
	}
	if limit <= 0 || limit > Limit {
		limit = Limit
	}

	// 全住民を順位付けしたCTEを組み、上位limit件と自分の行を別々に取り出す。
	// 同値は同順位(RANK)。レガシーは単純な連番だったが、並びが恣意的になるため
	// 同順位に改めている。
	valueExpr := "ps." + k.column // column は Keys の許可リスト由来のみ
	if k.column == "" {
		valueExpr = assetsExpr
	}
	dir := "DESC"
	if k.asc {
		dir = "ASC"
	}
	// 借金ワーストは総資産がマイナスの住民だけを対象にする。
	where := ""
	if k.Key == "debt" {
		where = "WHERE value < 0"
	}
	tie := ""
	if k.Key == "job_level" {
		tie = ", job_exp DESC" // 同レベルは経験値が多い順
	}

	sql := fmt.Sprintf(`
WITH base AS (
  SELECT p.id, p.display_name, ps.job, ps.job_level, ps.job_exp, (%[1]s) AS value
  FROM players p
  JOIN player_status ps ON ps.player_id = p.id
  LEFT JOIN player_loans pl ON pl.player_id = p.id
  WHERE p.deleted_at IS NULL
), ranked AS (
  SELECT id, display_name, job, job_level, value,
         RANK() OVER (ORDER BY value %[2]s%[3]s) AS rank,
         ROW_NUMBER() OVER (ORDER BY value %[2]s%[3]s, id ASC) AS rn
  FROM base %[4]s
)
SELECT rank, id, display_name, job, job_level, value FROM ranked
WHERE rn <= $1 OR id = $2
ORDER BY rn`, valueExpr, dir, tie, where)

	rows, err := s.pool.Query(ctx, sql, limit, selfID)
	if err != nil {
		return nil, fmt.Errorf("query ranking: %w", err)
	}
	defer rows.Close()

	res := &Result{Key: k.Key, Label: k.Label, Unit: k.Unit, Entries: []Entry{}}
	var self *Entry
	n := 0
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Rank, &e.ID, &e.DisplayName, &e.Job, &e.JobLevel, &e.Value); err != nil {
			return nil, fmt.Errorf("scan ranking: %w", err)
		}
		if n < limit {
			res.Entries = append(res.Entries, e)
			n++
			continue
		}
		// limit を超えて返るのは「自分の行」だけ(WHERE rn <= $1 OR id = $2)。
		row := e
		self = &row
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.Self = self
	return res, nil
}

// assetsExpr is the 総資産 formula: cash + 普通預金 + スーパー定期 - ローン残高.
// Balances come from the ledger (there are no money columns); the correlated
// sums read the same accounts as ledger.PlayerAccount / SavingsAccount /
// SuperSavingsAccount.
const assetsExpr = `
  COALESCE((SELECT SUM(e.delta) FROM ledger_entry e WHERE e.account = 'player:'        || p.id), 0)
+ COALESCE((SELECT SUM(e.delta) FROM ledger_entry e WHERE e.account = 'savings:'       || p.id), 0)
+ COALESCE((SELECT SUM(e.delta) FROM ledger_entry e WHERE e.account = 'super_savings:' || p.id), 0)
- COALESCE(pl.nitigaku * pl.kaisuu, 0)`
