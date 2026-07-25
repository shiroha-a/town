package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/shiroha-a/town/internal/action"
)

// cleagueRanking returns the league ranking. Public.
func (s *Server) cleagueRanking(w http.ResponseWriter, r *http.Request) {
	rank, err := s.cleague.Ranking(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rank)
}

// getCharacter returns a player's battle character (null if none).
func (s *Server) getCharacter(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := s.cleague.GetCharacter(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

type characterNameReq struct {
	Name           string `json:"name"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) setCharacterName(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req characterNameReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := s.actions.DoSetCharacterName(r.Context(), id, req.Name, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type growReq struct {
	Inputs         map[string]int `json:"inputs"`
	IdempotencyKey string         `json:"idempotency_key"`
}

func (s *Server) growCharacter(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req growReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := s.actions.DoGrowCharacter(r.Context(), id, req.Inputs, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type battleReq struct {
	OpponentID     int64  `json:"opponent_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) battle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req battleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, result, err := s.actions.DoBattle(r.Context(), id, req.OpponentID, req.IdempotencyKey)
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
