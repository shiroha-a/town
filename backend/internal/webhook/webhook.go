// Package webhook pushes "you would not notice this otherwise" events to an
// outside chat (Discord互換のwebhook)。
//
// 管理者向けの通知で、狙いは3つ:
//
//   - 異常の検知(台帳の破れ・日次処理の失敗・パニック)。今までログにしか
//     残っておらず、見に行かないと分からなかった
//   - 運営対応が要るもの(目安箱・NGワード・凍結・お金の書き換え)
//   - 住民の動き(入居・退会)
//
// イベントが起きた場所では外へ通信せず、webhook_outbox へ積むだけにする。
// 行動の処理(トランザクション)の中から外部へ出すと、失敗したときに巻き戻す/
// 巻き戻さないの判断が要るため。実際の送信は worker が拾ってやる(deliver.go)。
package webhook

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event names what happened. 宛先ごとの絞り込みにも使うので、増やすときは
// AllEvents にも足すこと(管理画面の選択肢がこれで作られる)。
type Event string

const (
	// 異常の検知。
	EventLedgerImbalance Event = "ledger.imbalance"
	EventDailyFailed     Event = "daily.failed"
	EventPanic           Event = "panic"
	EventStartup         Event = "startup"
	// 運営対応。
	EventFeedbackCreated   Event = "feedback.created"
	EventFeedbackCommented Event = "feedback.commented"
	EventNGWord            Event = "ngword.detected"
	EventSuspended         Event = "player.suspended"
	EventUnsuspended       Event = "player.unsuspended"
	EventMoneyAdjusted     Event = "admin.money_adjusted"
	EventTxReversed        Event = "admin.tx_reversed"
	// 住民の動き。
	EventRegistered    Event = "player.registered"
	EventRetired       Event = "player.retired"
	EventGuestNearFull Event = "guests.near_limit"
	// 設定画面の「試し送り」。宛先の登録には出すが、絞り込みの対象にはしない。
	EventTest Event = "test"
)

// Severity decides the colour on the receiving side.
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// EventInfo describes one event for the settings screen.
type EventInfo struct {
	Event    Event    `json:"event"`
	Label    string   `json:"label"`
	Group    string   `json:"group"`
	Severity Severity `json:"severity"`
}

// AllEvents is what the admin screen offers, in display order.
var AllEvents = []EventInfo{
	{EventLedgerImbalance, "台帳の不整合", "異常の検知", SeverityError},
	{EventDailyFailed, "日次処理の失敗", "異常の検知", SeverityError},
	{EventPanic, "エラーで落ちた", "異常の検知", SeverityError},
	{EventStartup, "起動", "異常の検知", SeverityInfo},
	{EventFeedbackCreated, "目安箱の新しい投稿", "運営対応", SeverityInfo},
	{EventFeedbackCommented, "目安箱のコメント", "運営対応", SeverityInfo},
	{EventNGWord, "NGワードの書き込み", "運営対応", SeverityWarn},
	{EventSuspended, "凍結した", "運営対応", SeverityWarn},
	{EventUnsuspended, "凍結を解除した", "運営対応", SeverityInfo},
	{EventMoneyAdjusted, "お金を書き換えた", "運営対応", SeverityWarn},
	{EventTxReversed, "お金の動きを取り消した", "運営対応", SeverityWarn},
	{EventRegistered, "新しい住民", "住民の動き", SeverityInfo},
	{EventRetired, "退会", "住民の動き", SeverityInfo},
	{EventGuestNearFull, "お試しプレイが上限に近い", "住民の動き", SeverityWarn},
}

// Field is one name/value pair shown under the message.
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Notice is one thing worth telling the admins.
type Notice struct {
	Event    Event
	Severity Severity
	Title    string
	Body     string
	Fields   []Field
}

// Service queues notices. nil でも呼べる(通知を使わない環境・テスト)。
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Post queues a notice for every destination that wants it.
//
// 宛先が1件も無ければ何も残らない(WHERE で0行になるだけ)。送信そのものは
// worker がやるので、ここは INSERT 1文で終わる。
func (s *Service) Post(ctx context.Context, n Notice) error {
	if s == nil || s.pool == nil {
		return nil
	}
	return s.post(ctx, s.pool, n)
}

// PostTx queues a notice inside the caller's transaction. 行動と一緒に巻き
// 戻したいときに使う(「起きなかったこと」を知らせないため)。
func (s *Service) PostTx(ctx context.Context, tx pgx.Tx, n Notice) error {
	if s == nil || s.pool == nil {
		return nil
	}
	return s.post(ctx, tx, n)
}

// execer is the part of *pgxpool.Pool and pgx.Tx we need, so the same code
// serves both「単発で積む」と「トランザクションの中で積む」。
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (s *Service) post(ctx context.Context, q execer, n Notice) error {
	if n.Severity == "" {
		n.Severity = SeverityInfo
	}
	fields := n.Fields
	if fields == nil {
		fields = []Field{}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("marshal fields: %w", err)
	}
	// 宛先ごとに1行。events が空配列なら「すべて」の意味。試し送りは
	// 絞り込みの対象外(その宛先へ必ず出す)。
	_, err = q.Exec(ctx, `
		INSERT INTO webhook_outbox (webhook_id, event, severity, title, body, fields)
		SELECT w.id, $1, $2, $3, $4, $5::jsonb
		  FROM webhooks w
		 WHERE w.enabled
		   AND ($1 = 'test' OR w.events = '[]'::jsonb OR w.events ? $1)`,
		string(n.Event), string(n.Severity), n.Title, n.Body, string(raw))
	if err != nil {
		return fmt.Errorf("queue notice: %w", err)
	}
	return nil
}

// PostQuiet queues a notice and swallows the error. 通知の失敗で本来の処理を
// 止めたくない場所(パニックの記録・住民の登録など)から使う。
func (s *Service) PostQuiet(ctx context.Context, n Notice) {
	_ = s.Post(ctx, n)
}
