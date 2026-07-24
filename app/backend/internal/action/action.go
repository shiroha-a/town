// Package action applies player actions (work, purchases, item use) through the
// data-driven effect engine. Every action is server-authoritative and atomic:
// condition check, status changes, money movement and the action log all commit
// together, and a client idempotency key makes retries safe.
package action

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/building"
	"github.com/shiroha-a/town/internal/casino"
	"github.com/shiroha-a/town/internal/cleague"
	"github.com/shiroha-a/town/internal/condition"
	"github.com/shiroha-a/town/internal/content"
	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/event"
	"github.com/shiroha-a/town/internal/gametime"
	"github.com/shiroha-a/town/internal/greeting"
	"github.com/shiroha-a/town/internal/jobrule"
	"github.com/shiroha-a/town/internal/keiba"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/news"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/rng"
	"github.com/shiroha-a/town/internal/settings"
	"github.com/shiroha-a/town/internal/stock"
)

// statusColumns whitelists the parameter -> column mapping. Effect params are
// validated by the effects package, and this map is the only place a param name
// reaches SQL, so column names are never attacker-controlled. Column names match
// the param names (romaji), so the map is identity over effects.AllParams.
var statusColumns = func() map[string]string {
	m := make(map[string]string, len(effects.AllParams))
	for _, p := range effects.AllParams {
		m[p] = p
	}
	return m
}()

// detailedParamMax is an overflow guard for the detailed parameters (国語/体力/…),
// which the legacy left effectively unbounded (青天井). The columns are BIGINT
// (migration 0036); this ceiling sits far below int64/BIGINT max so that
// clamp arithmetic and the derived power-max computation cannot overflow, while
// staying astronomically beyond any reachable gameplay value. It is not a
// gameplay cap. energy/nou_energy use their own *_max columns.
const detailedParamMax = 1_000_000_000_000_000 // 1e15

// satietyMax is the full 満腹度. Eating fills to this; food is blocked at full.
const satietyMax = 100

// fillSatiety sets 満腹度 to full and restarts its decay clock.
func fillSatiety(ctx context.Context, tx pgx.Tx, playerID int64) error {
	if _, err := tx.Exec(ctx,
		`UPDATE player_status SET satiety = $1, satiety_updated_at = now(), updated_at = now()
		 WHERE player_id = $2`, satietyMax, playerID); err != nil {
		return fmt.Errorf("fill satiety: %w", err)
	}
	return nil
}

// addWeightFromItem adds the item's calorie_g to the player's weight in grams
// (design 17.3, 飲食で体重増)。calorie_gはcontent_itemsから直接読み、0で下限クリップする。
func addWeightFromItem(ctx context.Context, tx pgx.Tx, playerID, itemID int64) error {
	if _, err := tx.Exec(ctx,
		`UPDATE player_status ps
		 SET weight_g = GREATEST(0, ps.weight_g + ci.calorie_g), updated_at = now()
		 FROM content_items ci
		 WHERE ps.player_id = $1 AND ci.id = $2`, playerID, itemID); err != nil {
		return fmt.Errorf("add weight: %w", err)
	}
	return nil
}

// ConditionError means the player did not meet an action's requirements.
type ConditionError struct{ Message string }

func (e *ConditionError) Error() string { return e.Message }

// ErrItemNotFound is returned when an item does not exist or is disabled.
var ErrItemNotFound = errors.New("item not found")

// zaikoAdjust divides the master stock to derive the shop-front daily stock
// (旧 zaiko_tyousetuti=2). remaining = max(1, ceil(stock_master / zaikoAdjust)).
const zaikoAdjust = 2

// Service applies actions.
type Service struct {
	pool            *pgxpool.Pool
	ledger          *ledger.Repo
	players         *player.Service
	rng             *rng.Rand
	loc             *time.Location
	dayBoundaryHour int
	settings        *settings.Store
}

func New(pool *pgxpool.Pool, led *ledger.Repo, players *player.Service, r *rng.Rand, loc *time.Location, dayBoundaryHour int, st *settings.Store) *Service {
	if loc == nil {
		loc = time.UTC
	}
	return &Service{
		pool: pool, ledger: led, players: players, rng: r, loc: loc,
		dayBoundaryHour: dayBoundaryHour, settings: st,
	}
}

// dailyMenuCond appends the "in today's rotation" filter to a load query when the
// facility rotates daily (depart/syokudou). Returns the extra SQL and args.
func (s *Service) dailyMenuCond(facility string, nextArg int) (string, []any) {
	cfg := s.settings.Get()
	var n int
	switch facility {
	case "":
		n = cfg.DepartDailyCount
	case "syokudou":
		n = cfg.SyokudouDailyCount
	case "hanbai":
		n = cfg.HanbaiDailyCount
	}
	if n <= 0 {
		return "", nil
	}
	cond := fmt.Sprintf(" AND id IN (SELECT id FROM daily_shop_ids($%d, $%d, $%d))", nextArg, nextArg+1, nextArg+2)
	return cond, []any{facility, gametime.DateKey(time.Now(), s.loc, s.dayBoundaryHour), n}
}

// gameDate returns the current game day (日境界 AM5:00 等) for stock partitioning.
func (s *Service) gameDate(now time.Time) time.Time {
	return gametime.Date(now, s.loc, s.dayBoundaryHour)
}

// consumeStock lazily creates today's shop-front stock for an item and decrements
// it by qty. Items with stock_master = NULL (services etc.) are treated as
// unlimited and skipped. Insufficient stock yields a ConditionError (売り切れ).
func (s *Service) consumeStock(ctx context.Context, tx pgx.Tx, facility string, itemID int64, qty int) error {
	var stockMaster *int
	if err := tx.QueryRow(ctx,
		`SELECT stock_master FROM content_items WHERE id = $1`, itemID).Scan(&stockMaster); err != nil {
		return fmt.Errorf("read stock_master: %w", err)
	}
	if stockMaster == nil {
		return nil // 在庫無制限
	}
	date := s.gameDate(time.Now())
	adjust := s.settings.Get().StockAdjust
	if adjust <= 0 {
		adjust = zaikoAdjust
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO shop_daily_stock (facility, item_id, game_date, remaining)
		 VALUES ($1, $2, $3, GREATEST(1, CEIL($4::numeric / $5)::int))
		 ON CONFLICT (facility, item_id, game_date) DO NOTHING`,
		facility, itemID, date, *stockMaster, adjust); err != nil {
		return fmt.Errorf("init stock: %w", err)
	}
	tag, err := tx.Exec(ctx,
		`UPDATE shop_daily_stock SET remaining = remaining - $4
		 WHERE facility = $1 AND item_id = $2 AND game_date = $3 AND remaining >= $4`,
		facility, itemID, date, qty)
	if err != nil {
		return fmt.Errorf("consume stock: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return &ConditionError{Message: "売り切れです。"}
	}
	return nil
}

// runAction wraps the common per-action transaction: it claims the idempotency
// key first (a duplicate short-circuits to a no-op), reads the player state,
// then runs body. On success it returns the refreshed player aggregate.
func (s *Service) runAction(ctx context.Context, playerID int64, actionType, idemKey string, body func(ctx context.Context, tx pgx.Tx, state effects.State) error) (*player.Player, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		duplicate, err := s.claimIdempotency(ctx, tx, playerID, actionType, idemKey)
		if err != nil {
			return err
		}
		if duplicate {
			return nil // 既に実行済み: no-op
		}
		state, err := s.readState(ctx, tx, playerID)
		if err != nil {
			return err
		}
		return body(ctx, tx, state)
	})
	if err != nil {
		return nil, err
	}
	return s.players.Get(ctx, playerID)
}

// WorkResult summarizes a single work action for the result screen (design 17.5).
type WorkResult struct {
	ExpGained   int      // 今回の経験値増減
	NewLevel    int      // 到達レベル
	LeveledUp   bool     // レベルが上がったか(昇給発生)
	ThisSalary  int64    // 昇給後の給料(1回あたり)
	Pay         int64    // 今回支給された給料(支払間隔到達時のみ>0)
	PayEvery    int      // 支払間隔(N回出勤ごと)
	Bonus       int64    // レベルアップ時ボーナス
	WorkBonus   int64    // 消費に見合う労働ボーナス(今回の給料に含まれる)
	WeightLossG int      // 今回の労働で減った体重(グラム)
	Mastered    []string // 今回新たにマスターした職業
}

// DoWork works the player's current job (design 17.5 do_work). Students cannot
// work. Working is gated by the work interval, ability requirements, power floor
// and BMI condition; it then earns condition-based experience (which may level
// the player up), pays salary on the pay interval, grants a level-up bonus,
// records job mastery at level 15, and spends power and weight. It returns a
// WorkResult so the UI can show the legacy-style salary/raise/bonus messages.
func (s *Service) DoWork(ctx context.Context, playerID int64, idempotencyKey string) (*player.Player, *WorkResult, error) {
	job, err := s.currentJob(ctx, playerID)
	if err != nil {
		return nil, nil, err
	}
	if job == "学生" {
		return nil, nil, &ConditionError{Message: "学生は働けません。職業安定所で仕事を探してください。"}
	}
	econ, err := s.loadJobEconomy(ctx, job)
	if err != nil {
		return nil, nil, err
	}
	var result WorkResult
	p, err := s.runAction(ctx, playerID, "work", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		// 1. 就労間隔(前回出勤からwork_interval_min分は再出勤不可)。
		var nextAt *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT next_available_at FROM player_facility_cooldowns WHERE player_id = $1 AND facility = 'work'`,
			playerID).Scan(&nextAt); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read work cooldown: %w", err)
		}
		if !s.settings.Get().DebugNoCooldown && nextAt != nil && time.Now().Before(*nextAt) {
			remain := int(time.Until(*nextAt).Minutes()) + 1
			return &ConditionError{Message: fmt.Sprintf("まだ働けません。あと約%d分お待ちください。", remain)}
		}
		// 2. 能力値必要条件・パワー下限。
		if ok, failed := econ.conds.Check(state); !ok {
			return &ConditionError{Message: conditionMessage(failed)}
		}
		if state.Params["energy"].Value < econ.bodyCost {
			return &ConditionError{Message: "身体パワーが足りません。"}
		}
		if state.Params["nou_energy"].Value < econ.nouCost {
			return &ConditionError{Message: "頭脳パワーが足りません。"}
		}
		// 2b. 体格(BMI)条件。身長は求職時に判定するためここでは体型のみ。
		var heightCm, weightG, diseaseIndex int
		if err := tx.QueryRow(ctx,
			`SELECT height_cm, weight_g, disease_index FROM player_status WHERE player_id = $1`,
			playerID).Scan(&heightCm, &weightG, &diseaseIndex); err != nil {
			return fmt.Errorf("read body: %w", err)
		}
		bmi := condition.BMI(heightCm, weightG)
		if (econ.bmiMin != nil && bmi < *econ.bmiMin) || (econ.bmiMax != nil && bmi > *econ.bmiMax) {
			return &ConditionError{Message: "体型がこの仕事の条件に合いません。"}
		}
		// 3. コンディション→経験値基礎値(重病・最悪は就労不可)。
		cond := condition.Compute(condition.Input{
			Energy: state.Params["energy"].Value, EnergyMax: state.Params["energy"].Max,
			NouEnergy: state.Params["nou_energy"].Value, NouEnergyMax: state.Params["nou_energy"].Max,
			Kenkou: state.Params["kenkou"].Value, Satiety: state.Params["satiety"].Value,
			BMI: bmi, DiseaseIndex: diseaseIndex,
		})
		base, canWork := condition.WorkExpBase(cond)
		if !canWork {
			return &ConditionError{Message: "今日は体調が悪すぎて働けません。病院で治療しましょう。"}
		}
		// 4. 経験値 = 基礎値 + rand(1..5)(RNGシード可能)。
		randed := base + 1 + s.rng.IntN(5)
		// 5. exp加算(下限0)とレベル(=floor(exp/100))・レベルアップ判定。
		var oldExp, kaisuu int
		var mastered []string
		if err := tx.QueryRow(ctx,
			`SELECT job_exp, job_kaisuu, mastered_jobs FROM player_status WHERE player_id = $1`,
			playerID).Scan(&oldExp, &kaisuu, &mastered); err != nil {
			return fmt.Errorf("read job progress: %w", err)
		}
		oldLevel := oldExp / 100
		newExp := oldExp + randed
		if newExp < 0 {
			newExp = 0
		}
		newLevel := newExp / 100
		leveledUp := newLevel > oldLevel
		// パワー消費 = 基準 + 基準×ランク係数(隠しランクで重みが変わる。青天井の
		// パラメータ値には依存しない。基準以下にはならない)。
		energySpend := jobrule.PowerSpend(econ.bodyCost, econ.rank)
		nouSpend := jobrule.PowerSpend(econ.nouCost, econ.rank)
		// 6. 今回の給料(レベル×昇給係数で増額)+ 消費した合計パワーに見合う労働ボーナス。
		workBonus := int64(energySpend+nouSpend) * jobrule.PayPerPower
		thisSalary := econ.salary + econ.salary*int64(newLevel)*int64(econ.raiseRate)/100 + workBonus
		// 7. 勤務回数を進め、支払間隔ごとにまとめて支給。
		payInterval := econ.payInterval
		if payInterval <= 0 {
			payInterval = 1
		}
		kaisuu++
		var pay int64
		if kaisuu%payInterval == 0 {
			pay = thisSalary * int64(payInterval)
		}
		// 8. レベルアップ時ボーナス(今回給料 × bonus_rate%)。
		var bonus int64
		if leveledUp {
			bonus = thisSalary * int64(econ.bonusRate) / 100
		}
		// 給料・ボーナスは台帳経由(system:payroll_source が原資、zero-sum維持)。
		if total := pay + bonus; total > 0 {
			if err := s.ledger.PostTx(ctx, tx, "work", "", []ledger.Entry{
				{Account: ledger.SystemAccount("payroll_source"), Delta: -total},
				{Account: ledger.PlayerAccount(playerID), Delta: total},
			}); err != nil {
				return fmt.Errorf("pay salary: %w", err)
			}
		}
		// 9. レベル15到達&未マスターならマスター認定。
		masteredNow := newLevel >= 15 && !contains(mastered, job)
		// 11. 体重減少 = body_cost×10。
		if _, err := tx.Exec(ctx, `
			UPDATE player_status SET
				job_exp = $1, job_level = $2, job_kaisuu = $3,
				energy = GREATEST(0, energy - $4),
				nou_energy = GREATEST(0, nou_energy - $5),
				weight_g = GREATEST(0, weight_g - $6),
				mastered_jobs = CASE WHEN $7 THEN array_append(mastered_jobs, $8) ELSE mastered_jobs END,
				updated_at = now()
			WHERE player_id = $9`,
			newExp, newLevel, kaisuu, energySpend, nouSpend, econ.bodyCost*10,
			masteredNow, job, playerID); err != nil {
			return fmt.Errorf("apply work: %w", err)
		}
		// 就労クールタイムを設定。
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_facility_cooldowns (player_id, facility, next_available_at)
			 VALUES ($1, 'work', now() + make_interval(mins => $2))
			 ON CONFLICT (player_id, facility)
			 DO UPDATE SET next_available_at = now() + make_interval(mins => $2)`,
			playerID, s.settings.Get().WorkIntervalMin); err != nil {
			return fmt.Errorf("set work cooldown: %w", err)
		}
		// 結果画面用にサマリを控える。
		result = WorkResult{
			ExpGained: randed, NewLevel: newLevel, LeveledUp: leveledUp,
			ThisSalary: thisSalary, Pay: pay, PayEvery: payInterval, Bonus: bonus,
			WorkBonus: workBonus, WeightLossG: econ.bodyCost * 10,
		}
		if masteredNow {
			result.Mastered = []string{job}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return p, &result, nil
}

// contains reports whether v is in s.
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// DoChangeJob changes the player's job at the job office (職業安定所). The target
// job's take-requirements are checked before switching; the job level resets.
func (s *Service) DoChangeJob(ctx context.Context, playerID int64, jobName, idempotencyKey string) (*player.Player, error) {
	if jobName == "学生" {
		return nil, &ConditionError{Message: "その仕事には就けません。"}
	}
	econ, err := s.loadJobEconomy(ctx, jobName)
	if err != nil {
		return nil, &ConditionError{Message: "そのような仕事はありません。"}
	}
	return s.runAction(ctx, playerID, "job_change", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if ok, failed := econ.conds.Check(state); !ok {
			return &ConditionError{Message: conditionMessage(failed)}
		}
		var oldJob string
		var mastered []string
		var heightCm int
		if err := tx.QueryRow(ctx,
			`SELECT job, mastered_jobs, height_cm FROM player_status WHERE player_id = $1`,
			playerID).Scan(&oldJob, &mastered, &heightCm); err != nil {
			return fmt.Errorf("read job: %w", err)
		}
		// 身長条件(求職時)。
		if econ.heightMin != nil && heightCm < *econ.heightMin {
			return &ConditionError{Message: "身長がこの仕事の条件に足りません。"}
		}
		// 前提マスター職の条件。
		if econ.requireMaster != nil && *econ.requireMaster != "" && !contains(mastered, *econ.requireMaster) {
			return &ConditionError{Message: fmt.Sprintf("この仕事に就くには「%s」をマスターする必要があります。", *econ.requireMaster)}
		}
		// 転職: レベル・経験値・勤務回数をリセット。
		if _, err := tx.Exec(ctx,
			`UPDATE player_status SET job = $1, job_level = 0, job_exp = 0, job_kaisuu = 0, updated_at = now() WHERE player_id = $2`,
			jobName, playerID); err != nil {
			return fmt.Errorf("change job: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO status_history (player_id, field, old_value, new_value, reason)
			 VALUES ($1, 'job', $2, $3, 'job_change')`, playerID, oldJob, jobName); err != nil {
			return fmt.Errorf("insert history: %w", err)
		}
		// 街のニュース(レガシー basic.cgi の news_kiroku("就職", ...))。
		name, err := news.ActorName(ctx, tx, playerID)
		if err != nil {
			return err
		}
		return news.RecordFor(ctx, tx, news.KindJob, playerID, name,
			fmt.Sprintf("%sさんが、%s から %s になりました。", name, oldJob, jobName), nil, true)
	})
}

