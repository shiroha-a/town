package httpapi

import "net/http"

// リクエストボディの上限。JSONのAPIは数KBで足りるが、上限が無いと
// json.Decode が際限なく読んでしまう。未ログインでも叩ける経路(MiAuthの
// コールバック等)に巨大なJSONを流し込むだけでメモリを食えるため、経路が
// 決まった時点で頭を抑える。
const (
	defaultBodyLimit = 64 << 10 // 64KiB。JSONの入力はどれもこれに収まる
	// 画像アップロードだけは別枠。1MBの画像がbase64で約1.37倍に膨らみ、
	// さらにJSONの殻が乗る。
	uploadBodyLimit = 4 << 20
	// 街マップ・背景・施設プリセット・街一覧・ゲーム設定は、画面全体ぶんを
	// 1回で送る。実測で背景が25KB程度なので、伸びしろを見て1MiB。
	bulkBodyLimit = 1 << 20
)

var bodyLimits = map[string]int64{
	"POST /api/v1/admin/assets":         uploadBodyLimit,
	"PUT /api/v1/admin/townmap":         bulkBodyLimit,
	"PUT /api/v1/admin/townassets":      bulkBodyLimit,
	"PUT /api/v1/admin/townmap/presets": bulkBodyLimit,
	"PUT /api/v1/admin/towns":           bulkBodyLimit,
	"PUT /api/v1/admin/settings":        bulkBodyLimit,
}

// limitBody caps the request body for the matched route. 上限を超えていたら
// 413 を返して true。
//
// 二段構えにしている: Content-Length が申告されていれば読む前に切り、
// chunked で長さを名乗らない場合は MaxBytesReader が読み取りの途中で止める
// (このときは各ハンドラの json.Decode が失敗して400になる)。
func limitBody(w http.ResponseWriter, r *http.Request, pattern string) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return false
	}
	limit := int64(defaultBodyLimit)
	if v, ok := bodyLimits[pattern]; ok {
		limit = v
	}
	if r.ContentLength > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "送信されたデータが大きすぎます。")
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	return false
}
