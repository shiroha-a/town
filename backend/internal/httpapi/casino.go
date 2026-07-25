package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/player"
)

type casinoReq struct {
	Bet            int64           `json:"bet"`
	Params         json.RawMessage `json:"params"`
	IdempotencyKey string          `json:"idempotency_key"`
}

type casinoResp struct {
	Player playerResp `json:"player"`
	Payout int64      `json:"payout"`
	Win    bool       `json:"win"`
	Detail any        `json:"detail"`
}

func (s *Server) casinoPlay(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	game := r.PathValue("game")
	var req casinoReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	res, err := s.actions.DoCasinoPlay(r.Context(), id, game, req.Bet, req.Params, req.IdempotencyKey)
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
	writeJSON(w, http.StatusOK, casinoResp{
		Player: toResp(res.Player),
		Payout: res.Payout,
		Win:    res.Win,
		Detail: res.Detail,
	})
}

func (s *Server) scratchState(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	st, err := s.actions.GetScratchState(r.Context(), id, r.PathValue("game"))
	if err != nil {
		var condErr *action.ConditionError
		if errors.As(err, &condErr) {
			writeError(w, http.StatusUnprocessableEntity, condErr.Message)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type scratchOpenReq struct {
	Card           int    `json:"card"`
	Cell           int    `json:"cell"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) scratchOpen(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req scratchOpenReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	res, err := s.actions.DoScratchOpen(r.Context(), id, r.PathValue("game"), req.Card, req.Cell, req.IdempotencyKey)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"player": toResp(res.Player),
		"value":  res.Value,
		"win":    res.Win,
		"bonus":  res.Bonus,
		"prize":  res.Prize,
		"state":  res.State,
	})
}

func (s *Server) bjRespond(w http.ResponseWriter, st *action.BJState, err error) {
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
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) bjState(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	st, err := s.actions.BJGetState(r.Context(), id)
	s.bjRespond(w, st, err)
}

type bjStartReq struct {
	Rate           int64  `json:"rate"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) bjStart(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req bjStartReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, err := s.actions.BJStart(r.Context(), id, req.Rate, req.IdempotencyKey)
	s.bjRespond(w, st, err)
}

func (s *Server) bjHit(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeBank(w, r)
	if !ok {
		return
	}
	st, err := s.actions.BJHit(r.Context(), id, req.IdempotencyKey)
	s.bjRespond(w, st, err)
}

func (s *Server) bjStand(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeBank(w, r)
	if !ok {
		return
	}
	st, err := s.actions.BJStand(r.Context(), id, req.IdempotencyKey)
	s.bjRespond(w, st, err)
}

func (s *Server) pokerRespond(w http.ResponseWriter, st *action.PokerState, err error) {
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
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) pokerState(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	st, err := s.actions.PokerGetState(r.Context(), id)
	s.pokerRespond(w, st, err)
}

func (s *Server) pokerBuy(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeBank(w, r)
	if !ok {
		return
	}
	st, err := s.actions.PokerBuy(r.Context(), id, req.IdempotencyKey)
	s.pokerRespond(w, st, err)
}

func (s *Server) pokerDeal(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeBank(w, r)
	if !ok {
		return
	}
	st, err := s.actions.PokerDeal(r.Context(), id, req.IdempotencyKey)
	s.pokerRespond(w, st, err)
}

type pokerDrawReq struct {
	Hold           []int  `json:"hold"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) pokerDraw(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req pokerDrawReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, err := s.actions.PokerDraw(r.Context(), id, req.Hold, req.IdempotencyKey)
	s.pokerRespond(w, st, err)
}

func (s *Server) pokerCashout(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeBank(w, r)
	if !ok {
		return
	}
	st, err := s.actions.PokerCashout(r.Context(), id, req.IdempotencyKey)
	s.pokerRespond(w, st, err)
}

func (s *Server) loto6Respond(w http.ResponseWriter, st *action.Loto6State, err error) {
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
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) loto6State(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	st, err := s.actions.Loto6GetState(r.Context(), id)
	s.loto6Respond(w, st, err)
}

type loto6BuyReq struct {
	Numbers        []int  `json:"numbers"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) loto6Buy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req loto6BuyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, err := s.actions.DoLoto6Buy(r.Context(), id, req.Numbers, req.IdempotencyKey)
	s.loto6Respond(w, st, err)
}
