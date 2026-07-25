// Package worker runs time-progression jobs (daily interest, economy ticks,
// random events). Only one worker acts at a time via a Redis leader lock, and
// daily jobs are idempotent per game date.
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/shiroha-a/town/internal/bank"
	"github.com/shiroha-a/town/internal/config"
	"github.com/shiroha-a/town/internal/gametime"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/rng"
	"github.com/shiroha-a/town/internal/settings"
	"github.com/shiroha-a/town/internal/stock"
)

const leaderKey = "town:worker:leader"

// stockVolatilityG is the volatility divisor for the stock price engine
// (legacy: online players + 1). Higher = rarer, gentler moves.
const stockVolatilityG = 20

// superInterestPermille is the daily rate for super time-deposits (10 = 1%),
// twice the ordinary savings rate. Legacy: fixed 1%.
const superInterestPermille = 10

// Worker drives scheduled game progression.
type Worker struct {
	rdb      *redis.Client
	pool     *pgxpool.Pool
	ledger   *ledger.Repo
	cfg      *config.Config
	settings *settings.Store
	logger   *slog.Logger
	loc      *time.Location
	rng      *rng.Rand
}

func New(rdb *redis.Client, pool *pgxpool.Pool, led *ledger.Repo, cfg *config.Config, st *settings.Store, logger *slog.Logger) *Worker {
	// タイムゾーンと日付の切り替わり時刻はDBの設定(管理画面で編集)から取る。
	loc, err := st.Get().Location()
	if err != nil {
		logger.Warn("invalid timezone, falling back to UTC", "timezone", st.Get().Timezone, "err", err)
	}
	return &Worker{rdb: rdb, pool: pool, ledger: led, cfg: cfg, settings: st, logger: logger, loc: loc, rng: rng.New(time.Now().UnixNano())}
}

// Run ticks until the context is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	interval := w.cfg.Worker.TickInterval.Std()
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	w.logger.Info("worker started", "tick", interval.String())
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("worker stopping")
			return nil
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick acquires the leader lock and, if acquired, runs due jobs. Non-leaders
// simply return until the lock expires.
func (w *Worker) tick(ctx context.Context) {
	ttl := w.cfg.Worker.LeaderLockTTL.Std()
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	acquired, err := w.rdb.SetNX(ctx, leaderKey, "1", ttl).Result()
	if err != nil {
		w.logger.Error("leader lock", "err", err)
		return
	}
	if !acquired {
		return
	}
	// 管理者が実行時に変更した設定を毎tickで取り込む(webとは別プロセスのため)。
	if err := w.settings.Reload(ctx); err != nil {
		w.logger.Error("reload settings", "err", err)
	}
	cfg := w.settings.Get()
	// 身体/頭脳パワーの自動回復と空腹値の減少(毎tick)。
	if n, err := RecoverPower(ctx, w.pool, cfg.EnergyRecoverySec, cfg.NouRecoverySec); err != nil {
		w.logger.Error("recover power", "err", err)
	} else if n > 0 {
		w.logger.Info("power recovered", "players", n)
	}
	if _, err := DecaySatiety(ctx, w.pool, cfg.SatietyDecaySec); err != nil {
		w.logger.Error("decay satiety", "err", err)
	}
	// 病気指数のコンディション評価(評価間隔を過ぎたプレイヤーを1回ぶん評価)。
	if n, err := EvaluateDisease(ctx, w.pool, cfg.ConditionEvalIntervalMin); err != nil {
		w.logger.Error("evaluate disease", "err", err)
	} else if n > 0 {
		w.logger.Info("disease evaluated", "players", n)
	}
	// 株価変動(event.pl相当)。毎tickで確率的に評価し、動いたら記録する。
	if moved, err := stock.MovePrices(ctx, w.pool, w.rng, stockVolatilityG); err != nil {
		w.logger.Error("stock move", "err", err)
	} else if moved {
		w.logger.Info("stock prices moved")
	}
	w.runDailyIfNeeded(ctx, time.Now())
}

// gameDate returns the game day for a wall-clock instant. The day rolls over at
// day_boundary_hour local time (e.g. AM 5:00), matching typical social-game
// reset behavior.
func (w *Worker) gameDate(now time.Time) time.Time {
	return gametime.Date(now, w.loc, w.settings.Get().DayBoundaryHour)
}

// runDailyIfNeeded runs the daily job exactly once per game date. The
// worker_jobs table provides the idempotency and catch-up guarantee.
func (w *Worker) runDailyIfNeeded(ctx context.Context, now time.Time) {
	date := w.gameDate(now)
	var (
		ran              bool
		interestAccounts int
	)
	// worker_jobsの請求と日次処理を同一トランザクションで行うことで、
	// 途中クラッシュ時はロールバックされ「請求済みだが未処理」を防ぐ。
	err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`INSERT INTO worker_jobs (job_date, job_type) VALUES ($1, 'daily')
			 ON CONFLICT (job_date, job_type) DO NOTHING`, date)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil // 本日分は実行済み
		}
		ran = true
		interestAccounts, err = bank.AccrueInterest(ctx, tx, w.ledger, "savings:", "interest:", w.settings.Get().DailyInterestPermille)
		if err != nil {
			return err
		}
		// スーパー定期は普通口座の倍(1%/日)の利息を付与する。
		if _, err := bank.AccrueInterest(ctx, tx, w.ledger, "super_savings:", "super_interest:", superInterestPermille); err != nil {
			return err
		}
		// 住宅ローンの日次返済(普通口座から日額を引き落とし、完済で消す)。
		if _, err := RepayLoans(ctx, tx, w.ledger); err != nil {
			return err
		}
		// 日単位耐久アイテムの残日数を1減らし、失効したものを削除する。
		if err := DecayDayItems(ctx, tx); err != nil {
			return err
		}
		// 期限切れのログインセッションと、承認されずに放置されたMiAuthの
		// 保留セッションを掃除する。
		if err := PurgeSessions(ctx, tx); err != nil {
			return err
		}
		// ロト6の日次抽選(前日以前の未抽選プールを抽選し賞金を銀行へ振り込む)。
		if _, err := DrawLoto6(ctx, tx, w.ledger, w.rng, date); err != nil {
			return err
		}
		// 運営/株式会社の日次収入(社員の職給与+総合能力値から算出)。
		if _, err := PayCompanyIncome(ctx, tx, w.ledger); err != nil {
			return err
		}
		return nil
		// TODO: 経済再計算・ランダムイベント等もここに追加する。
	})
	if err != nil {
		w.logger.Error("daily job", "err", err)
		return
	}
	if ran {
		w.logger.Info("daily job ran",
			"game_date", date.Format("2006-01-02"),
			"interest_accounts", interestAccounts)
	}
}
