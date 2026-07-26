package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/serial"
)

type serialRedeemReq struct {
	Code           string `json:"code"`
	IdempotencyKey string `json:"idempotency_key"`
}

type serialRedeemResp struct {
	Player *playerResp                `json:"player"`
	Result *action.SerialRedeemResult `json:"result"`
}

// redeemSerial redeems a serial code for the player (特典).
func (s *Server) redeemSerial(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req serialRedeemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, res, err := s.actions.DoRedeemSerial(r.Context(), id, req.Code, req.IdempotencyKey)
	if err != nil {
		var condErr *action.ConditionError
		switch {
		case errors.Is(err, player.ErrNotFound):
			writeError(w, http.StatusNotFound, "player not found")
		case errors.As(err, &condErr):
			writeError(w, http.StatusUnprocessableEntity, condErr.Message)
		default:
			writeInternal(w, r, err)
		}
		return
	}
	resp := toResp(p)
	writeJSON(w, http.StatusOK, serialRedeemResp{Player: &resp, Result: res})
}

// ── 管理画面 ─────────────────────────────────────────────

func (s *Server) adminListSerials(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	list, err := s.serial.List(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// serialReq is the editable shape of a code. It is kept separate from
// serial.Code so read-only fields (used_count/created_at) and the browser's
// datetime-local format do not have to round-trip through the API.
type serialReq struct {
	Code           string `json:"code"`
	Label          string `json:"label"`
	Message        string `json:"message"`
	Effect         string `json:"effect"`
	RewardItemID   *int64 `json:"reward_item_id"`
	RewardItemUses int    `json:"reward_item_uses"`
	MaxUses        int    `json:"max_uses"`
	StartsAt       string `json:"starts_at"`
	EndsAt         string `json:"ends_at"`
	Enabled        bool   `json:"enabled"`
}

// parseLocalTime accepts an empty string (=未設定), RFC3339, or the
// datetime-local value browsers submit ("2006-01-02T15:04").
func parseLocalTime(v string) (*time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("日時の形式が正しくありません: %s", v)
}

func (req serialReq) toCode() (serial.Code, error) {
	starts, err := parseLocalTime(req.StartsAt)
	if err != nil {
		return serial.Code{}, err
	}
	ends, err := parseLocalTime(req.EndsAt)
	if err != nil {
		return serial.Code{}, err
	}
	return serial.Code{
		Code: req.Code, Label: req.Label, Message: req.Message, EffectRaw: req.Effect,
		RewardItemID: req.RewardItemID, RewardItemUses: req.RewardItemUses,
		MaxUses: req.MaxUses, StartsAt: starts, EndsAt: ends, Enabled: req.Enabled,
	}, nil
}

func (s *Server) adminCreateSerial(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req serialReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	c, err := req.toCode()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := s.serial.Create(r.Context(), c)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminUpdateSerial(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req serialReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	c, err := req.toCode()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c.ID = id
	out, err := s.serial.Update(r.Context(), c)
	if errors.Is(err, serial.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminDeleteSerial(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.serial.Delete(r.Context(), id); errors.Is(err, serial.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// adminSerialUses lists who redeemed one code.
func (s *Server) adminSerialUses(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	uses, err := s.serial.Uses(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, uses)
}
