package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

// manifest serves the web app manifest. ホーム画面やデスクトップへ入れたときの
// 名前・アイコン・起動時の見た目を伝える。ゲーム名は管理画面から変えられる
// (settings.SiteTitle)ので、静的ファイルではなくここで組み立てる。
func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	cfg := s.settings.Get()
	name := strings.TrimSpace(cfg.SiteTitle)
	if name == "" {
		name = "TOWN"
	}
	desc := strings.TrimSpace(cfg.SiteTagline)
	if desc == "" {
		desc = name
	}
	// short_name はホーム画面のアイコン下に出る短い名前。長いと省略されるので詰める。
	short := []rune(name)
	if len(short) > 12 {
		short = short[:12]
	}

	m := map[string]any{
		"name":       name,
		"short_name": string(short),
		// description は特にデスクトップのインストール画面に出る。
		"description": desc,
		"lang":        "ja",
		"dir":         "ltr",
		"start_url":   "/",
		"scope":       "/",
		// standalone: ブラウザのUIを外して独立したウィンドウ/画面で開く。
		"display": "standalone",
		// 画面の向きは固定しない(街マップは横でも縦でも読める)。
		"background_color": "#ffffff",
		// 起動画面とタイトルバーの色。街マップの空の色に合わせる。
		"theme_color": "#8fd4f0",
		"icons": []map[string]any{
			{"src": "/icons/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any"},
			{"src": "/icons/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any"},
			// maskable はOSが好きな形に切り抜く用(Androidの丸/角丸アイコン)。
			{"src": "/icons/icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"},
		},
		// アイコンを長押し/右クリックしたときに出る行き先。よく開くものだけを
		// 4つに絞る(OSによっては先頭3〜4件しか出ない)。
		"shortcuts": []map[string]any{
			{"name": "持ち物", "url": "/item",
				"icons": []map[string]any{{"src": "/icons/icon-192.png", "sizes": "192x192"}}},
			{"name": "銀行", "url": "/bank",
				"icons": []map[string]any{{"src": "/icons/icon-192.png", "sizes": "192x192"}}},
			{"name": "役場(住民名鑑)", "url": "/yakuba",
				"icons": []map[string]any{{"src": "/icons/icon-192.png", "sizes": "192x192"}}},
			{"name": "メール", "url": "/mail",
				"icons": []map[string]any{{"src": "/icons/icon-192.png", "sizes": "192x192"}}},
		},
		// スクリーンショットがあると、特にデスクトップのインストール画面が
		// 名前だけの素っ気ないものから、絵の付いたものに変わる。
		"screenshots": []map[string]any{
			{"src": "/icons/screenshot-wide.png", "sizes": "1280x800", "type": "image/png",
				"form_factor": "wide", "label": "街のようす"},
			{"src": "/icons/screenshot-narrow.png", "sizes": "390x844", "type": "image/png",
				"form_factor": "narrow", "label": "街のようす"},
		},
	}
	// 名前を変えたらすぐ反映されるよう、長くは持たせない。
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// writeJSON は application/json を立ててしまうので、ここは自前で書く
	// (マニフェストの正しい型は application/manifest+json)。
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(m)
}
