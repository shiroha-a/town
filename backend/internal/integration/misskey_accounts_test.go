package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/shiroha-a/town/internal/player"
)

// Misskeyアカウントの複数連携。仕様: .tmp/design_misskey_accounts.md
//
// MiAuthの承認そのものは相手インスタンスが要るのでここでは通らない。承認の後に
// 呼ばれるサービス層(LinkMisskeyAccount)を直接叩き、そこから先の規則
// — 代表の切り替え・削除の禁止・他人のアカウントの拒否 — を確かめる。

type accountResp struct {
	Host         string `json:"host"`
	RemoteUserID string `json:"remote_user_id"`
	Username     string `json:"username"`
	Acct         string `json:"acct"`
	Primary      bool   `json:"primary"`
	HasToken     bool   `json:"has_token"`
}

func listAccounts(t *testing.T, base string, playerID int64) []accountResp {
	t.Helper()
	resp, err := http.Get(base + "/api/v1/players/" + strconv.FormatInt(playerID, 10) + "/misskey-accounts")
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("list accounts status %d: %s", resp.StatusCode, body)
	}
	var out []accountResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode accounts: %v", err)
	}
	return out
}

func accountReq(t *testing.T, method, base string, playerID int64, path string) (int, []byte) {
	t.Helper()
	url := base + "/api/v1/players/" + strconv.FormatInt(playerID, 10) + "/misskey-accounts" + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// errorOf pulls the message out of an API error body.
func errorOf(t *testing.T, body []byte) string {
	t.Helper()
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return out.Error
}

func TestMisskeyAccounts(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	alice := register(t, srv.URL, "a.example", "alice")
	bob := register(t, srv.URL, "b.example", "bob")

	// 住民は自分の身元が連携済みで、それが代表になっている。
	accs := listAccounts(t, srv.URL, alice.ID)
	if len(accs) != 1 {
		t.Fatalf("accounts = %d, want 1", len(accs))
	}
	if accs[0].Host != "a.example" || accs[0].RemoteUserID != "alice" || !accs[0].Primary {
		t.Fatalf("primary account = %+v", accs[0])
	}

	// 2つ目を足す(MiAuthの承認が済んだ後にサーバーが呼ぶのと同じ呼び出し)。
	if err := testPlayerSvc.LinkMisskeyAccount(ctx, alice.ID, "c.example", "alice2", "alice2", ""); err != nil {
		t.Fatalf("link: %v", err)
	}
	accs = listAccounts(t, srv.URL, alice.ID)
	if len(accs) != 2 {
		t.Fatalf("accounts after link = %d, want 2", len(accs))
	}
	// 代表は先頭で、足したほうは代表ではない。
	if !accs[0].Primary || accs[0].Host != "a.example" || accs[1].Primary {
		t.Fatalf("accounts after link = %+v", accs)
	}
	if accs[1].Acct != "@alice2@c.example" {
		t.Fatalf("acct = %q", accs[1].Acct)
	}

	// 2つ目のアカウントでも同じ住民としてログインできる(これが機能の目的)。
	id, err := testPlayerSvc.FindByMisskeyAccount(ctx, "c.example", "alice2")
	if err != nil || id != alice.ID {
		t.Fatalf("find by linked account = %d, %v; want %d", id, err, alice.ID)
	}

	// 他の住民が使っているアカウントは奪えない。
	err = testPlayerSvc.LinkMisskeyAccount(ctx, alice.ID, "b.example", "bob", "bob", "")
	if !errors.Is(err, player.ErrAccountTaken) {
		t.Fatalf("link taken account = %v, want ErrAccountTaken", err)
	}
	if owner, err := testPlayerSvc.FindByMisskeyAccount(ctx, "b.example", "bob"); err != nil || owner != bob.ID {
		t.Fatalf("bob's account moved: owner = %d, %v", owner, err)
	}

	// 代表は外せない。外せてしまうと、公開している身元が消える。
	status, body := accountReq(t, http.MethodDelete, srv.URL, alice.ID, "/a.example/alice")
	if status != http.StatusConflict || !strings.Contains(errorOf(t, body), "プロフィールに出している") {
		t.Fatalf("delete primary = %d: %s", status, body)
	}

	// 代表の切り替え。プロフィールと解決済みIDのキャッシュは捨てられる。
	if _, err := pool.Exec(ctx,
		`INSERT INTO misskey_profiles (player_id, username) VALUES ($1, 'alice')`, alice.ID); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO misskey_resolved_users (viewer_host, target_player_id, resolved_user_id)
		 VALUES ('viewer.example', $1, 'old')`, alice.ID); err != nil {
		t.Fatalf("seed resolved: %v", err)
	}
	status, body = accountReq(t, http.MethodPost, srv.URL, alice.ID, "/c.example/alice2/primary")
	if status != http.StatusOK {
		t.Fatalf("set primary = %d: %s", status, body)
	}
	var host, remote string
	if err := pool.QueryRow(ctx,
		`SELECT instance_host, remote_user_id FROM players WHERE id = $1`, alice.ID).
		Scan(&host, &remote); err != nil {
		t.Fatalf("read player: %v", err)
	}
	if host != "c.example" || remote != "alice2" {
		t.Fatalf("player identity = %s/%s, want c.example/alice2", host, remote)
	}
	var profiles, resolved int
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM misskey_profiles WHERE player_id = $1),
		        (SELECT count(*) FROM misskey_resolved_users WHERE target_player_id = $1)`,
		alice.ID).Scan(&profiles, &resolved); err != nil {
		t.Fatalf("read caches: %v", err)
	}
	if profiles != 0 || resolved != 0 {
		t.Fatalf("caches kept after switch: profiles=%d resolved=%d", profiles, resolved)
	}

	// 代表でなくなったほうは外せる。
	status, body = accountReq(t, http.MethodDelete, srv.URL, alice.ID, "/a.example/alice")
	if status != http.StatusOK {
		t.Fatalf("delete non-primary = %d: %s", status, body)
	}
	accs = listAccounts(t, srv.URL, alice.ID)
	if len(accs) != 1 || accs[0].Host != "c.example" {
		t.Fatalf("accounts after delete = %+v", accs)
	}

	// 連携の追加として始めた承認は、始めた本人以外が引き換えられない。
	// (承認そのものは相手インスタンスが要るので、ここは手前の判定だけ確かめる)
	var pending string
	if err := pool.QueryRow(ctx,
		`INSERT INTO auth_sessions (id, host, player_id)
		 VALUES (gen_random_uuid(), 'c.example', $1) RETURNING id`, alice.ID).Scan(&pending); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	// X-Acting-Player-Id を付けない = cookieが付かない(未ログイン)。
	resp, err := http.Post(srv.URL+"/api/v1/auth/callback", "application/json",
		strings.NewReader(`{"session":"`+pending+`"}`))
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	hijack, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("hijacked link callback = %d: %s", resp.StatusCode, hijack)
	}
	// 弾かれただけで、連携は増えていない。
	if accs = listAccounts(t, srv.URL, alice.ID); len(accs) != 1 {
		t.Fatalf("accounts after hijack = %+v", accs)
	}

	// 最後の1つは外せない。外すとログインできなくなる。理由も確かめる:
	// 代表の保護と同じ409になるので、文面まで見ないと片方が消えても気付けない。
	status, body = accountReq(t, http.MethodDelete, srv.URL, alice.ID, "/c.example/alice2")
	if status != http.StatusConflict || !strings.Contains(errorOf(t, body), "最後の1つ") {
		t.Fatalf("delete last = %d: %s", status, body)
	}
}
