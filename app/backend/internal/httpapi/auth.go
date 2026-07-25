package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/shiroha-a/town/internal/miauth"
	"github.com/shiroha-a/town/internal/session"
)

// miauthPermissions are the scopes we ask the instance for.
// read:account はプロフィール表示と ap/show(リモートフォローの解決)に、
// write:following はゲーム内からのフォローに要る。
var miauthPermissions = []string{"read:account", "write:following"}

// callbackPath is where the SPA handles the return from the instance.
const callbackPath = "/auth/callback"

// resolveOrigin validates the SPA origin the browser reports and returns it.
// The callback URL is built from this, so it must not be attacker-controlled:
// otherwise the MiAuth session id would be delivered to their site and could be
// exchanged for the user's token. "all" は**テスト環境専用**の全許可。
func (s *Server) resolveOrigin(raw string) (string, error) {
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", errors.New("origin is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("origin が正しくありません。")
	}
	normalized := u.Scheme + "://" + u.Host
	allowed := s.allowedOrigins
	if slices.Contains(allowed, "all") {
		return normalized, nil
	}
	for _, a := range allowed {
		if strings.TrimSuffix(strings.TrimSpace(a), "/") == normalized {
			return normalized, nil
		}
	}
	// どのオリジンが弾かれたかを返す。呼び出し側が送った値なので秘密ではなく、
	// アクセス経路を増やしたときの原因究明がすぐできる。
	return "", fmt.Errorf("このオリジン(%s)からのログインは許可されていません。管理者に連絡してください。", normalized)
}

type authStartReq struct {
	Instance string `json:"instance"`
	Origin   string `json:"origin"` // ブラウザの window.location.origin
}

type authStartResp struct {
	URL          string `json:"url"`           // ここへブラウザを飛ばす
	Instance     string `json:"instance"`      // 正規化後のホスト
	InstanceName string `json:"instance_name"` // 確認のために表示する
}

// authStart validates the instance and returns the MiAuth approval URL.
func (s *Server) authStart(w http.ResponseWriter, r *http.Request) {
	var req authStartReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// すでにログインしている場合は認可をやり直さない。MiAuthはログインのたびに
	// インスタンス側へ新しいアクセストークンを作り、それを我々からは失効させられない
	// (i/revoke-tokenはsecure:trueでアクセストークンからは呼べない)。無用な
	// 再認証で連携アプリ一覧を増やさないためのガード。
	if id := PlayerIDFrom(r.Context()); id != 0 {
		writeError(w, http.StatusConflict, "すでにログインしています。")
		return
	}
	host, err := miauth.NormalizeHost(req.Instance)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	origin, err := s.resolveOrigin(req.Origin)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 参加できるインスタンスか(ブラックリスト/ホワイトリスト)。
	ok, err := s.instanceRules.Allowed(r.Context(), s.instancePolicy(r), host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "このインスタンスからは参加できません。")
		return
	}
	// 実在するMisskeyかを先に確かめる(打ち間違いをリダイレクト前に弾く)。
	meta, err := s.miauth.FetchMeta(r.Context(), host)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	sessionID, err := miauth.NewSessionID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.pool.Exec(r.Context(),
		`INSERT INTO auth_sessions (id, host) VALUES ($1, $2)`, sessionID, host); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, authStartResp{
		URL:          miauth.AuthURL(host, sessionID, s.appName, origin+callbackPath, miauthPermissions),
		Instance:     host,
		InstanceName: meta.Name,
	})
}

type authCallbackReq struct {
	Session string `json:"session"`
}

// authCallback exchanges an approved MiAuth session for a login.
func (s *Server) authCallback(w http.ResponseWriter, r *http.Request) {
	var req authCallbackReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Session == "" {
		writeError(w, http.StatusBadRequest, "session is required")
		return
	}
	// 保留レコードと突き合わせる。これをしないと、任意の session と host を
	// 載せたコールバックで別インスタンスへ問い合わせさせられる。
	var host string
	err := s.pool.QueryRow(r.Context(),
		`SELECT host FROM auth_sessions WHERE id = $1 AND created_at > now() - interval '1 hour'`,
		req.Session).Scan(&host)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "認証セッションが見つかりません。もう一度お試しください。")
		return
	}

	res, err := s.miauth.Check(r.Context(), host, req.Session)
	if err != nil {
		// 失敗しても保留レコードは残す(原因調査のため。1時間で自動的に消える)。
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	// 引き換えに成功した時点で使い切りにする(再利用を防ぐ)。
	if _, err := s.pool.Exec(r.Context(), `DELETE FROM auth_sessions WHERE id = $1`, req.Session); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	displayName := res.User.Name
	if strings.TrimSpace(displayName) == "" {
		displayName = res.User.Username
	}
	p, err := s.players.Register(r.Context(), host, res.User.ID, displayName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Misskeyのアクセストークンを保存(prof表示やフォローで使う)。
	if err := s.players.SetMisskeyToken(r.Context(), p.ID, res.Token); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	token, err := s.sessions.Issue(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.sessions.SetCookie(w, token)
	writeJSON(w, http.StatusOK, toResp(p))
}

// authMe returns the logged-in player, or 401.
func (s *Server) authMe(w http.ResponseWriter, r *http.Request) {
	id, err := s.sessions.Lookup(r.Context(), session.TokenFrom(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "ログインしていません。")
		return
	}
	p, err := s.players.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "ログインしていません。")
		return
	}
	writeJSON(w, http.StatusOK, toResp(p))
}

// authLogout clears the session.
func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	_ = s.sessions.Revoke(r.Context(), session.TokenFrom(r))
	s.sessions.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── 管理画面: インスタンスの許可/拒否リスト ─────────────────

// adminListInstanceRules returns the allow/block lists and the current policy.
func (s *Server) adminListInstanceRules(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	rules, err := s.instanceRules.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"policy": string(s.instancePolicy(r)),
		"rules":  rules,
	})
}

type instanceRuleReq struct {
	Host string `json:"host"`
	Kind string `json:"kind"` // block | allow
	Note string `json:"note"`
}

// adminPutInstanceRule adds or replaces one rule.
func (s *Server) adminPutInstanceRule(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req instanceRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.instanceRules.Put(r.Context(), req.Host, req.Kind, req.Note); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminDeleteInstanceRule removes one rule.
func (s *Server) adminDeleteInstanceRule(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	host := r.PathValue("host")
	if host == "" {
		writeError(w, http.StatusBadRequest, "host is required")
		return
	}
	if err := s.instanceRules.Delete(r.Context(), host); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