func (s *Service) currentJob(ctx context.Context, playerID int64) (string, error) {
	var job string
	err := s.pool.QueryRow(ctx,
		`SELECT job FROM player_status WHERE player_id = $1`, playerID).Scan(&job)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", player.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read job: %w", err)
	}
	return job, nil
}

// DoDeposit moves cash into the player's bank savings.
func (s *Service) DoDeposit(ctx context.Context, playerID, amount int64, idempotencyKey string) (*player.Player, error) {
	if amount <= 0 {
		return nil, &ConditionError{Message: "金額が正しくありません。"}
	}
	return s.runAction(ctx, playerID, "deposit", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if state.Money < amount {
			return &ConditionError{Message: "お金が足りません。"}
		}
		// 冪等性は action_log(player_id, key) が保証するため台帳refは不要。
		return s.ledger.PostTx(ctx, tx, "deposit", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -amount},
			{Account: ledger.SavingsAccount(playerID), Delta: amount},
		})
	})
}

// DoWithdraw moves savings back into cash.
func (s *Service) DoWithdraw(ctx context.Context, playerID, amount int64, idempotencyKey string) (*player.Player, error) {
	if amount <= 0 {
		return nil, &ConditionError{Message: "金額が正しくありません。"}
	}
	return s.runAction(ctx, playerID, "withdraw", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var savings int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
			ledger.SavingsAccount(playerID)).Scan(&savings); err != nil {
			return fmt.Errorf("read savings: %w", err)
		}
		if savings < amount {
			return &ConditionError{Message: "貯金が足りません。"}
		}
		return s.ledger.PostTx(ctx, tx, "withdraw", "", []ledger.Entry{
			{Account: ledger.SavingsAccount(playerID), Delta: -amount},
			{Account: ledger.PlayerAccount(playerID), Delta: amount},
		})
	})
}

// transferLimit caps a single bank transfer; the excess is donated away (寄付),
// matching the legacy 振り込み. It is also the per-day cap per recipient.
const transferLimit = 1_000_000

// DoTransfer sends money from the player's savings to another member's savings,
// the recipient identified by display name. Amount above transferLimit is
// donated (removed from circulation). Students/day-laborers cannot send, self-
// transfer is rejected, and the daily total to one recipient is capped.
func (s *Service) DoTransfer(ctx context.Context, fromID int64, toName string, amount int64, idempotencyKey string) (*player.Player, error) {
	if amount <= 0 {
		return nil, &ConditionError{Message: "0円、マイナスの金額は振り込めません。"}
	}
	toName = strings.TrimSpace(toName)
	if toName == "" {
		return nil, &ConditionError{Message: "振込先の名前を入力してください。"}
	}
	return s.runAction(ctx, fromID, "transfer", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var job string
		if err := tx.QueryRow(ctx, `SELECT job FROM player_status WHERE player_id = $1`, fromID).Scan(&job); err != nil {
			return fmt.Errorf("read job: %w", err)
		}
		if job == "学生" || job == "日払いバイト" {
			return &ConditionError{Message: "学生・日払いバイト中は送金できません。"}
		}
		// 相手をメンバー名で逆引き。同名が複数いる場合は特定できないため断る。
		var cnt, toID int64
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*), COALESCE(MIN(id), 0) FROM players WHERE display_name = $1`,
			toName).Scan(&cnt, &toID); err != nil {
			return fmt.Errorf("lookup recipient: %w", err)
		}
		if cnt == 0 {
			return &ConditionError{Message: "その名前の参加者が見つかりません。"}
		}
		if cnt > 1 {
			return &ConditionError{Message: "同じ名前の参加者が複数います。振込できません。"}
		}
		if toID == fromID {
			return &ConditionError{Message: "自分宛に振り込むことはできません。"}
		}
		var savings int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
			ledger.SavingsAccount(fromID)).Scan(&savings); err != nil {
			return fmt.Errorf("read savings: %w", err)
		}
		if savings < amount {
			return &ConditionError{Message: "普通口座のお金が足りません。"}
		}
		// 上限超過分は寄付として自口座から引かれ、相手には届かない。
		sent, donation := amount, int64(0)
		if sent > transferLimit {
			donation = sent - transferLimit
			sent = transferLimit
		}
		// 同一相手への当日送金合計(相手に届いた額)が上限を超えないか。
		dayStart := gametime.Date(time.Now(), s.loc, s.dayBoundaryHour).Add(time.Duration(s.dayBoundaryHour) * time.Hour)
		var todaySum int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(amount), 0) FROM transfer_log WHERE from_id = $1 AND to_id = $2 AND created_at >= $3`,
			fromID, toID, dayStart).Scan(&todaySum); err != nil {
			return err
		}
		if todaySum+sent > transferLimit {
			return &ConditionError{Message: fmt.Sprintf("今日この相手への送金は合計%d円までです。", transferLimit)}
		}
		if err := s.ledger.PostTx(ctx, tx, "transfer", "", []ledger.Entry{
			{Account: ledger.SavingsAccount(fromID), Delta: -sent},
			{Account: ledger.SavingsAccount(toID), Delta: sent},
		}); err != nil {
			return fmt.Errorf("transfer: %w", err)
		}
		if donation > 0 {
			if err := s.ledger.PostTx(ctx, tx, "transfer_donation", "", []ledger.Entry{
				{Account: ledger.SavingsAccount(fromID), Delta: -donation},
				{Account: ledger.SystemAccount("donation"), Delta: donation},
			}); err != nil {
				return fmt.Errorf("donation: %w", err)
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO transfer_log (from_id, to_id, amount) VALUES ($1, $2, $3)`,
			fromID, toID, sent); err != nil {
			return err
		}
		return nil
	})
}

// superUnit is the deposit/cancel unit for the super time-deposit (100万円).
const superUnit = 1_000_000

// DoSuperDeposit moves cash into the super time-deposit in 100万円 units. It
// earns 1%/day interest, twice the ordinary savings rate.
func (s *Service) DoSuperDeposit(ctx context.Context, playerID, amount int64, idempotencyKey string) (*player.Player, error) {
	if amount <= 0 || amount%superUnit != 0 {
		return nil, &ConditionError{Message: "スーパー定期は100万円単位で預け入れてください。"}
	}
	return s.runAction(ctx, playerID, "super_deposit", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if state.Money < amount {
			return &ConditionError{Message: "お金が足りません。"}
		}
		return s.ledger.PostTx(ctx, tx, "super_deposit", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -amount},
			{Account: ledger.SuperSavingsAccount(playerID), Delta: amount},
		})
	})
}

// DoSuperCancel returns super time-deposit money to cash. Withdrawal is only by
// cancellation: the full balance (all=true) or a 100万円-unit amount. There is
// no early-cancellation penalty (legacy behavior).
func (s *Service) DoSuperCancel(ctx context.Context, playerID, amount int64, all bool, idempotencyKey string) (*player.Player, error) {
	if !all && (amount <= 0 || amount%superUnit != 0) {
		return nil, &ConditionError{Message: "スーパー定期の解約は100万円単位です。"}
	}
	return s.runAction(ctx, playerID, "super_cancel", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var super int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
			ledger.SuperSavingsAccount(playerID)).Scan(&super); err != nil {
			return fmt.Errorf("read super savings: %w", err)
		}
		amt := amount
		if all {
			amt = super
		}
		if amt <= 0 {
			return &ConditionError{Message: "解約できる定期預金がありません。"}
		}
		if super < amt {
			return &ConditionError{Message: "そんなに定期預金がありません。"}
		}
		return s.ledger.PostTx(ctx, tx, "super_cancel", "", []ledger.Entry{
			{Account: ledger.SuperSavingsAccount(playerID), Delta: -amt},
			{Account: ledger.PlayerAccount(playerID), Delta: amt},
		})
	})
}

// loanMaxLimit caps the assessed borrowable amount (legacy 融資上限 3000万).
const loanMaxLimit = 30_000_000

// loanPlans lists the selectable repayment counts and their interest rates
// (legacy: 12回5% … 120回14%, +1% per 12 installments).
var loanPlans = []struct {
	Count int
	Rate  int
}{
	{12, 5}, {24, 6}, {36, 7}, {48, 8}, {60, 9},
	{72, 10}, {84, 11}, {96, 12}, {108, 13}, {120, 14},
}

// loanDaily is the daily repayment for a principal over count installments at
// rate percent: floor((principal + principal*rate/100) / count).
func loanDaily(principal int64, rate, count int) int64 {
	total := principal + principal*int64(rate)/100
	return total / int64(count)
}

// loanLimit assesses the borrowable amount (1万円未満切り捨て、上限3000万)。
func loanLimit(salary, jobExp, jobKaisuu, savings, super int64) int64 {
	limit := int64(float64(salary)*(float64(jobExp)/50.0)*(float64(jobKaisuu)/50.0) +
		float64(savings)*2 + float64(super)*2.5)
	limit -= limit % 10000
	if limit > loanMaxLimit {
		limit = loanMaxLimit
	}
	if limit < 0 {
		limit = 0
	}
	return limit
}

// LoanPlanQuote is one repayment option in a loan quote.
type LoanPlanQuote struct {
	Count int   `json:"count"`
	Rate  int   `json:"rate"`
	Daily int64 `json:"daily"`
	Total int64 `json:"total"`
}

// LoanQuote is the borrowing assessment: the limit and per-plan daily payments.
type LoanQuote struct {
	Limit   int64           `json:"limit"`
	HasLoan bool            `json:"has_loan"`
	Plans   []LoanPlanQuote `json:"plans"`
}

// LoanQuote assesses the borrowable amount from salary, job experience/attendance
// and savings/super deposit, and the daily payment of each repayment plan.
func (s *Service) LoanQuote(ctx context.Context, playerID int64) (*LoanQuote, error) {
	var q LoanQuote
	var cnt int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM player_loans WHERE player_id = $1`, playerID).Scan(&cnt); err != nil {
		return nil, err
	}
	q.HasLoan = cnt > 0

	salary, jobExp, jobKaisuu, savings, super, err := s.loanFactors(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}
	q.Limit = loanLimit(salary, jobExp, jobKaisuu, savings, super)
	for _, pl := range loanPlans {
		d := loanDaily(q.Limit, pl.Rate, pl.Count)
		q.Plans = append(q.Plans, LoanPlanQuote{Count: pl.Count, Rate: pl.Rate, Daily: d, Total: d * int64(pl.Count)})
	}
	return &q, nil
}

// queryRower is satisfied by both *pgxpool.Pool and pgx.Tx.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// loanFactors gathers the inputs to the borrowing assessment.
func (s *Service) loanFactors(ctx context.Context, q queryRower, playerID int64) (salary, jobExp, jobKaisuu, savings, super int64, err error) {
	var job string
	if err = q.QueryRow(ctx,
		`SELECT job, job_exp, job_kaisuu FROM player_status WHERE player_id = $1`,
		playerID).Scan(&job, &jobExp, &jobKaisuu); err != nil {
		return
	}
	// 現職の基本給。学生など content_jobs に無い職は給料0(査定は貯蓄依存になる)。
	_ = q.QueryRow(ctx, `SELECT salary FROM content_jobs WHERE name = $1`, job).Scan(&salary)
	if savings, err = s.ledger.Balance(ctx, ledger.SavingsAccount(playerID)); err != nil {
		return
	}
	super, err = s.ledger.Balance(ctx, ledger.SuperSavingsAccount(playerID))
	return
}

// DoLoanBorrow borrows the assessed limit over the chosen repayment count. The
// funds go to the savings (普通口座). Re-borrowing is blocked until repaid.
func (s *Service) DoLoanBorrow(ctx context.Context, playerID int64, count int, idempotencyKey string) (*player.Player, error) {
	rate := 0
	for _, pl := range loanPlans {
		if pl.Count == count {
			rate = pl.Rate
		}
	}
	if rate == 0 {
		return nil, &ConditionError{Message: "返済回数の指定が正しくありません。"}
	}
	return s.runAction(ctx, playerID, "loan_borrow", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var cnt int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM player_loans WHERE player_id = $1`, playerID).Scan(&cnt); err != nil {
			return err
		}
		if cnt > 0 {
			return &ConditionError{Message: "ローンを完済するまで新しい融資はできません。"}
		}
		salary, jobExp, jobKaisuu, savings, super, err := s.loanFactors(ctx, tx, playerID)
		if err != nil {
			return err
		}
		limit := loanLimit(salary, jobExp, jobKaisuu, savings, super)
		if limit <= 0 {
			return &ConditionError{Message: "現在借り入れできる金額がありません。"}
		}
		daily := loanDaily(limit, rate, count)
		// 融資額を普通口座へ入金(原資は system:loan_faucet)。
		if err := s.ledger.PostTx(ctx, tx, "loan_borrow", "", []ledger.Entry{
			{Account: ledger.SystemAccount("loan_faucet"), Delta: -limit},
			{Account: ledger.SavingsAccount(playerID), Delta: limit},
		}); err != nil {
			return fmt.Errorf("loan borrow: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_loans (player_id, nitigaku, kaisuu) VALUES ($1, $2, $3)`,
			playerID, daily, count); err != nil {
			return err
		}
		return nil
	})
}

// DoLoanRepay repays the whole outstanding loan at once from savings.
func (s *Service) DoLoanRepay(ctx context.Context, playerID int64, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "loan_repay_full", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var nitigaku int64
		var kaisuu int
		err := tx.QueryRow(ctx, `SELECT nitigaku, kaisuu FROM player_loans WHERE player_id = $1`, playerID).Scan(&nitigaku, &kaisuu)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "返済中のローンはありません。"}
		}
		if err != nil {
			return err
		}
		remaining := nitigaku * int64(kaisuu)
		var savings int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`, ledger.SavingsAccount(playerID)).Scan(&savings); err != nil {
			return err
		}
		if savings < remaining {
			return &ConditionError{Message: "普通口座に十分な預金がありません。"}
		}
		if err := s.ledger.PostTx(ctx, tx, "loan_repay_full", "", []ledger.Entry{
			{Account: ledger.SavingsAccount(playerID), Delta: -remaining},
			{Account: ledger.SystemAccount("loan_sink"), Delta: remaining},
		}); err != nil {
			return fmt.Errorf("loan repay: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM player_loans WHERE player_id = $1`, playerID); err != nil {
			return err
		}
		return nil
	})
}

// gameLimits caps how often each game can be played (legacy 開催間隔・回数制限)。
// daily=0 means no daily cap, interval=0 means no per-play cooldown.
var gameLimits = map[string]struct {
	daily    int
	interval time.Duration
}{
	"saikoro":  {interval: time.Minute},
	"slot":     {interval: 3 * time.Second},
	"loto":     {interval: time.Minute},
	"kuji":     {interval: 30 * time.Second},
	"donuts":   {daily: 3},
	"omikuji":  {daily: 10, interval: time.Minute},
	"otakara":  {interval: time.Minute},
	"fukubiki": {daily: 1},
}

