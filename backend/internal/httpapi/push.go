package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/shiroha-a/town/internal/push"
)

// pushKey serves the VAPID public key. ブラウザが購読を作るのに要る。公開鍵なので
// ログイン不要で返してよい。
func (s *Server) pushKey(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeJSON(w, http.StatusOK, map[string]string{"key": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": s.push.PublicKey()})
}

type pushSubReq struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// pushSubscribe registers one device for the player.
func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if s.push == nil {
		writeError(w, http.StatusServiceUnavailable, "通知は利用できません。")
		return
	}
	var req pushSubReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.push.Subscribe(r.Context(), id, push.Subscription{
		Endpoint: req.Endpoint, P256dh: req.P256dh, Auth: req.Auth,
	}); err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": true})
}

// pushUnsubscribe forgets one device.
func (s *Server) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if _, ok := pathID(w, r); !ok {
		return
	}
	if s.push == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"subscribed": false})
		return
	}
	var req pushSubReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.push.Unsubscribe(r.Context(), req.Endpoint); err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": false})
}

// pushPrefsResp is what the settings screen shows.
type pushPrefsResp struct {
	push.Prefs
	// Subscribed はこの人のどれかの端末が登録済みか。全部オフでも登録は残る。
	Subscribed bool `json:"subscribed"`
}

// pushPrefs serves GET/PUT /players/{id}/push/prefs.
func (s *Server) pushPrefs(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if s.push == nil {
		writeJSON(w, http.StatusOK, pushPrefsResp{})
		return
	}
	if r.Method == http.MethodPut {
		var in push.Prefs
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := s.push.SetPrefs(r.Context(), id, in); err != nil {
			writeInternal(w, r, err)
			return
		}
	}
	p, err := s.push.GetPrefs(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	sub, err := s.push.HasSubscription(r.Context(), id)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pushPrefsResp{Prefs: p, Subscribed: sub})
}
