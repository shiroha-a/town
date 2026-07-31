package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/shiroha-a/town/internal/mail"
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
