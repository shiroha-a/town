package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/webhook"
)

// 管理画面から、住民の持ち物を直に触り、お金の動きを追うための口。
// すべて管理者のみ(authGuard が /api/v1/admin/ を見て弾く)。

type adminHeldItemResp struct {
	ItemID          int64      `json:"item_id"`
	Name            string     `json:"name"`
	Category        string     `json:"category"`
	Quantity        int        `json:"quantity"`
	RemainingUses   int        `json:"remaining_uses"`
	Sets            int        `json:"sets"`
	Durability      int        `json:"durability"`
	DurabilityUnit  string     `json:"durability_unit"`
	UseIntervalMin  int        `json:"use_interval_min"`
	Usable          bool       `json:"usable"`
	NextAvailableAt *time.Time `json:"next_available_at"`
}

// adminPathIDs pulls {id} と {itemId} out of the admin item routes.
func adminPathIDs(w http.ResponseWriter, r *http.Request, withItem bool) (int64, int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, 0, false
	}
	if !withItem {
		return id, 0, true
	}
	itemID, err := strconv.ParseInt(r.PathValue("itemId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return 0, 0, false
	}
	return id, itemID, true
}

func (s *Server) adminListPlayerItems(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, _, ok := adminPathIDs(w, r, false)
	if !ok {
		return
	}
	items, err := s.players.AdminListItems(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	out := make([]adminHeldItemResp, 0, len(items))
	for _, it := range items {
		out = append(out, adminHeldItemResp{
			ItemID: it.ItemID, Name: it.Name, Category: it.Category,
			Quantity: it.Quantity, RemainingUses: it.RemainingUses, Sets: it.Sets,
			Durability: it.Durability, DurabilityUnit: it.DurabilityUnit,
			UseIntervalMin: it.UseIntervalMin, Usable: it.Usable,
			NextAvailableAt: it.NextAvailableAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type adminSetItemReq struct {
	RemainingUses int  `json:"remaining_uses"`
	ClearCooldown bool `json:"clear_cooldown"`
}

func (s *Server) adminSetPlayerItem(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, itemID, ok := adminPathIDs(w, r, true)
	if !ok {
		return
	}
	var req adminSetItemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	err := s.players.AdminSetItem(r.Context(), id, itemID, req.RemainingUses, req.ClearCooldown)
	switch {
	case errors.Is(err, player.ErrNotFound):
		writeError(w, http.StatusNotFound, "player not found")
	case errors.Is(err, player.ErrItemNotFound):
		writeError(w, http.StatusNotFound, "item not found")
	case err != nil:
		writeInternal(w, r, err)
	default:
		s.adminListPlayerItems(w, r)
	}
}

func (s *Server) adminDeletePlayerItem(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, itemID, ok := adminPathIDs(w, r, true)
	if !ok {
		return
	}
	if err := s.players.AdminDeleteItem(r.Context(), id, itemID); err != nil {
		writeInternal(w, r, err)
		return
	}
	s.adminListPlayerItems(w, r)
}

type itemTotalResp struct {
	ItemID        int64  `json:"item_id"`
	Name          string `json:"name"`
	Category      string `json:"category"`
	Holders       int    `json:"holders"`
	Sets          int64  `json:"sets"`
	RemainingUses int64  `json:"remaining_uses"`
}

// adminItemTotals is 何がどれだけ世に出回っているか.
func (s *Server) adminItemTotals(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	totals, err := s.players.AdminItemTotals(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	out := make([]itemTotalResp, 0, len(totals))
	for _, t := range totals {
		out = append(out, itemTotalResp{
			ItemID: t.ItemID, Name: t.Name, Category: t.Category,
			Holders: t.Holders, Sets: t.Sets, RemainingUses: t.RemainingUses,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type moneyMovementResp struct {
	TxID       int64     `json:"tx_id"`
	PlayerID   int64     `json:"player_id"`
	Kind       string    `json:"kind"` // cash / savings / super_savings / system
	Delta      int64     `json:"delta"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
	Reversed   bool      `json:"reversed"`
	IsReversal bool      `json:"is_reversal"`
}

type reverseTxReq struct {
	TxID int64 `json:"tx_id"`
}

// adminReverseTx undoes one transaction's money by posting its mirror image.
// 台帳は追記専用なので行は消さない(消すと合計を後から検算できなくなる)。
func (s *Server) adminReverseTx(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req reverseTxReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.TxID <= 0 {
		writeError(w, http.StatusBadRequest, "tx_id is required")
		return
	}
	err := ledger.New(s.pool).Reverse(r.Context(), req.TxID)
	var neg *ledger.NegativeError
	switch {
	case errors.Is(err, ledger.ErrTxNotFound):
		writeError(w, http.StatusNotFound, "その取引は見つかりません。")
	case errors.Is(err, ledger.ErrAlreadyReversed):
		writeError(w, http.StatusUnprocessableEntity, "この取引は取り消し済みです。")
	case errors.As(err, &neg):
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf(
			"取り消すと%sの残高が%d円になります。先に所持金を調整してください。",
			neg.Account, neg.Result))
	case err != nil:
		writeInternal(w, r, err)
	default:
		s.webhooks.PostQuiet(r.Context(), webhook.Notice{
			Event:    webhook.EventTxReversed,
			Severity: webhook.SeverityWarn,
			Title:    "お金の動きを取り消しました",
			Body:     fmt.Sprintf("取引 #%d を逆仕訳で打ち消しました。", req.TxID),
			Fields: []webhook.Field{
				{Name: "操作した人", Value: s.playerLabel(r.Context(), PlayerIDFrom(r.Context()))},
			},
		})
		writeJSON(w, http.StatusOK, map[string]any{"reversed": true, "tx_id": req.TxID})
	}
}

type moneyReasonResp struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
	In     int64  `json:"in"`
	Out    int64  `json:"out"`
	Net    int64  `json:"net"`
}

type moneyAuditResp struct {
	PlayerID     int64               `json:"player_id"` // 0=全ユーザー
	Cash         int64               `json:"cash"`
	Savings      int64               `json:"savings"`
	SuperSavings int64               `json:"super_savings"`
	LoanRemain   int64               `json:"loan_remain"`
	Total        int64               `json:"total"` // 現金+貯金+定期-ローン
	ZeroSum      int64               `json:"zero_sum"`
	Reasons      []moneyReasonResp   `json:"reasons"`
	System       []accountTotalResp  `json:"system"`
	History      []moneyMovementResp `json:"history"`
}

type accountTotalResp struct {
	Account string `json:"account"`
	Balance int64  `json:"balance"`
}

// adminMoneyAudit returns money totals and recent movements. player_id=0(既定)は
// 全ユーザーぶん。台帳は複式なので、ここに出る数字は必ず帳尻が合う。
func (s *Server) adminMoneyAudit(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	playerID, _ := strconv.ParseInt(r.URL.Query().Get("player_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	led := ledger.New(s.pool)

	totals, err := led.Totals(r.Context(), playerID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	reasons, err := led.ReasonTotals(r.Context(), playerID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	history, err := led.History(r.Context(), playerID, limit)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	// システム勘定(蛇口とシンク)と帳尻は全体を見るときだけ意味を持つ。
	var system []ledger.AccountTotal
	var zeroSum int64
	if playerID == 0 {
		if system, err = led.SystemTotals(r.Context()); err != nil {
			writeInternal(w, r, err)
			return
		}
		if zeroSum, err = led.AuditZeroSum(r.Context()); err != nil {
			writeInternal(w, r, err)
			return
		}
	}

	out := moneyAuditResp{
		PlayerID: playerID,
		Cash:     totals.Cash, Savings: totals.Savings, SuperSavings: totals.SuperSavings,
		LoanRemain: totals.LoanRemain,
		Total:      totals.Cash + totals.Savings + totals.SuperSavings - totals.LoanRemain,
		ZeroSum:    zeroSum,
		Reasons:    make([]moneyReasonResp, 0, len(reasons)),
		System:     make([]accountTotalResp, 0, len(system)),
		History:    make([]moneyMovementResp, 0, len(history)),
	}
	for _, t := range reasons {
		out.Reasons = append(out.Reasons, moneyReasonResp{
			Reason: t.Reason, Count: t.Count, In: t.In, Out: t.Out, Net: t.Net,
		})
	}
	for _, a := range system {
		out.System = append(out.System, accountTotalResp{Account: a.Account, Balance: a.Balance})
	}
	for _, m := range history {
		out.History = append(out.History, moneyMovementResp{
			TxID: m.TxID, PlayerID: m.PlayerID, Kind: m.Kind,
			Delta: m.Delta, Reason: m.Reason, CreatedAt: m.CreatedAt,
			Reversed: m.Reversed, IsReversal: m.IsReversal,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
