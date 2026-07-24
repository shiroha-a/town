// Package content manages admin-authored, data-driven game content (items,
// jobs, and later events). All effect/condition JSON is validated through the
// effects engine before it is stored, and a simulator lets admins preview an
// effect's outcome before committing it.
package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/gametime"
	"github.com/shiroha-a/town/internal/jobrule"
	"github.com/shiroha-a/town/internal/settings"
)

// ValidationError is a client-fixable problem with admin input (e.g. a
// malformed effect). It maps to HTTP 400.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// Item is a content item definition. Effect is the item's use-time effect.
type Item struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Category    string          `json:"category"`
	Price       int64           `json:"price"`
	Effect      json.RawMessage `json:"effect"`
	Enabled     bool            `json:"enabled"`
	StockMaster *int            `json:"stock_master"` // 標準在庫数(NULL=無制限)
}

// Job is a content job definition (含む給与体系, design 17.5)。
type Job struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Requirements  json.RawMessage `json:"requirements"`
	Effect        json.RawMessage `json:"effect"`
	Salary        int64           `json:"salary"`
	PayInterval   int             `json:"pay_interval"`
	BonusRate     int             `json:"bonus_rate"`
	RaiseRate     int             `json:"raise_rate"`
	Rank          int             `json:"rank"`
	RequireMaster string          `json:"require_master"`
	BodyCost      int             `json:"body_cost"`
	NouCost       int             `json:"nou_cost"`
	Enabled       bool            `json:"enabled"`
}

// JobInput is the create/update payload for a job.
type JobInput struct {
	Name          string
	Requirements  []byte
	Effect        []byte
	Salary        int64
	PayInterval   int
	BonusRate     int
	RaiseRate     int
	Rank          int
	RequireMaster string // "" = 前提なし(NULL)
	BodyCost      int
	NouCost       int
	Enabled       bool
}

// jobCols is the shared column list / scan order for Job.
const jobCols = `id, name, requirements, effect, salary, pay_interval, bonus_rate, raise_rate, rank, COALESCE(require_master, ''), body_cost, nou_cost, enabled`

func scanJob(row pgx.Row, j *Job) error {
	return row.Scan(&j.ID, &j.Name, &j.Requirements, &j.Effect, &j.Salary, &j.PayInterval,
		&j.BonusRate, &j.RaiseRate, &j.Rank, &j.RequireMaster, &j.BodyCost, &j.NouCost, &j.Enabled)
}

// validateJobInput checks name/JSON and normalizes pay_interval/rank to >= 1.
func validateJobInput(in *JobInput) (req, eff []byte, master any, err error) {
	if in.Name == "" {
		return nil, nil, nil, &ValidationError{Message: "name is required"}
	}
	req = orEmptyArray(in.Requirements)
	eff = orEmptyArray(in.Effect)
	if _, e := effects.ParseConditions(req); e != nil {
		return nil, nil, nil, &ValidationError{Message: "requirements: " + e.Error()}
	}
	if _, e := effects.ParseEffect(eff); e != nil {
		return nil, nil, nil, &ValidationError{Message: "effect: " + e.Error()}
	}
	if in.PayInterval < 1 {
		in.PayInterval = 1
	}
	if in.Rank < 1 {
		in.Rank = 1
	}
	if in.RequireMaster != "" {
		master = in.RequireMaster
	}
	return req, eff, master, nil
}

// Service manages content rows.
type Service struct {
	pool            *pgxpool.Pool
	loc             *time.Location
	dayBoundaryHour int
	settings        *settings.Store
}

func New(pool *pgxpool.Pool, loc *time.Location, dayBoundaryHour int, st *settings.Store) *Service {
	if loc == nil {
		loc = time.UTC
	}
	return &Service{pool: pool, loc: loc, dayBoundaryHour: dayBoundaryHour, settings: st}
}

// dailyCountFor returns how many items to show today for a facility (0 = all).
func (s *Service) dailyCountFor(facility string) int {
	cfg := s.settings.Get()
	switch facility {
	case "":
		return cfg.DepartDailyCount
	case "syokudou":
		return cfg.SyokudouDailyCount
	case "hanbai":
		return cfg.HanbaiDailyCount
	default:
		return 0 // ジム/温泉などは日替わりなし
	}
}

func orEmptyArray(b []byte) []byte {
	if len(b) == 0 {
		return []byte("[]")
	}
	return b
}

