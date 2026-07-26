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

type workReq struct {
	IdempotencyKey string `json:"idempotency_key"`
}

// workResultResp is the salary/raise/bonus summary shown on the work result page.
type workResultResp struct {
	ExpGained   int      `json:"exp_gained"`
	NewLevel    int      `json:"new_level"`
	LeveledUp   bool     `json:"leveled_up"`
	ThisSalary  int64    `json:"this_salary"`
	Pay         int64    `json:"pay"`
	PayEvery    int      `json:"pay_every"`
	Bonus       int64    `json:"bonus"`
	WorkBonus   int64    `json:"work_bonus"`
	WeightLossG int      `json:"weight_loss_g"`
	Mastered    []string `json:"mastered"`
}

// workResp is the player state plus the just-completed work's summary.
type workResp struct {
	playerResp
	WorkResult workResultResp `json:"work_result"`
}

// work runs the "アルバイト" action for the player. The optional idempotency_key
// makes retries safe.
func (s *Server) work(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req workReq
	// bodyは任意(空でも可)。
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	p, result, err := s.actions.DoWork(r.Context(), id, req.IdempotencyKey)
	if err != nil {
		var condErr *action.ConditionError
		switch {
		case errors.Is(err, player.ErrNotFound):
			writeError(w, http.StatusNotFound, "player not found")
		case errors.As(err, &condErr):
			// ゲームロジック上の失敗(パワー不足等)は 422 で返す
			writeError(w, http.StatusUnprocessableEntity, condErr.Message)
		default:
			writeInternal(w, r, err)
		}
		return
	}
	mastered := result.Mastered
	if mastered == nil {
		mastered = []string{}
	}
	writeJSON(w, http.StatusOK, workResp{
		playerResp: toResp(p),
		WorkResult: workResultResp{
			ExpGained:   result.ExpGained,
			NewLevel:    result.NewLevel,
			LeveledUp:   result.LeveledUp,
			ThisSalary:  result.ThisSalary,
			Pay:         result.Pay,
			PayEvery:    result.PayEvery,
			Bonus:       result.Bonus,
			WorkBonus:   result.WorkBonus,
			WeightLossG: result.WeightLossG,
			Mastered:    mastered,
		},
	})
}