// checkGameLimit enforces the per-game daily cap and cooldown from game_plays.
func (s *Service) checkGameLimit(ctx context.Context, tx pgx.Tx, playerID int64, game string) error {
	lim, ok := gameLimits[game]
	if !ok {
		return nil
	}
	if lim.daily > 0 {
		dayStart := gametime.Date(time.Now(), s.loc, s.dayBoundaryHour).Add(time.Duration(s.dayBoundaryHour) * time.Hour)
		var cnt int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM game_plays WHERE game = $1 AND player_id = $2 AND created_at >= $3`,
			game, playerID, dayStart).Scan(&cnt); err != nil {
			return err
		}
		if cnt >= lim.daily {
			return &ConditionError{Message: fmt.Sprintf("このゲームは1日%d回までです。", lim.daily)}
		}
	}
	if lim.interval > 0 {
		var last *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT MAX(created_at) FROM game_plays WHERE game = $1 AND player_id = $2`,
			game, playerID).Scan(&last); err != nil {
			return err
		}
		if last != nil {
			if elapsed := time.Since(*last); elapsed < lim.interval {
				wait := int((lim.interval - elapsed).Seconds()) + 1
				return &ConditionError{Message: fmt.Sprintf("まだ遊べません。あと%d秒お待ちください。", wait)}
			}
		}
	}
	return nil
}

// scratchDef parameters a scratch-card variant (scratch: 3x2/当り<=3,
// sukuratti: 3x3/当り<=4). Both give +Prize per winning cell opened and a +Bonus
// when all OpenMax opened cells are winners.
type scratchDef struct {
	Cells    int   // 総セル数(6 or 9)
	Cols     int   // 表示列数
	AtariMax int   // この値以下が当たり
	OpenMax  int   // 1カードで開けられる数
	Cards    int   // 1日の枚数
	Prize    int64 // 当りセル1つの賞金
	Bonus    int64 // 全開封が当りのボーナス
}

var scratchDefs = map[string]scratchDef{
	"scratch":   {Cells: 6, Cols: 3, AtariMax: 3, OpenMax: 3, Cards: 5, Prize: 100000, Bonus: 300000},
	"sukuratti": {Cells: 9, Cols: 3, AtariMax: 4, OpenMax: 3, Cards: 5, Prize: 100000, Bonus: 300000},
}

// genScratchCells returns a random permutation of 1..n (randcheck相当)。
func genScratchCells(r *rng.Rand, n int) []int32 {
	cells := make([]int32, n)
	for i := range cells {
		cells[i] = int32(i + 1)
	}
	for i := n - 1; i > 0; i-- {
		j := r.IntN(i + 1)
		cells[i], cells[j] = cells[j], cells[i]
	}
	return cells
}

// ScratchCard is one card's display state: only opened cells reveal their value.
type ScratchCard struct {
	Index    int         `json:"index"`
	Values   map[int]int `json:"values"` // 開封セルindex -> 値(未開封は含まない)
	Opened   int         `json:"opened"`
	Finished bool        `json:"finished"`
	Atari    int         `json:"atari"`
}

// ScratchState is the current daily scratch board.
type ScratchState struct {
	Game     string        `json:"game"`
	Cols     int           `json:"cols"`
	Cells    int           `json:"cells"`
	AtariMax int           `json:"atari_max"`
	OpenMax  int           `json:"open_max"`
	Cards    []ScratchCard `json:"cards"`
}

// GetScratchState returns today's scratch cards, generating a fresh set on the
// first access of a new game day.
func (s *Service) GetScratchState(ctx context.Context, playerID int64, game string) (*ScratchState, error) {
	def, ok := scratchDefs[game]
	if !ok {
		return nil, &ConditionError{Message: "不明なゲームです。"}
	}
	today := gametime.Date(time.Now(), s.loc, s.dayBoundaryHour)
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var cnt int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM player_scratch_cards WHERE player_id=$1 AND game=$2 AND game_date=$3`,
			playerID, game, today).Scan(&cnt); err != nil {
			return err
		}
		if cnt == 0 {
			for i := 0; i < def.Cards; i++ {
				if _, err := tx.Exec(ctx,
					`INSERT INTO player_scratch_cards (player_id, game, game_date, card_index, cells)
					 VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
					playerID, game, today, i, genScratchCells(s.rng, def.Cells)); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s.readScratchState(ctx, playerID, game, today, def)
}

func (s *Service) readScratchState(ctx context.Context, playerID int64, game string, today time.Time, def scratchDef) (*ScratchState, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT card_index, cells, opened FROM player_scratch_cards
		 WHERE player_id=$1 AND game=$2 AND game_date=$3 ORDER BY card_index`,
		playerID, game, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	st := &ScratchState{Game: game, Cols: def.Cols, Cells: def.Cells, AtariMax: def.AtariMax, OpenMax: def.OpenMax}
	for rows.Next() {
		var idx int
		var cells, opened []int32
		if err := rows.Scan(&idx, &cells, &opened); err != nil {
			return nil, err
		}
		card := ScratchCard{Index: idx, Values: map[int]int{}}
		for _, o := range opened {
			v := int(cells[o])
			card.Values[int(o)] = v
			if v <= def.AtariMax {
				card.Atari++
			}
		}
		card.Opened = len(opened)
		card.Finished = card.Opened >= def.OpenMax
		st.Cards = append(st.Cards, card)
	}
	return st, rows.Err()
}

// ScratchOpenResult is returned after opening one scratch cell.
type ScratchOpenResult struct {
	Player *player.Player `json:"-"`
	Value  int            `json:"value"` // 開いたセルの値
	Win    bool           `json:"win"`   // そのセルが当たりか
	Bonus  bool           `json:"bonus"` // 全開封当りボーナスが出たか
	Prize  int64          `json:"prize"` // 今回の賞金
	State  *ScratchState  `json:"state"` // 更新後の盤面
}

// DoScratchOpen opens one cell of a scratch card, pays winning prizes, and
// returns the updated board. Cards are per game day and capped at OpenMax opens.
func (s *Service) DoScratchOpen(ctx context.Context, playerID int64, game string, cardIndex, cellIndex int, idempotencyKey string) (*ScratchOpenResult, error) {
	def, ok := scratchDefs[game]
	if !ok {
		return nil, &ConditionError{Message: "不明なゲームです。"}
	}
	today := gametime.Date(time.Now(), s.loc, s.dayBoundaryHour)
	out := &ScratchOpenResult{}
	p, err := s.runAction(ctx, playerID, "scratch:"+game, idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var cells, opened []int32
		err := tx.QueryRow(ctx,
			`SELECT cells, opened FROM player_scratch_cards
			 WHERE player_id=$1 AND game=$2 AND game_date=$3 AND card_index=$4 FOR UPDATE`,
			playerID, game, today, cardIndex).Scan(&cells, &opened)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "そのカードは今日ありません。"}
		}
		if err != nil {
			return err
		}
		if cellIndex < 0 || cellIndex >= len(cells) {
			return &ConditionError{Message: "セルの指定が正しくありません。"}
		}
		for _, o := range opened {
			if int(o) == cellIndex {
				return &ConditionError{Message: "そのマスはもう開いています。"}
			}
		}
		if len(opened) >= def.OpenMax {
			return &ConditionError{Message: "このカードはもう開けられません。"}
		}
		opened = append(opened, int32(cellIndex))
		val := int(cells[cellIndex])
		out.Value = val
		var prize int64
		if val <= def.AtariMax {
			out.Win = true
			prize += def.Prize
		}
		// 開封上限に達し、開けたセルがすべて当たりならボーナス。
		if len(opened) == def.OpenMax {
			atari := 0
			for _, o := range opened {
				if int(cells[o]) <= def.AtariMax {
					atari++
				}
			}
			if atari == def.OpenMax {
				out.Bonus = true
				prize += def.Bonus
			}
		}
		out.Prize = prize
		if _, err := tx.Exec(ctx,
			`UPDATE player_scratch_cards SET opened=$1
			 WHERE player_id=$2 AND game=$3 AND game_date=$4 AND card_index=$5`,
			opened, playerID, game, today, cardIndex); err != nil {
			return err
		}
		if prize > 0 {
			if err := s.ledger.PostTx(ctx, tx, "scratch:"+game, "", []ledger.Entry{
				{Account: ledger.SystemAccount("casino"), Delta: -prize},
				{Account: ledger.PlayerAccount(playerID), Delta: prize},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Player = p
	st, err := s.readScratchState(ctx, playerID, game, today, def)
	if err != nil {
		return nil, err
	}
	out.State = st
	return out, nil
}

// bjBets are the allowed blackjack stakes.
var bjBets = []int64{10000, 100000, 500000, 1000000}

// BJState is the blackjack table state for display. During play the dealer's
// hole card (oya[1..]) is hidden; at OVER the full hand is shown.
type BJState struct {
	Active    bool   `json:"active"`     // 進行中/決着ゲームが存在するか
	Rate      int64  `json:"rate"`       // 掛け金
	Ply       []int  `json:"ply"`        // 子の手札(全公開)
	PlyScore  int    `json:"ply_score"`  // 子の点数
	Oya       []int  `json:"oya"`        // 親の手札(playing中は1枚目のみ)
	OyaScore  int    `json:"oya_score"`  // 親の点数(playing中は見せている札のみ)
	OyaHidden int    `json:"oya_hidden"` // 伏せている親の枚数
	Phase     string `json:"phase"`      // 'playing' | 'over'
	Result    string `json:"result"`     // 'win'|'lose'|'push'(over時)
	Payout    int64  `json:"payout"`     // 決着時の払戻(掛け金返却含む)
}

// bjReadState loads the player's blackjack game (Active=false if none).
func (s *Service) bjReadState(ctx context.Context, q queryRower, playerID int64) (*BJState, error) {
	var rate int64
	var oya, ply []int32
	var phase, result string
	err := q.QueryRow(ctx,
		`SELECT rate, oya, ply, phase, result FROM player_blackjack WHERE player_id=$1`,
		playerID).Scan(&rate, &oya, &ply, &phase, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return &BJState{Active: false}, nil
	}
	if err != nil {
		return nil, err
	}
	st := &BJState{Active: true, Rate: rate, Phase: phase, Result: result}
	for _, c := range ply {
		st.Ply = append(st.Ply, int(c))
	}
	st.PlyScore = casino.BJScore(st.Ply)
	full := make([]int, len(oya))
	for i, c := range oya {
		full[i] = int(c)
	}
	if phase == "over" {
		st.Oya = full
		st.OyaScore = casino.BJScore(full)
		switch result {
		case "win":
			st.Payout = rate * 2
		case "push":
			st.Payout = rate
		}
	} else {
		// 進行中は親の1枚目のみ公開。
		if len(full) > 0 {
			st.Oya = full[:1]
			st.OyaScore = casino.BJScore(full[:1])
			st.OyaHidden = len(full) - 1
		}
	}
	return st, nil
}

// BJGetState returns the current blackjack state without changing it.
func (s *Service) BJGetState(ctx context.Context, playerID int64) (*BJState, error) {
	return s.bjReadState(ctx, s.pool, playerID)
}

// BJStart deals a new blackjack hand for the given stake. The stake is moved to
// the casino immediately; a settled hand pays out at hit-bust/stand.
func (s *Service) BJStart(ctx context.Context, playerID, rate int64, idempotencyKey string) (*BJState, error) {
	ok := false
	for _, b := range bjBets {
		if b == rate {
			ok = true
		}
	}
	if !ok {
		return nil, &ConditionError{Message: "掛け金の指定が正しくありません。"}
	}
	var out *BJState
	_, err := s.runAction(ctx, playerID, "bj_start", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var phase string
		err := tx.QueryRow(ctx, `SELECT phase FROM player_blackjack WHERE player_id=$1`, playerID).Scan(&phase)
		if err == nil && phase == "playing" {
			return &ConditionError{Message: "進行中のゲームがあります。"}
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if state.Money < rate {
			return &ConditionError{Message: "お金が足りません。"}
		}
		// 掛け金を胴元へ。決着時に payout を胴元から戻す。
		if err := s.ledger.PostTx(ctx, tx, "bj_bet", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -rate},
			{Account: ledger.SystemAccount("casino"), Delta: rate},
		}); err != nil {
			return err
		}
		// 親子に2枚ずつ。
		var used []int
		draw := func() int32 { c := casino.BJDraw(s.rng, used); used = append(used, c); return int32(c) }
		oya := []int32{draw(), draw()}
		ply := []int32{draw(), draw()}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_blackjack (player_id, rate, oya, ply, phase, result)
			 VALUES ($1,$2,$3,$4,'playing','')
			 ON CONFLICT (player_id) DO UPDATE SET rate=$2, oya=$3, ply=$4, phase='playing', result='', updated_at=now()`,
			playerID, rate, oya, ply); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out, err = s.bjReadState(ctx, s.pool, playerID)
	return out, err
}

// BJHit draws one card for the player; busting settles the hand as a loss.
func (s *Service) BJHit(ctx context.Context, playerID int64, idempotencyKey string) (*BJState, error) {
	return s.bjStep(ctx, playerID, "bj_hit", idempotencyKey, func(tx pgx.Tx, rate int64, oya, ply []int32) (bool, string, int64, []int32, []int32, error) {
		used := append(append([]int{}, toIntSlice(oya)...), toIntSlice(ply)...)
		ply = append(ply, int32(casino.BJDraw(s.rng, used)))
		if casino.BJScore(toIntSlice(ply)) > 21 {
			return true, "lose", 0, oya, ply, nil // バスト=子負け(払戻なし)
		}
		return false, "", 0, oya, ply, nil
	})
}

// BJStand makes the dealer hit until 17+, then settles the hand.
func (s *Service) BJStand(ctx context.Context, playerID int64, idempotencyKey string) (*BJState, error) {
	return s.bjStep(ctx, playerID, "bj_stand", idempotencyKey, func(tx pgx.Tx, rate int64, oya, ply []int32) (bool, string, int64, []int32, []int32, error) {
		used := append(append([]int{}, toIntSlice(oya)...), toIntSlice(ply)...)
		for casino.BJScore(toIntSlice(oya)) < 17 {
			c := int32(casino.BJDraw(s.rng, used))
			oya = append(oya, c)
			used = append(used, int(c))
		}
		p := casino.BJScore(toIntSlice(ply))
		o := casino.BJScore(toIntSlice(oya))
		var result string
		var payout int64
		switch {
		case o > 21:
			if p > 21 {
				result, payout = "push", rate
			} else {
				result, payout = "win", rate*2
			}
		case p == o:
			result, payout = "push", rate
		case p <= 21 && p > o:
			result, payout = "win", rate*2
		default:
			result, payout = "lose", 0
		}
		return true, result, payout, oya, ply, nil
	})
}

