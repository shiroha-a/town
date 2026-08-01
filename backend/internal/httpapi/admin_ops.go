package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shiroha-a/town/internal/gametime"
	"github.com/shiroha-a/town/internal/mail"
	"github.com/shiroha-a/town/internal/moderation"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/sysinfo"
	"github.com/shiroha-a/town/internal/webhook"
)

// 運営まわりの管理操作。すべて管理者のみ(authGuard が /api/v1/admin/ を見て弾く)。

type broadcastReq struct {
	Body string `json:"body"`
}

// adminBroadcastMail sends one announcement to every resident's inbox.
// 端末への通知はworkerが新着メールを見て送るので、ここでは何もしない。
func (s *Server) adminBroadcastMail(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req broadcastReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	sent, err := s.mail.Broadcast(r.Context(), PlayerIDFrom(r.Context()), req.Body)
	var v *mail.ErrValidation
	switch {
	case errors.As(err, &v):
		writeError(w, http.StatusUnprocessableEntity, v.Message)
	case err != nil:
		writeInternal(w, r, err)
	default:
		writeJSON(w, http.StatusOK, map[string]int{"sent": sent})
	}
}

type suspendReq struct {
	// Days は凍結する日数。0以下で無期限。
	Days   int    `json:"days"`
	Reason string `json:"reason"`
}

// adminSuspendPlayer blocks a resident from logging in.
func (s *Server) adminSuspendPlayer(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, _, ok := adminPathIDs(w, r, false)
	if !ok {
		return
	}
	var req suspendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	err := s.players.AdminSuspend(r.Context(), id, req.Days, req.Reason)
	var v *player.ErrValidation
	switch {
	case errors.Is(err, player.ErrNotFound):
		writeError(w, http.StatusNotFound, "player not found")
	case errors.As(err, &v):
		writeError(w, http.StatusUnprocessableEntity, v.Message)
	case err != nil:
		writeInternal(w, r, err)
	default:
		term := "無期限"
		if req.Days > 0 {
			term = fmt.Sprintf("%d日間", req.Days)
		}
		reason := req.Reason
		if strings.TrimSpace(reason) == "" {
			reason = "(記載なし)"
		}
		s.webhooks.PostQuiet(r.Context(), webhook.Notice{
			Event:    webhook.EventSuspended,
			Severity: webhook.SeverityWarn,
			Title:    "住民を凍結しました",
			Body:     s.playerLabel(r.Context(), id),
			Fields: []webhook.Field{
				{Name: "期間", Value: term},
				{Name: "理由", Value: reason},
				{Name: "操作した人", Value: s.playerLabel(r.Context(), PlayerIDFrom(r.Context()))},
			},
		})
		s.writeSuspension(w, r, id)
	}
}

// adminUnsuspendPlayer lifts the block.
func (s *Server) adminUnsuspendPlayer(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, _, ok := adminPathIDs(w, r, false)
	if !ok {
		return
	}
	if err := s.players.AdminUnsuspend(r.Context(), id); errors.Is(err, player.ErrNotFound) {
		writeError(w, http.StatusNotFound, "player not found")
		return
	} else if err != nil {
		writeInternal(w, r, err)
		return
	}
	s.webhooks.PostQuiet(r.Context(), webhook.Notice{
		Event:    webhook.EventUnsuspended,
		Severity: webhook.SeverityInfo,
		Title:    "凍結を解除しました",
		Body:     s.playerLabel(r.Context(), id),
		Fields: []webhook.Field{
			{Name: "操作した人", Value: s.playerLabel(r.Context(), PlayerIDFrom(r.Context()))},
		},
	})
	s.writeSuspension(w, r, id)
}

// playerLabel renders "名前(#id)" for a notice. 名前が引けなくても通知は
// 出したいので、失敗したら番号だけにする。
func (s *Server) playerLabel(ctx context.Context, id int64) string {
	if id == 0 {
		return "(不明)"
	}
	p, err := s.players.Get(ctx, id)
	if err != nil {
		return fmt.Sprintf("#%d", id)
	}
	return fmt.Sprintf("%s(#%d)", p.DisplayName, id)
}

