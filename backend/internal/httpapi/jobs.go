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

// jobs lists the jobs a player can take at the job office (公開).
func (s *Server) jobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.content.ListSelectableJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

type changeJobReq struct {
	JobName        string `json:"job_name"`
	IdempotencyKey string `json:"idempotency_key"`
}

// changeJob changes the player's job (職業安定所での転職).
func (s *Server) changeJob(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req changeJobReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.JobName == "" {
		writeError(w, http.StatusBadRequest, "job_name is required")
		return
	}
	p, err := s.actions.DoChangeJob(r.Context(), id, req.JobName, req.IdempotencyKey)
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
	writeJSON(w, http.StatusOK, toResp(p))
}
