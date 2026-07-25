package httpapi

import (
	"net/http"
	"strconv"
)

// attendanceBoard returns the attendance matrix and rate ranking. Public.
func (s *Server) attendanceBoard(w http.ResponseWriter, r *http.Request) {
	days := 14
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 60 {
			days = n
		}
	}
	board, err := s.attendance.Board(r.Context(), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, board)
}

// attendanceCheckin records the player's visit for today (called on town load).
func (s *Server) attendanceCheckin(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	recorded, err := s.attendance.Checkin(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"recorded": recorded})
}
