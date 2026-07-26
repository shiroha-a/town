// Package miauth implements Misskey's MiAuth login flow: the user names their
// own instance, we send them there to approve, and the callback is exchanged
// for an access token. Because the target host is user-supplied, every outbound
// request goes through an SSRF-guarded HTTP client (see client.go).
package miauth

import (
	"fmt"
	"net"
	"strings"
)

// NormalizeHost turns user input into a bare lower-case host name.
// "https://Misskey.io/", " misskey.io " and "misskey.io" all become "misskey.io".
// It rejects anything that could point somewhere other than a public https host:
// explicit ports, userinfo, paths, IP literals.
func NormalizeHost(input string) (string, error) {
	h := strings.TrimSpace(input)
	if h == "" {
		return "", fmt.Errorf("インスタンスを入力してください。")
	}
	// スキームは https のみ許す(付いていなければ補う)。
	switch {
	case strings.HasPrefix(strings.ToLower(h), "https://"):
		h = h[len("https://"):]
	case strings.HasPrefix(strings.ToLower(h), "http://"):
		return "", fmt.Errorf("httpsのインスタンスのみ利用できます。")
	case strings.Contains(h, "://"):
		return "", fmt.Errorf("インスタンスの指定が正しくありません。")
	}
	// パス・クエリ・フラグメントは落とす(ホストだけ見る)。
	h = strings.TrimSuffix(h, "/")
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if strings.Contains(h, "@") {
		return "", fmt.Errorf("インスタンスの指定が正しくありません。")
	}
	if strings.Contains(h, ":") {
		// ポート指定も IPv6 リテラルもここで弾く。
		return "", fmt.Errorf("ポート付きのインスタンスは利用できません。")
	}
	h = strings.ToLower(h)
	if h == "" {
		return "", fmt.Errorf("インスタンスを入力してください。")
	}
	// IPアドレス直指定は拒否(内部アドレスへの誘導を防ぐ)。
	if net.ParseIP(h) != nil {
		return "", fmt.Errorf("IPアドレスでは指定できません。")
	}
	// ドメインらしさの最低限のチェック。
	if !strings.Contains(h, ".") || strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") {
		return "", fmt.Errorf("インスタンスの指定が正しくありません。")
	}
	for _, r := range h {
		if !(r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return "", fmt.Errorf("インスタンスの指定が正しくありません。")
		}
	}
	return h, nil
}

// IsPublicIP reports whether an address is safe to connect to: it rejects
// loopback, private, link-local, unique-local and other non-public ranges so a
// user-supplied host cannot be pointed at our own network.
func IsPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		ip.IsUnspecified() {
		return false
	}
	// IPv4 の共有アドレス空間(100.64.0.0/10、CGNAT/Tailscale等)も外部扱いしない。
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
		// 0.0.0.0/8 と 240.0.0.0/4(予約)。
		if v4[0] == 0 || v4[0] >= 240 {
			return false
		}
	}
	return true
}
