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

type itemActionReq struct {
	ItemID         int64  `json:"item_id"`
	Sets           int    `json:"sets"`       // 購入セット数(buyのみ、0/未指定は1扱い)
	Facility       string `json:"facility"`   // 購入元(buyのみ。''=デパート, 'hanbai'=自販機)
	PayMethod      string `json:"pay_method"` // 購入元(buyのみ)。"cash"(既定)/"credit"
	IdempotencyKey string `json:"idempotency_key"`
}

func decodeItemAction(w http.ResponseWriter, r *http.Request) (int64, itemActionReq, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, itemActionReq{}, false
	}
	var req itemActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return 0, itemActionReq{}, false
	}
	if req.ItemID <= 0 {
		writeError(w, http.StatusBadRequest, "item_id is required")
		return 0, itemActionReq{}, false
	}
	return id, req, true
}

// writeItemActionResult maps action errors to HTTP responses.
func writeItemActionResult(w http.ResponseWriter, p *player.Player, err error) {
	if err != nil {
		var condErr *action.ConditionError
		switch {
		case errors.Is(err, player.ErrNotFound):
			writeError(w, http.StatusNotFound, "player not found")
		case errors.Is(err, action.ErrItemNotFound):
			writeError(w, http.StatusNotFound, "item not found")
		case errors.As(err, &condErr):
			writeError(w, http.StatusUnprocessableEntity, condErr.Message)
		default:
			writeInternal(w, nil, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, toResp(p))
}

func (s *Server) buy(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeItemAction(w, r)
	if !ok {
		return
	}
	p, err := s.actions.DoBuy(r.Context(), id, req.Facility, req.ItemID, req.Sets, req.PayMethod, req.IdempotencyKey)
	writeItemActionResult(w, p, err)
}

// useAllResp is the player state plus a summary of what the bulk use did.
type useAllResp struct {
	playerResp
	UseAllResult action.UseAllResult `json:"use_all_result"`
}

// useAll uses every held item once (満腹度が回復する品を除く).
func (s *Server) useAll(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	// item_id は要らないので decodeItemAction は使えない(あちらは必須にしている)。
	var req struct {
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, res, err := s.actions.DoUseAll(r.Context(), id, req.IdempotencyKey)
	if err != nil {
		writeItemActionResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, useAllResp{playerResp: toResp(p), UseAllResult: *res})
}

func (s *Server) use(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeItemAction(w, r)
	if !ok {
		return
	}
	p, err := s.actions.DoUse(r.Context(), id, req.ItemID, req.IdempotencyKey)
	writeItemActionResult(w, p, err)
}
