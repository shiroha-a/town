package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 前段が付けるヘッダは、接続元が信頼できる前段のときだけ採る。ここを常に
// 信じると、CF-Connecting-IP を書き換えるだけでIP単位の制限を素通りできる。
func TestClientIPTrustsHeadersOnlyFromProxy(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		header     string
		want       string
	}{
		{
			name:       "Tunnelから(ループバック)はヘッダを採る",
			remoteAddr: "127.0.0.1:40000",
			header:     "203.0.113.9",
			want:       "203.0.113.9",
		},
		{
			name:       "Dockerのブリッジ越しでもヘッダを採る",
			remoteAddr: "172.25.0.1:40000",
			header:     "203.0.113.9",
			want:       "203.0.113.9",
		},
		{
			name:       "外から直に来た接続のヘッダは無視する",
			remoteAddr: "198.51.100.7:40000",
			header:     "203.0.113.9",
			want:       "198.51.100.7",
		},
		{
			name:       "ヘッダが無ければ接続元をそのまま使う",
			remoteAddr: "127.0.0.1:40000",
			want:       "127.0.0.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.header != "" {
				r.Header.Set("CF-Connecting-IP", tt.header)
			}
			if got := clientIP(r); got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	// 既定はループバックとプライベート。
	def := loadTrustedProxies("")
	if len(def) == 0 {
		t.Fatal("既定の信頼範囲が空")
	}
	// 明示した場合はそれだけになる。書き間違いは落として狭い側に倒す。
	got := loadTrustedProxies("10.1.0.0/16, これはCIDRではない ,192.0.2.0/24")
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (書き間違いは無視される)", len(got))
	}
}
