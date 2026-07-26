package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

type onsenReq struct {
	BathID         int64  `json:"bath_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// onsenBathe accelerates the player's power recovery at the chosen bath.
func (s *Server) onsenBathe(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req onsenReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.BathID <= 0 {
		writeError(w, http.StatusBadRequest, "bath_id is required")
		return
	}
	p, err := s.actions.DoOnsen(r.Context(), id, req.BathID, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

// onsenLeave ends the onsen session, resetting recovery to normal speed.
func (s *Server) onsenLeave(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	p, err := s.actions.DoOnsenLeave(r.Context(), id)
	writeFacilityResult(w, p, err)
}

// onsenTick advances the bather's recovery to now so the bath screen can poll
// and show power rising smoothly, without waiting for the worker's coarse tick.
func (s *Server) onsenTick(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	p, err := s.actions.DoOnsenTick(r.Context(), id)
	writeFacilityResult(w, p, err)
}
