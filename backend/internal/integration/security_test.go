package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/shiroha-a/town/internal/mail"
)

// TestRetireReturnsAllAccounts は、退会で現金だけでなく普通預金・スーパー定期も
// 街へ返ることを見る。口座ごとに同じ ref で仕訳を切っていた頃は、2口座目以降が
// 「同一refの二重post」と見なされて捨てられ、誰のものでもない残高が台帳に
// 残っていた。
func TestRetireReturnsAllAccounts(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	alice := register(t, srv.URL, "misskey.example", "alice")
	bankAction(t, srv.URL, "/bank/deposit", alice.ID, 100000, "dep-1")

	bal := func(prefix string) int64 {
		var v int64
		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account = $1`,
			prefix+":"+strconv.FormatInt(alice.ID, 10)).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if bal("savings") == 0 {
		t.Fatal("前提が崩れている: 預金が入っていない")
	}

	body, _ := json.Marshal(map[string]string{"confirm": "alice"})
	resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/retire",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire status = %d", resp.StatusCode)
	}

	for _, acct := range []string{"player", "savings", "super_savings"} {
		if v := bal(acct); v != 0 {
			t.Errorf("退会後も %s に残高が残っている: %d", acct, v)
		}
	}
}

// TestLastAdminCannotBeDemoted は、管理者が1人しか居ないときにその人から
// 管理者権限を外せないことを見る。全員から外すと管理画面に入れなくなる。
func TestLastAdminCannotBeDemoted(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	admin := register(t, srv.URL, "misskey.example", "admin") // 最初の住民が管理者
	other := register(t, srv.URL, "misskey.example", "other")

	demote := func(target int64) int {
		body, _ := json.Marshal(map[string]any{
			"display_name": "n", "money": 0, "is_admin": false,
			"params": map[string]int{}, "job": "無職",
		})
		req, _ := http.NewRequest(http.MethodPut,
			srv.URL+"/api/v1/admin/players/"+strconv.FormatInt(target, 10), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Acting-Player-Id", strconv.FormatInt(admin.ID, 10))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if status := demote(admin.ID); status != http.StatusUnprocessableEntity {
		t.Errorf("最後の管理者を降格できてしまった: status = %d, want 422", status)
	}
	var isAdmin bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM player_roles WHERE player_id = $1 AND role = 'admin')`,
		admin.ID).Scan(&isAdmin); err != nil {
		t.Fatal(err)
	}
	if !isAdmin {
		t.Fatal("拒否されたのにロールが消えている")
	}

	// 2人目を管理者にすれば降ろせる。
	body, _ := json.Marshal(map[string]any{
		"display_name": "other", "money": 0, "is_admin": true,
		"params": map[string]int{}, "job": "無職",
	})
	req, _ := http.NewRequest(http.MethodPut,
		srv.URL+"/api/v1/admin/players/"+strconv.FormatInt(other.ID, 10), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Acting-Player-Id", strconv.FormatInt(admin.ID, 10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("2人目の管理者化に失敗: status = %d", resp.StatusCode)
	}
	if status := demote(admin.ID); status != http.StatusOK {
		t.Errorf("他に管理者が居るのに降格できない: status = %d", status)
	}
}

// TestBodyLimit は、大きすぎるリクエストボディが読まれる前に 413 で切られる
// ことを見る。上限が無いと、未ログインでも叩ける経路にJSONを流し込むだけで
// メモリを食える。
func TestBodyLimit(t *testing.T) {
	srv, _ := setup(t)

	// 既定(64KiB)を超える本文。中身はJSONとして正しい形にしておき、
	// 「大きさだけ」で弾かれることをはっきりさせる。
	huge, _ := json.Marshal(map[string]string{"session": strings.Repeat("a", 128<<10)})
	resp, err := http.Post(srv.URL+"/api/v1/auth/callback", "application/json", bytes.NewReader(huge))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("巨大なボディが通った: status = %d, want 413", resp.StatusCode)
	}

	// 普通の大きさは今までどおり処理される(見つからないので401)。
	small, _ := json.Marshal(map[string]string{"session": "nope"})
	resp2, err := http.Post(srv.URL+"/api/v1/auth/callback", "application/json", bytes.NewReader(small))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode == http.StatusRequestEntityTooLarge {
		t.Errorf("普通の大きさのボディまで弾かれている")
	}
}

// TestMailBodyLimit は住民どうしのメールの本文長。通数だけ絞っても1通が
// 青天井だと受信箱を重くできる。
func TestMailBodyLimit(t *testing.T) {
	srv, _ := setup(t)

	alice := register(t, srv.URL, "misskey.example", "alice")
	bob := register(t, srv.URL, "misskey.example", "bob")

	send := func(body string) int {
		b, _ := json.Marshal(map[string]any{"recipient_id": bob.ID, "body": body})
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/mail/send",
			"application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if status := send(strings.Repeat("あ", mail.MaxBody+1)); status == http.StatusOK {
		t.Errorf("上限を超える本文が通った")
	}
	if status := send(strings.Repeat("あ", 10)); status != http.StatusOK {
		t.Errorf("普通の本文が送れない: status = %d", status)
	}
}
