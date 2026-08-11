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

type bankReq struct {
	Amount         int64  `json:"amount"`
	IdempotencyKey string `json:"idempotency_key"`
}

func decodeBank(w http.ResponseWriter, r *http.Request) (int64, bankReq, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, bankReq{}, false
	}
	var req bankReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return 0, bankReq{}, false
	}
	return id, req, true
}

func writeActionResult(w http.ResponseWriter, p *player.Player, err error) {
	if err != nil {
		var condErr *action.ConditionError
		switch {
		case errors.Is(err, player.ErrNotFound):
			writeError(w, http.StatusNotFound, "player not found")
		case errors.As(err, &condErr):
			writeError(w, http.StatusUnprocessableEntity, condErr.Message)
		default:
			writeInternal(w, nil, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, toResp(p))
}

func (s *Server) deposit(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeBank(w, r)
	if !ok {
		return
	}
	p, err := s.actions.DoDeposit(r.Context(), id, req.Amount, req.IdempotencyKey)
	writeActionResult(w, p, err)
}

func (s *Server) withdraw(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeBank(w, r)
	if !ok {
		return
	}
	p, err := s.actions.DoWithdraw(r.Context(), id, req.Amount, req.IdempotencyKey)
	writeActionResult(w, p, err)
}

func (s *Server) bankStatement(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	// account=super でスーパー定期口座の明細(既定は普通口座)。
	var (
		entries []action.StatementEntry
	)
	if r.URL.Query().Get("account") == "super" {
		entries, err = s.actions.BankStatementSuper(r.Context(), id)
	} else {
		entries, err = s.actions.BankStatement(r.Context(), id)
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

type transferReq struct {
	ToID           int64  `json:"to_id"`
	Amount         int64  `json:"amount"`
	IdempotencyKey string `json:"idempotency_key"`
}

// transferResultResp tells the screen what actually moved. Sent < Requested
// means the cap trimmed it (差額は自分の口座に残っている)。
type transferResultResp struct {
	Requested      int64  `json:"requested"`
	Sent           int64  `json:"sent"`
	Limit          int64  `json:"limit"`
	RemainingToday int64  `json:"remaining_today"`
	ToName         string `json:"to_name"`
}

// transferResp is the player state plus the just-made transfer's summary.
type transferResp struct {
	playerResp
	Transfer transferResultResp `json:"transfer"`
}

func (s *Server) bankTransfer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req transferReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, result, err := s.actions.DoTransfer(r.Context(), id, req.ToID, req.Amount, req.IdempotencyKey)
	if err != nil {
		writeActionResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, transferResp{
		playerResp: toResp(p),
		Transfer: transferResultResp{
			Requested:      result.Requested,
			Sent:           result.Sent,
			Limit:          result.Limit,
			RemainingToday: result.RemainingToday,
			ToName:         result.ToName,
		},
	})
}

// transferCandidateResp is one selectable recipient with today's remaining
// allowance toward them.
type transferCandidateResp struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
	SentToday   int64  `json:"sent_today"`
	Remaining   int64  `json:"remaining"`
}

type transferInfoResp struct {
	Limit      int64                   `json:"limit"`
	Recipients []transferCandidateResp `json:"recipients"`
}

// bankTransferInfo backs the 振込先セレクト: who can be paid and how much of
// today's cap is left for each.
func (s *Server) bankTransferInfo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	info, err := s.actions.TransferInfo(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	out := transferInfoResp{Limit: info.Limit, Recipients: []transferCandidateResp{}}
	for _, c := range info.Recipients {
		out.Recipients = append(out.Recipients, transferCandidateResp{
			ID: c.ID, DisplayName: c.DisplayName, SentToday: c.SentToday, Remaining: c.Remaining,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) superDeposit(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeBank(w, r)
	if !ok {
		return
	}
	p, err := s.actions.DoSuperDeposit(r.Context(), id, req.Amount, req.IdempotencyKey)
	writeActionResult(w, p, err)
}

type superCancelReq struct {
	Amount         int64  `json:"amount"`
	All            bool   `json:"all"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) superCancel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req superCancelReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := s.actions.DoSuperCancel(r.Context(), id, req.Amount, req.All, req.IdempotencyKey)
	writeActionResult(w, p, err)
}

func (s *Server) loanQuote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	q, err := s.actions.LoanQuote(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

type loanBorrowReq struct {
	Count          int    `json:"count"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) loanBorrow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req loanBorrowReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := s.actions.DoLoanBorrow(r.Context(), id, req.Count, req.IdempotencyKey)
	writeActionResult(w, p, err)
}

func (s *Server) loanRepay(w http.ResponseWriter, r *http.Request) {
	id, req, ok := decodeBank(w, r)
	if !ok {
		return
	}
	p, err := s.actions.DoLoanRepay(r.Context(), id, req.IdempotencyKey)
	writeActionResult(w, p, err)
}
