package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/shiroha-a/town/internal/session"
)

type ctxKey int

const ctxPlayerID ctxKey = iota

// PlayerIDFrom returns the logged-in player's id, or 0 when not logged in.
func PlayerIDFrom(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxPlayerID).(int64)
	return id
}

// playerRoutePrefix is the path segment that identifies a per-player route.
const playerRoutePrefix = "/api/v1/players/"

// publicPlayerPatterns are the /players/{id}/... routes that are public reads
// *about another resident* (住民名鑑・役場の出来事)。The rest of the per-player
// routes expose or mutate that player's own data and require ownership.
var publicPlayerPatterns = map[string]bool{
	"GET /api/v1/players/{id}/profile": true,
	"GET /api/v1/players/{id}/news":    true,
}

// residentViewPatterns are reads *about another resident* that any logged-in
// player may make (prof画面のMisskeyプロフィール)。誰でも見られる公開GETとは
// 分けている: 表示のたびに相手インスタンスへ問い合わせが飛ぶため。
var residentViewPatterns = map[string]bool{
	"GET /api/v1/players/{id}/misskey": true,
}

// loginRequiredPatterns need a session but are not scoped to a player id — the
// acting player always comes from the session.
var loginRequiredPatterns = map[string]bool{
	"POST /api/v1/misskey/follow":   true,
	"POST /api/v1/misskey/unfollow": true,
	// 絵文字ピッカーと、その選択の検証。相手インスタンスへ問い合わせが飛ぶので
	// 未ログインには開けない(描画用の辞書 /emojis/used は公開GET)。
	"GET /api/v1/emojis":          true,
	"POST /api/v1/emojis/resolve": true,
}

// guestBlocked reports whether a お試しプレイ(ゲスト)には使わせない経路か。
// ゲストは1時間で消える使い捨てなので、次の3つに当たる操作を止める:
//
//   - 街に痕跡が残るもの(建築・掲示板・あいさつ・出席)
//   - 他人へ価値を渡せるもの(振込・メール・ギフト・闇市・会社)。ゲストは
//     いくらでも作れるため、ここを開けると無限の蛇口になる
//   - ゲストには意味がないもの(銀行・ローン・Misskey連携・シリアルコード)
//
// 制限はサーバー側で掛ける。画面を隠すだけでは意味がないため。
func guestBlocked(pattern string) bool {
	switch {
	case strings.Contains(pattern, "/bank/"),
		strings.Contains(pattern, "/building/"),
		strings.Contains(pattern, "/mail"),
		strings.Contains(pattern, "/greetings"),
		strings.Contains(pattern, "/gifts"),
		strings.Contains(pattern, "/serial/"),
		strings.Contains(pattern, "/misskey"),
		strings.Contains(pattern, "/attendance/"):
		return true
	}
	return false
}

// pathPlayerID pulls the {id} out of /api/v1/players/{id}/... .
func pathPlayerID(path string) (int64, bool) {
	rest, ok := strings.CutPrefix(path, playerRoutePrefix)
	if !ok {
		return 0, false
	}
	seg := rest
	if i := strings.IndexByte(seg, '/'); i >= 0 {
		seg = seg[:i]
	}
	id, err := strconv.ParseInt(seg, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// authGuard resolves the session cookie and enforces, in one place, the two
// rules that used to be missing entirely:
//
//   - /api/v1/players/{id}/... may only be used by that player
//   - /api/v1/admin/... requires the admin role
//
// Everything else (town map, roster, rankings, news, login) stays public so the
// game is browsable before logging in.
func (s *Server) authGuard(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 経路ごとの方針は「マッチしたパターン」で決める。ハンドラ134本を
		// 個別に直さずに済ませるため。
		_, pattern := mux.Handler(r)

		// 認証まわりの経路は未ログインでも叩けるので、認可より先に頭を抑える。
		if s.rateLimited(w, r, pattern) {
			return
		}

		token := session.TokenFrom(r)
		playerID, slid, err := s.sessions.Lookup(r.Context(), token)
		if err != nil {
			playerID = 0
		}
		// 有効期限が延びたらcookieも貼り直す。DBの行だけ延ばしてもブラウザは
		// 発行時の期限で捨てるので、遊び続けても再ログインが要るままになる。
		// Lookup側でtouchIntervalに間引かれているので毎回は出ない。
		if slid {
			s.sessions.SetCookie(w, token)
		}
		r = r.WithContext(context.WithValue(r.Context(), ctxPlayerID, playerID))

		switch {
		case strings.Contains(pattern, "/api/v1/admin/"):
			if playerID == 0 {
				writeError(w, http.StatusUnauthorized, "ログインしてください。")
				return
			}
			ok, err := s.players.HasRole(r.Context(), playerID, "admin")
			if err != nil {
				writeInternal(w, r, err)
				return
			}
			if !ok {
				writeError(w, http.StatusForbidden, "管理者権限が必要です。")
				return
			}

		case publicPlayerPatterns[pattern]:
			// 他の住民について見るための公開GET。認証不要。

		case residentViewPatterns[pattern], loginRequiredPatterns[pattern]:
			// ログインは要るが、対象は自分でなくてよい経路。
			if playerID == 0 {
				writeError(w, http.StatusUnauthorized, "ログインしてください。")
				return
			}
			if guestBlocked(pattern) {
				guest, err := s.players.IsGuest(r.Context(), playerID)
				if err != nil {
					writeInternal(w, r, err)
					return
				}
				if guest {
					writeError(w, http.StatusForbidden,
						"お試しプレイでは使えません。Misskeyアカウントでログインすると使えます。")
					return
				}
			}

		case strings.Contains(pattern, playerRoutePrefix+"{id}"):
			if playerID == 0 {
				writeError(w, http.StatusUnauthorized, "ログインしてください。")
				return
			}
			if guestBlocked(pattern) {
				guest, err := s.players.IsGuest(r.Context(), playerID)
				if err != nil {
					writeInternal(w, r, err)
					return
				}
				if guest {
					writeError(w, http.StatusForbidden,
						"お試しプレイでは使えません。Misskeyアカウントでログインすると使えます。")
					return
				}
			}
			target, ok := pathPlayerID(r.URL.Path)
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid id")
				return
			}
			if target != playerID {
				// 管理者は他プレイヤーの状態を「読む」ことだけ許す(管理画面の
				// プレイヤー編集が本人データを引くため)。書き込みは管理者でも
				// 通さない: 他人になりすました操作は台帳やクールタイムの記録が
				// 実際の行為者とずれるため、変更は /admin/players/{id} を使う。
				if r.Method != http.MethodGet {
					writeError(w, http.StatusForbidden, "他のプレイヤーの操作はできません。")
					return
				}
				isAdmin, err := s.players.HasRole(r.Context(), playerID, "admin")
				if err != nil {
					writeInternal(w, r, err)
					return
				}
				if !isAdmin {
					writeError(w, http.StatusForbidden, "他のプレイヤーの操作はできません。")
					return
				}
			}
		}

		mux.ServeHTTP(w, r)
	})
}