// CreateItem validates the effect and inserts a new item.
func (s *Service) CreateItem(ctx context.Context, name, category string, price int64, effect []byte, stockMaster *int) (Item, error) {
	if name == "" {
		return Item{}, &ValidationError{Message: "name is required"}
	}
	if price < 0 {
		return Item{}, &ValidationError{Message: "price must be >= 0"}
	}
	effect = orEmptyArray(effect)
	if _, err := effects.ParseEffect(effect); err != nil {
		return Item{}, &ValidationError{Message: "effect: " + err.Error()}
	}
	var it Item
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO content_items (name, category, price, effect, stock_master)
		 VALUES ($1, $2, $3, $4::jsonb, $5)
		 RETURNING id, name, COALESCE(category, ''), price, effect, enabled, stock_master`,
		name, category, price, string(effect), stockMaster).
		Scan(&it.ID, &it.Name, &it.Category, &it.Price, &it.Effect, &it.Enabled, &it.StockMaster); err != nil {
		return Item{}, fmt.Errorf("insert item: %w", err)
	}
	return it, nil
}

// UpdateItem validates and updates an existing item (including enabled/無効化).
func (s *Service) UpdateItem(ctx context.Context, id int64, name, category string, price int64, effect []byte, enabled bool, stockMaster *int) (Item, error) {
	if name == "" {
		return Item{}, &ValidationError{Message: "name is required"}
	}
	if price < 0 {
		return Item{}, &ValidationError{Message: "price must be >= 0"}
	}
	effect = orEmptyArray(effect)
	if _, err := effects.ParseEffect(effect); err != nil {
		return Item{}, &ValidationError{Message: "effect: " + err.Error()}
	}
	var it Item
	err := s.pool.QueryRow(ctx,
		`UPDATE content_items SET name = $2, category = $3, price = $4, effect = $5::jsonb, enabled = $6, stock_master = $7
		 WHERE id = $1
		 RETURNING id, name, COALESCE(category, ''), price, effect, enabled, stock_master`,
		id, name, category, price, string(effect), enabled, stockMaster).
		Scan(&it.ID, &it.Name, &it.Category, &it.Price, &it.Effect, &it.Enabled, &it.StockMaster)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, &ValidationError{Message: "そのアイテムはありません。"}
	}
	if err != nil {
		return Item{}, fmt.Errorf("update item: %w", err)
	}
	return it, nil
}

// DeleteItem hard-deletes an item. Items owned by any player cannot be deleted
// (player_items has ON DELETE RESTRICT); disable them instead.
func (s *Service) DeleteItem(ctx context.Context, id int64) error {
	var owned bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM player_items WHERE item_id = $1)`, id).Scan(&owned); err != nil {
		return fmt.Errorf("check ownership: %w", err)
	}
	if owned {
		return &ValidationError{Message: "このアイテムは所有しているプレイヤーがいるため削除できません。無効化してください。"}
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM content_items WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return &ValidationError{Message: "そのアイテムはありません。"}
	}
	return nil
}

// ListItems returns all items ordered by id.
func (s *Service) ListItems(ctx context.Context) ([]Item, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, COALESCE(category, ''), price, effect, enabled, stock_master
		 FROM content_items ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Category, &it.Price, &it.Effect, &it.Enabled, &it.StockMaster); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// effectSummary parses an effect and returns its net money delta and non-zero
// per-parameter deltas (for displaying "rising parameters").
func effectSummary(effJSON []byte) (int64, map[string]int) {
	params := map[string]int{}
	eff, err := effects.ParseEffect(effJSON)
	if err != nil {
		return 0, params
	}
	for k, v := range eff.ParamSum() {
		if v != 0 {
			params[k] = v
		}
	}
	return eff.MoneySum(), params
}

// requirementSummary parses conditions into per-parameter minimums (for
// displaying "required parameters").
func requirementSummary(reqJSON []byte) map[string]int {
	c, err := effects.ParseConditions(reqJSON)
	if err != nil {
		return map[string]int{}
	}
	return c.ParamMins()
}

// ShopItem is the public view of a purchasable item, including the parameters
// its use raises (to mirror the legacy department-store table).
type ShopItem struct {
	ID              int64          `json:"id"`
	Name            string         `json:"name"`
	Category        string         `json:"category"`
	Price           int64          `json:"price"`
	Money           int64          `json:"money"`            // 使用時のお金増減
	Params          map[string]int `json:"params"`           // 使用時の上昇パラメータ
	IntervalMin     int            `json:"interval_min"`     // 使用間隔(分。0=なし)
	Durability      int            `json:"durability"`       // 1セットあたりの耐久(使用可能回数/日数)
	DurabilityUnit  string         `json:"durability_unit"`  // 'use'(回) or 'day'(日)
	PowerMultiplier int            `json:"power_multiplier"` // 温泉の回復速度倍率(0=温泉ではない)
	CalorieG        int            `json:"calorie_g"`        // 摂取カロリー(食べると体重+calorie_g g)
	Stock           int            `json:"stock"`            // 本日の店頭在庫(-1=無制限)
}