// bjStep runs a blackjack transition: it loads the playing game, applies fn
// (which returns whether the hand is settled, the result, payout, and new hands),
// pays out on settlement, and persists the new state.
func (s *Service) bjStep(ctx context.Context, playerID int64, action, idempotencyKey string, fn func(tx pgx.Tx, rate int64, oya, ply []int32) (settled bool, result string, payout int64, newOya, newPly []int32, err error)) (*BJState, error) {
	_, err := s.runAction(ctx, playerID, action, idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var rate int64
		var oya, ply []int32
		var phase string
		err := tx.QueryRow(ctx,
			`SELECT rate, oya, ply, phase FROM player_blackjack WHERE player_id=$1 FOR UPDATE`,
			playerID).Scan(&rate, &oya, &ply, &phase)
		if errors.Is(err, pgx.ErrNoRows) || phase != "playing" {
			return &ConditionError{Message: "進行中のゲームがありません。"}
		}
		if err != nil {
			return err
		}
		settled, result, payout, newOya, newPly, err := fn(tx, rate, oya, ply)
		if err != nil {
			return err
		}
		newPhase := "playing"
		if settled {
			newPhase = "over"
			if payout > 0 {
				if err := s.ledger.PostTx(ctx, tx, action+"_payout", "", []ledger.Entry{
					{Account: ledger.SystemAccount("casino"), Delta: -payout},
					{Account: ledger.PlayerAccount(playerID), Delta: payout},
				}); err != nil {
					return err
				}
			}
		}
		_, err = tx.Exec(ctx,
			`UPDATE player_blackjack SET oya=$2, ply=$3, phase=$4, result=$5, updated_at=now() WHERE player_id=$1`,
			playerID, newOya, newPly, newPhase, result)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.bjReadState(ctx, s.pool, playerID)
}

// toIntSlice converts an int32 slice to int for scoring helpers.
func toIntSlice(xs []int32) []int {
	out := make([]int, len(xs))
	for i, x := range xs {
		out[i] = int(x)
	}
	return out
}

const (
	pokerBuyCost   int64 = 5000 // 5000円で5ポイント購入
	pokerBuyPoints       = 5
	pokerPointYen  int64 = 1000 // 清算時 1000円/ポイント(手数料として-1点)
)

// PokerState is the poker table state for display.
type PokerState struct {
	Active     bool   `json:"active"`      // 購入済み(points>0)
	Points     int    `json:"points"`      // 所持ポイント
	Hand       []int  `json:"hand"`        // 手札(配札後)
	Phase      string `json:"phase"`       // 'none'|'ready'|'dealt'
	Result     int    `json:"result"`      // 直近の役(判定後、-1=未判定)
	ResultName string `json:"result_name"` // 直近の役名
	Gain       int    `json:"gain"`        // 直近のポイント増減(result-1)
}

func (s *Service) pokerReadState(ctx context.Context, q queryRower, playerID int64) (*PokerState, error) {
	var points int
	var hand []int32
	var phase string
	err := q.QueryRow(ctx, `SELECT points, hand, phase FROM player_poker WHERE player_id=$1`, playerID).Scan(&points, &hand, &phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return &PokerState{Active: false, Phase: "none", Result: -1}, nil
	}
	if err != nil {
		return nil, err
	}
	st := &PokerState{Active: points > 0 || phase != "none", Points: points, Phase: phase, Result: -1}
	st.Hand = toIntSlice(hand)
	return st, nil
}

// PokerGetState returns the current poker state.
func (s *Service) PokerGetState(ctx context.Context, playerID int64) (*PokerState, error) {
	return s.pokerReadState(ctx, s.pool, playerID)
}

// PokerBuy spends 5000 yen for 5 points to start a session.
func (s *Service) PokerBuy(ctx context.Context, playerID int64, idempotencyKey string) (*PokerState, error) {
	_, err := s.runAction(ctx, playerID, "poker_buy", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var points int
		err := tx.QueryRow(ctx, `SELECT points FROM player_poker WHERE player_id=$1 FOR UPDATE`, playerID).Scan(&points)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if points > 0 {
			return &ConditionError{Message: "まだポイントが残っています。"}
		}
		if state.Money < pokerBuyCost {
			return &ConditionError{Message: "お金が足りません。"}
		}
		if err := s.ledger.PostTx(ctx, tx, "poker_buy", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -pokerBuyCost},
			{Account: ledger.SystemAccount("casino"), Delta: pokerBuyCost},
		}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO player_poker (player_id, points, hand, phase) VALUES ($1,$2,'{}','ready')
			 ON CONFLICT (player_id) DO UPDATE SET points=$2, hand='{}', phase='ready', updated_at=now()`,
			playerID, pokerBuyPoints)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.pokerReadState(ctx, s.pool, playerID)
}

// PokerDeal deals 5 cards (phase ready -> dealt).
func (s *Service) PokerDeal(ctx context.Context, playerID int64, idempotencyKey string) (*PokerState, error) {
	_, err := s.runAction(ctx, playerID, "poker_deal", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var points int
		var phase string
		err := tx.QueryRow(ctx, `SELECT points, phase FROM player_poker WHERE player_id=$1 FOR UPDATE`, playerID).Scan(&points, &phase)
		if errors.Is(err, pgx.ErrNoRows) || points <= 0 {
			return &ConditionError{Message: "先にポイントを購入してください。"}
		}
		if err != nil {
			return err
		}
		if phase != "ready" {
			return &ConditionError{Message: "先に交換してください。"}
		}
		var used []int
		hand := make([]int32, 5)
		for i := range hand {
			c := casino.BJDraw(s.rng, used)
			used = append(used, c)
			hand[i] = int32(c)
		}
		_, err = tx.Exec(ctx, `UPDATE player_poker SET hand=$2, phase='dealt', updated_at=now() WHERE player_id=$1`, playerID, hand)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.pokerReadState(ctx, s.pool, playerID)
}

// PokerDraw swaps the non-held cards, evaluates the hand, and adds result-1 to
// points. Running out of points ends the session.
func (s *Service) PokerDraw(ctx context.Context, playerID int64, hold []int, idempotencyKey string) (*PokerState, error) {
	var out PokerState
	_, err := s.runAction(ctx, playerID, "poker_draw", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var points int
		var hand []int32
		var phase string
		err := tx.QueryRow(ctx, `SELECT points, hand, phase FROM player_poker WHERE player_id=$1 FOR UPDATE`, playerID).Scan(&points, &hand, &phase)
		if errors.Is(err, pgx.ErrNoRows) || phase != "dealt" {
			return &ConditionError{Message: "配札されていません。"}
		}
		if err != nil {
			return err
		}
		held := map[int]bool{}
		for _, h := range hold {
			if h >= 0 && h < len(hand) {
				held[h] = true
			}
		}
		used := toIntSlice(hand)
		for i := range hand {
			if !held[i] {
				c := casino.BJDraw(s.rng, used)
				used = append(used, c)
				hand[i] = int32(c)
			}
		}
		result := casino.PokerEval(toIntSlice(hand))
		gain := result - 1
		points += gain
		out.Result = result
		out.ResultName = casino.PokerHandName(result)
		out.Gain = gain
		phase = "ready"
		if points <= 0 {
			// ポイント切れでゲームオーバー(セッション消滅)。
			points = 0
			phase = "none"
			_, err = tx.Exec(ctx, `UPDATE player_poker SET points=0, hand='{}', phase='none', updated_at=now() WHERE player_id=$1`, playerID)
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE player_poker SET points=$2, hand=$3, phase=$4, updated_at=now() WHERE player_id=$1`, playerID, points, hand, phase)
		return err
	})
	if err != nil {
		return nil, err
	}
	st, err := s.pokerReadState(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}
	st.Result = out.Result
	st.ResultName = out.ResultName
	st.Gain = out.Gain
	return st, nil
}

// PokerCashout converts remaining points to cash at 1000 yen/point minus a
// 1-point fee, and ends the session.
func (s *Service) PokerCashout(ctx context.Context, playerID int64, idempotencyKey string) (*PokerState, error) {
	_, err := s.runAction(ctx, playerID, "poker_cashout", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var points int
		err := tx.QueryRow(ctx, `SELECT points FROM player_poker WHERE player_id=$1 FOR UPDATE`, playerID).Scan(&points)
		if errors.Is(err, pgx.ErrNoRows) || points <= 0 {
			return &ConditionError{Message: "清算できるポイントがありません。"}
		}
		if err != nil {
			return err
		}
		payout := int64(points-1) * pokerPointYen
		if payout > 0 {
			if err := s.ledger.PostTx(ctx, tx, "poker_cashout", "", []ledger.Entry{
				{Account: ledger.SystemAccount("casino"), Delta: -payout},
				{Account: ledger.PlayerAccount(playerID), Delta: payout},
			}); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE player_poker SET points=0, hand='{}', phase='none', updated_at=now() WHERE player_id=$1`, playerID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.pokerReadState(ctx, s.pool, playerID)
}

// Loto6Ticket is one purchased ticket for display.
type Loto6Ticket struct {
	Numbers []int `json:"numbers"`
}

// Loto6DrawInfo is a past draw result for display.
type Loto6DrawInfo struct {
	Date    string `json:"date"`
	Winning []int  `json:"winning"`
}

// Loto6State is the player's loto6 view: their tickets for today, the daily cap,
// and the most recent draw.
type Loto6State struct {
	MyTickets  []Loto6Ticket  `json:"my_tickets"`
	TodayCount int            `json:"today_count"`
	DailyLimit int            `json:"daily_limit"`
	Cost       int64          `json:"cost"`
	LastDraw   *Loto6DrawInfo `json:"last_draw"`
}

// Loto6GetState returns the player's tickets for today and the latest draw.
func (s *Service) Loto6GetState(ctx context.Context, playerID int64) (*Loto6State, error) {
	today := gametime.Date(time.Now(), s.loc, s.dayBoundaryHour)
	st := &Loto6State{DailyLimit: casino.Loto6DailyLimit, Cost: casino.Loto6Cost, MyTickets: []Loto6Ticket{}}
	rows, err := s.pool.Query(ctx,
		`SELECT numbers FROM loto6_tickets WHERE player_id=$1 AND game_date=$2 ORDER BY id`,
		playerID, today)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var nums []int32
		if err := rows.Scan(&nums); err != nil {
			rows.Close()
			return nil, err
		}
		st.MyTickets = append(st.MyTickets, Loto6Ticket{Numbers: toIntSlice(nums)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	st.TodayCount = len(st.MyTickets)

	var date time.Time
	var winning []int32
	err = s.pool.QueryRow(ctx, `SELECT game_date, winning FROM loto6_draws ORDER BY game_date DESC LIMIT 1`).Scan(&date, &winning)
	if err == nil {
		st.LastDraw = &Loto6DrawInfo{Date: date.Format("2006-01-02"), Winning: toIntSlice(winning)}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return st, nil
}

// DoLoto6Buy buys one loto6 ticket with the given numbers (validated), capped at
// Loto6DailyLimit per day. The cost goes to the casino; prizes are paid at the
// daily draw.
func (s *Service) DoLoto6Buy(ctx context.Context, playerID int64, numbers []int, idempotencyKey string) (*Loto6State, error) {
	if !casino.ValidLoto6Pick(numbers) {
		return nil, &ConditionError{Message: "1から36の異なる数字を6個選んでください。"}
	}
	today := gametime.Date(time.Now(), s.loc, s.dayBoundaryHour)
	nums := make([]int32, len(numbers))
	for i, n := range numbers {
		nums[i] = int32(n)
	}
	_, err := s.runAction(ctx, playerID, "loto6_buy", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var cnt int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM loto6_tickets WHERE player_id=$1 AND game_date=$2`, playerID, today).Scan(&cnt); err != nil {
			return err
		}
		if cnt >= casino.Loto6DailyLimit {
			return &ConditionError{Message: fmt.Sprintf("ロト6は1日%d口までです。", casino.Loto6DailyLimit)}
		}
		if state.Money < casino.Loto6Cost {
			return &ConditionError{Message: "お金が足りません。"}
		}
		if err := s.ledger.PostTx(ctx, tx, "loto6_buy", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -casino.Loto6Cost},
			{Account: ledger.SystemAccount("loto"), Delta: casino.Loto6Cost},
		}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO loto6_tickets (player_id, game_date, numbers) VALUES ($1,$2,$3)`, playerID, today, nums)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Loto6GetState(ctx, playerID)
}

// CasinoResult is returned after a minigame play: the updated player and the
// game outcome for presentation.
type CasinoResult struct {
	Player *player.Player `json:"-"`
	Payout int64          `json:"payout"`
	Win    bool           `json:"win"`
	Detail any            `json:"detail"`
}

// DoCasinoPlay runs one round of a single-shot minigame: it validates the bet,
// plays the (pure) game, then in one transaction moves the stake to the casino,
// pays out any winnings, and records the play to game_plays.
func (s *Service) DoCasinoPlay(ctx context.Context, playerID int64, game string, bet int64, params json.RawMessage, idempotencyKey string) (*CasinoResult, error) {
	g := casino.Lookup(game)
	if g == nil {
		return nil, &ConditionError{Message: "不明なゲームです。"}
	}
	// 掛け金は0以上(福引き/おみくじは無料/賽銭のためbet=0を許す)。
	if bet < 0 {
		return nil, &ConditionError{Message: "掛け金が正しくありません。"}
	}
	if allowed := g.Bets(); len(allowed) > 0 {
		ok := false
		for _, b := range allowed {
			if b == bet {
				ok = true
			}
		}
		if !ok {
			return nil, &ConditionError{Message: "掛け金の指定が正しくありません。"}
		}
	}
	res, err := g.Play(s.rng, bet, params)
	if err != nil {
		return nil, &ConditionError{Message: err.Error()}
	}
	out := &CasinoResult{Payout: res.Payout, Win: res.Win, Detail: res.Detail}
	p, err := s.runAction(ctx, playerID, "casino:"+game, idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if err := s.checkGameLimit(ctx, tx, playerID, game); err != nil {
			return err
		}
		// 掛け金 + (賽銭など)支払い分を所持金でまかなえるか。
		needed := bet
		if res.MoneyDelta < 0 {
			needed += -res.MoneyDelta
		}
		if state.Money < needed {
			return &ConditionError{Message: "お金が足りません。"}
		}
		// 掛け金を胴元へ、払戻・直接金銭効果(MoneyDelta)を胴元とやり取りする。
		// ネットで system:casino が損益を吸収し、ゼロ和を保つ。
		var entries []ledger.Entry
		if bet > 0 {
			entries = append(entries,
				ledger.Entry{Account: ledger.PlayerAccount(playerID), Delta: -bet},
				ledger.Entry{Account: ledger.SystemAccount("casino"), Delta: bet},
			)
		}
		if res.Payout > 0 {
			entries = append(entries,
				ledger.Entry{Account: ledger.SystemAccount("casino"), Delta: -res.Payout},
				ledger.Entry{Account: ledger.PlayerAccount(playerID), Delta: res.Payout},
			)
		}
		if res.MoneyDelta != 0 {
			entries = append(entries,
				ledger.Entry{Account: ledger.SystemAccount("casino"), Delta: -res.MoneyDelta},
				ledger.Entry{Account: ledger.PlayerAccount(playerID), Delta: res.MoneyDelta},
			)
		}
		if len(entries) > 0 {
			if err := s.ledger.PostTx(ctx, tx, "casino:"+game, "", entries); err != nil {
				return err
			}
		}
		// ステータス変更(add_param)を適用する。
		if len(res.Params) > 0 {
			eff := effects.Effect{}
			for _, pd := range res.Params {
				eff.Ops = append(eff.Ops, effects.Op{Kind: "add_param", Param: pd.Param, Amount: pd.Amount})
			}
			if err := s.applyEffect(ctx, tx, playerID, "casino:"+game, eff, state); err != nil {
				return err
			}
		}
		// アイテム付与。
		for _, it := range res.Items {
			if err := s.grantItem(ctx, tx, playerID, it.Name, it.Qty); err != nil {
				return err
			}
		}
		detailJSON, _ := json.Marshal(res.Detail)
		if _, err := tx.Exec(ctx,
			`INSERT INTO game_plays (game, player_id, bet, payout, detail) VALUES ($1, $2, $3, $4, $5)`,
			game, playerID, bet, res.Payout, detailJSON); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Player = p
	return out, nil
}

// grantItem adds qty of the named content item to the player, initializing its
// remaining uses from the item's durability (matching purchase behavior).
// Unknown item names are silently skipped so a game can reference optional prizes.
func (s *Service) grantItem(ctx context.Context, tx pgx.Tx, playerID int64, name string, qty int) error {
	if qty <= 0 || name == "" {
		return nil
	}
	var itemID int64
	var durability int
	err := tx.QueryRow(ctx, `SELECT id, durability FROM content_items WHERE name = $1`, name).Scan(&itemID, &durability)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lookup item %q: %w", name, err)
	}
	if durability < 1 {
		durability = 1
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (player_id, item_id)
		 DO UPDATE SET quantity = player_items.quantity + $3,
		               remaining_uses = player_items.remaining_uses + $4,
		               updated_at = now()`,
		playerID, itemID, qty, durability*qty)
	return err
}

// StatementEntry is one line of a savings-account passbook (入出金明細).
type StatementEntry struct {
	At      time.Time `json:"at"`
	Label   string    `json:"label"`
	Amount  int64     `json:"amount"`  // 符号付き(入金+/出金-)
	Balance int64     `json:"balance"` // その取引直後の口座残高
}

// bankStatementLimit caps how many recent passbook lines are returned.
const bankStatementLimit = 30

// BankStatement returns the player's recent savings-account movements, newest
// first, each labelled from the ledger reason (預け入れ/引き出し/利息/おさい銭…).
func (s *Service) BankStatement(ctx context.Context, playerID int64) ([]StatementEntry, error) {
	return s.bankStatementFor(ctx, ledger.SavingsAccount(playerID))
}