type suspensionResp struct {
	Active  bool       `json:"active"`
	Forever bool       `json:"forever"`
	Until   *time.Time `json:"until"`
	Reason  string     `json:"reason"`
}

func (s *Server) writeSuspension(w http.ResponseWriter, r *http.Request, id int64) {
	sus, err := s.players.GetSuspension(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, suspensionResp{
		Active: sus.Active(), Forever: sus.Forever, Until: sus.Until, Reason: sus.Reason,
	})
}

// adminListPosts returns recent writings across every board.
func (s *Server) adminListPosts(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	q := r.URL.Query()
	playerID, _ := strconv.ParseInt(q.Get("player_id"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	posts, err := moderation.New(s.pool).List(r.Context(), q.Get("source"), playerID, limit)
	if errors.Is(err, moderation.ErrUnknownSource) {
		writeError(w, http.StatusBadRequest, "その出どころはありません。")
		return
	} else if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, posts)
}

// adminDeletePost removes one writing.
func (s *Server) adminDeletePost(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("postId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	err = moderation.New(s.pool).Delete(r.Context(), r.PathValue("source"), id)
	if errors.Is(err, moderation.ErrUnknownSource) {
		writeError(w, http.StatusBadRequest, "その出どころはありません。")
		return
	} else if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

type actionLogResp struct {
	ID        int64           `json:"id"`
	Type      string          `json:"type"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"created_at"`
}

type statusHistoryResp struct {
	ID        int64     `json:"id"`
	Field     string    `json:"field"`
	OldValue  *string   `json:"old_value"`
	NewValue  *string   `json:"new_value"`
	Reason    *string   `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type playerLogResp struct {
	Actions []actionLogResp     `json:"actions"`
	Status  []statusHistoryResp `json:"status"`
}

// adminPlayerLog returns what a resident did and how their stats moved.
// psqlを叩かずに「この人がいつ何をしたか」を追えるようにするためのもの。
func (s *Server) adminPlayerLog(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, _, ok := adminPathIDs(w, r, false)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := playerLogResp{Actions: []actionLogResp{}, Status: []statusHistoryResp{}}

	rows, err := s.pool.Query(r.Context(),
		`SELECT id, action_type, detail, created_at FROM action_log
		 WHERE player_id = $1 ORDER BY id DESC LIMIT $2`, id, limit)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	for rows.Next() {
		var a actionLogResp
		if err := rows.Scan(&a.ID, &a.Type, &a.Detail, &a.CreatedAt); err != nil {
			rows.Close()
			writeInternal(w, r, err)
			return
		}
		out.Actions = append(out.Actions, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeInternal(w, r, err)
		return
	}

	hrows, err := s.pool.Query(r.Context(),
		`SELECT id, field, old_value, new_value, reason, created_at FROM status_history
		 WHERE player_id = $1 ORDER BY id DESC LIMIT $2`, id, limit)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	defer hrows.Close()
	for hrows.Next() {
		var h statusHistoryResp
		if err := hrows.Scan(&h.ID, &h.Field, &h.OldValue, &h.NewValue, &h.Reason, &h.CreatedAt); err != nil {
			writeInternal(w, r, err)
			return
		}
		out.Status = append(out.Status, h)
	}
	if err := hrows.Err(); err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type dbTableResp struct {
	Table string `json:"table"`
	Rows  int64  `json:"rows"`
	Bytes int64  `json:"bytes"`
}

type workerJobResp struct {
	JobType string    `json:"job_type"`
	JobDate string    `json:"job_date"`
	RanAt   time.Time `json:"ran_at"`
}

type dashboardResp struct {
	Host sysinfo.Host `json:"host"`
	DB   struct {
		SizeB       int64         `json:"size_b"`
		Connections int           `json:"connections"`
		MaxConns    int           `json:"max_conns"`
		Tables      []dbTableResp `json:"tables"`
	} `json:"db"`
	Worker struct {
		// Today は街の今日の日付(設定の日付境界に合わせたもの)。
		Today   string          `json:"today"`
		DaySeen bool            `json:"day_seen"` // 今日ぶんの日次処理が済んでいるか
		Recent  []workerJobResp `json:"recent"`
	} `json:"worker"`
	Players struct {
		Total     int `json:"total"`
		Guests    int `json:"guests"`
		Suspended int `json:"suspended"`
		Active24h int `json:"active_24h"`
	} `json:"players"`
	ServerNow time.Time `json:"server_now"`
}

// adminDashboard is the one screen that answers "動いているか".
func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	ctx := r.Context()
	var out dashboardResp
	out.ServerNow = time.Now()
	// ディスクはデータの置き場を見る。web からは書き込み先が見えないので、
	// 少なくとも同じファイルシステムに載っている作業ディレクトリで代用する。
	// CPU使用率は累計の差分でしか出せないので、200ms空けて2回測る。手で開く
	// 画面なのでこの待ちは問題にならない。
	out.Host = sysinfo.Read(".", 200*time.Millisecond)

	if err := s.pool.QueryRow(ctx,
		`SELECT pg_database_size(current_database()),
		        (SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()),
		        current_setting('max_connections')::int`).
		Scan(&out.DB.SizeB, &out.DB.Connections, &out.DB.MaxConns); err != nil {
		writeInternal(w, r, err)
		return
	}
	// 行数は pg_class の推定値。正確に数えると表ごとに全走査になるので使わない。
	rows, err := s.pool.Query(ctx, `
		SELECT c.relname, GREATEST(c.reltuples, 0)::bigint,
		       pg_total_relation_size(c.oid)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		ORDER BY pg_total_relation_size(c.oid) DESC LIMIT 12`)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	out.DB.Tables = []dbTableResp{}
	for rows.Next() {
		var t dbTableResp
		if err := rows.Scan(&t.Table, &t.Rows, &t.Bytes); err != nil {
			rows.Close()
			writeInternal(w, r, err)
			return
		}
		out.DB.Tables = append(out.DB.Tables, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeInternal(w, r, err)
		return
	}

	g := s.settings.Get()
	// 街の「今日」は設定のタイムゾーンと日付境界で決まる。実時刻の日付とは
	// ずれるので、日次処理が済んだかの判定はこちらで行う。
	loc, err := time.LoadLocation(g.Timezone)
	if err != nil {
		loc = time.Local
	}
	today := gametime.Date(time.Now(), loc, g.DayBoundaryHour)
	out.Worker.Today = today.Format("2006-01-02")
	wrows, err := s.pool.Query(ctx,
		`SELECT job_type, job_date, ran_at FROM worker_jobs ORDER BY job_date DESC, job_type LIMIT 20`)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	out.Worker.Recent = []workerJobResp{}
	for wrows.Next() {
		var j workerJobResp
		var d time.Time
		if err := wrows.Scan(&j.JobType, &d, &j.RanAt); err != nil {
			wrows.Close()
			writeInternal(w, r, err)
			return
		}
		j.JobDate = d.Format("2006-01-02")
		if j.JobDate == out.Worker.Today && j.JobType == "daily" {
			out.Worker.DaySeen = true
		}
		out.Worker.Recent = append(out.Worker.Recent, j)
	}
	wrows.Close()
	if err := wrows.Err(); err != nil {
		writeInternal(w, r, err)
		return
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE NOT is_guest),
		       count(*) FILTER (WHERE is_guest),
		       count(*) FILTER (WHERE suspended_until IS NOT NULL
		                          AND (suspended_until = 'infinity'::timestamptz OR suspended_until > now())),
		       count(*) FILTER (WHERE last_seen_at > now() - interval '24 hours')
		FROM players WHERE deleted_at IS NULL`).
		Scan(&out.Players.Total, &out.Players.Guests, &out.Players.Suspended, &out.Players.Active24h); err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
