package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/shiroha-a/town/internal/webhook"
)

// adminJSON sends a request as the admin and returns the status and body.
func adminJSON(t *testing.T, method, url string, adminID int64, body any) (int, []byte) {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Acting-Player-Id", strconv.FormatInt(adminID, 10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := new(bytes.Buffer)
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.Bytes()
}

// TestWebhookCRUD covers 宛先の登録・URLの検証・イベントの絞り込み・削除。
func TestWebhookCRUD(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	admin := register(t, srv.URL, "misskey.example", "admin")
	base := srv.URL + "/api/v1/admin/webhooks"

	// httpsでないURLは弾く(通知の中身が平文で流れないように)。
	if status, _ := adminJSON(t, http.MethodPost, base, admin.ID, map[string]any{
		"url": "http://example.com/hook", "enabled": true,
	}); status != http.StatusUnprocessableEntity {
		t.Errorf("http のURLが通った: status = %d, want 422", status)
	}

	// 登録。知らないイベント名は落とされる。
	status, body := adminJSON(t, http.MethodPost, base, admin.ID, map[string]any{
		"url":     "https://discord.com/api/webhooks/1/abc",
		"label":   "運営",
		"enabled": true,
		"events":  []string{string(webhook.EventRegistered), "しらないイベント"},
	})
	if status != http.StatusOK {
		t.Fatalf("create status = %d: %s", status, body)
	}
	var created webhook.Hook
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if len(created.Events) != 1 || created.Events[0] != webhook.EventRegistered {
		t.Errorf("events = %v, want [player.registered]", created.Events)
	}

	// 一覧にはイベントの定義も付く(画面がこれで選択肢を作る)。
	status, body = adminJSON(t, http.MethodGet, base, admin.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("list status = %d", status)
	}
	var listed struct {
		Webhooks []webhook.Hook      `json:"webhooks"`
		Events   []webhook.EventInfo `json:"events"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Webhooks) != 1 {
		t.Fatalf("webhooks = %d, want 1", len(listed.Webhooks))
	}
	if len(listed.Events) == 0 {
		t.Error("イベントの一覧が空")
	}

	// 削除すると、積まれていた控えも一緒に消える(ON DELETE CASCADE)。
	if status, _ := adminJSON(t, http.MethodDelete,
		base+"/"+strconv.FormatInt(created.ID, 10), admin.ID, nil); status != http.StatusOK {
		t.Errorf("delete status = %d", status)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM webhooks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("削除後も宛先が残っている: %d", n)
	}
}

// TestWebhookQueueing は、宛先の絞り込みどおりに控えが積まれることを見る。
// 実際の送信(HTTP)は worker がやるので、ここでは積まれ方だけを確かめる。
func TestWebhookQueueing(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	admin := register(t, srv.URL, "misskey.example", "admin")
	base := srv.URL + "/api/v1/admin/webhooks"

	// 「入居」だけを受け取る宛先と、すべてを受け取る宛先。
	if status, body := adminJSON(t, http.MethodPost, base, admin.ID, map[string]any{
		"url": "https://example.com/only-register", "enabled": true,
		"events": []string{string(webhook.EventRegistered)},
	}); status != http.StatusOK {
		t.Fatalf("create status = %d: %s", status, body)
	}
	if status, body := adminJSON(t, http.MethodPost, base, admin.ID, map[string]any{
		"url": "https://example.com/everything", "enabled": true, "events": []string{},
	}); status != http.StatusOK {
		t.Fatalf("create status = %d: %s", status, body)
	}
	// 停止中の宛先には積まない。
	if status, body := adminJSON(t, http.MethodPost, base, admin.ID, map[string]any{
		"url": "https://example.com/off", "enabled": false, "events": []string{},
	}); status != http.StatusOK {
		t.Fatalf("create status = %d: %s", status, body)
	}

	queued := func(event webhook.Event) int {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM webhook_outbox WHERE event = $1`, string(event)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 入居: 「入居だけ」と「すべて」の2件に積まれる(停止中には積まれない)。
	register(t, srv.URL, "misskey.example", "newcomer")
	if got := queued(webhook.EventRegistered); got != 2 {
		t.Errorf("入居の控え = %d, want 2", got)
	}

	// 凍結: 「すべて」の1件だけ。
	target := register(t, srv.URL, "misskey.example", "target")
	if status, body := adminJSON(t, http.MethodPost,
		srv.URL+"/api/v1/admin/players/"+strconv.FormatInt(target.ID, 10)+"/suspend",
		admin.ID, map[string]any{"days": 3, "reason": "テスト"}); status != http.StatusOK {
		t.Fatalf("suspend status = %d: %s", status, body)
	}
	if got := queued(webhook.EventSuspended); got != 1 {
		t.Errorf("凍結の控え = %d, want 1", got)
	}
}

// TestWebhookDeliver は配送の成功と失敗の記録を見る。宛先は httptest で
// 立てた受け口……ではなく、SSRFガードが内部アドレスを拒むことの確認になる。
// 実際に外へ出すわけにはいかないので、ここでは「内部宛ては送られない」ことを
// 押さえる(これが破れると、この街が社内ネットワークへの踏み台になる)。
func TestWebhookRefusesInternalAddress(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	admin := register(t, srv.URL, "misskey.example", "admin")

	// 受け口を立てるが、ここへは届かないのが正しい(127.0.0.1のため)。
	got := make(chan struct{}, 1)
	sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()

	// URLの検証(ポート付き不可)を通すため、宛先は直にDBへ入れる。
	var hookID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO webhooks (url, label, enabled) VALUES ($1, 'internal', TRUE) RETURNING id`,
		sink.URL).Scan(&hookID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO webhook_outbox (webhook_id, event, severity, title)
		 VALUES ($1, 'test', 'info', '試し')`, hookID); err != nil {
		t.Fatal(err)
	}

	svc := webhook.New(pool)
	sent, err := svc.Deliver(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sent != 0 {
		t.Errorf("内部アドレスへ送ってしまった: sent = %d", sent)
	}
	select {
	case <-got:
		t.Error("内部の受け口に届いてしまった")
	default:
	}
	// 失敗として記録され、次の試行が先に延びている。
	var attempts int
	var lastErr string
	if err := pool.QueryRow(ctx,
		`SELECT attempts, last_error FROM webhook_outbox WHERE webhook_id = $1`,
		hookID).Scan(&attempts, &lastErr); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || lastErr == "" {
		t.Errorf("失敗が記録されていない: attempts = %d, err = %q", attempts, lastErr)
	}
	_ = admin
}
