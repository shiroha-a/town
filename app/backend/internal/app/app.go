// Package app wires the dependencies and runs the selected mode.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/attendance"
	"github.com/shiroha-a/town/internal/building"
	"github.com/shiroha-a/town/internal/cleague"
	"github.com/shiroha-a/town/internal/config"
	"github.com/shiroha-a/town/internal/content"
	"github.com/shiroha-a/town/internal/db"
	"github.com/shiroha-a/town/internal/greeting"
	"github.com/shiroha-a/town/internal/httpapi"
	"github.com/shiroha-a/town/internal/keiba"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/mail"
	"github.com/shiroha-a/town/internal/news"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/ranking"
	"github.com/shiroha-a/town/internal/rediscli"
	"github.com/shiroha-a/town/internal/rng"
	"github.com/shiroha-a/town/internal/settings"
	"github.com/shiroha-a/town/internal/stock"
	"github.com/shiroha-a/town/internal/townmap"
	"github.com/shiroha-a/town/internal/worker"
)

// Run boots the given mode ("web" or "worker") with shared infrastructure.
func Run(ctx context.Context, mode string, cfg *config.Config) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// マイグレーションは両モードで実行(冪等)。
	if err := db.Migrate(cfg.Database.URL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	pool, err := db.Connect(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer pool.Close()

	rdb, err := rediscli.Connect(ctx, cfg.Redis.Addr, cfg.Redis.DB)
	if err != nil {
		return err
	}
	defer rdb.Close()

	loc, err := time.LoadLocation(cfg.Game.Timezone)
	if err != nil {
		logger.Warn("invalid timezone, falling back to UTC", "timezone", cfg.Game.Timezone, "err", err)
		loc = time.UTC
	}

	// 実行時に編集可能なゲーム設定(初回はdefault.ymlからシードしDBに永続化)。
	st, err := settings.NewStore(ctx, pool, settings.Game{
		InitialMoney:             cfg.Game.InitialMoney,
		DailyInterestPermille:    cfg.Game.DailyInterestPermille,
		EnergyRecoverySec:        cfg.Game.EnergyRecoverySec,
		NouRecoverySec:           cfg.Game.NouRecoverySec,
		SatietyDecaySec:          cfg.Game.SatietyDecaySec,
		ConditionEvalIntervalMin: cfg.Game.ConditionEvalIntervalMin,
		WorkIntervalMin:          cfg.Game.WorkIntervalMin,
		DebugNoCooldown:          cfg.Game.DebugNoCooldown,
		DepartDailyCount:         cfg.Game.DepartDailyCount,
		SyokudouDailyCount:       cfg.Game.SyokudouDailyCount,
		HanbaiDailyCount:         cfg.Game.HanbaiDailyCount,
		ItemKindLimit:            cfg.Game.ItemKindLimit,
		StockAdjust:              cfg.Game.StockAdjust,
		MoveWalkSecs:             cfg.Game.MoveWalkSecs,
		MoveBusSecs:              cfg.Game.MoveBusSecs,
		Towns:                    defaultTownConfigs(),
	})
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	// 街の一覧(名前・地価)を実行時キャッシュ(building)へ同期する。
	syncTowns(st.Get().Towns)

	// 実行時に編集可能な街マップ(初回は既定の施設配置をシード)。webのみ使用。
	tmap, err := townmap.NewStore(ctx, pool, townmap.Default())
	if err != nil {
		return fmt.Errorf("load town map: %w", err)
	}

	led := ledger.New(pool)
	rnd := rng.New(cfg.Game.RNGSeed)
	players := player.New(pool, led, rnd, st)
	actions := action.New(pool, led, players, rnd, loc, cfg.Game.DayBoundaryHour, st)
	contentSvc := content.New(pool, loc, cfg.Game.DayBoundaryHour, st)
	stockSvc := stock.New(pool)
	keibaSvc := keiba.New(pool, rng.New(0)) // レース生成用に独立した(非決定的)乱数源
	mailSvc := mail.New(pool, loc, cfg.Game.DayBoundaryHour)
	greetingSvc := greeting.New(pool)
	attendanceSvc := attendance.New(pool, loc, cfg.Game.DayBoundaryHour)
	cleagueSvc := cleague.New(pool)
	newsSvc := news.New(pool)
	rankingSvc := ranking.New(pool)

	switch mode {
	case "web":
		return runWeb(ctx, cfg, logger, players, actions, contentSvc, st, tmap, stockSvc, keibaSvc, mailSvc, greetingSvc, attendanceSvc, cleagueSvc, newsSvc, rankingSvc)
	case "worker":
		return worker.New(rdb, pool, led, cfg, st, logger).Run(ctx)
	default:
		return fmt.Errorf("unknown mode %q (want web|worker)", mode)
	}
}

func runWeb(ctx context.Context, cfg *config.Config, logger *slog.Logger, players *player.Service, actions *action.Service, contentSvc *content.Service, st *settings.Store, tmap *townmap.Store, stockSvc *stock.Service, keibaSvc *keiba.Service, mailSvc *mail.Service, greetingSvc *greeting.Service, attendanceSvc *attendance.Service, cleagueSvc *cleague.Service, newsSvc *news.Service, rankingSvc *ranking.Service) error {
	srv := &http.Server{
		Addr:              cfg.Server.HTTPAddr,
		Handler:           httpapi.NewServer(players, actions, contentSvc, st, tmap, stockSvc, keibaSvc, mailSvc, greetingSvc, attendanceSvc, cleagueSvc, newsSvc, rankingSvc),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info("http server listening", "addr", cfg.Server.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http serve: %w", err)
	}
	return nil
}

// defaultTownConfigs returns the legacy default towns as settings.TownConfig
// (for seeding fresh installs).
func defaultTownConfigs() []settings.TownConfig {
	out := []settings.TownConfig{}
	for _, t := range building.DefaultTowns() {
		out = append(out, settings.TownConfig{Name: t.Name, LandPrice: t.LandPrice, Hidden: t.Hidden})
	}
	return out
}

// syncTowns pushes the persisted town list into the building runtime cache.
func syncTowns(tcs []settings.TownConfig) {
	if len(tcs) == 0 {
		return // 空なら組み込みの既定を維持
	}
	ts := make([]building.Town, len(tcs))
	for i, tc := range tcs {
		ts[i] = building.Town{No: i, Name: tc.Name, LandPrice: tc.LandPrice, Hidden: tc.Hidden}
	}
	building.SetTowns(ts)
}
