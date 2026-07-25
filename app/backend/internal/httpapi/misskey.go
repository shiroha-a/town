package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/profile"
)

// misskeyProfileResp is the prof facility's payload: the resident's Misskey
// account plus the viewer's relation to it.
type misskeyProfileResp struct {
	PlayerID    int64                `json:"player_id"`
	DisplayName string               `json:"display_name"`
	Profile     *profile.Profile     `json:"profile"`
	Follow      *profile.FollowState `json:"follow"`
}

// misskeyProfile serves GET /players/{id}/misskey. Here {id} is the resident
// being looked at, not the viewer — any logged-in player may read it.
func (s *Server) misskeyProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	p, err := s.players.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "その住民は見つかりません。")
		return
	}
	prof, err := s.profiles.Get(r.Context(), id)
	if errors.Is(err, profile.ErrNoProfile) {
		writeError(w, http.StatusNotFound, "Misskeyのプロフィールを取得できませんでした。")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// フォロー状態は取れなくても表示は続ける(相手インスタンスの不調で
	// プロフィールごと見えなくなるのを避ける)。
	state, err := s.profiles.State(r.Context(), PlayerIDFrom(r.Context()), id)
	if err != nil {
		state = nil
	}
	writeJSON(w, http.StatusOK, misskeyProfileResp{
		PlayerID: id, DisplayName: p.DisplayName, Profile: prof, Follow: state,
	})
}

type followReq struct {
	TargetID int64 `json:"target_id"`
}

// misskeyFollow serves POST /misskey/follow. The follower is always the
// session's player — following happens on *their* instance with *their* token,
// so there is no acting-as-someone-else form of this call.
func (s *Server) misskeyFollow(w http.ResponseWriter, r *http.Request) {
	s.followAction(w, r, false)
}

// misskeyUnfollow serves POST /misskey/unfollow.
func (s *Server) misskeyUnfollow(w http.ResponseWriter, r *http.Request) {
	s.followAction(w, r, true)
}

func (s *Server) followAction(w http.ResponseWriter, r *http.Request, undo bool) {
	var req followReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.TargetID == 0 {
		writeError(w, http.StatusBadRequest, "target_id が必要です。")
		return
	}
	viewer := PlayerIDFrom(r.Context())
	var (
		res *profile.FollowResult
		err error
	)
	if undo {
		res, err = s.profiles.Unfollow(r.Context(), viewer, req.TargetID)
	} else {
		res, err = s.profiles.Follow(r.Context(), viewer, req.TargetID)
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
