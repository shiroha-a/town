// Package httpapi exposes the REST API under /api/v1.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/attendance"
	"github.com/shiroha-a/town/internal/cleague"
	"github.com/shiroha-a/town/internal/content"
	"github.com/shiroha-a/town/internal/emoji"
	"github.com/shiroha-a/town/internal/greeting"
	"github.com/shiroha-a/town/internal/keiba"
	"github.com/shiroha-a/town/internal/mail"
	"github.com/shiroha-a/town/internal/miauth"
	"github.com/shiroha-a/town/internal/news"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/profile"
	"github.com/shiroha-a/town/internal/ranking"
	"github.com/shiroha-a/town/internal/serial"
	"github.com/shiroha-a/town/internal/session"
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
	serial     *serial.Service
	greetHub   *greetHub // あいさつSSE配信のプロセス内ハブ

	// MiAuth(ログイン)まわり。
	pool           *pgxpool.Pool
	miauth         *miauth.Client
	instanceRules  *miauth.Rules
	sessions       *session.Store
	profiles       *profile.Service
	emojis         *emoji.Service
	appName        string
	allowedOrigins []string
	limiter        *limiter
}

// instancePolicy reads the current instance policy from settings.
func (s *Server) instancePolicy(_ *http.Request) miauth.Policy {
	if s.settings.Get().InstancePolicy == string(miauth.Whitelist) {
		return miauth.Whitelist
	}
	return miauth.Blacklist
}

// AuthDeps bundles the MiAuth/Misskey dependencies so NewServer's signature
// does not grow another six positional arguments.
type AuthDeps struct {
	Pool          *pgxpool.Pool
	MiAuth        *miauth.Client
	InstanceRules *miauth.Rules
	Sessions      *session.Store
	Profiles      *profile.Service
	Emojis        *emoji.Service
	// WebDir はビルド済みフロントエンドの置き場。空なら配信しない
	// (開発でViteの開発サーバを使う場合)。
	WebDir         string
	AppName        string
	AllowedOrigins []string
}

