package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type eventRollReq struct {
	IdempotencyKey string `json:"idempotency_key"`
}

// eventRoll rolls for a random event (called on town load). Returns the updated
// player and the event outcome (null if no event fired). The idempotency key
// makes a retry of the same roll safe while distinct town-loads roll anew.
func (s *Server) eventRoll(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req eventRollReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, outcome, err := s.actions.DoEventRoll(r.Context(), id, req.IdempotencyKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"player": toResp(p), "event": outcome})
}
