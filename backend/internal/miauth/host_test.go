package miauth

import (
	"net"
	"testing"
)

func TestNormalizeHostAccepts(t *testing.T) {
	cases := map[string]string{
		"misskey.io":              "misskey.io",
		"  misskey.io  ":          "misskey.io",
		"https://misskey.io":      "misskey.io",
		"https://Misskey.IO/":     "misskey.io",
		"https://misskey.io/some": "misskey.io",
		"https://misskey.io/?a=1": "misskey.io",
		"sub.example.co.jp":       "sub.example.co.jp",
	}
	for in, want := range cases {
		got, err := NormalizeHost(in)
		if err != nil {
			t.Errorf("NormalizeHost(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeHostRejects(t *testing.T) {
	// 内部ホストやポート指定など、SSRFの足がかりになる入力を弾くこと。
	bad := []string{
		"", "   ",
		"http://misskey.io", // httpは不可
		"ftp://misskey.io",  // 未知のスキーム
		"misskey.io:3000",   // ポート指定
		"localhost",         // ドットなし
		"127.0.0.1",         // IPリテラル
		"192.168.1.1",       //
		"[::1]",             // IPv6リテラル
		"user@misskey.io",   // userinfo
		"https://user:pw@misskey.io",
		".misskey.io", "misskey.io.",
		"missk ey.io",   // 空白
		"misskey_io.jp", // 使えない文字
	}
	for _, in := range bad {
		if got, err := NormalizeHost(in); err == nil {
			t.Errorf("NormalizeHost(%q) = %q, want error", in, got)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	public := []string{"1.1.1.1", "8.8.8.8", "203.0.113.10", "2606:4700:4700::1111"}
	for _, s := range public {
		if !IsPublicIP(net.ParseIP(s)) {
			t.Errorf("%s should be public", s)
		}
	}
	private := []string{
		"127.0.0.1", "::1", // loopback
		"10.0.0.1", "172.16.0.1", "192.168.1.1", // private v4
		"fd00::1",         // unique local
		"169.254.169.254", // link-local (クラウドのメタデータ)
		"fe80::1",
		"100.64.0.1", "100.127.255.255", // CGNAT/Tailscale
		"0.0.0.0", "240.0.0.1", // 予約
		"224.0.0.1", // multicast
	}
	for _, s := range private {
		if IsPublicIP(net.ParseIP(s)) {
			t.Errorf("%s should NOT be public", s)
		}
	}
	if IsPublicIP(nil) {
		t.Error("nil should not be public")
	}
}

func TestAuthURL(t *testing.T) {
	got := AuthURL("misskey.io", "abc-123", "TOWN", "https://town.example/auth/callback",
		[]string{"read:account", "write:following"})
	want := "https://misskey.io/miauth/abc-123?" +
		"callback=https%3A%2F%2Ftown.example%2Fauth%2Fcallback" +
		"&name=TOWN" +
		"&permission=read%3Aaccount%2Cwrite%3Afollowing"
	if got != want {
		t.Errorf("AuthURL =\n %q\nwant\n %q", got, want)
	}
}
