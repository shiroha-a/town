package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/shiroha-a/town/internal/miauth"
	"github.com/shiroha-a/town/internal/player"
)

// Misskeyアカウントの複数連携。仕様: .tmp/design_misskey_accounts.md
//
// 経路を /players/{id}/ の下に置いているのは、authGuard が「セッションの本人か」と
// 「お試しプレイでないか」を既に見ているため。ここで認可を書き直さずに済む。

// misskeyAccounts serves GET /players/{id}/misskey-accounts.
func (s *Server) misskeyAccounts(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	accounts, err := s.players.ListMisskeyAccounts(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

// misskeyAccountStart serves POST /players/{id}/misskey-accounts/start — 連携を
// 追加するための認可URLを発行する。ログイン用の /auth/start と違い、ログイン済みで
// あることが前提(誰に足すのかが決まっていないと始められない)。
func (s *Server) misskeyAccountStart(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req authStartReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.beginMiAuth(w, r, req, id)
}

// accountKey pulls the {host}/{user} pair out of the path. PathValue はすでに
// パーセントデコード済みなので、ここで解き直さない(二重にデコードしてしまう)。
func accountKey(w http.ResponseWriter, r *http.Request) (host, user string, ok bool) {
	host = r.PathValue("host")
	if host == "" {
		writeError(w, http.StatusBadRequest, "host is required")
		return "", "", false
	}
	user = r.PathValue("user")
	if user == "" {
		writeError(w, http.StatusBadRequest, "user is required")
		return "", "", false
	}
	// 保存時と同じ正規化を通す。画面から来た値が大文字でも取り違えない。
	if h, err := miauth.NormalizeHost(host); err == nil {
		host = h
	}
	return host, user, true
}

// setPrimaryMisskeyAccount serves POST /players/{id}/misskey-accounts/{host}/{user}/primary.
// 代表はプロフィールに出るアカウントであり、フォローの発信元インスタンスでもある。
func (s *Server) setPrimaryMisskeyAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	host, user, ok := accountKey(w, r)
	if !ok {
		return
	}
	err := s.players.SetPrimaryMisskeyAccount(r.Context(), id, host, user)
	if errors.Is(err, player.ErrAccountNotFound) {
		writeError(w, http.StatusNotFound, "そのアカウントは連携されていません。")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	// 新しい代表でプロフィールを取り込み直す。ここが失敗しても切り替え自体は
	// 成立させる: ログイン手段の付け替えを、相手インスタンスの不調で止めない。
	if s.profiles != nil {
		_, _ = s.profiles.Refresh(r.Context(), id)
	}
	accounts, err := s.players.ListMisskeyAccounts(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

// unlinkMisskeyAccount serves DELETE /players/{id}/misskey-accounts/{host}/{user}.
func (s *Server) unlinkMisskeyAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	host, user, ok := accountKey(w, r)
	if !ok {
		return
	}
	err := s.players.UnlinkMisskeyAccount(r.Context(), id, host, user)
	switch {
	case errors.Is(err, player.ErrAccountNotFound):
		writeError(w, http.StatusNotFound, "そのアカウントは連携されていません。")
		return
	case errors.Is(err, player.ErrLastAccount):
		writeError(w, http.StatusConflict,
			"最後の1つは外せません。外すとログインできなくなります。")
		return
	case errors.Is(err, player.ErrPrimaryAccount):
		writeError(w, http.StatusConflict,
			"プロフィールに出しているアカウントは外せません。先に別のアカウントへ切り替えてください。")
		return
	case err != nil:
		writeInternal(w, r, err)
		return
	}
	accounts, err := s.players.ListMisskeyAccounts(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}
