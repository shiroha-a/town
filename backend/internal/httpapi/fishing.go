package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/player"
)

// fishing returns the 釣りゲーム state (in-progress round, or the usable bait).
func (s *Server) fishing(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	st, err := s.actions.FishingState(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type fishingStartReq struct {
	ItemID         int64  `json:"item_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// fishingStart spends one bait and deals the first round.
func (s *Server) fishingStart(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req fishingStartReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.ItemID <= 0 {
		writeError(w, http.StatusBadRequest, "item_id is required")
		return
	}
	p, err := s.actions.DoFishingStart(r.Context(), id, req.ItemID, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type fishingPickReq struct {
	Card           int    `json:"card"`
	IdempotencyKey string `json:"idempotency_key"`
}

type fishingPickResp struct {
	Player *playerResp           `json:"player"`
	Result *action.FishingResult `json:"result"`
}

// fishingPick resolves one card pick.
func (s *Server) fishingPick(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req fishingPickReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, res, err := s.actions.DoFishingPick(r.Context(), id, req.Card, req.IdempotencyKey)
	if err != nil {
		var condErr *action.ConditionError
		switch {
		case errors.Is(err, player.ErrNotFound):
			writeError(w, http.StatusNotFound, "player not found")
		case errors.As(err, &condErr):
			writeError(w, http.StatusUnprocessableEntity, condErr.Message)
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	resp := toResp(p)
	writeJSON(w, http.StatusOK, fishingPickResp{Player: &resp, Result: res})
}
