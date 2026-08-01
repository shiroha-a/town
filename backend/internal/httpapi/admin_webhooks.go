package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/webhook"
)

type webhookReq struct {
	URL     string          `json:"url"`
	Label   string          `json:"label"`
	Enabled bool            `json:"enabled"`
	Events  []webhook.Event `json:"events"`
}

// hooksReady guards the destination CRUD. 通知そのものは積むだけなら nil でも
// 素通りするが、宛先の読み書きはDBが要る。配線されていない環境で500を出さない。
func (s *Server) hooksReady(w http.ResponseWriter) bool {
	if s.webhooks == nil {
		writeError(w, http.StatusServiceUnavailable, "外部通知は利用できません。")
		return false
	}
	return true
}

// adminListWebhooks returns the destinations and the catalogue of events the
// screen offers.
func (s *Server) adminListWebhooks(w http.ResponseWriter, r *http.Request) {
	if !s.hooksReady(w) {
		return
	}
	list, err := s.webhooks.List(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"webhooks": list,
		"events":   webhook.AllEvents,
	})
}

func (s *Server) adminCreateWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.hooksReady(w) {
		return
	}
	var req webhookReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	h, err := s.webhooks.Create(r.Context(), req.URL, req.Label, req.Events, req.Enabled)
	if writeWebhookErr(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) adminUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.hooksReady(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("wid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req webhookReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	h, err := s.webhooks.Update(r.Context(), id, req.URL, req.Label, req.Events, req.Enabled)
	if writeWebhookErr(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) adminDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.hooksReady(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("wid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if writeWebhookErr(w, r, s.webhooks.Delete(r.Context(), id)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// adminTestWebhook queues a試し送り. 実際に出るのはworkerの次のtick。
func (s *Server) adminTestWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.hooksReady(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("wid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if writeWebhookErr(w, r, s.webhooks.Test(r.Context(), id)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"queued": true})
}

// writeWebhookErr maps the package errors onto responses. 返り値trueで応答済み。
func writeWebhookErr(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	var invalid *webhook.ErrValidation
	switch {
	case errors.Is(err, webhook.ErrNotFound):
		writeError(w, http.StatusNotFound, "宛先が見つかりません。")
	case errors.As(err, &invalid):
		writeError(w, http.StatusUnprocessableEntity, invalid.Message)
	default:
		writeInternal(w, r, err)
	}
	return true
}