// BankStatementSuper returns the recent passbook lines of the スーパー定期口座.
func (s *Service) BankStatementSuper(ctx context.Context, playerID int64) ([]StatementEntry, error) {
	return s.bankStatementFor(ctx, ledger.SuperSavingsAccount(playerID))
}

// bankStatementFor lists one account's passbook (口座ごとに別の明細)。
func (s *Service) bankStatementFor(ctx context.Context, account string) ([]StatementEntry, error) {
	// running=そのentry時点の口座残高(累積和)。全履歴で累積してから最新N件を返す。
	rows, err := s.pool.Query(ctx,
		`SELECT created_at, reason, delta, running
		 FROM (
		   SELECT e.id, t.created_at, t.reason, e.delta,
		          SUM(e.delta) OVER (ORDER BY e.id) AS running
		   FROM ledger_entry e
		   JOIN ledger_tx t ON t.id = e.tx_id
		   WHERE e.account = $1
		 ) x
		 ORDER BY id DESC
		 LIMIT $2`, account, bankStatementLimit)
	if err != nil {
		return nil, fmt.Errorf("query statement: %w", err)
	}
	defer rows.Close()
	out := []StatementEntry{}
	for rows.Next() {
		var e StatementEntry
		var reason string
		if err := rows.Scan(&e.At, &reason, &e.Amount, &e.Balance); err != nil {
			return nil, fmt.Errorf("scan statement: %w", err)
		}
		e.Label = statementLabel(reason, e.Amount)
		out = append(out, e)
	}
	return out, rows.Err()
}

// statementLabel maps a ledger reason (and the entry sign) to a Japanese
// passbook label. Transfers share one reason for both parties, so the sign of
// the amount distinguishes send (out) from receive (in).
func statementLabel(reason string, amount int64) string {
	switch {
	case reason == "deposit":
		return "預け入れ"
	case reason == "withdraw":
		return "引き出し"
	case strings.HasPrefix(reason, "interest:"):
		return "利息"
	case reason == "offer":
		return "おさい銭"
	case reason == "saisen":
		return "おさい銭"
	case reason == "shiire":
		return "仕入れ"
	// 家の店・闇市の代金は売り手の普通口座に入る(買い手側は現金なので通帳には出ない)。
	case reason == "house_shop_buy" || reason == "shop_buy":
		return "売上"
	case reason == "yami_buy":
		return "売上(闇市)"
	case reason == "build_house":
		return "家の建築"
	case reason == "rebuild_house":
		return "家の建て替え"
	case reason == "transfer":
		if amount < 0 {
			return "振込(送金)"
		}
		return "振込(入金)"
	case reason == "transfer_donation":
		return "寄付"
	case reason == "super_deposit":
		return "定期預入"
	case reason == "super_cancel":
		return "定期解約"
	case strings.HasPrefix(reason, "super_interest:"):
		return "定期利息"
	case reason == "loan_borrow":
		return "住宅ローン"
	case reason == "loan_repay":
		return "ローン返済"
	case reason == "loan_repay_full":
		return "ローン一括返済"
	default:
		return "取引"
	}
}

// DoHospitalTreat cures the player's current disease at the hospital: it reads
// the disease index server-side, charges the fee for the resulting disease name,
// and resets the disease index to the healthy baseline (50). A healthy player
// may buy a preventive 「元気」 shot. Money is moved through the ledger to
// system:hospital_sink. (design 17.4)
func (s *Service) DoHospitalTreat(ctx context.Context, playerID int64, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "hospital", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var index int
		if err := tx.QueryRow(ctx,
			`SELECT disease_index FROM player_status WHERE player_id = $1`, playerID).Scan(&index); err != nil {
			return fmt.Errorf("read disease index: %w", err)
		}
		// 治療費はサーバ側で病名から決定する(旧のクライアント任せを是正)。
		fee := condition.TreatmentFee(condition.DiseaseName(index))
		if state.Money < fee {
			return &ConditionError{Message: "お金が足りません。"}
		}
		if err := s.ledger.PostTx(ctx, tx, "hospital", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -fee},
			{Account: ledger.SystemAccount("hospital_sink"), Delta: fee},
		}); err != nil {
			return fmt.Errorf("pay treatment: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE player_status SET disease_index = 50, disease_evaled_at = now(), updated_at = now()
			 WHERE player_id = $1`, playerID); err != nil {
			return fmt.Errorf("cure disease: %w", err)
		}
		return nil
	})
}

// DoOnsen bathes at the hot spring. The legacy onsen does not restore a fixed
// amount: it accelerates the player's natural power recovery by the bath's
// multiplier over the elapsed time and clamps to the maximum (design 17 / legacy
// basic.cgi onsen). It charges the fee, advances the recovery timestamps so the
// worker does not double-recover, and has no cooldown. Bathing while both power
// bars are already full is rejected to avoid wasting money.
func (s *Service) DoOnsen(ctx context.Context, playerID, bathID int64, idempotencyKey string) (*player.Player, error) {
	price, multiplier, err := s.loadOnsenBath(ctx, bathID)
	if err != nil {
		return nil, err
	}
	return s.runAction(ctx, playerID, "onsen", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if state.Money < price {
			return &ConditionError{Message: "お金が足りません。"}
		}
		energy := state.Params["energy"]
		nou := state.Params["nou_energy"]
		if energy.Value >= energy.Max && nou.Value >= nou.Max {
			return &ConditionError{Message: "すでに元気いっぱいです。"}
		}
		if err := s.ledger.PostTx(ctx, tx, "onsen", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -price},
			{Account: ledger.SystemAccount("onsen_sink"), Delta: price},
		}); err != nil {
			return fmt.Errorf("pay: %w", err)
		}
		// 入浴した瞬間に経過ぶんを倍率で一括回復し、以降はonsen_multiplierを立てて
		// workerが倍率速度で回復し続ける(満タンか退室で1に戻る)。
		cfg := s.settings.Get()
		energyRecSec, nouRecSec := cfg.EnergyRecoverySec, cfg.NouRecoverySec
		if energyRecSec <= 0 {
			energyRecSec = 60
		}
		if nouRecSec <= 0 {
			nouRecSec = 60
		}
		if _, err := tx.Exec(ctx, `
			UPDATE player_status SET
				energy = LEAST(energy_max,
					energy + FLOOR(EXTRACT(EPOCH FROM (now() - energy_recovered_at)) / $2 * $3)::int + 1),
				energy_recovered_at = now(),
				nou_energy = LEAST(nou_energy_max,
					nou_energy + FLOOR(EXTRACT(EPOCH FROM (now() - nou_recovered_at)) / $4 * $3)::int + 1),
				nou_recovered_at = now(),
				onsen_multiplier = $3,
				updated_at = now()
			WHERE player_id = $1`,
			playerID, energyRecSec, multiplier, nouRecSec); err != nil {
			return fmt.Errorf("onsen recover: %w", err)
		}
		return nil
	})
}

// DoOnsenLeave ends an onsen session by resetting the recovery multiplier to
// normal. Called when the player leaves the bath screen (or picks another bath).
func (s *Service) DoOnsenLeave(ctx context.Context, playerID int64) (*player.Player, error) {
	return s.runAction(ctx, playerID, "onsen_leave", "", func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if _, err := tx.Exec(ctx,
			`UPDATE player_status SET onsen_multiplier = 1, updated_at = now() WHERE player_id = $1`,
			playerID); err != nil {
			return fmt.Errorf("onsen leave: %w", err)
		}
		return nil
	})
}

// DoOnsenTick advances this player's power recovery up to now so the bath screen
// can poll it and show power rising smoothly, without waiting for the worker's
// coarse tick. RecoverPowerと同じ式の1人版で、満タンになったら倍率を1に戻す。
func (s *Service) DoOnsenTick(ctx context.Context, playerID int64) (*player.Player, error) {
	cfg := s.settings.Get()
	energySec, nouSec := cfg.EnergyRecoverySec, cfg.NouRecoverySec
	if energySec <= 0 {
		energySec = 60
	}
	if nouSec <= 0 {
		nouSec = 60
	}
	return s.runAction(ctx, playerID, "onsen_tick", "", func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if _, err := tx.Exec(ctx, `
			UPDATE player_status ps SET
				energy = LEAST(ps.energy_max, ps.energy + g.gain_e),
				energy_recovered_at = ps.energy_recovered_at + make_interval(secs => (g.gain_e * $2::double precision / ps.onsen_multiplier)),
				nou_energy = LEAST(ps.nou_energy_max, ps.nou_energy + g.gain_n),
				nou_recovered_at = ps.nou_recovered_at + make_interval(secs => (g.gain_n * $3::double precision / ps.onsen_multiplier)),
				onsen_multiplier = CASE
					WHEN LEAST(ps.energy_max, ps.energy + g.gain_e) >= ps.energy_max
					 AND LEAST(ps.nou_energy_max, ps.nou_energy + g.gain_n) >= ps.nou_energy_max
					THEN 1 ELSE ps.onsen_multiplier END,
				updated_at = now()
			FROM (
				SELECT player_id,
					FLOOR(EXTRACT(EPOCH FROM (now() - energy_recovered_at)) / ($2::double precision / onsen_multiplier))::int AS gain_e,
					FLOOR(EXTRACT(EPOCH FROM (now() - nou_recovered_at)) / ($3::double precision / onsen_multiplier))::int AS gain_n
				FROM player_status WHERE player_id = $1
			) g
			WHERE ps.player_id = g.player_id`,
			playerID, energySec, nouSec); err != nil {
			return fmt.Errorf("onsen tick: %w", err)
		}
		return nil
	})
}

func (s *Service) loadOnsenBath(ctx context.Context, bathID int64) (price int64, multiplier int, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT price, power_multiplier FROM content_items
		 WHERE id = $1 AND enabled AND facility = 'onsen'`, bathID).Scan(&price, &multiplier)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrItemNotFound
	}
	if err != nil {
		return 0, 0, fmt.Errorf("load onsen bath: %w", err)
	}
	if multiplier <= 0 {
		multiplier = 1
	}
	return price, multiplier, nil
}

