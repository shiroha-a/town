package miauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UserAgent identifies this app to the instance.
const UserAgent = "TOWN/1.0 (+https://github.com/shiroha-a/town-rewrite)"

// Limits for outbound calls to a user-supplied instance.
const (
	dialTimeout     = 5 * time.Second
	requestTimeout  = 10 * time.Second
	maxResponseSize = 1 << 20 // 1MiB
	maxRedirects    = 3
)

// Client talks to a Misskey instance. All connections are checked against
// IsPublicIP at dial time, so a hostname that resolves to an internal address
// (or is re-pointed at one between checks) cannot be reached.
type Client struct {
	http *http.Client
}

// NewClient builds the SSRF-guarded HTTP client.
func NewClient() *Client {
	dialer := &net.Dialer{Timeout: dialTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("名前解決に失敗しました: %w", err)
			}
			// 検査に通ったIPへ「直接」繋ぐ。ホスト名で繋ぎ直すと、検査後に
			// 別のIPへ解決され得る(DNSリバインディング)。
			var lastErr error
			for _, ip := range ips {
				if !IsPublicIP(ip.IP) {
					lastErr = fmt.Errorf("内部アドレスには接続できません")
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("接続先が見つかりません")
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout: dialTimeout,
	}
	return &Client{http: &http.Client{
		Transport: transport,
		Timeout:   requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("リダイレクトが多すぎます")
			}
			// リダイレクト先も https 限定(DialContextでIPは毎回検査される)。
			if req.URL.Scheme != "https" {
				return fmt.Errorf("https以外へのリダイレクトは許可されません")
			}
			return nil
		},
	}}
}

// postJSON sends a JSON POST to https://{host}{path} and decodes the response.
func (c *Client) postJSON(ctx context.Context, host, path string, body, out any) error {
	// Misskey(Fastify)は Content-Type: application/json で空ボディだと
	// FST_ERR_CTP_EMPTY_JSON_BODY で拒否する。パラメータの無いエンドポイント
	// (miauth check など)にも必ず空オブジェクトを送る。
	if body == nil {
		body = map[string]any{}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	payload := strings.NewReader(string(b))
	// 変数名に url を使うと net/url を隠してしまうので endpoint とする。
	endpoint := "https://" + host + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// User-Agentを名乗る。既定の "Go-http-client/..." はCDN(Cloudflare等)に
	// bot として弾かれることがある。
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("そのインスタンスに接続できません: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 応答の冒頭を添える。原因(未対応API/CDNの遮断/フォーク差異)の切り分けに要る。
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("%s がエラーを返しました (HTTP %d) %s",
			host, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("インスタンスの応答を解釈できません")
	}
	return nil
}

// Meta is the subset of /api/meta we use to confirm the host is a Misskey
// instance and to show its name on the login screen.
type Meta struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	IconURL     string `json:"iconUrl"`
}

// FetchMeta verifies the host speaks the Misskey API (POST /api/meta is
// requireCredential:false) and returns its display info.
func (c *Client) FetchMeta(ctx context.Context, host string) (*Meta, error) {
	var m Meta
	if err := c.postJSON(ctx, host, "/api/meta", map[string]any{"detail": false}, &m); err != nil {
		return nil, err
	}
	if m.Version == "" {
		return nil, fmt.Errorf("%s はMisskeyインスタンスではないようです。", host)
	}
	return &m, nil
}

// CheckUser is the account returned by the MiAuth check endpoint.
type CheckUser struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	AvatarURL string `json:"avatarUrl"`
}

// CheckResult is the response of POST /api/miauth/{session}/check.
type CheckResult struct {
	OK    bool       `json:"ok"`
	Token string     `json:"token"`
	User  *CheckUser `json:"user"`
}

// Check exchanges an approved MiAuth session for an access token.
func (c *Client) Check(ctx context.Context, host, session string) (*CheckResult, error) {
	var res CheckResult
	if err := c.postJSON(ctx, host, "/api/miauth/"+session+"/check", nil, &res); err != nil {
		return nil, err
	}
	if res.Token == "" || res.User == nil || res.User.ID == "" {
		return nil, fmt.Errorf("認証が完了していません。")
	}
	return &res, nil
}

// AuthURL builds the URL the browser is sent to for approval.
func AuthURL(host, session, appName, callback string, permissions []string) string {
	q := url.Values{}
	q.Set("name", appName)
	if callback != "" {
		q.Set("callback", callback)
	}
	if len(permissions) > 0 {
		q.Set("permission", strings.Join(permissions, ","))
	}
	return "https://" + host + "/miauth/" + session + "?" + q.Encode()
}
