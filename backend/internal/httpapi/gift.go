package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// giftShop returns the ギフト屋 state (held gifts + convertible items).
func (s *Server) giftShop(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	st, err := s.actions.GiftShopState(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type giftConvertReq struct {
	ItemID         int64  `json:"item_id"`
	Uses           int    `json:"uses"` // 0以下=まとめて全部
	IdempotencyKey string `json:"idempotency_key"`
}

// giftConvert wraps part of a held item into a gift for the fee.
func (s *Server) giftConvert(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req giftConvertReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.ItemID <= 0 {
		writeError(w, http.StatusBadRequest, "item_id is required")
		return
	}
	p, err := s.actions.DoGiftConvert(r.Context(), id, req.ItemID, req.Uses, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}
