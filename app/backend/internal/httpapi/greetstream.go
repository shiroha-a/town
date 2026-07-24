package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// greetHub fans out greeting-change signals to SSE subscribers. 投稿/削除は
// すべてこのwebプロセスを通るため、プロセス内ブロードキャストで足りる
// (web多重化する場合はPostgres LISTEN/NOTIFY等に置き換える)。
type greetHub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func newGreetHub() *greetHub {
	return &greetHub{subs: map[chan struct{}]struct{}{}}
}

// subscribe registers a listener channel and returns an unsubscribe func.
func (h *greetHub) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// notify signals every subscriber without blocking (合流分は1回にまとまる)。
func (h *greetHub) notify() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// greetingsStream streams the latest greetings over SSE: 接続時に最新一覧を
// 送り、以後は投稿/削除のたびに更新された一覧をpushする(街トップのチャット窓の
// リアルタイム表示用)。25秒ごとのコメント行はプロキシのタイムアウト対策。
func (s *Server) greetingsStream(w http.ResponseWriter, r *http.Request) {
	limit := 6
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 30 {
			limit = n
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func() bool {
		list, err := s.greeting.List(r.Context(), limit)
		if err != nil {
			return false
		}
		data, err := json.Marshal(list)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}
	ch, unsub := s.greetHub.subscribe()
	defer unsub()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if !send() {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