// NewServer builds the HTTP handler for the REST API.
func NewServer(players *player.Service, actions *action.Service, contentSvc *content.Service, st *settings.Store, tmap *townmap.Store, stockSvc *stock.Service, keibaSvc *keiba.Service, mailSvc *mail.Service, greetingSvc *greeting.Service, attendanceSvc *attendance.Service, cleagueSvc *cleague.Service, newsSvc *news.Service, rankingSvc *ranking.Service, serialSvc *serial.Service, auth AuthDeps) http.Handler {
	s := &Server{players: players, actions: actions, content: contentSvc, settings: st, townmap: tmap, stock: stockSvc, keiba: keibaSvc, mail: mailSvc, greeting: greetingSvc, attendance: attendanceSvc, cleague: cleagueSvc, news: newsSvc, ranking: rankingSvc, serial: serialSvc, greetHub: newGreetHub(),
		pool: auth.Pool, miauth: auth.MiAuth, instanceRules: auth.InstanceRules,
		sessions: auth.Sessions, profiles: auth.Profiles, emojis: auth.Emojis,
		appName: auth.AppName, allowedOrigins: auth.AllowedOrigins, limiter: newLimiter()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/site", s.site)
	mux.HandleFunc("GET /api/v1/players", s.listPlayers)
	mux.HandleFunc("GET /api/v1/players/{id}", s.getPlayer)
	mux.HandleFunc("GET /api/v1/participants", s.participants)
	mux.HandleFunc("GET /api/v1/players/{id}/profile", s.playerProfile)
	mux.HandleFunc("GET /api/v1/players/{id}/fishing", s.fishing)
	mux.HandleFunc("POST /api/v1/players/{id}/fishing/start", s.fishingStart)
	mux.HandleFunc("POST /api/v1/players/{id}/fishing/pick", s.fishingPick)
	mux.HandleFunc("GET /api/v1/admin/instances", s.adminListInstanceRules)
	mux.HandleFunc("PUT /api/v1/admin/instances", s.adminPutInstanceRule)
	mux.HandleFunc("DELETE /api/v1/admin/instances/{host}", s.adminDeleteInstanceRule)
	mux.HandleFunc("POST /api/v1/auth/start", s.authStart)
	mux.HandleFunc("POST /api/v1/auth/callback", s.authCallback)
	mux.HandleFunc("GET /api/v1/auth/me", s.authMe)
	mux.HandleFunc("POST /api/v1/auth/logout", s.authLogout)
	mux.HandleFunc("POST /api/v1/auth/guest", s.authGuest)
	mux.HandleFunc("GET /api/v1/players/{id}/bingo", s.bingo)
	mux.HandleFunc("POST /api/v1/players/{id}/bingo/card", s.bingoTakeCard)
	mux.HandleFunc("POST /api/v1/players/{id}/bingo/claim", s.bingoClaim)
	mux.HandleFunc("POST /api/v1/admin/bingo", s.adminStartBingo)
	mux.HandleFunc("POST /api/v1/players/{id}/serial/redeem", s.redeemSerial)
	mux.HandleFunc("GET /api/v1/admin/serials", s.adminListSerials)
	mux.HandleFunc("POST /api/v1/admin/serials", s.adminCreateSerial)
	mux.HandleFunc("PUT /api/v1/admin/serials/{sid}", s.adminUpdateSerial)
	mux.HandleFunc("DELETE /api/v1/admin/serials/{sid}", s.adminDeleteSerial)
	mux.HandleFunc("GET /api/v1/admin/serials/{sid}/uses", s.adminSerialUses)
	mux.HandleFunc("GET /api/v1/players/{id}/gifts", s.giftShop)
	mux.HandleFunc("POST /api/v1/players/{id}/gifts/convert", s.giftConvert)
	mux.HandleFunc("GET /api/v1/news", s.townNews)
	mux.HandleFunc("GET /api/v1/players/{id}/news", s.playerNews)
	mux.HandleFunc("GET /api/v1/ranking", s.townRanking)
	mux.HandleFunc("GET /api/v1/ranking/keys", s.rankingKeys)
	mux.HandleFunc("GET /api/v1/townmap", s.townMap)
	mux.HandleFunc("GET /api/v1/houses", s.publicHouses)
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

	// Misskey連携(prof施設)。プロフィール参照は対象の住民、フォローは
	// セッションの本人が実行者なのでパスにIDを取らない。
	mux.HandleFunc("GET /api/v1/players/{id}/misskey", s.misskeyProfile)
	mux.HandleFunc("POST /api/v1/players/{id}/misskey/refresh", s.refreshMisskeyProfile)
	mux.HandleFunc("GET /api/v1/players/{id}/settings", s.userSettings)
	mux.HandleFunc("PUT /api/v1/players/{id}/settings", s.updateUserSettings)
	mux.HandleFunc("POST /api/v1/players/{id}/retire", s.retire)
	mux.HandleFunc("POST /api/v1/misskey/follow", s.misskeyFollow)
	mux.HandleFunc("POST /api/v1/misskey/unfollow", s.misskeyUnfollow)

	// カスタム絵文字。使われた絵文字の辞書は投稿の描画に要るので公開GET。
	mux.HandleFunc("GET /api/v1/emojis", s.emojiList)
	mux.HandleFunc("POST /api/v1/emojis/resolve", s.emojiResolve)
	mux.HandleFunc("GET /api/v1/emojis/used", s.emojiUsed)

	// 管理者API(認可はauthGuardで一括: セッション + adminロール)
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
	api := recoverer(securityHeaders(s.authGuard(mux)))
	if auth.WebDir == "" {
		return api
	}
	// 画面もこのプロセスから配る(オリジンを1つにする)。
	// PWAのマニフェストはゲーム名が管理画面から変わるので動的に返す。spaHandlerは
	// /api/以外を静的ファイル扱いにしてしまうため、その手前で受ける。
	// 認証は要らない(マニフェストはcookie無しで取得される)。
	web := http.NewServeMux()
	web.HandleFunc("GET /manifest.webmanifest", s.manifest)
	web.Handle("/", spaHandler(auth.WebDir, api))
	return web
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeInternal reports an unexpected failure. 詳細はログにだけ残し、応答は
// 固定の文言にする(DBのエラー文には表や列の名前が入るため、外に出さない)。
func writeInternal(w http.ResponseWriter, r *http.Request, err error) {
	// r は共通ヘルパから呼ぶときに nil のことがある。
	path, method := "", ""
	if r != nil {
		path, method = r.URL.Path, r.Method
	}
	slog.Error("internal error", "path", path, "method", method, "err", err)
	writeError(w, http.StatusInternalServerError, "処理に失敗しました。時間をおいてお試しください。")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// site returns the display name of this town. ログイン前の入口でも使うため公開。
func (s *Server) site(w http.ResponseWriter, _ *http.Request) {
	g := s.settings.Get()
	writeJSON(w, http.StatusOK, map[string]string{
		"title":   g.SiteTitle,
		"tagline": g.SiteTagline,
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// securityHeaders sets the response headers that cost nothing and close off
// whole classes of mistakes:
//
//   - nosniff: アップロード画像を配信する経路があるので、ブラウザに中身を
//     推測させない(宣言したMIMEとして扱わせる)
//   - frame-options / CSP frame-ancestors: 他サイトに埋め込ませない
//     (クリックジャッキング対策)
//   - Referrer-Policy: 外部リンク(Misskeyのプロフィール等)へURLを漏らさない
//
// APIの応答は常にJSONか画像なので、default-src 'none' まで絞れる。
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
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
