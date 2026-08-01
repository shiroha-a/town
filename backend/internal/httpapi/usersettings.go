package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/webhook"
)

// userSettings serves GET /players/{id}/settings — 住民が自分で変えられる設定。
func (s *Server) userSettings(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	out, err := s.players.GetSettings(r.Context(), id)
	if errors.Is(err, player.ErrNotFound) {
		writeError(w, http.StatusNotFound, "player not found")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// updateUserSettings serves PUT /players/{id}/settings.
func (s *Server) updateUserSettings(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in player.UserSettings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	out, err := s.players.UpdateSettings(r.Context(), id, in)
	if errors.Is(err, player.ErrBadName) {
		// 本人が直せる入力の誤りなので理由を返す。
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// refreshMisskeyProfile serves POST /players/{id}/misskey/refresh — 自分の
// Misskey情報を取り直す。通常は6時間キャッシュされるので、プロフィールを
// 変えた直後に反映したいときに使う。
func (s *Server) refreshMisskeyProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.profiles.Refresh(r.Context(), id)
	if err != nil {
		// 相手インスタンスの不調はこちらの障害ではないので502で返す。
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type retireReq struct {
	// Confirm は誤操作防止。画面には自分の名前を入力させる。
	Confirm string `json:"confirm"`
}

// retire serves POST /players/{id}/retire — 退会(自分のデータを消して街を出る)。
func (s *Server) retire(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req retireReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	cur, err := s.players.GetSettings(r.Context(), id)
	if errors.Is(err, player.ErrNotFound) {
		writeError(w, http.StatusNotFound, "player not found")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	// 取り消せない操作なので、名前の一致を求める。
	if req.Confirm != cur.DisplayName {
		writeError(w, http.StatusBadRequest, "確認のため、街での名前を正確に入力してください。")
		return
	}
	if err := s.players.Retire(r.Context(), id); err != nil {
		writeInternal(w, r, err)
		return
	}
	s.webhooks.PostQuiet(r.Context(), webhook.Notice{
		Event:    webhook.EventRetired,
		Severity: webhook.SeverityInfo,
		Title:    "退会",
		Body:     fmt.Sprintf("%s さんが街を出ました。", cur.DisplayName),
	})
	// 住民が消えたのでセッションも無効にする。
	_ = s.sessions.RevokeAll(r.Context(), id)
	s.sessions.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"retired": true})
}
