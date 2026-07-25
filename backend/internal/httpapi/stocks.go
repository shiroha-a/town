package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/stock"
)

// stocks returns the current prices and recent price-movement log. Public
// (used by the town-screen ticker and the 株 view).
func (s *Server) stocks(w http.ResponseWriter, r *http.Request) {
	prices, err := s.stock.Prices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log, err := s.stock.EventLog(r.Context(), 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prices": prices, "event_log": log})
}

// playerStocks returns a player's holdings (with P&L) and trade history.
func (s *Server) playerStocks(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	holdings, err := s.stock.Holdings(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	history, err := s.stock.History(r.Context(), id, stock.TradeLogKeep)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"holdings": holdings, "history": history})
}

type stockTradeReq struct {
	Symbol         string `json:"symbol"`
	Quantity       int    `json:"quantity"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) stockBuy(w http.ResponseWriter, r *http.Request)  { s.stockTrade(w, r, false) }
func (s *Server) stockSell(w http.ResponseWriter, r *http.Request) { s.stockTrade(w, r, true) }

// stockTrade handles buy and sell (sell=true).
func (s *Server) stockTrade(w http.ResponseWriter, r *http.Request, sell bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req stockTradeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !stock.ValidSymbol(req.Symbol) || req.Quantity <= 0 {
		writeError(w, http.StatusBadRequest, "symbol and positive quantity are required")
		return
	}
	var p *player.Player
	if sell {
		p, err = s.actions.DoSellStock(r.Context(), id, req.Symbol, req.Quantity, req.IdempotencyKey)
	} else {
		p, err = s.actions.DoBuyStock(r.Context(), id, req.Symbol, req.Quantity, req.IdempotencyKey)
	}
	writeStockResult(w, p, err)
}

type stockSettleReq struct {
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) stockSettle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req stockSettleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := s.actions.DoSettleStock(r.Context(), id, req.IdempotencyKey)
	writeStockResult(w, p, err)
}

// writeStockResult maps the action result to a response, translating the
// bad-symbol error to 400.
func writeStockResult(w http.ResponseWriter, p *player.Player, err error) {
	if errors.Is(err, action.ErrBadStock) {
		writeError(w, http.StatusBadRequest, "invalid stock symbol or quantity")
		return
	}
	writeFacilityResult(w, p, err)
}
