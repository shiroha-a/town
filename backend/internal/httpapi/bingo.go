package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/player"
)

// bingo returns the running event and the player's cards.
func (s *Server) bingo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	st, err := s.actions.BingoState(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type bingoCardReq struct {
	IdempotencyKey string `json:"idempotency_key"`
}

// bingoTakeCard hands the player one more card.
func (s *Server) bingoTakeCard(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req bingoCardReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := s.actions.DoBingoTakeCard(r.Context(), id, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type bingoClaimReq struct {
	CardID         int64  `json:"card_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type bingoClaimResp struct {
	Player *playerResp              `json:"player"`
	Result *action.BingoClaimResult `json:"result"`
}

// bingoClaim finalises a completed card and pays the prize.
func (s *Server) bingoClaim(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req bingoClaimReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, res, err := s.actions.DoBingoClaim(r.Context(), id, req.CardID, req.IdempotencyKey)
	if err != nil {
		var condErr *action.ConditionError
		switch {
		case errors.Is(err, player.ErrNotFound):
			writeError(w, http.StatusNotFound, "player not found")
		case errors.As(err, &condErr):
			writeError(w, http.StatusUnprocessableEntity, condErr.Message)
		default:
			writeInternal(w, r, err)
		}
		return
	}
	resp := toResp(p)
	writeJSON(w, http.StatusOK, bingoClaimResp{Player: &resp, Result: res})
}

type adminBingoReq struct {
	MaxNumber  int `json:"max_number"`
	PerDay     int `json:"per_day"`
	Days       int `json:"days"`
	LinesToWin int `json:"lines_to_win"`
}

// adminStartBingo opens a new bingo event.
func (s *Server) adminStartBingo(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req adminBingoReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.actions.StartBingo(r.Context(), req.MaxNumber, req.PerDay, req.Days, req.LinesToWin); err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"started": true})
}
