// Package push sends Web Push notifications to players who asked for them.
//
// このゲームは「時間で回復してまた来る」構造なので、閉じている間に起きたこと
// (メールが届いた・パワーが満タンになった・仕事ができるようになった)を知らせる
// 価値がある。逆に言えばそれ以外は送らない。
//
// 送信は worker から定期的に呼ぶ。行動の処理(トランザクション)の中では送らない。
// 外部への通信が絡むと、失敗したときに巻き戻す/巻き戻さないの判断が要るため。
package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kind is what happened. 通知の種類ごとに人が個別にオン/オフできる。
type Kind string

const (
	KindMail   Kind = "mail"
	KindEnergy Kind = "energy"
	KindWork   Kind = "work"
)

// Service sends notifications.
type Service struct {
	pool *pgxpool.Pool
	// subject はVAPIDの連絡先。配信元(Apple/Googleなど)がこちらに連絡する必要が
	// 生じたときに使う。https: か mailto: でないと配信元に拒否される。
	subject string
	pub     string
	priv    string
	logger  *slog.Logger
}

// New loads (or creates) the VAPID key pair. 鍵は設定ファイルではなくDBに置く。
// 環境ごとに手で用意させると、設定し忘れた環境で通知だけ黙って動かない状態に
// なりやすいため。
func New(ctx context.Context, pool *pgxpool.Pool, baseURL string, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Service{pool: pool, subject: strings.TrimSpace(baseURL), logger: logger}
	if s.subject == "" {
		// 公開アドレスが渡っていないと配信元に拒否される。黙って的外れな値を
		// 使うと「送ったのに届かない」になるので、はっきり残す。
		logger.Warn("push: base_url が空。通知は配信元に拒否される可能性が高い")
		s.subject = "https://localhost"
	}
	err := pool.QueryRow(ctx, `SELECT public_key, private_key FROM push_keys WHERE id = 1`).
		Scan(&s.pub, &s.priv)
	if errors.Is(err, pgx.ErrNoRows) {
		priv, pub, gerr := webpush.GenerateVAPIDKeys()
		if gerr != nil {
			return nil, fmt.Errorf("generate vapid keys: %w", gerr)
		}
		if _, ierr := pool.Exec(ctx,
			`INSERT INTO push_keys (id, public_key, private_key) VALUES (1, $1, $2)
			 ON CONFLICT (id) DO NOTHING`, pub, priv); ierr != nil {
			return nil, fmt.Errorf("store vapid keys: %w", ierr)
		}
		// 競合したときは入っている方を採る(鍵が食い違うと購読が無効になる)。
		if serr := pool.QueryRow(ctx,
			`SELECT public_key, private_key FROM push_keys WHERE id = 1`).Scan(&s.pub, &s.priv); serr != nil {
			return nil, fmt.Errorf("reload vapid keys: %w", serr)
		}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load vapid keys: %w", err)
	}
	return s, nil
}

// PublicKey is handed to the browser so it can create a subscription.
func (s *Service) PublicKey() string { return s.pub }

// maxSubscriptionsPerPlayer は1人が持てる宛先の数。複数端末で使う人を想定しつつ、
// 登録し直しで増えた古い宛先が溜まらない程度に抑える。
const maxSubscriptionsPerPlayer = 3

// Subscription is one device's push endpoint.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// Subscribe stores (or refreshes) a device's endpoint for a player.
func (s *Service) Subscribe(ctx context.Context, playerID int64, sub Subscription) error {
	if sub.Endpoint == "" || sub.P256dh == "" || sub.Auth == "" {
		return fmt.Errorf("incomplete subscription")
	}
	// 同じ端末が別の人でログインし直すこともあるので、endpoint を持ち主ごと更新する。
	_, err := s.pool.Exec(ctx,
		`INSERT INTO push_subscriptions (endpoint, player_id, p256dh, auth)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (endpoint) DO UPDATE
		   SET player_id = EXCLUDED.player_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth`,
		sub.Endpoint, playerID, sub.P256dh, sub.Auth)
	if err != nil {
		return fmt.Errorf("save subscription: %w", err)
	}
	// 同じ人の宛先が増え続けないように上限を設ける。iOSではSafariのタブと
	// ホーム画面のアプリが別々の登録になり、登録し直すたびに宛先も変わるため、
	// 放っておくと1台の端末に同じ通知が何通も届く。古いものから落とす。
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM push_subscriptions
		  WHERE player_id = $1 AND endpoint NOT IN (
		    SELECT endpoint FROM push_subscriptions
		     WHERE player_id = $1 ORDER BY created_at DESC LIMIT $2)`,
		playerID, maxSubscriptionsPerPlayer); err != nil {
		return fmt.Errorf("trim subscriptions: %w", err)
	}
	// 通知の設定行を用意する(中身は全部オフのまま)。
	_, err = s.pool.Exec(ctx,
		`INSERT INTO player_notify (player_id) VALUES ($1) ON CONFLICT DO NOTHING`, playerID)
	if err != nil {
		return fmt.Errorf("init notify settings: %w", err)
	}
	return nil
}

// Unsubscribe forgets one device.
func (s *Service) Unsubscribe(ctx context.Context, endpoint string) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM push_subscriptions WHERE endpoint = $1`, endpoint); err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	return nil
}

