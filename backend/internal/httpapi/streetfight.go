package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/streetfight"
)

type streetFightReq struct {
	IdempotencyKey string `json:"idempotency_key"`
}

// streetFight walks the player into a random monster (ストリートファイト).
func (s *Server) streetFight(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req streetFightReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, result, err := s.actions.DoStreetFight(r.Context(), id, req.IdempotencyKey)
	if err != nil {
		var condErr *action.ConditionError
		switch {
		case errors.As(err, &condErr):
			writeError(w, http.StatusUnprocessableEntity, condErr.Message)
		default:
			writeInternal(w, r, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"player": toResp(p), "result": result})
}

// adminListMonsters lists the street-fight monsters (admin).
func (s *Server) adminListMonsters(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	list, err := s.monsters.List(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// adminCreateMonster adds a monster (admin).
func (s *Server) adminCreateMonster(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var in streetfight.Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	m, err := s.monsters.Create(r.Context(), in)
	if err != nil {
		writeMonsterErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// adminUpdateMonster replaces a monster (admin).
func (s *Server) adminUpdateMonster(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in streetfight.Input
	if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	m, err := s.monsters.Update(r.Context(), id, in)
	if err != nil {
		writeMonsterErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// adminDeleteMonster removes a monster (admin).
func (s *Server) adminDeleteMonster(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.monsters.Delete(r.Context(), id); err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// writeMonsterErr maps a validation error to 422 and anything else to 500.
func writeMonsterErr(w http.ResponseWriter, r *http.Request, err error) {
	var vErr *streetfight.ValidationError
	if errors.As(err, &vErr) {
		writeError(w, http.StatusUnprocessableEntity, vErr.Message)
		return
	}
	writeInternal(w, r, err)
}
