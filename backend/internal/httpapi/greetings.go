package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/webhook"
)

// greetings returns the recent town-chat posts. Public.
func (s *Server) greetings(w http.ResponseWriter, r *http.Request) {
	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	list, err := s.greeting.List(r.Context(), limit)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type greetReq struct {
	Category       string `json:"category"`
	Body           string `json:"body"`
	Color          string `json:"color"`
	Janken         string `json:"janken"`
	IdempotencyKey string `json:"idempotency_key"`
}

// postGreeting posts a greeting and returns the updated player and result.
func (s *Server) postGreeting(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req greetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Category == "" {
		writeError(w, http.StatusBadRequest, "category is required")
		return
	}
	if tooManyEmoji(w, req.Body) {
		return
	}
	p, result, err := s.actions.DoGreet(r.Context(), id, req.Category, req.Body, req.Color, req.Janken, req.IdempotencyKey)
	if errors.Is(err, action.ErrBadGreet) {
		writeError(w, http.StatusBadRequest, "本文は1〜60字、色は#rrggbbで指定してください。")
		return
	}
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
	// SSE購読者(街トップのチャット窓)へ新着を配信する。
	s.greetHub.notify()
	// NGワードが伏せ字になった書き込みは運営に知らせる。荒らしの兆候が
	// 「掲示板を見に行かないと分からない」状態を無くすため。
	//
	// 街に出るのは伏せ字だが、通知には書かれたままの本文を載せる。宛先は
	// 運営だけが見る場所で、対応を決めるには何が書かれたかが要るため
	// (「NG」だけ届いても、様子見か凍結かの判断ができない)。
	if result != nil && result.Fine {
		s.webhooks.PostQuiet(r.Context(), webhook.Notice{
			Event:    webhook.EventNGWord,
			Severity: webhook.SeverityWarn,
			Title:    "NGワードの書き込み",
			Body:     req.Body,
			Fields: []webhook.Field{
				{Name: "書いた人", Value: s.playerLabel(r.Context(), id)},
				{Name: "区分", Value: req.Category},
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"player": toResp(p), "result": result})
}

// deleteGreeting removes a greeting (admin moderation).
func (s *Server) deleteGreeting(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	gid, err := strconv.ParseInt(r.PathValue("gid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.greeting.Delete(r.Context(), gid); err != nil {
		writeInternal(w, r, err)
		return
	}
	s.greetHub.notify()
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