// Prefs is what a player wants to be told about.
type Prefs struct {
	Mail   bool `json:"mail"`
	Energy bool `json:"energy"`
	Work   bool `json:"work"`
}

// GetPrefs reads a player's notification settings (未設定なら全部オフ)。
func (s *Service) GetPrefs(ctx context.Context, playerID int64) (Prefs, error) {
	var p Prefs
	err := s.pool.QueryRow(ctx,
		`SELECT mail_enabled, energy_enabled, work_enabled FROM player_notify WHERE player_id = $1`,
		playerID).Scan(&p.Mail, &p.Energy, &p.Work)
	if errors.Is(err, pgx.ErrNoRows) {
		return Prefs{}, nil
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("read notify prefs: %w", err)
	}
	return p, nil
}

// SetPrefs stores a player's notification settings.
func (s *Service) SetPrefs(ctx context.Context, playerID int64, p Prefs) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO player_notify (player_id, mail_enabled, energy_enabled, work_enabled)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (player_id) DO UPDATE
		   SET mail_enabled = EXCLUDED.mail_enabled,
		       energy_enabled = EXCLUDED.energy_enabled,
		       work_enabled = EXCLUDED.work_enabled`,
		playerID, p.Mail, p.Energy, p.Work)
	if err != nil {
		return fmt.Errorf("save notify prefs: %w", err)
	}
	return nil
}

// HasSubscription reports whether the player has at least one device registered.
func (s *Service) HasSubscription(ctx context.Context, playerID int64) (bool, error) {
	var ok bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM push_subscriptions WHERE player_id = $1)`, playerID).
		Scan(&ok); err != nil {
		return false, fmt.Errorf("check subscription: %w", err)
	}
	return ok, nil
}

// payload is what the service worker receives.
type payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// URL は通知をタップしたときに開く画面。ルーティングを入れたので直接指せる。
	URL  string `json:"url"`
	Kind string `json:"kind"`
}

// Send delivers one notification to every device of a player.
//
// 返り値の delivered は「1台でも受け取ってもらえたか」。呼び出し側はこれが真の
// ときだけ「送った」と記録する。通信が一時的に失敗しただけで送信済みにすると、
// その用件は二度と知らせられなくなるため。
//
// 宛先が期限切れ(404/410)なら、その端末の購読を消す。ブラウザ側で許可を
// 取り消された場合など。
func (s *Service) Send(ctx context.Context, playerID int64, title, body, url string, kind Kind) (delivered bool, err error) {
	rows, err := s.pool.Query(ctx,
		`SELECT endpoint, p256dh, auth FROM push_subscriptions WHERE player_id = $1`, playerID)
	if err != nil {
		return false, fmt.Errorf("list subscriptions: %w", err)
	}
	type target struct{ endpoint, p256dh, auth string }
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.endpoint, &t.p256dh, &t.auth); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan subscription: %w", err)
		}
		targets = append(targets, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate subscriptions: %w", err)
	}

	msg, err := json.Marshal(payload{Title: title, Body: body, URL: url, Kind: string(kind)})
	if err != nil {
		return false, fmt.Errorf("encode payload: %w", err)
	}
	var dead []string
	for _, t := range targets {
		sub := &webpush.Subscription{
			Endpoint: t.endpoint,
			Keys:     webpush.Keys{P256dh: t.p256dh, Auth: t.auth},
		}
		res, err := webpush.SendNotificationWithContext(ctx, msg, sub, &webpush.Options{
			Subscriber:      s.subject,
			VAPIDPublicKey:  s.pub,
			VAPIDPrivateKey: s.priv,
			TTL:             int((30 * time.Minute).Seconds()),
			Urgency:         webpush.UrgencyNormal,
		})
		if err != nil {
			// 一時的な失敗。次の機会に送り直される(送信済みにはしない)。
			s.logger.Warn("push: 送信に失敗", "player", playerID, "kind", kind, "err", err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		_ = res.Body.Close()
		if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone {
			dead = append(dead, t.endpoint)
			continue
		}
		if res.StatusCode >= 300 {
			// 配信元が受け取らなかった。鍵やsubjectの不備はここで分かる。
			s.logger.Warn("push: 配信元が拒否", "player", playerID, "kind", kind,
				"status", res.StatusCode, "body", string(body), "endpoint", t.endpoint)
		}
		if res.StatusCode < 300 {
			delivered = true
			_, _ = s.pool.Exec(ctx,
				`UPDATE push_subscriptions SET last_ok_at = now() WHERE endpoint = $1`, t.endpoint)
		}
	}
	if len(dead) > 0 {
		_, _ = s.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ANY($1)`, dead)
	}
	return delivered, nil
}
