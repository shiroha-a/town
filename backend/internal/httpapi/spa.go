package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// spaHandler serves the built frontend next to the API from a single origin.
//
// 開発ではViteの開発サーバが画面を出し /api をこちらへ中継するが、公開環境で
// それを使い続けると、開発用サーバをそのまま外に晒すことになり、経路も
// (Tunnel → Vite → API)と一段増える。ビルド済みの静的ファイルをAPIと同じ
// プロセスから配ることで、オリジンが1つになり中継も無くなる。
//
// SPAなので、存在しないパスは index.html を返す(MiAuthのコールバックが
// /auth/callback?session=... に戻ってくるため、404にしてはいけない)。
func spaHandler(dir string, api http.Handler) http.Handler {
	files := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		setSPAHeaders(w)

		// 実ファイルがあればそれを返す。ハッシュ付きのアセットは長くキャッシュ
		// してよいが、index.html は毎回確認させる(更新が届かなくなるため)。
		clean := filepath.Clean(r.URL.Path)
		if p := filepath.Join(dir, clean); clean != "/" && fileExists(p) {
			if strings.HasPrefix(clean, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// setSPAHeaders is the画面向けのヘッダ. APIと違い、自分のJS/CSSと外部の画像
// (Misskeyのアイコン・カスタム絵文字)を許す必要がある。
func setSPAHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"img-src 'self' https: data:",
		"style-src 'self' 'unsafe-inline'",
		"script-src 'self'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'none'",
	}, "; "))
}