// ListShopItems returns the general department-store items (facility=”).
func (s *Service) ListShopItems(ctx context.Context) ([]ShopItem, error) {
	return s.listItems(ctx, "")
}

// ListFacilityMenu returns the menu items for a facility (e.g. 'syokudou').
func (s *Service) ListFacilityMenu(ctx context.Context, facility string) ([]ShopItem, error) {
	return s.listItems(ctx, facility)
}

func (s *Service) listItems(ctx context.Context, facility string) ([]ShopItem, error) {
	// 在庫: stock_master=NULLは無制限(-1)。それ以外は本日の店頭在庫(shop_daily_stockが
	// 未生成なら初期値 max(1, ceil(stock_master/2)))。zaikoAdjust=2はaction.consumeStockと一致。
	gameDate := gametime.Date(time.Now(), s.loc, s.dayBoundaryHour)
	adjust := s.settings.Get().StockAdjust
	if adjust <= 0 {
		adjust = 2
	}
	query := `SELECT ci.id, ci.name, COALESCE(ci.category, ''), ci.price, ci.effect,
	                 ci.use_interval_min, ci.durability, ci.durability_unit, ci.power_multiplier, ci.calorie_g,
	                 CASE WHEN ci.stock_master IS NULL THEN -1
	                      ELSE COALESCE(sds.remaining, GREATEST(1, CEIL(ci.stock_master::numeric / $3)::int))
	                 END AS stock
	          FROM content_items ci
	          LEFT JOIN shop_daily_stock sds
	                 ON sds.facility = ci.facility AND sds.item_id = ci.id AND sds.game_date = $2
	          WHERE ci.enabled AND ci.facility = $1`
	args := []any{facility, gameDate, adjust}
	// デパート/食堂は毎日一部だけを品揃えする(旧仕様)。
	if n := s.dailyCountFor(facility); n > 0 {
		query += ` AND ci.id IN (SELECT id FROM daily_shop_ids($1, $4, $5))`
		args = append(args, gametime.DateKey(time.Now(), s.loc, s.dayBoundaryHour), n)
	}
	query += ` ORDER BY ci.id`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()
	items := []ShopItem{}
	for rows.Next() {
		var (
			it      ShopItem
			effJSON []byte
		)
		if err := rows.Scan(&it.ID, &it.Name, &it.Category, &it.Price, &effJSON, &it.IntervalMin, &it.Durability, &it.DurabilityUnit, &it.PowerMultiplier, &it.CalorieG, &it.Stock); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		it.Money, it.Params = effectSummary(effJSON)
		items = append(items, it)
	}
	return items, rows.Err()
}

// JobOption is the public view of a job at the job office, including the
// parameters required to take it and those working it changes.
type JobOption struct {
	ID            int64          `json:"id"`
	Name          string         `json:"name"`
	Pay           int64          `json:"pay"`             // 基本給(1回)。salaryと同値
	Salary        int64          `json:"salary"`          // 基本給(1回)
	Rank          int            `json:"rank"`            // ランク(星)
	RequireMaster string         `json:"require_master"`  // 前提マスター職(なければ空)
	Requirements  map[string]int `json:"requirements"`    // 就くための必要パラメータ
	WorkParams    map[string]int `json:"work_params"`     // 働いたときの上昇/消費パラメータ
	EnergyCost    int            `json:"energy_cost"`     // 1回の身体パワー消費(ランク係数込み)
	NouEnergyCost int            `json:"nou_energy_cost"` // 1回の頭脳パワー消費(ランク係数込み)
	PayInterval   int            `json:"pay_interval"`    // 支払間隔(N回出勤ごと。1=日払い)
}