// DoBuy purchases `sets` sets of a department-store item (facility=”). It
// enforces the shop stock, the per-item ownership cap (max_sets), charges the
// total price out of circulation and adds the durability to the inventory.
// remaining_uses accumulates durability×sets ('use'なら残回数, 'day'なら残日数)。
func (s *Service) DoBuy(ctx context.Context, playerID int64, facility string, itemID int64, sets int, idempotencyKey string) (*player.Player, error) {
	if sets <= 0 {
		sets = 1
	}
	price, durability, maxSets, err := s.loadItemBuy(ctx, facility, itemID)
	if err != nil {
		return nil, err
	}
	if sets > maxSets {
		return nil, &ConditionError{Message: fmt.Sprintf("一度に購入できるのは%dセットまでです。", maxSets)}
	}
	total := price * int64(sets)
	add := durability * sets
	return s.runAction(ctx, playerID, "buy", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if state.Money < total {
			return &ConditionError{Message: "お金が足りません。"}
		}
		// 所持上限: 追加後の残量が max_sets×durability を超えないこと。
		var current int
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(remaining_uses, 0) FROM player_items WHERE player_id = $1 AND item_id = $2`,
			playerID, itemID).Scan(&current); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read remaining_uses: %w", err)
		}
		if current+add > maxSets*durability {
			return &ConditionError{Message: fmt.Sprintf("これ以上は持てません(最大%dセット)。", maxSets)}
		}
		// 所持種類の上限(旧TOWN 25品目)。まだ持っていない種類は、上限に達していると買えない。
		if limit := s.settings.Get().ItemKindLimit; limit > 0 && current == 0 {
			var kinds int
			if err := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM player_items WHERE player_id = $1 AND remaining_uses > 0`,
				playerID).Scan(&kinds); err != nil {
				return fmt.Errorf("count item kinds: %w", err)
			}
			if kinds >= limit {
				return &ConditionError{Message: fmt.Sprintf("持てる所有物は%d品目までです。", limit)}
			}
		}
		// 在庫を減らす(デパートは facility=''、自販機は 'hanbai')。
		if err := s.consumeStock(ctx, tx, facility, itemID, sets); err != nil {
			return err
		}
		if err := s.ledger.PostTx(ctx, tx, "buy", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -total},
			{Account: ledger.SystemAccount("shop_sink"), Delta: total},
		}); err != nil {
			return fmt.Errorf("pay: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (player_id, item_id)
			 DO UPDATE SET quantity = player_items.quantity + $3,
			               remaining_uses = player_items.remaining_uses + $4,
			               updated_at = now()`,
			playerID, itemID, sets, add); err != nil {
			return fmt.Errorf("grant item: %w", err)
		}
		return nil
	})
}

// DoUse consumes one unit of a held item and applies its effect. Using an item
// is blocked while the item's use interval (クールタイム) has not elapsed.
// NOTE: レガシーと異なり、売却はクールタイム中でも可能とする(売却実装時は間隔チェック不要)。
func (s *Service) DoUse(ctx context.Context, playerID, itemID int64, idempotencyKey string) (*player.Player, error) {
	eff, intervalMin, fillsSatiety, durUnit, err := s.loadItemUse(ctx, itemID)
	if err != nil {
		return nil, err
	}
	return s.runAction(ctx, playerID, "use", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if !s.settings.Get().DebugNoCooldown && fillsSatiety && state.Params["satiety"].Value >= satietyMax {
			return &ConditionError{Message: "お腹がいっぱいです。今は食べられません。"}
		}
		if !s.settings.Get().DebugNoCooldown && intervalMin > 0 {
			var lastUsed *time.Time
			err := tx.QueryRow(ctx,
				`SELECT last_used_at FROM player_items WHERE player_id = $1 AND item_id = $2`,
				playerID, itemID).Scan(&lastUsed)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("read last_used_at: %w", err)
			}
			if lastUsed != nil {
				next := lastUsed.Add(time.Duration(intervalMin) * time.Minute)
				if time.Now().Before(next) {
					remain := int(time.Until(next).Minutes()) + 1
					return &ConditionError{Message: fmt.Sprintf("まだ使用できません。あと約%d分お待ちください。", remain)}
				}
			}
		}
		// 'use'単位は使用ごとに残回数-1。'day'単位は日数経過で減るため使用では減らさない。
		var affected int64
		if durUnit == "day" {
			tag, err := tx.Exec(ctx,
				`UPDATE player_items SET last_used_at = now(), updated_at = now()
				 WHERE player_id = $1 AND item_id = $2 AND remaining_uses > 0`, playerID, itemID)
			if err != nil {
				return fmt.Errorf("use item: %w", err)
			}
			affected = tag.RowsAffected()
		} else {
			tag, err := tx.Exec(ctx,
				`UPDATE player_items pi
				 SET remaining_uses = pi.remaining_uses - 1,
				     quantity = CEIL((pi.remaining_uses - 1)::numeric / ci.durability),
				     last_used_at = now(), updated_at = now()
				 FROM content_items ci
				 WHERE pi.player_id = $1 AND pi.item_id = $2 AND pi.item_id = ci.id AND pi.remaining_uses > 0`,
				playerID, itemID)
			if err != nil {
				return fmt.Errorf("consume item: %w", err)
			}
			affected = tag.RowsAffected()
		}
		if affected == 0 {
			return &ConditionError{Message: "そのアイテムを持っていません。"}
		}
		if err := s.applyEffect(ctx, tx, playerID, "use", eff, state); err != nil {
			return err
		}
		// アイテムにカロリーがあれば(高級弁当など)体重が増える。
		if err := addWeightFromItem(ctx, tx, playerID, itemID); err != nil {
			return err
		}
		// 残量が尽きたら持ち物から削除する。
		if _, err := tx.Exec(ctx,
			`DELETE FROM player_items WHERE player_id = $1 AND item_id = $2 AND remaining_uses <= 0`,
			playerID, itemID); err != nil {
			return fmt.Errorf("drop empty item: %w", err)
		}
		if fillsSatiety {
			return fillSatiety(ctx, tx, playerID)
		}
		return nil
	})
}

// DoEat eats a food from the 食堂 menu: it charges the price and applies the
// food's param effect. Eating fills 満腹度 to full and is blocked while already
// full (満腹). 食事の間隔は満腹→時間経過で下がることで自然に生じる。
func (s *Service) DoEat(ctx context.Context, playerID, foodID int64, idempotencyKey string) (*player.Player, error) {
	price, eff, err := s.loadFood(ctx, foodID)
	if err != nil {
		return nil, err
	}
	return s.runAction(ctx, playerID, "eat", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if !s.settings.Get().DebugNoCooldown && state.Params["satiety"].Value >= satietyMax {
			return &ConditionError{Message: "お腹がいっぱいです。今は食べられません。"}
		}
		if state.Money < price {
			return &ConditionError{Message: "お金が足りません。"}
		}
		// 食堂メニューの在庫を1食ぶん減らす(売り切れなら食事不可)。
		if err := s.consumeStock(ctx, tx, "syokudou", foodID, 1); err != nil {
			return err
		}
		if err := s.ledger.PostTx(ctx, tx, "eat", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -price},
			{Account: ledger.SystemAccount("shop_sink"), Delta: price},
		}); err != nil {
			return fmt.Errorf("pay: %w", err)
		}
		if err := s.applyEffect(ctx, tx, playerID, "eat", eff, state); err != nil {
			return err
		}
		// 食事のカロリーぶん体重が増える。
		if err := addWeightFromItem(ctx, tx, playerID, foodID); err != nil {
			return err
		}
		return fillSatiety(ctx, tx, playerID)
	})
}

func (s *Service) loadFood(ctx context.Context, foodID int64) (int64, effects.Effect, error) {
	var (
		price   int64
		effJSON []byte
	)
	cond, extra := s.dailyMenuCond("syokudou", 2)
	args := append([]any{foodID}, extra...)
	err := s.pool.QueryRow(ctx,
		`SELECT price, effect FROM content_items
		 WHERE id = $1 AND enabled AND facility = 'syokudou'`+cond, args...).Scan(&price, &effJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, effects.Effect{}, ErrItemNotFound
	}
	if err != nil {
		return 0, effects.Effect{}, fmt.Errorf("load food: %w", err)
	}
	eff, err := effects.ParseEffect(effJSON)
	if err != nil {
		return 0, effects.Effect{}, fmt.Errorf("food %d effect: %w", foodID, err)
	}
	return price, eff, nil
}

// DoFacilityAction runs a generic facility menu action (e.g. ジムのトレーニング):
// it enforces a per-(player,facility) cooldown, checks power, charges the price,
// applies the effect (param上昇 + power消費), and advances the cooldown by the
// menu item's interval.
func (s *Service) DoFacilityAction(ctx context.Context, playerID int64, facility string, menuID int64, idempotencyKey string) (*player.Player, error) {
	price, eff, intervalMin, err := s.loadFacilityMenuItem(ctx, facility, menuID)
	if err != nil {
		return nil, err
	}
	return s.runAction(ctx, playerID, facility, idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var nextAt *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT next_available_at FROM player_facility_cooldowns WHERE player_id = $1 AND facility = $2`,
			playerID, facility).Scan(&nextAt); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read cooldown: %w", err)
		}
		if !s.settings.Get().DebugNoCooldown && nextAt != nil && time.Now().Before(*nextAt) {
			remain := int(time.Until(*nextAt).Minutes()) + 1
			return &ConditionError{Message: fmt.Sprintf("まだ利用できません。あと約%d分お待ちください。", remain)}
		}
		if param, short := eff.InsufficientParam(state); short {
			return &ConditionError{Message: paramShortMessage(param)}
		}
		if state.Money < price {
			return &ConditionError{Message: "お金が足りません。"}
		}
		if err := s.ledger.PostTx(ctx, tx, facility, "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -price},
			{Account: ledger.SystemAccount(facility + "_sink"), Delta: price},
		}); err != nil {
			return fmt.Errorf("pay: %w", err)
		}
		if err := s.applyEffect(ctx, tx, playerID, facility, eff, state); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_facility_cooldowns (player_id, facility, next_available_at)
			 VALUES ($1, $2, now() + make_interval(mins => $3))
			 ON CONFLICT (player_id, facility)
			 DO UPDATE SET next_available_at = now() + make_interval(mins => $3)`,
			playerID, facility, intervalMin); err != nil {
			return fmt.Errorf("set cooldown: %w", err)
		}
		return nil
	})
}

func (s *Service) loadFacilityMenuItem(ctx context.Context, facility string, menuID int64) (int64, effects.Effect, int, error) {
	var (
		price       int64
		effJSON     []byte
		intervalMin int
	)
	err := s.pool.QueryRow(ctx,
		`SELECT price, effect, use_interval_min FROM content_items
		 WHERE id = $1 AND enabled AND facility = $2`, menuID, facility).
		Scan(&price, &effJSON, &intervalMin)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, effects.Effect{}, 0, ErrItemNotFound
	}
	if err != nil {
		return 0, effects.Effect{}, 0, fmt.Errorf("load facility item: %w", err)
	}
	eff, err := effects.ParseEffect(effJSON)
	if err != nil {
		return 0, effects.Effect{}, 0, fmt.Errorf("facility item %d effect: %w", menuID, err)
	}
	return price, eff, intervalMin, nil
}

// nextDayBoundary returns the wall-clock instant of the next game-day rollover
// (used for once-per-game-day facilities like the school). The game day rolls
// over at dayBoundaryHour local time.
func (s *Service) nextDayBoundary(now time.Time) time.Time {
	start := gametime.Date(now, s.loc, s.dayBoundaryHour) // 当日ゲーム日の 00:00(loc)
	return start.AddDate(0, 0, 1).Add(time.Duration(s.dayBoundaryHour) * time.Hour)
}

// DoSchool attends a school course: raises brain parameters once per game day,
// consuming money and brain power (nou_energy). Legacy: command.pl sub do_school.
// The daily limit reuses player_facility_cooldowns by setting next_available_at
// to the next game-day boundary.
func (s *Service) DoSchool(ctx context.Context, playerID, courseID int64, idempotencyKey string) (*player.Player, error) {
	price, eff, _, err := s.loadFacilityMenuItem(ctx, "school", courseID)
	if err != nil {
		return nil, err
	}
	return s.runAction(ctx, playerID, "school", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		// 1日1回(ゲーム日境界でリセット)。前回受講時に次境界をcooldownへ入れているため、
		// それが未来なら本日分は受講済み。
		var nextAt *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT next_available_at FROM player_facility_cooldowns WHERE player_id = $1 AND facility = 'school'`,
			playerID).Scan(&nextAt); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read school cooldown: %w", err)
		}
		if !s.settings.Get().DebugNoCooldown && nextAt != nil && time.Now().Before(*nextAt) {
			return &ConditionError{Message: "今日の受講は終了しています。受講できるのは1日1回です。"}
		}
		if param, short := eff.InsufficientParam(state); short {
			return &ConditionError{Message: paramShortMessage(param)}
		}
		if state.Money < price {
			return &ConditionError{Message: "お金が足りません。"}
		}
		if err := s.ledger.PostTx(ctx, tx, "school", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -price},
			{Account: ledger.SystemAccount("school_sink"), Delta: price},
		}); err != nil {
			return fmt.Errorf("pay school: %w", err)
		}
		if err := s.applyEffect(ctx, tx, playerID, "school", eff, state); err != nil {
			return err
		}
		next := s.nextDayBoundary(time.Now())
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_facility_cooldowns (player_id, facility, next_available_at)
			 VALUES ($1, 'school', $2)
			 ON CONFLICT (player_id, facility) DO UPDATE SET next_available_at = $2`,
			playerID, next); err != nil {
			return fmt.Errorf("set school cooldown: %w", err)
		}
		return nil
	})
}

// ErrBadStock is returned for an unknown symbol or non-positive quantity.
var ErrBadStock = errors.New("invalid stock symbol or quantity")

// DoBuyStock buys qty shares of a symbol at the current shared price. Money moves
// through the ledger; there are no fees (legacy). Per-symbol cap is 200 shares.
// Note: legacy also gated on bank>=cost (a likely bug); we gate on money only.
func (s *Service) DoBuyStock(ctx context.Context, playerID int64, symbol string, qty int, idempotencyKey string) (*player.Player, error) {
	if !stock.ValidSymbol(symbol) || qty <= 0 {
		return nil, ErrBadStock
	}
	return s.runAction(ctx, playerID, "stock_buy", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var price int64
		if err := tx.QueryRow(ctx, `SELECT price FROM stock_price WHERE symbol = $1`, symbol).Scan(&price); err != nil {
			return fmt.Errorf("read price: %w", err)
		}
		var shares int
		var costTotal int64
		err := tx.QueryRow(ctx,
			`SELECT shares, cost_total FROM player_stock WHERE player_id = $1 AND symbol = $2`,
			playerID, symbol).Scan(&shares, &costTotal)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read holding: %w", err)
		}
		if shares+qty > stock.MaxShares {
			return &ConditionError{Message: fmt.Sprintf("この銘柄の保有上限(%d株)を超えています。", stock.MaxShares)}
		}
		cost := price * int64(qty)
		if state.Money < cost {
			return &ConditionError{Message: "持ち金が足りません。"}
		}
		// refは空。冪等性はrunActionのidempotencyKeyが担う(refに銘柄名を使うと
		// 別取引が同一refで衝突しno-opになるため)。
		if err := s.ledger.PostTx(ctx, tx, "stock_buy", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -cost},
			{Account: ledger.SystemAccount("stock_market"), Delta: cost},
		}); err != nil {
			return fmt.Errorf("pay: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_stock (player_id, symbol, shares, cost_total, inv_total, ret_total)
			 VALUES ($1, $2, $3, $4, $4, 0)
			 ON CONFLICT (player_id, symbol) DO UPDATE SET
			   shares = player_stock.shares + $3,
			   cost_total = player_stock.cost_total + $4,
			   inv_total = player_stock.inv_total + $4`,
			playerID, symbol, qty, cost); err != nil {
			return fmt.Errorf("upsert holding: %w", err)
		}
		return s.logStockTrade(ctx, tx, playerID,
			fmt.Sprintf("%s株を%d株購入(@%d円 計%d円)", symbol, qty, price, cost))
	})
}

// DoSellStock sells qty shares of a symbol at the current price. Cost basis is
// reduced proportionally; when the position hits zero the cost basis clears.
func (s *Service) DoSellStock(ctx context.Context, playerID int64, symbol string, qty int, idempotencyKey string) (*player.Player, error) {
	if !stock.ValidSymbol(symbol) || qty <= 0 {
		return nil, ErrBadStock
	}
	return s.runAction(ctx, playerID, "stock_sell", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var price int64
		if err := tx.QueryRow(ctx, `SELECT price FROM stock_price WHERE symbol = $1`, symbol).Scan(&price); err != nil {
			return fmt.Errorf("read price: %w", err)
		}
		var shares int
		var costTotal int64
		err := tx.QueryRow(ctx,
			`SELECT shares, cost_total FROM player_stock WHERE player_id = $1 AND symbol = $2`,
			playerID, symbol).Scan(&shares, &costTotal)
		if errors.Is(err, pgx.ErrNoRows) || shares < qty {
			return &ConditionError{Message: "保有株数が足りません。"}
		}
		if err != nil {
			return fmt.Errorf("read holding: %w", err)
		}
		proceeds := price * int64(qty)
		costReduce := costTotal * int64(qty) / int64(shares) // 平均原価ぶんを比例で減らす
		newShares := shares - qty
		newCost := costTotal - costReduce
		if newShares == 0 {
			newCost = 0
		}
		if err := s.ledger.PostTx(ctx, tx, "stock_sell", "", []ledger.Entry{
			{Account: ledger.SystemAccount("stock_market"), Delta: -proceeds},
			{Account: ledger.PlayerAccount(playerID), Delta: proceeds},
		}); err != nil {
			return fmt.Errorf("payout: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE player_stock SET shares = $3, cost_total = $4, ret_total = ret_total + $5
			 WHERE player_id = $1 AND symbol = $2`,
			playerID, symbol, newShares, newCost, proceeds); err != nil {
			return fmt.Errorf("update holding: %w", err)
		}
		return s.logStockTrade(ctx, tx, playerID,
			fmt.Sprintf("%s株を%d株売却(@%d円 計%d円)", symbol, qty, price, proceeds))
	})
}

// DoSettleStock liquidates all holdings at current prices, pays the player, and
// clears their positions (legacy command=del 精算).
func (s *Service) DoSettleStock(ctx context.Context, playerID int64, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "stock_settle", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		var total int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(sp.price * ps.shares), 0)
			 FROM player_stock ps JOIN stock_price sp ON sp.symbol = ps.symbol
			 WHERE ps.player_id = $1 AND ps.shares > 0`, playerID).Scan(&total); err != nil {
			return fmt.Errorf("sum holdings: %w", err)
		}
		if total > 0 {
			if err := s.ledger.PostTx(ctx, tx, "stock_settle", "", []ledger.Entry{
				{Account: ledger.SystemAccount("stock_market"), Delta: -total},
				{Account: ledger.PlayerAccount(playerID), Delta: total},
			}); err != nil {
				return fmt.Errorf("payout: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM player_stock WHERE player_id = $1`, playerID); err != nil {
			return fmt.Errorf("clear holdings: %w", err)
		}
		return s.logStockTrade(ctx, tx, playerID, fmt.Sprintf("%d円の精算を行いました。", total))
	})
}

// logStockTrade appends a trade message and trims the player's history.
func (s *Service) logStockTrade(ctx context.Context, tx pgx.Tx, playerID int64, msg string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO stock_trade_log (player_id, message) VALUES ($1, $2)`, playerID, msg); err != nil {
		return fmt.Errorf("log trade: %w", err)
	}
	_, err := tx.Exec(ctx,
		`DELETE FROM stock_trade_log WHERE player_id = $1 AND id NOT IN (
		   SELECT id FROM stock_trade_log WHERE player_id = $1 ORDER BY id DESC LIMIT $2)`,
		playerID, stock.TradeLogKeep)
	return err
}

// KeibaBetResult is the outcome of a horse-race bet, returned alongside the
// updated player.
type KeibaBetResult struct {
	WinnerIndex int           `json:"winner_index"`
	WinnerName  string        `json:"winner_name"`
	WinnerOdds  int           `json:"winner_odds"`
	Payout      int64         `json:"payout"`   // 払戻金
	Invested    int64         `json:"invested"` // 総購入額
	Steps       [][]int       `json:"steps"`    // 各馬の歩幅列(アニメ用)
	Lineup      []keiba.Horse `json:"lineup"`
}

// ErrBadBet is returned for a malformed or stale bet.
var ErrBadBet = errors.New("invalid or stale keiba bet")

// DoKeibaBet places bets on the player's pending race, runs the server-side
// simulation, pays out winnings and updates the ranking. bets holds tickets per
// horse index (length keiba.Runners). Legacy: keiba.cgi command=start.
func (s *Service) DoKeibaBet(ctx context.Context, playerID, raceID int64, bets []int, idempotencyKey string) (*player.Player, *KeibaBetResult, error) {
	if len(bets) != keiba.Runners {
		return nil, nil, ErrBadBet
	}
	total, horsesBet := 0, 0
	for _, t := range bets {
		if t < 0 {
			return nil, nil, ErrBadBet
		}
		total += t
		if t > 0 {
			horsesBet++
		}
	}

	var result *KeibaBetResult
	p, err := s.runAction(ctx, playerID, "keiba_bet", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		storedRace, lineup, err := keiba.LoadRace(ctx, tx, playerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "レース情報がありません。画面を開き直してください。"}
		}
		if err != nil {
			return fmt.Errorf("load race: %w", err)
		}
		if storedRace != raceID {
			return &ConditionError{Message: "レース情報が古くなっています。画面を開き直してください。"}
		}
		if total == 0 {
			return &ConditionError{Message: "馬券が購入されていません。"}
		}
		if horsesBet > keiba.MaxHorsesBet {
			return &ConditionError{Message: fmt.Sprintf("%d頭までしか賭けられません。", keiba.MaxHorsesBet)}
		}
		if total > keiba.MaxTickets {
			return &ConditionError{Message: fmt.Sprintf("一度に購入できる馬券は%d枚までです。", keiba.MaxTickets)}
		}
		cost := int64(total) * keiba.TicketPrice
		if state.Money < cost {
			return &ConditionError{Message: "お金が足りません。"}
		}
		if err := s.ledger.PostTx(ctx, tx, "keiba_bet", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -cost},
			{Account: ledger.SystemAccount("keiba"), Delta: cost},
		}); err != nil {
			return fmt.Errorf("pay: %w", err)
		}

		race := keiba.Simulate(lineup, s.rng)
		win := race.WinnerIndex
		var payout int64
		if bets[win] > 0 {
			payout = int64(lineup[win].Odds) * int64(bets[win]) * keiba.TicketPrice
		}
		if payout > 0 {
			if err := s.ledger.PostTx(ctx, tx, "keiba_payout", "", []ledger.Entry{
				{Account: ledger.SystemAccount("keiba"), Delta: -payout},
				{Account: ledger.PlayerAccount(playerID), Delta: payout},
			}); err != nil {
				return fmt.Errorf("payout: %w", err)
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO keiba_ranking (player_id, invested, won, last_played)
			 VALUES ($1, $2, $3, now())
			 ON CONFLICT (player_id) DO UPDATE SET
			   invested = keiba_ranking.invested + $2,
			   won = keiba_ranking.won + $3,
			   last_played = now()`,
			playerID, cost, payout); err != nil {
			return fmt.Errorf("rank: %w", err)
		}
		result = &KeibaBetResult{
			WinnerIndex: win,
			WinnerName:  lineup[win].Name,
			WinnerOdds:  lineup[win].Odds,
			Payout:      payout,
			Invested:    cost,
			Steps:       race.Steps,
			Lineup:      lineup,
		}
		return nil
	})
	return p, result, err
}

