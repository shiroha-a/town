package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 認証まわりの経路は未ログインでも叩けるうえ、1回ごとにDBへ行を作ったり
// 外部インスタンスへリクエストを飛ばしたりする。制限が無いと、
//   - ゲストを量産してDBを膨らませる
//   - このサーバーを踏み台にして任意の公開ホストを叩かせる
//
// ができてしまうので、IP単位と全体の二段で頭を抑える。
//
// IP単位だけでは、送信元を詐称・分散されると効かない。全体の上限は
// 「他所に迷惑をかける量」を確実に頭打ちにするために置いている。
type rateRule struct {
	perIP   int // 1ウィンドウあたりの同一IPからの上限
	global  int // 1ウィンドウあたりの全体上限(0=無制限)
	window  time.Duration
	message string
}

var rateRules = map[string]rateRule{
	// ゲスト作成は住民の行が増える。普通に試すぶんには数回で足りる。
	"POST /api/v1/auth/guest": {
		perIP: 5, global: 60, window: 10 * time.Minute,
		message: "お試しプレイの作成が多すぎます。しばらく待ってからお試しください。",
	},
	// ログイン開始は指定されたインスタンスへこちらから接続しに行く。
	"POST /api/v1/auth/start": {
		perIP: 10, global: 60, window: time.Minute,
		message: "ログインの試行が多すぎます。少し待ってからお試しください。",
	},
	// プロフィールの再取得も相手インスタンスへ問い合わせる。
	"POST /api/v1/players/{id}/misskey/refresh": {
		perIP: 10, global: 60, window: time.Minute,
		message: "更新が多すぎます。少し待ってからお試しください。",
	},
	// コールバックの引き換えも外部への問い合わせを伴う。
	"POST /api/v1/auth/callback": {
		perIP: 20, global: 120, window: time.Minute,
		message: "ログインの試行が多すぎます。少し待ってからお試しください。",
	},
}

// limiter is a fixed-window counter. プロセス内だけで完結させている(web は1台
// なので十分。増やすときはRedisへ移す)。
type limiter struct {
	mu      sync.Mutex
	windows map[string]*counter
}

type counter struct {
	count int
	until time.Time
}

func newLimiter() *limiter {
	l := &limiter{windows: map[string]*counter{}}
	go l.sweep()
	return l
}

// allow counts one hit and reports whether it is within the limit.
func (l *limiter) allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.windows[key]
	if !ok || now.After(c.until) {
		l.windows[key] = &counter{count: 1, until: now.Add(window)}
		return true
	}
	c.count++
	return c.count <= limit
}

// sweep drops finished windows so the map does not grow without bound.
func (l *limiter) sweep() {
	for range time.Tick(5 * time.Minute) {
		now := time.Now()
		l.mu.Lock()
		for k, c := range l.windows {
			if now.After(c.until) {
				delete(l.windows, k)
			}
		}
		l.mu.Unlock()
	}
}

// clientIP resolves the caller's address. リバースプロキシ(Cloudflare Tunnel →
// Vite)の裏に居るため、RemoteAddr はプロキシのアドレスになる。前段が付ける
// ヘッダを見る。
//
// ヘッダは詐称できるので、これだけを頼りにはしない(全体上限を併用する)。
func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		// 先頭が元のクライアント。以降は経由したプロキシ。
		if i := strings.IndexByte(v, ','); i >= 0 {
			v = v[:i]
		}
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimited applies the rule for the matched route, if any. 制限に掛かったら
// 429 を返して true を返す。
func (s *Server) rateLimited(w http.ResponseWriter, r *http.Request, pattern string) bool {
	rule, ok := rateRules[pattern]
	if !ok {
		return false
	}
	if !s.limiter.allow(pattern+"|"+clientIP(r), rule.perIP, rule.window) {
		writeError(w, http.StatusTooManyRequests, rule.message)
		return true
	}
	if !s.limiter.allow(pattern+"|*", rule.global, rule.window) {
		writeError(w, http.StatusTooManyRequests, rule.message)
		return true
	}
	return false
}
