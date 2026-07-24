package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/shiroha-a/town/internal/building"
	"github.com/shiroha-a/town/internal/content"
	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/settings"
	"github.com/shiroha-a/town/internal/townmap"
)

// towns returns the configured town list (public: needed to render names/prices).
func (s *Server) towns(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, building.Towns())
}

type townReq struct {
	Name      string `json:"name"`
	LandPrice int    `json:"land_price"`
	Hidden    bool   `json:"hidden"`
}

// syncBuildingTowns pushes the town config into the building runtime cache.
func syncBuildingTowns(tcs []settings.TownConfig) {
	ts := make([]building.Town, len(tcs))
	for i, tc := range tcs {
		ts[i] = building.Town{No: i, Name: tc.Name, LandPrice: tc.LandPrice, Hidden: tc.Hidden}
	}
	building.SetTowns(ts)
}

// adminUpdateTowns replaces the town list (name + land price). 街番号は並び順。
func (s *Server) adminUpdateTowns(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var reqs []townReq
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(reqs) < 1 || len(reqs) > townmap.MaxTowns {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("街は1〜%d個にしてください。", townmap.MaxTowns))
		return
	}
	tcs := make([]settings.TownConfig, len(reqs))
	for i, tr := range reqs {
		if strings.TrimSpace(tr.Name) == "" {
			writeError(w, http.StatusBadRequest, "街の名前を入力してください。")
			return
		}
		if tr.LandPrice < 0 {
			writeError(w, http.StatusBadRequest, "地価は0以上にしてください。")
			return
		}
		tcs[i] = settings.TownConfig{Name: tr.Name, LandPrice: tr.LandPrice, Hidden: tr.Hidden}
	}
	g := s.settings.Get()
	g.Towns = tcs
	if err := s.settings.Set(r.Context(), g); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	syncBuildingTowns(tcs)
	writeJSON(w, http.StatusOK, building.Towns())
}

// townMap returns the current town map. Public: every player needs it to render
// the main screen.
func (s *Server) townMap(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.townmap.Get())
}

func (s *Server) adminUpdateTownMap(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var fs []townmap.Facility
	if err := json.NewDecoder(r.Body).Decode(&fs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// 家が建っているマスから空き地(akichi)を外すと家が孤立する。家のあるマスに
	// akichi施設が残っていることを検証する(UI迂回対策)。
	houseCells, err := s.content.ListHouseCells(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	akichi := make(map[[3]int]bool)
	for _, f := range fs {
		if f.Key == "akichi" {
			akichi[[3]int{f.Town, f.Row, f.Col}] = true
		}
	}
	for _, h := range houseCells {
		if !akichi[[3]int{h.Town, h.Row, h.Col}] {
			writeError(w, http.StatusUnprocessableEntity, "家が建っているマスの空き地は変更できません。")
			return
		}
	}
	if err := s.townmap.Set(r.Context(), fs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.townmap.Get())
}

// adminHouseCells returns cells with houses (for the facility editor to lock).
func (s *Server) adminHouseCells(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	cells, err := s.content.ListHouseCells(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cells)
}

// townAssets returns the background layer. Public: every player needs it to
// render the main screen beneath the facility layer.
func (s *Server) townAssets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.townmap.GetAssets())
}

func (s *Server) adminUpdateTownAssets(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var as []townmap.Asset
	if err := json.NewDecoder(r.Body).Decode(&as); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.townmap.SetAssets(r.Context(), as); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.townmap.GetAssets())
}

// adminFacilityPresets returns the saved facility presets (施設プリセット).
func (s *Server) adminFacilityPresets(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.townmap.GetPresets())
}

// adminUpdateFacilityPresets replaces the facility preset list.
func (s *Server) adminUpdateFacilityPresets(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var ps []townmap.FacilityPreset
	if err := json.NewDecoder(r.Body).Decode(&ps); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.townmap.SetPresets(r.Context(), ps); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.townmap.GetPresets())
}

