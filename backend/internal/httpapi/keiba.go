package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/action"
)

// keibaRace returns the player's current race lineup and the profit ranking.
func (s *Server) keibaRace(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	raceID, lineup, err := s.keiba.GetOrCreateRace(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ranking, err := s.keiba.Ranking(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"race_id": raceID,
		"lineup":  lineup,
		"ranking": ranking,
	})
}

type keibaBetReq struct {
	RaceID         int64  `json:"race_id"`
	Bets           []int  `json:"bets"` // 馬インデックスごとの購入枚数(長さ6)
	IdempotencyKey string `json:"idempotency_key"`
}

// keibaBet places bets, runs the race and returns the result plus the updated player.
func (s *Server) keibaBet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req keibaBetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, result, err := s.actions.DoKeibaBet(r.Context(), id, req.RaceID, req.Bets, req.IdempotencyKey)
	if errors.Is(err, action.ErrBadBet) {
		writeError(w, http.StatusBadRequest, "invalid or stale bet")
		return
	}
	if err != nil {
		var condErr *action.ConditionError
		switch {
		case errors.As(err, &condErr):
			writeError(w, http.StatusUnprocessableEntity, condErr.Message)
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"player": toResp(p), "result": result})
}
