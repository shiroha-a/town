package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/shiroha-a/town/internal/mail"
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
