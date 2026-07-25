package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/mail"
)

// mailbox returns the player's mail view and marks the inbox as checked.
func (s *Server) mailbox(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	mb, err := s.mail.GetMailbox(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.mail.MarkChecked(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mb)
}

// mailUnread returns just the unread count (does not mark as checked).
func (s *Server) mailUnread(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	n, err := s.mail.UnreadCount(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread": n})
}

type mailSendReq struct {
	RecipientID int64  `json:"recipient_id"`
	Body        string `json:"body"`
	GiftID      int64  `json:"gift_id"` // >0で贈り物を1個添付する
}

func (s *Server) mailSend(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req mailSendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.RecipientID <= 0 {
		writeError(w, http.StatusBadRequest, "recipient_id is required")
		return
	}
	writeMailResult(w, s.mail.Send(r.Context(), id, req.RecipientID, req.Body, req.GiftID, s.actions.GrantGift))
}

func (s *Server) mailDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	msgID, err := strconv.ParseInt(r.PathValue("msgId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid message id")
		return
	}
	writeMailResult(w, s.mail.Delete(r.Context(), id, msgID))
}

type mailSaveReq struct {
	Saved bool `json:"saved"`
}

func (s *Server) mailSave(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	msgID, err := strconv.ParseInt(r.PathValue("msgId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid message id")
		return
	}
	var req mailSaveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	writeMailResult(w, s.mail.SetSaved(r.Context(), id, msgID, req.Saved))
}

// pathID parses the {id} path value.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

// writeMailResult maps a mail write result: validation errors to 422, ok to 204.
func writeMailResult(w http.ResponseWriter, err error) {
	var verr *mail.ErrValidation
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case errors.As(err, &verr):
		writeError(w, http.StatusUnprocessableEntity, verr.Message)
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
