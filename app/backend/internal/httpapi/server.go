// Package httpapi exposes the REST API under /api/v1.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/attendance"
	"github.com/shiroha-a/town/internal/cleague"
	"github.com/shiroha-a/town/internal/content"
	"github.com/shiroha-a/town/internal/greeting"
	"github.com/shiroha-a/town/internal/keiba"
	"github.com/shiroha-a/town/internal/mail"
	"github.com/shiroha-a/town/internal/news"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/ranking"
	"github.com/shiroha-a/town/internal/settings"
	"github.com/shiroha-a/town/internal/stock"
	"github.com/shiroha-a/town/internal/townmap"
)

// Server holds the API dependencies.
type Server struct {
	players    *player.Service
	actions    *action.Service
	content    *content.Service
	settings   *settings.Store
	townmap    *townmap.Store
	stock      *stock.Service
	keiba      *keiba.Service
	mail       *mail.Service
	greeting   *greeting.Service
	attendance *attendance.Service
	cleague    *cleague.Service
	news       *news.Service
	ranking    *ranking.Service
	greetHub   *greetHub // あいさつSSE配信のプロセス内ハブ
}

// NewServer builds the HTTP handler for the REST API.
func NewServer(players *player.Service, actions *action.Service, contentSvc *content.Service, st *settings.Store, tmap *townmap.Store, stockSvc *stock.Service, keibaSvc *keiba.Service, mailSvc *mail.Service, greetingSvc *greeting.Service, attendanceSvc *attendance.Service, cleagueSvc *cleague.Service, newsSvc *news.Service, rankingSvc *ranking.Service) http.Handler {
	s := &Server{players: players, actions: actions, content: contentSvc, settings: st, townmap: tmap, stock: stockSvc, keiba: keibaSvc, mail: mailSvc, greeting: greetingSvc, attendance: attendanceSvc, cleague: cleagueSvc, news: newsSvc, ranking: rankingSvc, greetHub: newGreetHub()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("POST /api/v1/players", s.registerPlayer)
	mux.HandleFunc("GET /api/v1/players", s.listPlayers)
	mux.HandleFunc("GET /api/v1/players/{id}", s.getPlayer)
	mux.HandleFunc("GET /api/v1/participants", s.participants)
	mux.HandleFunc("GET /api/v1/players/{id}/profile", s.playerProfile)
	mux.HandleFunc("GET /api/v1/news", s.townNews)
	mux.HandleFunc("GET /api/v1/players/{id}/news", s.playerNews)
	mux.HandleFunc("GET /api/v1/ranking", s.townRanking)
	mux.HandleFunc("GET /api/v1/ranking/keys", s.rankingKeys)
	mux.HandleFunc("GET /api/v1/townmap", s.townMap)
	mux.HandleFunc("GET /api/v1/townassets", s.townAssets)
	mux.HandleFunc("GET /api/v1/towns", s.towns)
	mux.HandleFunc("GET /api/v1/players/{id}/houses", s.houses)
	mux.HandleFunc("GET /api/v1/assets/{name}", s.serveAsset)
	mux.HandleFunc("GET /api/v1/stocks", s.stocks)
	mux.HandleFunc("GET /api/v1/players/{id}/stocks", s.playerStocks)
	mux.HandleFunc("POST /api/v1/players/{id}/stocks/buy", s.stockBuy)
	mux.HandleFunc("POST /api/v1/players/{id}/stocks/sell", s.stockSell)
	mux.HandleFunc("POST /api/v1/players/{id}/stocks/settle", s.stockSettle)
	mux.HandleFunc("GET /api/v1/players/{id}/keiba", s.keibaRace)
	mux.HandleFunc("POST /api/v1/players/{id}/keiba/bet", s.keibaBet)
	mux.HandleFunc("GET /api/v1/players/{id}/mail", s.mailbox)
	mux.HandleFunc("GET /api/v1/players/{id}/mail/unread", s.mailUnread)
	mux.HandleFunc("POST /api/v1/players/{id}/mail/send", s.mailSend)
	mux.HandleFunc("DELETE /api/v1/players/{id}/mail/{msgId}", s.mailDelete)
	mux.HandleFunc("PUT /api/v1/players/{id}/mail/{msgId}/save", s.mailSave)
	mux.HandleFunc("GET /api/v1/greetings", s.greetings)
	mux.HandleFunc("GET /api/v1/greetings/stream", s.greetingsStream)
	mux.HandleFunc("POST /api/v1/players/{id}/greetings", s.postGreeting)
	mux.HandleFunc("DELETE /api/v1/admin/greetings/{gid}", s.deleteGreeting)
	mux.HandleFunc("GET /api/v1/attendance", s.attendanceBoard)
	mux.HandleFunc("POST /api/v1/players/{id}/attendance/checkin", s.attendanceCheckin)
	mux.HandleFunc("POST /api/v1/players/{id}/events/roll", s.eventRoll)
	mux.HandleFunc("GET /api/v1/cleague", s.cleagueRanking)
	mux.HandleFunc("GET /api/v1/players/{id}/character", s.getCharacter)
	mux.HandleFunc("POST /api/v1/players/{id}/character", s.setCharacterName)
	mux.HandleFunc("POST /api/v1/players/{id}/character/grow", s.growCharacter)
	mux.HandleFunc("POST /api/v1/players/{id}/character/battle", s.battle)
	mux.HandleFunc("GET /api/v1/items", s.shopItems)
	mux.HandleFunc("GET /api/v1/facilities/{facility}/menu", s.facilityMenu)
	mux.HandleFunc("POST /api/v1/players/{id}/eat", s.eat)
	mux.HandleFunc("POST /api/v1/players/{id}/hospital/treat", s.hospitalTreat)
	mux.HandleFunc("POST /api/v1/players/{id}/onsen/bathe", s.onsenBathe)
	mux.HandleFunc("POST /api/v1/players/{id}/onsen/leave", s.onsenLeave)
	mux.HandleFunc("POST /api/v1/players/{id}/onsen/tick", s.onsenTick)
	mux.HandleFunc("GET /api/v1/players/{id}/building", s.building)
	mux.HandleFunc("POST /api/v1/players/{id}/move", s.moveTown)
	mux.HandleFunc("POST /api/v1/players/{id}/warp", s.warp)
	mux.HandleFunc("POST /api/v1/players/{id}/building/build", s.buildHouse)
	mux.HandleFunc("POST /api/v1/players/{id}/building/sell", s.sellHouse)
	mux.HandleFunc("POST /api/v1/players/{id}/building/rebuild", s.rebuildHouse)
	mux.HandleFunc("POST /api/v1/players/{id}/building/comment", s.houseComment)
	mux.HandleFunc("POST /api/v1/players/{id}/building/contents", s.houseContents)
	mux.HandleFunc("POST /api/v1/players/{id}/building/saisen", s.saisen)
	mux.HandleFunc("POST /api/v1/players/{id}/building/shop/open", s.openHouseShop)
	mux.HandleFunc("GET /api/v1/players/{id}/building/orosi", s.orosi)
	mux.HandleFunc("POST /api/v1/players/{id}/building/shiire", s.shiire)
	mux.HandleFunc("GET /api/v1/players/{id}/building/shop", s.houseShop)
	mux.HandleFunc("POST /api/v1/players/{id}/building/shop/buy", s.buyFromHouseShop)
	mux.HandleFunc("GET /api/v1/players/{id}/building/yami", s.yamiShop)
	mux.HandleFunc("GET /api/v1/players/{id}/building/yami/inventory", s.yamiInventory)
	mux.HandleFunc("POST /api/v1/players/{id}/building/yami/list", s.yamiList)
	mux.HandleFunc("POST /api/v1/players/{id}/building/yami/buy", s.yamiBuy)
	mux.HandleFunc("GET /api/v1/players/{id}/building/company", s.companyView)
	mux.HandleFunc("POST /api/v1/players/{id}/building/company/staff", s.companyStaffAdd)
	mux.HandleFunc("POST /api/v1/players/{id}/building/company/educate", s.companyEducate)
	mux.HandleFunc("POST /api/v1/players/{id}/building/company/bbs", s.companyBbsPost)
	mux.HandleFunc("POST /api/v1/players/{id}/building/company/approve", s.companyApprove)
	mux.HandleFunc("POST /api/v1/players/{id}/building/company/kick", s.companyKick)
	mux.HandleFunc("POST /api/v1/players/{id}/building/company/bbs/delete", s.companyBbsDelete)
	mux.HandleFunc("POST /api/v1/players/{id}/building/company/seizou", s.companySeizou)
	mux.HandleFunc("GET /api/v1/players/{id}/building/bbs", s.houseBbs)
	mux.HandleFunc("POST /api/v1/players/{id}/building/bbs/post", s.postBbs)
	mux.HandleFunc("POST /api/v1/players/{id}/building/bbs/delete", s.deleteBbs)
	mux.HandleFunc("GET /api/v1/players/{id}/building/shop/stock", s.houseShopStock)
	mux.HandleFunc("POST /api/v1/players/{id}/building/shop/price", s.setShopPrice)
	mux.HandleFunc("POST /api/v1/players/{id}/facilities/{facility}/use", s.facilityUse)
	mux.HandleFunc("POST /api/v1/players/{id}/school/attend", s.schoolAttend)
	mux.HandleFunc("GET /api/v1/jobs", s.jobs)
	mux.HandleFunc("POST /api/v1/players/{id}/work", s.work)
	mux.HandleFunc("POST /api/v1/players/{id}/job", s.changeJob)
	mux.HandleFunc("POST /api/v1/players/{id}/buy", s.buy)
	mux.HandleFunc("POST /api/v1/players/{id}/use", s.use)
	mux.HandleFunc("POST /api/v1/players/{id}/bank/deposit", s.deposit)
	mux.HandleFunc("POST /api/v1/players/{id}/bank/withdraw", s.withdraw)
	mux.HandleFunc("GET /api/v1/players/{id}/bank/statement", s.bankStatement)
	mux.HandleFunc("POST /api/v1/players/{id}/bank/transfer", s.bankTransfer)
	mux.HandleFunc("POST /api/v1/players/{id}/bank/super/deposit", s.superDeposit)
	mux.HandleFunc("POST /api/v1/players/{id}/bank/super/cancel", s.superCancel)
	mux.HandleFunc("POST /api/v1/players/{id}/casino/{game}/play", s.casinoPlay)
	mux.HandleFunc("GET /api/v1/players/{id}/scratch/{game}", s.scratchState)
	mux.HandleFunc("POST /api/v1/players/{id}/scratch/{game}/open", s.scratchOpen)
	mux.HandleFunc("GET /api/v1/players/{id}/blackjack", s.bjState)
	mux.HandleFunc("POST /api/v1/players/{id}/blackjack/start", s.bjStart)
	mux.HandleFunc("POST /api/v1/players/{id}/blackjack/hit", s.bjHit)
	mux.HandleFunc("POST /api/v1/players/{id}/blackjack/stand", s.bjStand)
	mux.HandleFunc("GET /api/v1/players/{id}/poker", s.pokerState)
	mux.HandleFunc("POST /api/v1/players/{id}/poker/buy", s.pokerBuy)
	mux.HandleFunc("POST /api/v1/players/{id}/poker/deal", s.pokerDeal)
	mux.HandleFunc("POST /api/v1/players/{id}/poker/draw", s.pokerDraw)
	mux.HandleFunc("POST /api/v1/players/{id}/poker/cashout", s.pokerCashout)
	mux.HandleFunc("GET /api/v1/players/{id}/loto6", s.loto6State)
	mux.HandleFunc("POST /api/v1/players/{id}/loto6/buy", s.loto6Buy)
	mux.HandleFunc("GET /api/v1/players/{id}/bank/loan/quote", s.loanQuote)
	mux.HandleFunc("POST /api/v1/players/{id}/bank/loan/borrow", s.loanBorrow)
	mux.HandleFunc("POST /api/v1/players/{id}/bank/loan/repay", s.loanRepay)

	// 管理者API(暫定認可: X-Acting-Player-Idヘッダのadminロール。将来MiAuthで置換)
	mux.HandleFunc("POST /api/v1/admin/items", s.createItem)
	mux.HandleFunc("GET /api/v1/admin/items", s.listItems)
	mux.HandleFunc("PUT /api/v1/admin/items/{id}", s.updateItem)
	mux.HandleFunc("DELETE /api/v1/admin/items/{id}", s.deleteItem)
	mux.HandleFunc("POST /api/v1/admin/jobs", s.createJob)
	mux.HandleFunc("GET /api/v1/admin/jobs", s.listJobs)
	mux.HandleFunc("PUT /api/v1/admin/jobs/{id}", s.updateJob)
	mux.HandleFunc("DELETE /api/v1/admin/jobs/{id}", s.deleteJob)
	mux.HandleFunc("POST /api/v1/admin/simulate", s.simulate)
	mux.HandleFunc("GET /api/v1/admin/settings", s.adminGetSettings)
	mux.HandleFunc("PUT /api/v1/admin/settings", s.adminUpdateSettings)
	mux.HandleFunc("PUT /api/v1/admin/townmap", s.adminUpdateTownMap)
	mux.HandleFunc("PUT /api/v1/admin/towns", s.adminUpdateTowns)
	mux.HandleFunc("GET /api/v1/admin/townmap/houses", s.adminHouseCells)
	mux.HandleFunc("GET /api/v1/admin/assets", s.adminListAssets)
	mux.HandleFunc("POST /api/v1/admin/assets", s.adminUploadAsset)
	mux.HandleFunc("DELETE /api/v1/admin/assets/{name}", s.adminDeleteAsset)
	mux.HandleFunc("PUT /api/v1/admin/townassets", s.adminUpdateTownAssets)
	mux.HandleFunc("GET /api/v1/admin/townmap/presets", s.adminFacilityPresets)
	mux.HandleFunc("PUT /api/v1/admin/townmap/presets", s.adminUpdateFacilityPresets)
	mux.HandleFunc("GET /api/v1/admin/events", s.adminListEvents)
	mux.HandleFunc("POST /api/v1/admin/events", s.adminCreateEvent)
	mux.HandleFunc("PUT /api/v1/admin/events/{eid}", s.adminUpdateEvent)
	mux.HandleFunc("DELETE /api/v1/admin/events/{eid}", s.adminDeleteEvent)
	mux.HandleFunc("GET /api/v1/admin/players", s.adminListPlayers)
	mux.HandleFunc("PUT /api/v1/admin/players/{id}", s.adminUpdatePlayer)
	mux.HandleFunc("DELETE /api/v1/admin/players/{id}", s.adminDeletePlayer)
	return recoverer(mux)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// recoverer converts panics into 500 responses instead of dropping the connection.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
