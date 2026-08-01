// Package app wires the dependencies and runs the selected mode.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/attendance"
	"github.com/shiroha-a/town/internal/building"
	"github.com/shiroha-a/town/internal/cleague"
	"github.com/shiroha-a/town/internal/config"
	"github.com/shiroha-a/town/internal/content"
	"github.com/shiroha-a/town/internal/db"
	"github.com/shiroha-a/town/internal/emoji"
	"github.com/shiroha-a/town/internal/feedback"
	"github.com/shiroha-a/town/internal/greeting"
	"github.com/shiroha-a/town/internal/httpapi"
	"github.com/shiroha-a/town/internal/keiba"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/mail"
	"github.com/shiroha-a/town/internal/miauth"
	"github.com/shiroha-a/town/internal/news"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/profile"
	"github.com/shiroha-a/town/internal/push"
	"github.com/shiroha-a/town/internal/ranking"
	"github.com/shiroha-a/town/internal/rediscli"
	"github.com/shiroha-a/town/internal/rng"
	"github.com/shiroha-a/town/internal/serial"
	"github.com/shiroha-a/town/internal/session"
	"github.com/shiroha-a/town/internal/settings"
	"github.com/shiroha-a/town/internal/stock"
	"github.com/shiroha-a/town/internal/streetfight"
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

	// ゲーム設定はDBに持ち、管理画面から編集する。設定ファイルはインフラ
	// (DB/Redis/ポート)だけを持ち、ゲームの値は持たない。初回起動時は
	// settings.Defaults() をシードする。
	defaults := settings.Defaults()
	defaults.Towns = defaultTownConfigs()
	defaults.InstancePolicy = string(miauth.Blacklist)
	st, err := settings.NewStore(ctx, pool, defaults)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	game := st.Get()
	loc, err := game.Location()
	if err != nil {
		logger.Warn("invalid timezone, falling back to UTC", "timezone", game.Timezone, "err", err)
	}

	// 街の一覧(名前・地価)を実行時キャッシュ(building)へ同期する。
	syncTowns(st.Get().Towns)

	// 実行時に編集可能な街マップ(初回は既定の施設配置をシード)。webのみ使用。
	tmap, err := townmap.NewStore(ctx, pool, townmap.Default(), townmap.DefaultAssets())
	if err != nil {
		return fmt.Errorf("load town map: %w", err)
	}

	led := ledger.New(pool)
	// 乱数のシード。0(既定)は時刻ベース。決定的な再現が要るときだけ
	// TOWN_RNG_SEED で固定する(開発・テスト用)。
	seed, _ := strconv.ParseInt(os.Getenv("TOWN_RNG_SEED"), 10, 64)
	rnd := rng.New(seed)
	players := player.New(pool, led, rnd, st)
	actions := action.New(pool, led, players, rnd, loc, game.DayBoundaryHour, st)
	contentSvc := content.New(pool, loc, game.DayBoundaryHour, st)
	stockSvc := stock.New(pool)
	keibaSvc := keiba.New(pool, rng.New(0)) // レース生成用に独立した(非決定的)乱数源
	mailSvc := mail.New(pool, loc, game.DayBoundaryHour)
	greetingSvc := greeting.New(pool)
	attendanceSvc := attendance.New(pool, loc, game.DayBoundaryHour)
	cleagueSvc := cleague.New(pool)
	monsterSvc := streetfight.New(pool)
	feedbackSvc := feedback.New(pool)
	newsSvc := news.New(pool)
	rankingSvc := ranking.New(pool)
	serialSvc := serial.New(pool, rng.New(0))
	// MiAuth(ログイン)。トークンの暗号鍵が無い環境ではトークンを保存しない。
	miauthClient := miauth.NewClient()
	instanceRules := miauth.NewRules(pool)
	// cookieのSecureはHTTPSでのみ有効にする。開発はTailscale等の素のHTTPで
	// アクセスするため既定はオフで、TOWN_COOKIE_SECURE=1 で有効化する。
	sessions := session.New(pool, os.Getenv("TOWN_COOKIE_SECURE") == "1", st)
	// 保管時の暗号鍵。Misskeyのアクセストークンと、通知(VAPID)の秘密鍵に使う。
	var tokenCipher *miauth.TokenCipher
	if key := os.Getenv("TOWN_TOKEN_KEY"); key != "" {
		tc, err := miauth.NewTokenCipher(key)
		if err != nil {
			return fmt.Errorf("token cipher: %w", err)
		}
		tokenCipher = tc
		players = players.WithTokenCipher(tc)
	} else {
		logger.Warn("TOWN_TOKEN_KEY が未設定のため、Misskeyのアクセストークンは保存されません")
	}

	emojis := emoji.New(pool, miauthClient)
	profiles := profile.New(pool, miauthClient, players, emojis)
	// 通知(Web Push)。VAPID鍵はDBに置き、無ければここで作る(設定不要)。
	pushSvc, err := push.New(ctx, pool, cfg.Server.BaseURL, logger, tokenCipher)
	if err != nil {
		logger.Error("push init", "err", err)
		pushSvc = nil // 通知だけ諦める。ゲーム本体は動かす
	}

	authDeps := httpapi.AuthDeps{
		Pool:           pool,
		MiAuth:         miauthClient,
		InstanceRules:  instanceRules,
		Sessions:       sessions,
		Profiles:       profiles,
		Emojis:         emojis,
		Push:           pushSvc,
		AppName:        cfg.Server.AppName,
		WebDir:         cfg.Server.WebDir,
		AllowedOrigins: cfg.Server.AllowedOrigins(),
	}

	switch mode {
	case "web":
		return runWeb(ctx, cfg, logger, players, actions, contentSvc, st, tmap, stockSvc, keibaSvc, mailSvc, greetingSvc, attendanceSvc, cleagueSvc, monsterSvc, feedbackSvc, newsSvc, rankingSvc, serialSvc, authDeps)
	case "worker":
		wk := worker.New(rdb, pool, led, cfg, st, logger)
		wk.SetPush(pushSvc)
		return wk.Run(ctx)
	default:
		return fmt.Errorf("unknown mode %q (want web|worker)", mode)
	}
}

func runWeb(ctx context.Context, cfg *config.Config, logger *slog.Logger, players *player.Service, actions *action.Service, contentSvc *content.Service, st *settings.Store, tmap *townmap.Store, stockSvc *stock.Service, keibaSvc *keiba.Service, mailSvc *mail.Service, greetingSvc *greeting.Service, attendanceSvc *attendance.Service, cleagueSvc *cleague.Service, monsterSvc *streetfight.Service, feedbackSvc *feedback.Service, newsSvc *news.Service, rankingSvc *ranking.Service, serialSvc *serial.Service, authDeps httpapi.AuthDeps) error {
	srv := &http.Server{
		Addr:              cfg.Server.HTTPAddr,
		Handler:           httpapi.NewServer(players, actions, contentSvc, st, tmap, stockSvc, keibaSvc, mailSvc, greetingSvc, attendanceSvc, cleagueSvc, monsterSvc, feedbackSvc, newsSvc, rankingSvc, serialSvc, authDeps),
		ReadHeaderTimeout: 5 * time.Second,
		// ボディをだらだら送り続ける接続を切る。あいさつのSSEは繋ぎっぱなしに
		// なるが、あちらはハンドラ側で期限を外している。
		ReadTimeout: 30 * time.Second,
		// 遊んでいない接続を抱えない。WriteTimeout は置かない(置くとSSEが
		// その時間で切れる)。
		IdleTimeout: 120 * time.Second,
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
