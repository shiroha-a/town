package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/shiroha-a/town/internal/emoji"
	"github.com/shiroha-a/town/internal/miauth"
)

// emojiList serves the picker: GET /emojis?host=. Defaults to the viewer's own
// instance. The entries are provisional — the license we require is not in this
// response, so the decision happens in emojiResolve when one is picked.
func (s *Server) emojiList(w http.ResponseWriter, r *http.Request) {
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	if host == "" {
		p, err := s.players.Get(r.Context(), PlayerIDFrom(r.Context()))
		if err != nil {
			writeError(w, http.StatusBadRequest, "インスタンスを指定してください。")
			return
		}
		host = p.InstanceHost
	}
	host, err := miauth.NormalizeHost(host)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := s.emojis.List(r.Context(), host)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// 既に判定済みのものはピッカー側で印を付けられるよう一緒に返す。
	verdicts, err := s.emojis.Verdicts(r.Context(), host)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"host": host, "emojis": items, "verdicts": verdicts,
	})
}

type emojiResolveReq struct {
	Host string `json:"host"`
	Name string `json:"name"`
}

// emojiResolve checks one emoji against the usage rules and caches the verdict.
func (s *Server) emojiResolve(w http.ResponseWriter, r *http.Request) {
	var req emojiResolveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	host, err := miauth.NormalizeHost(req.Host)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "絵文字名が必要です。")
		return
	}
	e, err := s.emojis.Resolve(r.Context(), host, req.Name)
	if errors.Is(err, emoji.ErrRejected) {
		reason := emoji.ReasonOf(err)
		writeJSON(w, http.StatusOK, map[string]any{
			"allowed": false, "reason": reason, "message": emoji.ReasonMessage(reason),
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"allowed": true, "emoji": e, "shortcode": e.Shortcode(),
	})
}

// emojiUsed is the dictionary the client renders posts with.
func (s *Server) emojiUsed(w http.ResponseWriter, r *http.Request) {
	items, err := s.emojis.Used(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"emojis": items})
}

// tooManyEmoji answers and reports true when a post carries more custom emoji
// than allowed. 表示崩れと荒らしを防ぐための上限で、投稿系すべてで同じ数にする。
func tooManyEmoji(w http.ResponseWriter, body string) bool {
	if emoji.CountRefs(body) > emoji.MaxPerPost {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("絵文字は1回の投稿に%d個までです。", emoji.MaxPerPost))
		return true
	}
	return false
}
