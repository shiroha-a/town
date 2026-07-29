package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/feedback"
)

// writeFeedbackErr maps the package's errors onto status codes.
func writeFeedbackErr(w http.ResponseWriter, r *http.Request, err error) {
	var vErr *feedback.ErrValidation
	var fErr *feedback.ErrForbidden
	switch {
	case errors.As(err, &vErr):
		writeError(w, http.StatusUnprocessableEntity, vErr.Message)
	case errors.As(err, &fErr):
		writeError(w, http.StatusForbidden, fErr.Message)
	case errors.Is(err, feedback.ErrNotFound):
		writeError(w, http.StatusNotFound, "その投稿はありません。")
	default:
		writeInternal(w, r, err)
	}
}

// postIDFromPath pulls {pid} out of the route.
func postIDFromPath(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

// feedbackList serves the 目安箱 list. Public: the town is browsable before
// logging in, and so is this.
func (s *Server) feedbackList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.feedback.List(r.Context(), feedback.ListOptions{
		Kind:   q.Get("kind"),
		Status: q.Get("status"),
		Sort:   q.Get("sort"),
		Viewer: PlayerIDFrom(r.Context()),
	})
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// feedbackGet serves one post with its comments. Public.
func (s *Server) feedbackGet(w http.ResponseWriter, r *http.Request) {
	id, ok := postIDFromPath(w, r, "pid")
	if !ok {
		return
	}
	d, err := s.feedback.Get(r.Context(), id, PlayerIDFrom(r.Context()))
	if err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type feedbackPostReq struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// feedbackCreate posts a new entry (login required; guests are blocked by the
// auth guard through the /players/{id}/ path).
func (s *Server) feedbackCreate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req feedbackPostReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	postID, err := s.feedback.Create(r.Context(), id, req.Kind, req.Title, req.Body)
	if err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	d, err := s.feedback.Get(r.Context(), postID, id)
	if err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type feedbackCommentReq struct {
	Body string `json:"body"`
}

// feedbackComment replies to a post. An admin's reply is marked as staff and the
// post's author gets an in-game mail about it.
func (s *Server) feedbackComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	postID, ok := postIDFromPath(w, r, "pid")
	if !ok {
		return
	}
	var req feedbackCommentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	isAdmin, err := s.players.HasRole(r.Context(), id, "admin")
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	res, err := s.feedback.Comment(r.Context(), postID, id, isAdmin, req.Body)
	if err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	// 返信は待たれているので、投稿者にはゲーム内メールで知らせる(メール通知が
	// オンなら端末にも届く)。送れなくても返信自体は成立させる。
	if res.NotifyOwner != nil {
		body := fmt.Sprintf("目安箱の「%s」に返信がつきました。\n\n%s", res.PostTitle, req.Body)
		if err := s.mail.Send(r.Context(), id, *res.NotifyOwner, body, 0, nil); err != nil {
			slog.Warn("feedback: 返信のメール送信に失敗", "post", res.PostID, "err", err)
		}
	}
	d, err := s.feedback.Get(r.Context(), postID, id)
	if err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// feedbackVote agrees with a post, or takes the agreement back.
func (s *Server) feedbackVote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	postID, ok := postIDFromPath(w, r, "pid")
	if !ok {
		return
	}
	if _, err := s.feedback.Vote(r.Context(), postID, id); err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	d, err := s.feedback.Get(r.Context(), postID, id)
	if err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// feedbackDeletePost removes a post (author or admin).
func (s *Server) feedbackDeletePost(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	postID, ok := postIDFromPath(w, r, "pid")
	if !ok {
		return
	}
	isAdmin, err := s.players.HasRole(r.Context(), id, "admin")
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	if err := s.feedback.DeletePost(r.Context(), postID, id, isAdmin); err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// feedbackDeleteComment removes a comment (author or admin).
func (s *Server) feedbackDeleteComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	commentID, ok := postIDFromPath(w, r, "cid")
	if !ok {
		return
	}
	isAdmin, err := s.players.HasRole(r.Context(), id, "admin")
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	if err := s.feedback.DeleteComment(r.Context(), commentID, id, isAdmin); err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

type feedbackStatusReq struct {
	Status string `json:"status"`
}

// adminFeedbackStatus moves a post's status (admin only).
func (s *Server) adminFeedbackStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	postID, ok := postIDFromPath(w, r, "pid")
	if !ok {
		return
	}
	var req feedbackStatusReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.feedback.SetStatus(r.Context(), postID, req.Status); err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	d, err := s.feedback.Get(r.Context(), postID, PlayerIDFrom(r.Context()))
	if err != nil {
		writeFeedbackErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