// GreetResult is the outcome of posting a greeting (reward/janken/fine).
type GreetResult struct {
	Reward   int64  `json:"reward"`    // 得た報酬(宣伝は負、管理人は0)
	Jackpot  bool   `json:"jackpot"`   // 大当たり(base==7)
	Janken   string `json:"janken"`    // 勝ち/負け/あいこ/""
	JankenPC string `json:"janken_pc"` // PCの手 グー/チョキ/パー/""
	Fine     bool   `json:"fine"`      // NG罰金
}

// ErrBadGreet is returned for invalid greeting input.
var ErrBadGreet = errors.New("invalid greeting")

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

var jankenHands = map[string]int{"gu": 0, "choki": 1, "pa": 2}
var jankenNames = []string{"グー", "チョキ", "パー"}

// DoGreet posts a greeting to the town board and applies its game effects:
// a random reward for normal posts (jackpot on base==7, doubled/halved by a
// rock-paper-scissors result), a 20000 charge for 宣伝, and a 30000 fine for NG
// words. Legacy: event.pl sub aisatu (command=aisatu_do).
func (s *Service) DoGreet(ctx context.Context, playerID int64, category, body, color, janken, idempotencyKey string) (*player.Player, *GreetResult, error) {
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > greeting.MaxBodyRunes {
		return nil, nil, ErrBadGreet
	}
	if color == "" {
		color = "#333333"
	}
	if !colorRe.MatchString(color) {
		return nil, nil, ErrBadGreet
	}
	// NGワードは本文をマスクし罰金フラグを立てる。
	fine := false
	for _, ng := range greeting.NGWords {
		if strings.Contains(body, ng) {
			body = strings.ReplaceAll(body, ng, "NG")
			fine = true
		}
	}

	var result *GreetResult
	p, err := s.runAction(ctx, playerID, "greeting", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var name string
		if err := tx.QueryRow(ctx, `SELECT display_name FROM players WHERE id = $1`, playerID).Scan(&name); err != nil {
			return fmt.Errorf("player: %w", err)
		}
		res := &GreetResult{Fine: fine}

		switch category {
		case greeting.CatAdmin:
			var isAdmin bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM player_roles WHERE player_id = $1 AND role = 'admin')`, playerID).Scan(&isAdmin); err != nil {
				return fmt.Errorf("role: %w", err)
			}
			if !isAdmin {
				return &ConditionError{Message: "管理人枠は管理者のみ投稿できます。"}
			}
			color = "#ff0000"
		case greeting.CatAd:
			if state.Money < greeting.AdRevCost {
				return &ConditionError{Message: "宣伝費の2万円が足りません。"}
			}
			if err := s.ledger.PostTx(ctx, tx, "greeting_ad", "", []ledger.Entry{
				{Account: ledger.PlayerAccount(playerID), Delta: -greeting.AdRevCost},
				{Account: ledger.SystemAccount("chat"), Delta: greeting.AdRevCost},
			}); err != nil {
				return fmt.Errorf("ad charge: %w", err)
			}
			res.Reward = -greeting.AdRevCost
			color = "#0000ff"
		default:
			// ジャンケン(手が指定されていれば)で報酬倍率を決める。
			mult := 1.0
			if janken != "" && janken != "none" {
				pc := s.rng.IntN(3)
				res.JankenPC = jankenNames[pc]
				me := jankenHands[janken]
				if janken == "omakase" {
					me = s.rng.IntN(3)
				}
				switch {
				case me == pc:
					res.Janken = "あいこ"
				case (me+1)%3 == pc:
					res.Janken = "勝ち"
					mult = 2.0
				default:
					res.Janken = "負け"
					mult = 0.5
				}
			}
			base := int64(s.rng.IntN(10) + 1)
			var gain int64
			if base == 7 {
				gain = base + int64(s.rng.IntN(10000)+5000)
				res.Jackpot = true
			} else {
				gain = base + int64(s.rng.IntN(2000)+1000)
			}
			gain = int64(float64(gain) * mult)
			if gain > 0 {
				if err := s.ledger.PostTx(ctx, tx, "greeting_reward", "", []ledger.Entry{
					{Account: ledger.SystemAccount("chat"), Delta: -gain},
					{Account: ledger.PlayerAccount(playerID), Delta: gain},
				}); err != nil {
					return fmt.Errorf("reward: %w", err)
				}
			}
			res.Reward = gain
		}

		if fine {
			if err := s.ledger.PostTx(ctx, tx, "greeting_fine", "", []ledger.Entry{
				{Account: ledger.PlayerAccount(playerID), Delta: -greeting.NGFine},
				{Account: ledger.SystemAccount("chat"), Delta: greeting.NGFine},
			}); err != nil {
				return fmt.Errorf("fine: %w", err)
			}
			res.Reward -= greeting.NGFine
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO greetings (user_id, user_name, category, body, color, janken)
			 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
			playerID, name, category, body, color, res.Janken); err != nil {
			return fmt.Errorf("insert greeting: %w", err)
		}
		// 最新Keep件に切り詰める。
		if _, err := tx.Exec(ctx,
			`DELETE FROM greetings WHERE id NOT IN (SELECT id FROM greetings ORDER BY id DESC LIMIT $1)`,
			greeting.Keep); err != nil {
			return fmt.Errorf("trim greetings: %w", err)
		}
		result = res
		return nil
	})
	return p, result, err
}

// eventRollInterval rate-limits random-event rolls per player (anti-spam).
const eventRollInterval = 15 * time.Second

// DoEventRoll rolls for a random event (legacy event.pl event_happen), applying
// its money/parameter/disease/weight effects. Rolls are rate-limited to one per
// eventRollInterval per player. A nil result means no event fired (rate-limited
// or the 11/12 no-fire outcome).
func (s *Service) DoEventRoll(ctx context.Context, playerID int64, idempotencyKey string) (*player.Player, *event.Outcome, error) {
	var result *event.Outcome
	p, err := s.runAction(ctx, playerID, "event_roll", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if !s.settings.Get().DebugNoCooldown {
			var nextAt *time.Time
			if err := tx.QueryRow(ctx,
				`SELECT next_available_at FROM player_facility_cooldowns WHERE player_id = $1 AND facility = 'event_roll'`,
				playerID).Scan(&nextAt); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("read event cooldown: %w", err)
			}
			if nextAt != nil && time.Now().Before(*nextAt) {
				return nil // レート制限中: 抽選しない
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO player_facility_cooldowns (player_id, facility, next_available_at)
				 VALUES ($1, 'event_roll', now() + make_interval(secs => $2))
				 ON CONFLICT (player_id, facility) DO UPDATE SET next_available_at = now() + make_interval(secs => $2)`,
				playerID, eventRollInterval.Seconds()); err != nil {
				return fmt.Errorf("set event cooldown: %w", err)
			}
		}

		// 管理画面で追加されたカスタムイベントを組み込みプールに合流させる
		// (条件付きイベントは条件を満たすプレイヤーにだけ候補になる)。
		customs, err := s.loadCustomEvents(ctx, tx, playerID, state)
		if err != nil {
			return err
		}
		occurred, o := event.RollAll(s.rng, state.Money, state.Params["speed"].Value, customs)
		if !occurred {
			return nil
		}
		// メッセージのプレースホルダー({money}/{name}/{job}/{town})を実値に展開する。
		if msg, err := s.renderEventMessage(ctx, tx, playerID, o); err != nil {
			return err
		} else {
			o.Message = msg
		}
		if err := s.applyEventOutcome(ctx, tx, playerID, state, o); err != nil {
			return err
		}
		result = &o
		return nil
	})
	return p, result, err
}

// loadCustomEvents reads the enabled admin-defined events for the roll pool and
// filters them by their eligibility conditions (所持金/パラメータ/所持アイテム/職業)。
func (s *Service) loadCustomEvents(ctx context.Context, tx pgx.Tx, playerID int64, state effects.State) ([]event.Custom, error) {
	rows, err := tx.Query(ctx,
		`SELECT name, message, good, money_min, money_max, params, disease_set, weight_g, weight, conditions
		 FROM content_events WHERE enabled`)
	if err != nil {
		return nil, fmt.Errorf("load custom events: %w", err)
	}
	defer rows.Close()
	type candidate struct {
		c     event.Custom
		conds []content.EventCond
	}
	var cands []candidate
	needItems := map[int64]bool{}
	needJob := false
	for rows.Next() {
		var cd candidate
		if err := rows.Scan(&cd.c.Name, &cd.c.Message, &cd.c.Good, &cd.c.MoneyMin, &cd.c.MoneyMax,
			&cd.c.Params, &cd.c.DiseaseSet, &cd.c.WeightG, &cd.c.Weight, &cd.conds); err != nil {
			return nil, fmt.Errorf("scan custom event: %w", err)
		}
		for _, cond := range cd.conds {
			if cond.Pred == "has_item" {
				needItems[cond.ItemID] = true
			}
			if cond.Pred == "job_is" {
				needJob = true
			}
		}
		cands = append(cands, cd)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	// 条件評価に必要な補助データ(所持アイテム/職業)をまとめて読む。
	owned := map[int64]bool{}
	if len(needItems) > 0 {
		ids := make([]int64, 0, len(needItems))
		for id := range needItems {
			ids = append(ids, id)
		}
		irows, err := tx.Query(ctx,
			`SELECT item_id FROM player_items WHERE player_id = $1 AND item_id = ANY($2) AND remaining_uses > 0`,
			playerID, ids)
		if err != nil {
			return nil, fmt.Errorf("load owned items: %w", err)
		}
		for irows.Next() {
			var id int64
			if err := irows.Scan(&id); err != nil {
				irows.Close()
				return nil, err
			}
			owned[id] = true
		}
		irows.Close()
		if err := irows.Err(); err != nil {
			return nil, err
		}
	}
	job := ""
	if needJob {
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(job, '') FROM player_status WHERE player_id = $1`, playerID).Scan(&job); err != nil {
			return nil, fmt.Errorf("load job: %w", err)
		}
	}
	var out []event.Custom
	for _, cd := range cands {
		if eventCondsPass(cd.conds, state, owned, job) {
			out = append(out, cd.c)
		}
	}
	return out, nil
}

// renderEventMessage expands message placeholders with live values:
// {money}=増減額の絶対値(カンマ区切り) {name}=プレイヤー名 {job}=職業 {town}=今いる街名。
func (s *Service) renderEventMessage(ctx context.Context, tx pgx.Tx, playerID int64, o event.Outcome) (string, error) {
	msg := o.Message
	if !strings.Contains(msg, "{") {
		return msg, nil
	}
	if strings.Contains(msg, "{money}") {
		amount := o.MoneyDelta
		if amount < 0 {
			amount = -amount
		}
		msg = strings.ReplaceAll(msg, "{money}", yenComma(amount))
	}
	if strings.Contains(msg, "{name}") || strings.Contains(msg, "{job}") || strings.Contains(msg, "{town}") {
		var (
			name string
			job  string
			town int
		)
		if err := tx.QueryRow(ctx,
			`SELECT p.display_name, COALESCE(ps.job, ''), p.current_town
			 FROM players p LEFT JOIN player_status ps ON ps.player_id = p.id
			 WHERE p.id = $1`, playerID).Scan(&name, &job, &town); err != nil {
			return "", fmt.Errorf("load player for message: %w", err)
		}
		msg = strings.ReplaceAll(msg, "{name}", name)
		msg = strings.ReplaceAll(msg, "{job}", job)
		townName := ""
		for _, t := range building.Towns() {
			if t.No == town {
				townName = t.Name
				break
			}
		}
		msg = strings.ReplaceAll(msg, "{town}", townName)
	}
	return msg, nil
}

// yenComma formats an amount with ja-JP style thousands separators.
func yenComma(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// eventCondsPass reports whether every condition holds for the player.
func eventCondsPass(conds []content.EventCond, state effects.State, owned map[int64]bool, job string) bool {
	for _, c := range conds {
		switch c.Pred {
		case "money_gte":
			if state.Money < c.Value {
				return false
			}
		case "money_lte":
			if state.Money > c.Value {
				return false
			}
		case "param_gte":
			if int64(state.Params[c.Param].Value) < c.Value {
				return false
			}
		case "param_lte":
			if int64(state.Params[c.Param].Value) > c.Value {
				return false
			}
		case "has_item":
			if !owned[c.ItemID] {
				return false
			}
		case "job_is":
			if job != c.Job {
				return false
			}
		default:
			return false // 未知の条件は安全側に倒して発生させない
		}
	}
	return true
}

// applyEventOutcome persists one event's effects: money via the ledger, params
// via the effect engine, disease/weight directly, and the special charity /
// confiscate side effects.
func (s *Service) applyEventOutcome(ctx context.Context, tx pgx.Tx, playerID int64, state effects.State, o event.Outcome) error {
	switch o.Special {
	case "charity":
		// ログイン中に関係なく、自分以外のランダムなプレイヤーの貯金へ1万円贈る。
		var recipient int64
		err := tx.QueryRow(ctx,
			`SELECT id FROM players WHERE id <> $1 AND deleted_at IS NULL ORDER BY random() LIMIT 1`, playerID).Scan(&recipient)
		if errors.Is(err, pgx.ErrNoRows) {
			o.MoneyDelta = 0 // 相手がいなければ何もしない
		} else if err != nil {
			return fmt.Errorf("charity recipient: %w", err)
		} else if state.Money >= 10000 {
			if err := s.ledger.PostTx(ctx, tx, "event_charity", "", []ledger.Entry{
				{Account: ledger.PlayerAccount(playerID), Delta: -10000},
				{Account: ledger.SavingsAccount(recipient), Delta: 10000},
			}); err != nil {
				return fmt.Errorf("charity: %w", err)
			}
		} else {
			o.MoneyDelta = 0 // 持ち金不足なら贈らない
		}
	case "confiscate":
		// 所持品からランダムに1個(数量-1)。無ければ何もしない。
		if _, err := tx.Exec(ctx,
			`UPDATE player_items SET quantity = quantity - 1
			 WHERE (player_id, item_id) = (
			   SELECT player_id, item_id FROM player_items
			   WHERE player_id = $1 AND quantity > 0 ORDER BY random() LIMIT 1)`, playerID); err != nil {
			return fmt.Errorf("confiscate: %w", err)
		}
	default:
		if o.MoneyDelta != 0 {
			if err := s.ledger.PostTx(ctx, tx, "event_money", "", []ledger.Entry{
				{Account: ledger.PlayerAccount(playerID), Delta: o.MoneyDelta},
				{Account: ledger.SystemAccount("event"), Delta: -o.MoneyDelta},
			}); err != nil {
				return fmt.Errorf("event money: %w", err)
			}
		}
	}

	if len(o.Params) > 0 {
		eff := effects.Effect{Ops: make([]effects.Op, 0, len(o.Params))}
		for k, v := range o.Params {
			eff.Ops = append(eff.Ops, effects.Op{Kind: "add_param", Param: k, Amount: int64(v)})
		}
		if err := s.applyEffect(ctx, tx, playerID, "event", eff, state); err != nil {
			return err
		}
	}
	if o.DiseaseSet != nil {
		// レガシーは加算ではなく代入($byouki_sisuu = N)。健康の貯金があっても
		// 即その病状になる(悪い病気が軽くなる方向もレガシーどおり許す)。
		if _, err := tx.Exec(ctx,
			`UPDATE player_status SET disease_index = $2 WHERE player_id = $1`,
			playerID, *o.DiseaseSet); err != nil {
			return fmt.Errorf("event disease: %w", err)
		}
	}
	if o.WeightG != 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE player_status SET weight_g = GREATEST(1000, weight_g + $2) WHERE player_id = $1`,
			playerID, o.WeightG); err != nil {
			return fmt.Errorf("event weight: %w", err)
		}
	}

	// 役場のイベント履歴に残す。イベントは頻度が高いので既定では本人の履歴だけに
	// 入れ、高額(TownWideMoneyThreshold以上)のものだけ街のニュースへ昇格させる
	// (レガシーは「地震」「運用」という大金の動くイベントだけを news_kiroku していた)。
	name, err := news.ActorName(ctx, tx, playerID)
	if err != nil {
		return err
	}
	good := o.Good
	townWide := o.MoneyDelta >= news.TownWideMoneyThreshold || o.MoneyDelta <= -news.TownWideMoneyThreshold
	return news.RecordFor(ctx, tx, news.KindEvent, playerID, name,
		fmt.Sprintf("%sさん：%s", name, o.Message), &good, townWide)
}