// ListSelectableJobs returns enabled jobs for the job-office UI.
func (s *Service) ListSelectableJobs(ctx context.Context) ([]JobOption, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, requirements, effect, salary, rank, require_master,
		        body_cost, nou_cost, pay_interval
		 FROM content_jobs WHERE enabled ORDER BY rank, id`)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	jobs := []JobOption{}
	for rows.Next() {
		var (
			opt               JobOption
			reqJSON, effJSON  []byte
			requireMaster     *string
			bodyCost, nouCost int
		)
		if err := rows.Scan(&opt.ID, &opt.Name, &reqJSON, &effJSON,
			&opt.Salary, &opt.Rank, &requireMaster,
			&bodyCost, &nouCost, &opt.PayInterval); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		opt.Pay = opt.Salary
		_, opt.WorkParams = effectSummary(effJSON)
		opt.Requirements = requirementSummary(reqJSON)
		// 表示用の1回消費量(実際のdo_workと同じ式: 基準+基準×ランク係数)。
		opt.EnergyCost = jobrule.PowerSpend(bodyCost, opt.Rank)
		opt.NouEnergyCost = jobrule.PowerSpend(nouCost, opt.Rank)
		if opt.PayInterval <= 0 {
			opt.PayInterval = 1
		}
		if requireMaster != nil {
			opt.RequireMaster = *requireMaster
		}
		jobs = append(jobs, opt)
	}
	return jobs, rows.Err()
}

// CreateJob validates the requirements and effect, then inserts a new job.
func (s *Service) CreateJob(ctx context.Context, in JobInput) (Job, error) {
	req, eff, master, err := validateJobInput(&in)
	if err != nil {
		return Job{}, err
	}
	var j Job
	if err := scanJob(s.pool.QueryRow(ctx,
		`INSERT INTO content_jobs
		   (name, requirements, effect, salary, pay_interval, bonus_rate, raise_rate, rank, require_master, body_cost, nou_cost, enabled)
		 VALUES ($1, $2::jsonb, $3::jsonb, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING `+jobCols,
		in.Name, string(req), string(eff), in.Salary, in.PayInterval, in.BonusRate, in.RaiseRate,
		in.Rank, master, in.BodyCost, in.NouCost, in.Enabled), &j); err != nil {
		return Job{}, fmt.Errorf("insert job: %w", err)
	}
	return j, nil
}

// UpdateJob validates and updates an existing job (含む給与体系・enabled)。
func (s *Service) UpdateJob(ctx context.Context, id int64, in JobInput) (Job, error) {
	req, eff, master, err := validateJobInput(&in)
	if err != nil {
		return Job{}, err
	}
	var j Job
	err = scanJob(s.pool.QueryRow(ctx,
		`UPDATE content_jobs SET
		   name = $2, requirements = $3::jsonb, effect = $4::jsonb, salary = $5, pay_interval = $6,
		   bonus_rate = $7, raise_rate = $8, rank = $9, require_master = $10, body_cost = $11, nou_cost = $12, enabled = $13
		 WHERE id = $1
		 RETURNING `+jobCols,
		id, in.Name, string(req), string(eff), in.Salary, in.PayInterval, in.BonusRate, in.RaiseRate,
		in.Rank, master, in.BodyCost, in.NouCost, in.Enabled), &j)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, &ValidationError{Message: "その職業はありません。"}
	}
	if err != nil {
		return Job{}, fmt.Errorf("update job: %w", err)
	}
	return j, nil
}

// DeleteJob hard-deletes a job. A job currently held by a player, or required as
// a master prerequisite by another job, cannot be deleted; disable it instead.
func (s *Service) DeleteJob(ctx context.Context, id int64) error {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM content_jobs WHERE id = $1`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ValidationError{Message: "その職業はありません。"}
	}
	if err != nil {
		return fmt.Errorf("read job: %w", err)
	}
	var inUse bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM player_status WHERE job = $1)`, name).Scan(&inUse); err != nil {
		return fmt.Errorf("check job in use: %w", err)
	}
	if inUse {
		return &ValidationError{Message: "この職業に就いているプレイヤーがいるため削除できません。無効化してください。"}
	}
	var isMaster bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM content_jobs WHERE require_master = $1)`, name).Scan(&isMaster); err != nil {
		return fmt.Errorf("check master ref: %w", err)
	}
	if isMaster {
		return &ValidationError{Message: "この職業を前提マスター職にしている職業があるため削除できません。無効化してください。"}
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM content_jobs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return &ValidationError{Message: "その職業はありません。"}
	}
	return nil
}

// ListJobs returns all jobs ordered by id.
func (s *Service) ListJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+jobCols+` FROM content_jobs ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		var j Job
		if err := scanJob(rows, &j); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// SimResult is the outcome of a dry-run effect simulation.
type SimResult struct {
	Plan     effects.Plan `json:"plan"`
	Warnings []string     `json:"warnings"`
}

// Simulate validates an effect and computes its result against a hypothetical
// state without applying anything. It surfaces economy-impact warnings so an
// admin can catch a money faucet before committing the content.
func Simulate(effectJSON []byte, state effects.State) (SimResult, error) {
	eff, err := effects.ParseEffect(orEmptyArray(effectJSON))
	if err != nil {
		return SimResult{}, &ValidationError{Message: "effect: " + err.Error()}
	}
	plan := eff.Plan(state)
	warnings := []string{}
	if plan.MoneyDelta > 0 {
		warnings = append(warnings,
			fmt.Sprintf("この効果は所持金を%d円増やします(faucet)。経済への影響に注意してください。", plan.MoneyDelta))
	}
	return SimResult{Plan: plan, Warnings: warnings}, nil
}