// adminListEvents returns every custom random event (disabled included).
func (s *Server) adminListEvents(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	list, err := s.content.ListAdminEvents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// adminCreateEvent adds a custom random event.
func (s *Server) adminCreateEvent(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var e content.AdminEvent
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	out, err := s.content.CreateAdminEvent(r.Context(), e)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// adminUpdateEvent replaces a custom random event.
func (s *Server) adminUpdateEvent(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	eid, err := strconv.ParseInt(r.PathValue("eid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var e content.AdminEvent
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	e.ID = eid
	out, err := s.content.UpdateAdminEvent(r.Context(), e)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// adminDeleteEvent removes a custom random event.
func (s *Server) adminDeleteEvent(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	eid, err := strconv.ParseInt(r.PathValue("eid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.content.DeleteAdminEvent(r.Context(), eid); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) adminGetSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.settings.Get())
}

func (s *Server) adminUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var g settings.Game
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.settings.Set(r.Context(), g); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 街の一覧が含まれていればbuildingキャッシュへ同期する。
	if len(g.Towns) > 0 {
		syncBuildingTowns(g.Towns)
	}
	writeJSON(w, http.StatusOK, s.settings.Get())
}

// requireAdmin is an INTERIM authorization check used until MiAuth provides the
// authenticated session. The acting player is passed via the X-Acting-Player-Id
// header and must hold the admin role. This is deliberately temporary — once
// MiAuth lands, the acting identity comes from the session, not a header.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	h := r.Header.Get("X-Acting-Player-Id")
	if h == "" {
		writeError(w, http.StatusUnauthorized, "認証が必要です(X-Acting-Player-Id)")
		return false
	}
	id, err := strconv.ParseInt(h, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid X-Acting-Player-Id")
		return false
	}
	ok, err := s.players.HasRole(r.Context(), id, "admin")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !ok {
		writeError(w, http.StatusForbidden, "権限がありません")
		return false
	}
	return true
}

func writeContentErr(w http.ResponseWriter, err error) {
	var v *content.ValidationError
	if errors.As(err, &v) {
		writeError(w, http.StatusBadRequest, v.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

type createItemReq struct {
	Name        string          `json:"name"`
	Category    string          `json:"category"`
	Price       int64           `json:"price"`
	Effect      json.RawMessage `json:"effect"`
	StockMaster *int            `json:"stock_master"`
}

func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req createItemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	it, err := s.content.CreateItem(r.Context(), req.Name, req.Category, req.Price, req.Effect, req.StockMaster)
	if err != nil {
		writeContentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

type updateItemReq struct {
	Name        string          `json:"name"`
	Category    string          `json:"category"`
	Price       int64           `json:"price"`
	Effect      json.RawMessage `json:"effect"`
	Enabled     bool            `json:"enabled"`
	StockMaster *int            `json:"stock_master"`
}

func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req updateItemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	it, err := s.content.UpdateItem(r.Context(), id, req.Name, req.Category, req.Price, req.Effect, req.Enabled, req.StockMaster)
	if err != nil {
		writeContentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.content.DeleteItem(r.Context(), id); err != nil {
		writeContentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	items, err := s.content.ListItems(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

type jobReq struct {
	Name          string          `json:"name"`
	Requirements  json.RawMessage `json:"requirements"`
	Effect        json.RawMessage `json:"effect"`
	Salary        int64           `json:"salary"`
	PayInterval   int             `json:"pay_interval"`
	BonusRate     int             `json:"bonus_rate"`
	RaiseRate     int             `json:"raise_rate"`
	Rank          int             `json:"rank"`
	RequireMaster string          `json:"require_master"`
	BodyCost      int             `json:"body_cost"`
	NouCost       int             `json:"nou_cost"`
	Enabled       bool            `json:"enabled"`
}

func (req jobReq) toInput() content.JobInput {
	return content.JobInput{
		Name: req.Name, Requirements: req.Requirements, Effect: req.Effect,
		Salary: req.Salary, PayInterval: req.PayInterval, BonusRate: req.BonusRate,
		RaiseRate: req.RaiseRate, Rank: req.Rank, RequireMaster: req.RequireMaster,
		BodyCost: req.BodyCost, NouCost: req.NouCost, Enabled: req.Enabled,
	}
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req jobReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	in := req.toInput()
	in.Enabled = true // 新規作成は有効で作る
	j, err := s.content.CreateJob(r.Context(), in)
	if err != nil {
		writeContentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (s *Server) updateJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req jobReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	j, err := s.content.UpdateJob(r.Context(), id, req.toInput())
	if err != nil {
		writeContentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (s *Server) deleteJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.content.DeleteJob(r.Context(), id); err != nil {
		writeContentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	jobs, err := s.content.ListJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

type adminPlayerSummaryResp struct {
	ID          int64    `json:"id"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
	Money       int64    `json:"money"`
	Job         string   `json:"job"`
	JobLevel    int      `json:"job_level"`
}

func (s *Server) adminListPlayers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	players, err := s.players.AdminList(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]adminPlayerSummaryResp, 0, len(players))
	for _, p := range players {
		roles := p.Roles
		if roles == nil {
			roles = []string{}
		}
		out = append(out, adminPlayerSummaryResp{
			ID: p.ID, DisplayName: p.DisplayName, Roles: roles, Money: p.Money, Job: p.Job, JobLevel: p.JobLevel,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type adminPlayerReq struct {
	DisplayName  string     `json:"display_name"`
	Money        int64      `json:"money"`
	IsAdmin      bool       `json:"is_admin"`
	Params       paramsResp `json:"params"`
	Energy       int        `json:"energy"`
	NouEnergy    int        `json:"nou_energy"`
	Satiety      int        `json:"satiety"`
	Job          string     `json:"job"`
	JobLevel     int        `json:"job_level"`
	JobExp       int        `json:"job_exp"`
	DiseaseIndex int        `json:"disease_index"`
	HeightCm     int        `json:"height_cm"`
	WeightG      int        `json:"weight_g"`
}

func (s *Server) adminUpdatePlayer(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req adminPlayerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	err = s.players.AdminUpdate(r.Context(), id, player.AdminPlayerUpdate{
		DisplayName: req.DisplayName, Money: req.Money, IsAdmin: req.IsAdmin,
		Params: player.Params(req.Params),
		Energy: req.Energy, NouEnergy: req.NouEnergy, Satiety: req.Satiety,
		Job: req.Job, JobLevel: req.JobLevel, JobExp: req.JobExp,
		DiseaseIndex: req.DiseaseIndex, HeightCm: req.HeightCm, WeightG: req.WeightG,
	})
	if errors.Is(err, player.ErrNotFound) {
		writeError(w, http.StatusNotFound, "player not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	p, err := s.players.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toResp(p))
}

func (s *Server) adminDeletePlayer(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.players.AdminSoftDelete(r.Context(), id); errors.Is(err, player.ErrNotFound) {
		writeError(w, http.StatusNotFound, "player not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

type simulateReq struct {
	Effect json.RawMessage `json:"effect"`
	State  struct {
		Money  int64 `json:"money"`
		Params map[string]struct {
			Value int `json:"value"`
			Max   int `json:"max"`
		} `json:"params"`
	} `json:"state"`
}

// simulate dry-runs an effect against a hypothetical state and returns the plan
// plus economy warnings, without persisting anything.
func (s *Server) simulate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req simulateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	state := effects.State{Money: req.State.Money, Params: map[string]effects.ParamState{}}
	for name, p := range req.State.Params {
		state.Params[name] = effects.ParamState{Value: p.Value, Max: p.Max}
	}
	res, err := content.Simulate(req.Effect, state)
	if err != nil {
		writeContentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
