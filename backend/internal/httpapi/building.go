package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/shiroha-a/town/internal/action"
)

// building returns the construction-company screen state: the five towns and
// their land prices, the exterior/interior catalog, every player's houses (for
// grid rendering), and the caller's own houses.
func (s *Server) building(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	st, err := s.content.Building(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// houses returns every house across all towns (for main-screen grid rendering).
// Own is relative to the caller (playerID).
// publicHouses lists the houses on the map for visitors (入口の街マップ)。
// 所有者名と外観は住民名鑑と同じく公開情報。playerID=0 なので own は常にfalse。
func (s *Server) publicHouses(w http.ResponseWriter, r *http.Request) {
	list, err := s.content.ListHouses(r.Context(), 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) houses(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	list, err := s.content.ListHouses(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type buildHouseReq struct {
	Town           int    `json:"town"`
	Row            int    `json:"row"`
	Col            int    `json:"col"`
	Exterior       string `json:"exterior"`
	InteriorRank   int    `json:"interior_rank"`
	Tuika          int    `json:"tuika"` // 2軒目以降の追加種別(0=家のみ)
	IdempotencyKey string `json:"idempotency_key"`
}

// buildHouse builds a house for the player at the chosen plot (建設会社).
func (s *Server) buildHouse(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req buildHouseReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Exterior == "" {
		writeError(w, http.StatusBadRequest, "exterior is required")
		return
	}
	p, err := s.actions.DoBuildHouse(r.Context(), id, req.Town, req.Row, req.Col, req.Exterior, req.InteriorRank, req.Tuika, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type sellHouseReq struct {
	HouseID        int64  `json:"house_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// sellHouse demolishes the player's house and refunds the land price (建設会社).
func (s *Server) sellHouse(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req sellHouseReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	p, err := s.actions.DoSellHouse(r.Context(), id, req.HouseID, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type rebuildHouseReq struct {
	HouseID        int64  `json:"house_id"`
	Exterior       string `json:"exterior"`
	InteriorRank   int    `json:"interior_rank"`
	IdempotencyKey string `json:"idempotency_key"`
}

// rebuildHouse rebuilds the player's house with a new exterior/interior (建設会社).
func (s *Server) rebuildHouse(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req rebuildHouseReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.Exterior == "" {
		writeError(w, http.StatusBadRequest, "house_id and exterior are required")
		return
	}
	p, err := s.actions.DoRebuildHouse(r.Context(), id, req.HouseID, req.Exterior, req.InteriorRank, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type houseCommentReq struct {
	HouseID        int64  `json:"house_id"`
	Setumei        string `json:"setumei"`
	IdempotencyKey string `json:"idempotency_key"`
}

// houseComment sets the mouse-over comment of the player's house (マイホーム設定).
func (s *Server) houseComment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req houseCommentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	p, err := s.actions.DoSetHouseComment(r.Context(), id, req.HouseID, req.Setumei, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type houseContentsReq struct {
	HouseID  int64 `json:"house_id"`
	Contents []struct {
		Slot    int    `json:"slot"`
		Kind    string `json:"kind"`
		Title   string `json:"title"`
		URL     string `json:"url"`
		Comment string `json:"comment"`
	} `json:"contents"`
	IdempotencyKey string `json:"idempotency_key"`
}

// houseContents sets the house's content slots (コンテンツ枠設定)。
func (s *Server) houseContents(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req houseContentsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	contents := make([]action.HouseContentSlot, 0, len(req.Contents))
	for _, c := range req.Contents {
		contents = append(contents, action.HouseContentSlot{Slot: c.Slot, Kind: c.Kind, Title: c.Title, URL: c.URL, Comment: c.Comment})
	}
	p, err := s.actions.DoSetHouseContents(r.Context(), id, req.HouseID, contents, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type saisenReq struct {
	HouseID        int64  `json:"house_id"`
	Amount         int64  `json:"amount"`
	IdempotencyKey string `json:"idempotency_key"`
}

// saisen offers money at a house's offering box (さい銭).
func (s *Server) saisen(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req saisenReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "house_id and amount are required")
		return
	}
	p, err := s.actions.DoSaisen(r.Context(), id, req.HouseID, req.Amount, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type openShopReq struct {
	HouseID        int64   `json:"house_id"`
	Title          string  `json:"title"`
	Syubetu        string  `json:"syubetu"`
	Markup         float64 `json:"markup"`
	IdempotencyKey string  `json:"idempotency_key"`
}

// openHouseShop opens or reconfigures the shop attached to a house (店設定).
func (s *Server) openHouseShop(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req openShopReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.Syubetu == "" {
		writeError(w, http.StatusBadRequest, "house_id and syubetu are required")
		return
	}
	p, err := s.actions.DoOpenHouseShop(r.Context(), id, req.HouseID, req.Title, req.Syubetu, req.Markup, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

// orosi returns the wholesaler catalog for a house shop (卸問屋).
func (s *Server) orosi(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	houseID, err := strconv.ParseInt(r.URL.Query().Get("house_id"), 10, 64)
	if err != nil || houseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	st, err := s.content.Orosi(r.Context(), id, houseID)
	if err != nil {
		writeContentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type shiireReq struct {
	HouseID        int64  `json:"house_id"`
	ItemID         int64  `json:"item_id"`
	Qty            int    `json:"qty"`
	IdempotencyKey string `json:"idempotency_key"`
}

// shiire purchases items from the wholesaler into the house shop (仕入れ).
func (s *Server) shiire(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req shiireReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.ItemID <= 0 || req.Qty <= 0 {
		writeError(w, http.StatusBadRequest, "house_id, item_id, qty are required")
		return
	}
	p, err := s.actions.DoShiire(r.Context(), id, req.HouseID, req.ItemID, req.Qty, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

// houseShop returns a house shop's on-sale items for a visitor (店表示).
func (s *Server) houseShop(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	houseID, err := strconv.ParseInt(r.URL.Query().Get("house_id"), 10, 64)
	if err != nil || houseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	view, err := s.content.HouseShop(r.Context(), id, houseID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type buyHouseShopReq struct {
	HouseID        int64  `json:"house_id"`
	ItemID         int64  `json:"item_id"`
	Qty            int    `json:"qty"`
	PayMethod      string `json:"pay_method"` // "cash"(既定)/"credit"
	IdempotencyKey string `json:"idempotency_key"`
}

type buyResultResp struct {
	Total    int64  `json:"total"`
	Cashback int64  `json:"cashback"`
	Paid     int64  `json:"paid"`
	Method   string `json:"method"`
}

// buyResp is the player state plus the purchase summary (cashback etc.).
type buyResp struct {
	playerResp
	BuyResult buyResultResp `json:"buy_result"`
}

// buyFromHouseShop buys an item from a house shop (訪問販売).
func (s *Server) buyFromHouseShop(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req buyHouseShopReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.ItemID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id and item_id are required")
		return
	}
	p, result, err := s.actions.DoBuyFromHouseShop(r.Context(), id, req.HouseID, req.ItemID, req.Qty, req.PayMethod, req.IdempotencyKey)
	if err != nil {
		writeFacilityResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, buyResp{
		playerResp: toResp(p),
		BuyResult: buyResultResp{
			Total:    result.Total,
			Cashback: result.Cashback,
			Paid:     result.Paid,
			Method:   result.Method,
		},
	})
}

// yamiShop returns the 持ち物販売店(闇市) shelf of a house.
func (s *Server) yamiShop(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	houseID, err := strconv.ParseInt(r.URL.Query().Get("house_id"), 10, 64)
	if err != nil || houseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	view, err := s.content.Yami(r.Context(), id, houseID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// yamiInventory lists the player's own items for listing onto their 闇市.
func (s *Server) yamiInventory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	items, err := s.content.YamiInventory(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

type yamiListReq struct {
	HouseID        int64  `json:"house_id"`
	ItemID         int64  `json:"item_id"`
	Price          int64  `json:"price"`     // 0=既定価格(単価×残耐久)
	Warehouse      bool   `json:"warehouse"` // true=預ける(倉庫)
	IdempotencyKey string `json:"idempotency_key"`
}

// yamiList puts one of the player's items onto their 闇市 shelf (販売/預ける).
func (s *Server) yamiList(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req yamiListReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.ItemID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id and item_id are required")
		return
	}
	p, err := s.actions.DoYamiList(r.Context(), id, req.HouseID, req.ItemID, req.Price, req.Warehouse, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type yamiBuyReq struct {
	HouseID        int64  `json:"house_id"`
	ListingID      int64  `json:"listing_id"`
	PayMethod      string `json:"pay_method"` // cash / credit
	IdempotencyKey string `json:"idempotency_key"`
}

// yamiBuy buys one listing from a 闇市 (家主は500円で回収).
func (s *Server) yamiBuy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req yamiBuyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.ListingID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id and listing_id are required")
		return
	}
	p, result, err := s.actions.DoYamiBuy(r.Context(), id, req.HouseID, req.ListingID, req.PayMethod, req.IdempotencyKey)
	if err != nil {
		writeFacilityResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		playerResp
		YamiResult *action.YamiBuyResult `json:"yami_result"`
	}{toResp(p), result})
}

// companyView returns the 運営/株式会社 screen state of a house.
func (s *Server) companyView(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	houseID, err := strconv.ParseInt(r.URL.Query().Get("house_id"), 10, 64)
	if err != nil || houseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	view, err := s.content.Company(r.Context(), id, houseID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type companyStaffReq struct {
	HouseID        int64  `json:"house_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// companyStaffAdd adds an employee to the player's 運営/株式会社.
func (s *Server) companyStaffAdd(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req companyStaffReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	p, err := s.actions.DoStaffAdd(r.Context(), id, req.HouseID, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type companyEducateReq struct {
	HouseID        int64  `json:"house_id"`
	StaffID        int64  `json:"staff_id"`
	Param          string `json:"param"`  // player_statusの列名(kokugo..omoshirosa)
	Amount         int    `json:"amount"` // 消費する自分のパラメータ量(1..1000)
	PayMethod      string `json:"pay_method"`
	IdempotencyKey string `json:"idempotency_key"`
}

// companyEducate performs 社員教育 (自分のパラメータを社員へ移転).
func (s *Server) companyEducate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req companyEducateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.StaffID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id and staff_id are required")
		return
	}
	p, result, err := s.actions.DoStaffEducate(r.Context(), id, req.HouseID, req.StaffID, req.Param, req.Amount, req.PayMethod, req.IdempotencyKey)
	if err != nil {
		writeFacilityResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		playerResp
		EduResult *action.StaffEduResult `json:"edu_result"`
	}{toResp(p), result})
}

type companyBbsReq struct {
	HouseID        int64  `json:"house_id"`
	Board          string `json:"board"` // open / member
	Body           string `json:"body"`
	WantJoin       bool   `json:"want_join"`  // オープン板: 入会希望
	WantLeave      bool   `json:"want_leave"` // メンバー板: 退会希望
	IdempotencyKey string `json:"idempotency_key"`
}

// companyBbsPost writes to the 会社BBS (入会/退会希望付き投稿にも使う).
func (s *Server) companyBbsPost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req companyBbsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	if tooManyEmoji(w, req.Body) {
		return
	}
	p, err := s.actions.DoCompanyBbsPost(r.Context(), id, req.HouseID, req.Board, req.Body, req.WantJoin, req.WantLeave, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type companyApproveReq struct {
	HouseID        int64  `json:"house_id"`
	PostID         int64  `json:"post_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// companyApprove approves a pending 入会/退会 request (オーナーのみ).
func (s *Server) companyApprove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req companyApproveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.PostID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id and post_id are required")
		return
	}
	p, err := s.actions.DoCompanyApprove(r.Context(), id, req.HouseID, req.PostID, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type companyKickReq struct {
	HouseID        int64  `json:"house_id"`
	OfficerID      int64  `json:"officer_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// companyKick removes an officer (オーナーの退会指定).
func (s *Server) companyKick(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req companyKickReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.OfficerID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id and officer_id are required")
		return
	}
	p, err := s.actions.DoCompanyKick(r.Context(), id, req.HouseID, req.OfficerID, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type companyBbsDeleteReq struct {
	HouseID        int64  `json:"house_id"`
	Board          string `json:"board"`
	No             int    `json:"no"`
	IdempotencyKey string `json:"idempotency_key"`
}

// companyBbsDelete deletes a 会社BBS post by number (オーナーのみ).
func (s *Server) companyBbsDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req companyBbsDeleteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.No <= 0 {
		writeError(w, http.StatusBadRequest, "house_id and no are required")
		return
	}
	p, err := s.actions.DoCompanyBbsDelete(r.Context(), id, req.HouseID, req.Board, req.No, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

type companySeizouReq struct {
	HouseID        int64              `json:"house_id"`
	Input          action.SeizouInput `json:"input"`
	IdempotencyKey string             `json:"idempotency_key"`
}

// companySeizou manufactures an original product (オーナーのみ・1日1回).
func (s *Server) companySeizou(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req companySeizouReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	p, result, err := s.actions.DoSeizou(r.Context(), id, req.HouseID, req.Input, req.IdempotencyKey)
	if err != nil {
		writeFacilityResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		playerResp
		SeizouResult *action.SeizouResult `json:"seizou_result"`
	}{toResp(p), result})
}

// houseBbs returns a house's bulletin-board posts (誰でも閲覧可).
func (s *Server) houseBbs(w http.ResponseWriter, r *http.Request) {
	houseID, err := strconv.ParseInt(r.URL.Query().Get("house_id"), 10, 64)
	if err != nil || houseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	posts, err := s.content.HouseBbs(r.Context(), houseID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, posts)
}

type postBbsReq struct {
	HouseID        int64  `json:"house_id"`
	Kind           string `json:"kind"`
	Title          string `json:"title"`     // 家主板の記事タイトル(任意)
	ParentNo       int    `json:"parent_no"` // 通常掲示板のレス先スレッドNO(0=新規)
	Body           string `json:"body"`
	IdempotencyKey string `json:"idempotency_key"`
}

// postBbs writes a message to a house's bulletin board (掲示板書き込み).
// On success the response carries the money reward for the post (bbs_result).
func (s *Server) postBbs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req postBbsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.Body == "" {
		writeError(w, http.StatusBadRequest, "house_id and body are required")
		return
	}
	if tooManyEmoji(w, req.Body) {
		return
	}
	p, result, err := s.actions.DoPostBbs(r.Context(), id, req.HouseID, req.Kind, req.Title, req.Body, req.ParentNo, req.IdempotencyKey)
	if err != nil {
		writeFacilityResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		playerResp
		BbsResult *action.BbsPostResult `json:"bbs_result"`
	}{toResp(p), result})
}

type deleteBbsReq struct {
	HouseID        int64  `json:"house_id"`
	Kind           string `json:"kind"`
	ArticleNo      int64  `json:"article_no"` // 記事no.(投稿ID)
	ThreadNo       int64  `json:"thread_no"`  // 親記事no.(NO.x、スレッドごと削除)
	All            bool   `json:"all"`        // 全記事削除
	IdempotencyKey string `json:"idempotency_key"`
}

// deleteBbs deletes bulletin-board posts (記事no./親記事no./全記事削除).
func (s *Server) deleteBbs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req deleteBbsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	p, err := s.actions.DoDeleteBbs(r.Context(), id, req.HouseID, req.Kind, req.ArticleNo, req.ThreadNo, req.All, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

// houseShopStock returns the owner's shop stock for price setting (my_syouhin).
func (s *Server) houseShopStock(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	houseID, err := strconv.ParseInt(r.URL.Query().Get("house_id"), 10, 64)
	if err != nil || houseID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id is required")
		return
	}
	view, err := s.content.ShopStock(r.Context(), id, houseID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type setPriceReq struct {
	HouseID        int64  `json:"house_id"`
	ItemID         int64  `json:"item_id"`
	SellPrice      int64  `json:"sell_price"`
	IdempotencyKey string `json:"idempotency_key"`
}

// setShopPrice sets a per-item shelf price (個別価格設定).
func (s *Server) setShopPrice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req setPriceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HouseID <= 0 || req.ItemID <= 0 {
		writeError(w, http.StatusBadRequest, "house_id and item_id are required")
		return
	}
	p, err := s.actions.DoSetShopPrice(r.Context(), id, req.HouseID, req.ItemID, req.SellPrice, req.IdempotencyKey)
	writeFacilityResult(w, p, err)
}

// 空き地は施設(key='akichi')に統合された。空地の設定は施設編集(adminUpdateTownMap)で
// 行うため、専用の plots エンドポイントは廃止した。