// cleagueMatchInterval rate-limits battles per player (legacy 疲労クールタイム).
const cleagueMatchInterval = 5 * time.Minute

// DoSetCharacterName creates or renames the player's battle character (free).
func (s *Service) DoSetCharacterName(ctx context.Context, playerID int64, name, idempotencyKey string) (*player.Player, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 30 {
		return nil, &ConditionError{Message: "キャラ名は1〜30文字で入力してください。"}
	}
	return s.runAction(ctx, playerID, "cleague_name", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO battle_characters (owner_id, name, abilities) VALUES ($1, $2, '{}')
			 ON CONFLICT (owner_id) DO UPDATE SET name = $2`, playerID, name)
		return err
	})
}

// DoGrowCharacter feeds the player's parameters and money into their character:
// each ability transfers the input value from the owner (owner-param > input
// required) and costs input×10000 yen. Legacy: make_chara.
func (s *Service) DoGrowCharacter(ctx context.Context, playerID int64, inputs map[string]int, idempotencyKey string) (*player.Player, error) {
	for k, v := range inputs {
		if !cleague.IsAbility(k) || v < 0 {
			return nil, &ConditionError{Message: "入力が不正です。"}
		}
	}
	return s.runAction(ctx, playerID, "cleague_grow", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		var abJSON []byte
		if err := tx.QueryRow(ctx, `SELECT abilities FROM battle_characters WHERE owner_id = $1`, playerID).Scan(&abJSON); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &ConditionError{Message: "先にキャラを作成してください。"}
			}
			return fmt.Errorf("character: %w", err)
		}
		abilities := map[string]int{}
		_ = json.Unmarshal(abJSON, &abilities)

		var totalUnits int
		eff := effects.Effect{}
		for k, v := range inputs {
			if v <= 0 {
				continue
			}
			if state.Params[k].Value <= v {
				return &ConditionError{Message: fmt.Sprintf("%sが足りません(本人の値より小さい値を入力)。", k)}
			}
			totalUnits += v
			abilities[k] += v
			eff.Ops = append(eff.Ops, effects.Op{Kind: "add_param", Param: k, Amount: int64(-v)})
		}
		if totalUnits == 0 {
			return &ConditionError{Message: "育成する能力を入力してください。"}
		}
		cost := int64(totalUnits) * 10000
		if state.Money < cost {
			return &ConditionError{Message: "お金が足りません。"}
		}
		if err := s.ledger.PostTx(ctx, tx, "cleague_grow", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -cost},
			{Account: ledger.SystemAccount("cleague"), Delta: cost},
		}); err != nil {
			return fmt.Errorf("pay: %w", err)
		}
		if err := s.applyEffect(ctx, tx, playerID, "cleague", eff, state); err != nil {
			return err
		}
		newAb, _ := json.Marshal(abilities)
		if _, err := tx.Exec(ctx, `UPDATE battle_characters SET abilities = $2 WHERE owner_id = $1`, playerID, newAb); err != nil {
			return fmt.Errorf("update character: %w", err)
		}
		return nil
	})
}

// DoBattle fights the player's character against an opponent's, records the
// result for both, and returns the round-by-round outcome. Legacy: c_league/battle.
func (s *Service) DoBattle(ctx context.Context, playerID, opponentID int64, idempotencyKey string) (*player.Player, *cleague.BattleResult, error) {
	if playerID == opponentID {
		return nil, nil, &ConditionError{Message: "自分とは対戦できません。"}
	}
	var result *cleague.BattleResult
	p, err := s.runAction(ctx, playerID, "cleague_battle", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		mine, err := loadAbilities(ctx, tx, playerID)
		if err != nil {
			return err
		}
		if mine == nil {
			return &ConditionError{Message: "先にキャラを作成してください。"}
		}
		opp, err := loadAbilities(ctx, tx, opponentID)
		if err != nil {
			return err
		}
		if opp == nil {
			return &ConditionError{Message: "相手のキャラがいません。"}
		}
		if !s.settings.Get().DebugNoCooldown {
			var last *time.Time
			if err := tx.QueryRow(ctx, `SELECT last_match_at FROM battle_characters WHERE owner_id = $1`, playerID).Scan(&last); err != nil {
				return err
			}
			if last != nil && time.Since(*last) < cleagueMatchInterval {
				return &ConditionError{Message: "キャラが疲れています。少し休ませてください。"}
			}
		}
		res := cleague.Battle(mine, opp, s.rng)
		result = &res
		// 戦績更新。
		switch res.Winner {
		case "a":
			if err := bumpRecord(ctx, tx, playerID, "wins"); err != nil {
				return err
			}
			if err := bumpRecord(ctx, tx, opponentID, "losses"); err != nil {
				return err
			}
		case "b":
			if err := bumpRecord(ctx, tx, playerID, "losses"); err != nil {
				return err
			}
			if err := bumpRecord(ctx, tx, opponentID, "wins"); err != nil {
				return err
			}
		default:
			if err := bumpRecord(ctx, tx, playerID, "draws"); err != nil {
				return err
			}
			if err := bumpRecord(ctx, tx, opponentID, "draws"); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE battle_characters SET last_match_at = now() WHERE owner_id = $1`, playerID)
		return err
	})
	return p, result, err
}

func loadAbilities(ctx context.Context, tx pgx.Tx, ownerID int64) (map[string]int, error) {
	var abJSON []byte
	err := tx.QueryRow(ctx, `SELECT abilities FROM battle_characters WHERE owner_id = $1`, ownerID).Scan(&abJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("abilities: %w", err)
	}
	ab := map[string]int{}
	_ = json.Unmarshal(abJSON, &ab)
	return ab, nil
}

func bumpRecord(ctx context.Context, tx pgx.Tx, ownerID int64, col string) error {
	// col は "wins"/"losses"/"draws" の固定値のみ(内部呼び出し)。
	_, err := tx.Exec(ctx, `UPDATE battle_characters SET `+col+` = `+col+` + 1 WHERE owner_id = $1`, ownerID)
	return err
}

// jobEconomy holds a job's take-requirements and pay/level rules (design 17.5).
type jobEconomy struct {
	conds         effects.Conditions
	salary        int64
	payInterval   int
	bonusRate     int
	raiseRate     int
	bodyCost      int
	nouCost       int
	rank          int
	bmiMin        *int
	bmiMax        *int
	heightMin     *int
	requireMaster *string
}

func (s *Service) loadJobEconomy(ctx context.Context, name string) (jobEconomy, error) {
	var (
		reqJSON []byte
		e       jobEconomy
	)
	err := s.pool.QueryRow(ctx,
		`SELECT requirements, salary, pay_interval, bonus_rate, raise_rate,
		        body_cost, nou_cost, rank, bmi_min, bmi_max, height_min, require_master
		 FROM content_jobs WHERE name = $1 AND enabled`, name).
		Scan(&reqJSON, &e.salary, &e.payInterval, &e.bonusRate, &e.raiseRate,
			&e.bodyCost, &e.nouCost, &e.rank, &e.bmiMin, &e.bmiMax, &e.heightMin, &e.requireMaster)
	if errors.Is(err, pgx.ErrNoRows) {
		return jobEconomy{}, fmt.Errorf("job %q not found", name)
	}
	if err != nil {
		return jobEconomy{}, fmt.Errorf("load job: %w", err)
	}
	conds, err := effects.ParseConditions(reqJSON)
	if err != nil {
		return jobEconomy{}, fmt.Errorf("job %q requirements: %w", name, err)
	}
	e.conds = conds
	return e, nil
}

// loadItemBuy returns a purchasable department-store item's price, durability
// and per-item ownership cap (max_sets). Only facility=” items are sellable at
// the department store; 食堂(syokudou)は DoEat、ジム/温泉は DoFacilityAction 経由。
func (s *Service) loadItemBuy(ctx context.Context, facility string, itemID int64) (price int64, durability, maxSets int, err error) {
	cond, extra := s.dailyMenuCond(facility, 3)
	args := append([]any{itemID, facility}, extra...)
	err = s.pool.QueryRow(ctx,
		`SELECT price, durability, max_sets FROM content_items
		 WHERE id = $1 AND enabled AND facility = $2`+cond, args...).Scan(&price, &durability, &maxSets)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, 0, ErrItemNotFound
	}
	if err != nil {
		return 0, 0, 0, fmt.Errorf("load item buy: %w", err)
	}
	return price, durability, maxSets, nil
}

// loadItemUse returns an item's use-effect, its use interval (minutes), whether
// using it fills 満腹度 (food items), and its durability unit ('use'/'day').
func (s *Service) loadItemUse(ctx context.Context, itemID int64) (effects.Effect, int, bool, string, error) {
	var (
		effJSON      []byte
		intervalMin  int
		fillsSatiety bool
		durUnit      string
	)
	// enabledは購入カタログの掲載可否であり、所持済みアイテムの使用は妨げない
	// (マスタを無効化しても手持ちが使えなくならないように)。
	// 食べ物カテゴリはフラグの設定漏れがあっても満腹化する(カテゴリ由来で自動判定)。
	err := s.pool.QueryRow(ctx,
		`SELECT effect, use_interval_min,
		        (fills_satiety OR category IN ('食料品', 'ファーストフード')),
		        durability_unit
		 FROM content_items WHERE id = $1`,
		itemID).Scan(&effJSON, &intervalMin, &fillsSatiety, &durUnit)
	if errors.Is(err, pgx.ErrNoRows) {
		return effects.Effect{}, 0, false, "", ErrItemNotFound
	}
	if err != nil {
		return effects.Effect{}, 0, false, "", fmt.Errorf("load item: %w", err)
	}
	eff, err := effects.ParseEffect(effJSON)
	if err != nil {
		return effects.Effect{}, 0, false, "", fmt.Errorf("item %d effect: %w", itemID, err)
	}
	return eff, intervalMin, fillsSatiety, durUnit, nil
}

// claimIdempotency inserts the action log row. It returns duplicate=true when
// the idempotency key was already used.
func (s *Service) claimIdempotency(ctx context.Context, tx pgx.Tx, playerID int64, actionType, key string) (bool, error) {
	if key == "" {
		if _, err := tx.Exec(ctx,
			`INSERT INTO action_log (player_id, action_type) VALUES ($1, $2)`,
			playerID, actionType); err != nil {
			return false, fmt.Errorf("action_log insert: %w", err)
		}
		return false, nil
	}
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO action_log (player_id, action_type, idempotency_key)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (player_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		 RETURNING id`, playerID, actionType, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("action_log claim: %w", err)
	}
	return false, nil
}

func (s *Service) readState(ctx context.Context, tx pgx.Tx, playerID int64) (effects.State, error) {
	var (
		energy, energyMax, nou, nouMax                              int
		satiety                                                     int
		kokugo, suugaku, rika, syakai, eigo, ongaku, bijutsu        int
		looks, tairyoku, kenkou, speed, power, wanryoku, kyakuryoku int
		love, omoshirosa                                            int
	)
	err := tx.QueryRow(ctx,
		`SELECT energy, energy_max, nou_energy, nou_energy_max, satiety,
		        kokugo, suugaku, rika, syakai, eigo, ongaku, bijutsu,
		        looks, tairyoku, kenkou, speed, power, wanryoku, kyakuryoku,
		        love, omoshirosa
		 FROM player_status WHERE player_id = $1`, playerID).
		Scan(&energy, &energyMax, &nou, &nouMax, &satiety,
			&kokugo, &suugaku, &rika, &syakai, &eigo, &ongaku, &bijutsu,
			&looks, &tairyoku, &kenkou, &speed, &power, &wanryoku, &kyakuryoku,
			&love, &omoshirosa)
	if errors.Is(err, pgx.ErrNoRows) {
		return effects.State{}, player.ErrNotFound
	}
	if err != nil {
		return effects.State{}, fmt.Errorf("read status: %w", err)
	}

	var money int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
		ledger.PlayerAccount(playerID)).Scan(&money); err != nil {
		return effects.State{}, fmt.Errorf("read money: %w", err)
	}

	m := detailedParamMax
	return effects.State{
		Money: money,
		Params: map[string]effects.ParamState{
			"energy":     {Value: energy, Max: energyMax},
			"nou_energy": {Value: nou, Max: nouMax},
			"satiety":    {Value: satiety, Max: 100},
			"kokugo":     {Value: kokugo, Max: m},
			"suugaku":    {Value: suugaku, Max: m},
			"rika":       {Value: rika, Max: m},
			"syakai":     {Value: syakai, Max: m},
			"eigo":       {Value: eigo, Max: m},
			"ongaku":     {Value: ongaku, Max: m},
			"bijutsu":    {Value: bijutsu, Max: m},
			"looks":      {Value: looks, Max: m},
			"tairyoku":   {Value: tairyoku, Max: m},
			"kenkou":     {Value: kenkou, Max: m},
			"speed":      {Value: speed, Max: m},
			"power":      {Value: power, Max: m},
			"wanryoku":   {Value: wanryoku, Max: m},
			"kyakuryoku": {Value: kyakuryoku, Max: m},
			"love":       {Value: love, Max: m},
			"omoshirosa": {Value: omoshirosa, Max: m},
		},
	}, nil
}

func (s *Service) applyEffect(ctx context.Context, tx pgx.Tx, playerID int64, actionType string, eff effects.Effect, state effects.State) error {
	plan := eff.Plan(state)

	for _, pc := range plan.Params {
		col, ok := statusColumns[pc.Name]
		if !ok {
			return fmt.Errorf("no status column for param %q", pc.Name)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE player_status SET `+col+` = $1, updated_at = now() WHERE player_id = $2`,
			pc.NewValue, playerID); err != nil {
			return fmt.Errorf("update %s: %w", col, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO status_history (player_id, field, old_value, new_value, reason)
			 VALUES ($1, $2, $3, $4, $5)`,
			playerID, pc.Name, strconv.Itoa(pc.OldValue), strconv.Itoa(pc.NewValue), actionType); err != nil {
			return fmt.Errorf("insert status_history: %w", err)
		}
	}
	// パラメータが変化したらパワー上限(パラメータ由来)を再計算する。
	if len(plan.Params) > 0 {
		if err := player.RefreshPowerMax(ctx, tx, playerID); err != nil {
			return err
		}
	}

	if plan.MoneyDelta != 0 {
		// 冪等性は action_log(player_id, key) が保証するため台帳refは不要。
		if err := s.ledger.PostTx(ctx, tx, actionType, "", []ledger.Entry{
			{Account: ledger.SystemAccount(actionType + "_income"), Delta: -plan.MoneyDelta},
			{Account: ledger.PlayerAccount(playerID), Delta: plan.MoneyDelta},
		}); err != nil {
			return fmt.Errorf("apply money: %w", err)
		}
	}
	return nil
}

// conditionMessage maps a failed predicate to a player-facing message.
func conditionMessage(p *effects.Pred) string {
	if p == nil {
		return "条件を満たしていません。"
	}
	switch {
	case p.Kind == "param_gte" && p.Param == "energy":
		return "身体パワーが足りません。"
	case p.Kind == "param_gte" && p.Param == "nou_energy":
		return "頭脳パワーが足りません。"
	case p.Kind == "money_gte":
		return "お金が足りません。"
	default:
		return "条件を満たしていません。"
	}
}

// paramShortMessage maps an insufficient parameter to a player-facing message.
func paramShortMessage(param string) string {
	switch param {
	case "energy":
		return "身体パワーが足りません。"
	case "nou_energy":
		return "頭脳パワーが足りません。"
	default:
		return "パラメータが足りません。"
	}
}
