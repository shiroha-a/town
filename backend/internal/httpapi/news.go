package httpapi

import (
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/ranking"
)

// newsLimit parses ?limit= with a default and cap.
func newsLimit(r *http.Request, def, max int) int {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= max {
			return n
		}
	}
	return def
}

// townNews returns the town-wide news feed (役場「街のニュース」). Public.
func (s *Server) townNews(w http.ResponseWriter, r *http.Request) {
	list, err := s.news.ListTownWide(r.Context(), newsLimit(r, 100, 200))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// playerNews returns one resident's event history (役場「最近の出来事」). Public:
// unlike the legacy 個人イベント (self only), any resident's history is readable
// from the 住民名鑑.
func (s *Server) playerNews(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.news.ListByActor(r.Context(), id, newsLimit(r, 50, 200))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// rankingKeys lists the selectable rankings for the 役場 dropdown. Public.
func (s *Server) rankingKeys(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ranking.Keys)
}

// townRanking returns one ranking (?key=, ?limit=, ?self= for the caller's own
// row when out of the top rows). Public.
func (s *Server) townRanking(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		key = "assets"
	}
	var self int64
	if v := r.URL.Query().Get("self"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			self = n
		}
	}
	res, err := s.ranking.Rank(r.Context(), key, newsLimit(r, ranking.Limit, ranking.Limit), self)
	if err == ranking.ErrUnknownKey {
		writeError(w, http.StatusBadRequest, "unknown ranking key")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
