package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/push"
)

// notifyCandidate is one player to be told about one thing.
type notifyCandidate struct {
	playerID int64
	name     string
	count    int // メールの未読数(他の種類では使わない)
}

// RunNotifications tells players about things that happened while they were away.
//
// 送るのは3つだけ: メール着信・身体パワー満タン・仕事の解禁。いずれも
// 「待っていたことが起きた」であり、本人が設定でオンにしたものだけを送る。
//
// 同じ用件を繰り返し送らないよう、送ったら player_notify に印を付ける。
// パワーと仕事は、条件から外れた(=また消費した)ときに印を消して次に備える。
func RunNotifications(ctx context.Context, pool *pgxpool.Pool, p *push.Service) error {
	if p == nil {
		return nil
	}
	// 先に印を戻す。満タンでなくなった/また働いた人は、次に条件を満たしたとき
	// もう一度知らせてよい。
	if _, err := pool.Exec(ctx, `
		UPDATE player_notify n SET energy_sent_at = NULL
		  FROM player_status s
		 WHERE s.player_id = n.player_id AND n.energy_sent_at IS NOT NULL AND s.energy < s.energy_max`); err != nil {
		return fmt.Errorf("reset energy mark: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE player_notify n SET work_sent_at = NULL
		  FROM player_facility_cooldowns c
		 WHERE c.player_id = n.player_id AND c.facility = 'work'
		   AND n.work_sent_at IS NOT NULL AND c.next_available_at > now()`); err != nil {
		return fmt.Errorf("reset work mark: %w", err)
	}

	if err := notifyMail(ctx, pool, p); err != nil {
		return err
	}
	if err := notifyEnergy(ctx, pool, p); err != nil {
		return err
	}
	return notifyWork(ctx, pool, p)
}

// notifyMail tells players about unread mail newer than the last notice.
func notifyMail(ctx context.Context, pool *pgxpool.Pool, p *push.Service) error {
	rows, err := pool.Query(ctx, `
		SELECT n.player_id, COUNT(*)::int
		  FROM player_notify n
		  JOIN messages m ON m.owner_id = n.player_id AND m.direction = 'received'
		  LEFT JOIN mail_check mc ON mc.player_id = n.player_id
		 WHERE n.mail_enabled
		   -- 未読 = 本人が受信箱を開いた時刻より後に届いたもの(mail.UnreadCountと同じ定義)
		   AND m.sent_at > COALESCE(mc.last_checked_at, to_timestamp(0))
		   AND (n.mail_sent_at IS NULL OR m.sent_at > n.mail_sent_at)
		   AND EXISTS (SELECT 1 FROM push_subscriptions ps WHERE ps.player_id = n.player_id)
		 GROUP BY n.player_id`)
	if err != nil {
		return fmt.Errorf("find mail targets: %w", err)
	}
	var list []notifyCandidate
	for rows.Next() {
		var c notifyCandidate
		if err := rows.Scan(&c.playerID, &c.count); err != nil {
			rows.Close()
			return fmt.Errorf("scan mail target: %w", err)
		}
		list = append(list, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate mail targets: %w", err)
	}
	for _, c := range list {
		body := fmt.Sprintf("受信箱に%d通の新しいメッセージが届いています。", c.count)
		ok, err := p.Send(ctx, c.playerID, "メールが届きました", body, "/mail", push.KindMail)
		if err != nil || !ok {
			continue // 届かなかったものは記録しない(次の機会に送り直す)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE player_notify SET mail_sent_at = now() WHERE player_id = $1`, c.playerID); err != nil {
			return fmt.Errorf("mark mail sent: %w", err)
		}
	}
	return nil
}

// notifyEnergy tells players their body energy is back to full.
func notifyEnergy(ctx context.Context, pool *pgxpool.Pool, p *push.Service) error {
	rows, err := pool.Query(ctx, `
		SELECT n.player_id
		  FROM player_notify n
		  JOIN player_status s ON s.player_id = n.player_id
		 WHERE n.energy_enabled AND n.energy_sent_at IS NULL
		   AND s.energy >= s.energy_max AND s.energy_max > 0
		   AND EXISTS (SELECT 1 FROM push_subscriptions ps WHERE ps.player_id = n.player_id)`)
	if err != nil {
		return fmt.Errorf("find energy targets: %w", err)
	}
	ids, err := scanIDs(rows)
	if err != nil {
		return err
	}
	for _, id := range ids {
		ok, err := p.Send(ctx, id, "パワーが満タンです",
			"身体パワーが回復しました。働きに行けます。", "/", push.KindEnergy)
		if err != nil || !ok {
			continue
		}
		if _, err := pool.Exec(ctx,
			`UPDATE player_notify SET energy_sent_at = now() WHERE player_id = $1`, id); err != nil {
			return fmt.Errorf("mark energy sent: %w", err)
		}
	}
	return nil
}

// notifyWork tells players the work cooldown has passed.
func notifyWork(ctx context.Context, pool *pgxpool.Pool, p *push.Service) error {
	rows, err := pool.Query(ctx, `
		SELECT n.player_id
		  FROM player_notify n
		  JOIN player_facility_cooldowns c
		    ON c.player_id = n.player_id AND c.facility = 'work'
		 WHERE n.work_enabled AND n.work_sent_at IS NULL
		   AND c.next_available_at <= now()
		   -- 一度も働いていない人には送らない(解禁もなにも無いので)
		   AND EXISTS (SELECT 1 FROM push_subscriptions ps WHERE ps.player_id = n.player_id)`)
	if err != nil {
		return fmt.Errorf("find work targets: %w", err)
	}
	ids, err := scanIDs(rows)
	if err != nil {
		return err
	}
	for _, id := range ids {
		ok, err := p.Send(ctx, id, "仕事に行けます",
			"次の勤務までの時間が過ぎました。", "/", push.KindWork)
		if err != nil || !ok {
			continue
		}
		if _, err := pool.Exec(ctx,
			`UPDATE player_notify SET work_sent_at = now() WHERE player_id = $1`, id); err != nil {
			return fmt.Errorf("mark work sent: %w", err)
		}
	}
	return nil
}

// scanIDs collects player ids from a query and closes the rows.
func scanIDs(rows interface {
	Next() bool
	Scan(...any) error
	Close()
	Err() error
}) ([]int64, error) {
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan player id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate players: %w", err)
	}
	return out, nil
}

// notifyInterval throttles the scan. 10秒ごとのtickで毎回全表を舐める必要はない。
const notifyInterval = time.Minute

var lastNotifyRun time.Time

// maybeNotify runs RunNotifications at most once per notifyInterval.
func maybeNotify(ctx context.Context, pool *pgxpool.Pool, p *push.Service, now time.Time) {
	if p == nil || now.Sub(lastNotifyRun) < notifyInterval {
		return
	}
	lastNotifyRun = now
	_ = RunNotifications(ctx, pool, p)
}
