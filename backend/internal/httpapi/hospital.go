package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

type hospitalTreatReq struct {
	IdempotencyKey string `json:"idempotency_key"`
}

// hospitalTreat cures the player's current disease at the hospital. The fee is
// derived server-side from the disease name; body is optional.
func (s *Server) hospitalTreat(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req hospitalTreatReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := s.actions.DoHospitalTreat(r.Context(), id, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}
