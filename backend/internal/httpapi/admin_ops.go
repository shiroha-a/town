package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/shiroha-a/town/internal/mail"
	"github.com/shiroha-a/town/internal/moderation"
	"github.com/shiroha-a/town/internal/player"
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
	s.writeSuspension(w, r, id)
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
