// Package integration holds API-level tests that exercise the full stack
// against a real PostgreSQL instance. They run only when TOWN_TEST_DATABASE_URL
// is set (e.g. pointed at the compose test database).
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/shiroha-a/town/internal/action"
	"github.com/shiroha-a/town/internal/attendance"
	"github.com/shiroha-a/town/internal/bank"
	"github.com/shiroha-a/town/internal/building"
	"github.com/shiroha-a/town/internal/cleague"
	"github.com/shiroha-a/town/internal/content"
	"github.com/shiroha-a/town/internal/db"
	"github.com/shiroha-a/town/internal/gametime"
	"github.com/shiroha-a/town/internal/greeting"
	"github.com/shiroha-a/town/internal/httpapi"
	"github.com/shiroha-a/town/internal/keiba"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/mail"
	"github.com/shiroha-a/town/internal/news"
	"github.com/shiroha-a/town/internal/player"
	"github.com/shiroha-a/town/internal/ranking"
	"github.com/shiroha-a/town/internal/rng"
	"github.com/shiroha-a/town/internal/settings"
	"github.com/shiroha-a/town/internal/stock"
	"github.com/shiroha-a/town/internal/townmap"
	"github.com/shiroha-a/town/internal/worker"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type playerResp struct {
	ID          int64    `json:"id"`
	Roles       []string `json:"roles"`
	Money       int64    `json:"money"`
	Savings     int64    `json:"savings"`
	CurrentTown int      `json:"current_town"`
	Status      struct {
		Job          string   `json:"job"`
		JobLevel     int      `json:"job_level"`
		JobExp       int      `json:"job_exp"`
		JobKaisuu    int      `json:"job_kaisuu"`
		MasteredJobs []string `json:"mastered_jobs"`
		Energy       int      `json:"energy"`
		EnergyMax    int      `json:"energy_max"`
		NouEnergy    int      `json:"nou_energy"`
		NouEnergyMax int      `json:"nou_energy_max"`
		Satiety      int      `json:"satiety"`
		HeightCm     int      `json:"height_cm"`
		WeightG      int      `json:"weight_g"`
		BMI          int      `json:"bmi"`
		BodyType     string   `json:"body_type"`
		DiseaseIndex int      `json:"disease_index"`
		DiseaseName  string   `json:"disease_name"`
		Condition    string   `json:"condition"`
	} `json:"status"`
	Items []struct {
		ItemID        int64  `json:"item_id"`
		Name          string `json:"name"`
		Quantity      int    `json:"quantity"`
		RemainingUses int    `json:"remaining_uses"`
		Sets          int    `json:"sets"`
	} `json:"items"`
}

func (p playerResp) itemQty(itemID int64) int {
	for _, it := range p.Items {
		if it.ItemID == itemID {
			return it.Quantity
		}
	}
	return 0
}

func (p playerResp) itemRemaining(itemID int64) int {
	for _, it := range p.Items {
		if it.ItemID == itemID {
			return it.RemainingUses
		}
	}
	return 0
}

func setup(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TOWN_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TOWN_TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	// building は街リストをプロセス共有のグローバルに持つため、テスト間の汚染を
	// 避けて既定の5街にリセットする(TestTownsが変更するため)。
	building.SetTowns(building.DefaultTowns())
	if err := db.Migrate(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// 各テスト前にワールド状態をまっさらにする(content_jobsのシードは残す)。
	// shop_daily_stockはgame_date別に在庫を持つため、テスト間で在庫が持ち越されて
	// 枯渇しないようリセット対象に含める。
	if _, err := pool.Exec(ctx,
		`TRUNCATE players, player_roles, player_status, status_history,
		 ledger_entry, ledger_tx, action_log, worker_jobs, shop_daily_stock RESTART IDENTITY CASCADE`); err != nil {
		pool.Close()
		t.Fatalf("truncate: %v", err)
	}

	led := ledger.New(pool)
	// 既存テストは日次ローテを無効(0=全件)にして全アイテムを購入可能に保つ。
	// 日次ローテ自体は TestDailyShop が専用サービスで検証する。
	st := newTestSettings(t, ctx, pool, settings.Game{
		InitialMoney:      500000,
		EnergyRecoverySec: 60,
		NouRecoverySec:    60,
		WorkIntervalMin:   0,
		DebugNoCooldown:   false,
	})
	svc := player.New(pool, led, rng.New(1), st)
	actions := action.New(pool, led, svc, rng.New(2), time.UTC, 5, st)
	contentSvc := content.New(pool, time.UTC, 5, st)
	tmap, err := townmap.NewStore(ctx, pool, townmap.Default())
	if err != nil {
		pool.Close()
		t.Fatalf("townmap: %v", err)
	}
	// town_map行はTRUNCATE対象外で前回実行のシードが残るため、現在のDefault()へ強制的に
	// 正規化して決定論にする(settingsのnewTestSettingsと同じ考え方)。
	if err := tmap.Set(ctx, townmap.Default()); err != nil {
		pool.Close()
		t.Fatalf("townmap set: %v", err)
	}
	srv := httptest.NewServer(httpapi.NewServer(svc, actions, contentSvc, st, tmap, stock.New(pool), keiba.New(pool, rng.New(7)), mail.New(pool, time.UTC, 5), greeting.New(pool), attendance.New(pool, time.UTC, 5), cleague.New(pool), news.New(pool), ranking.New(pool)))
	t.Cleanup(func() {
		srv.Close()
		pool.Close()
	})
	return srv, pool
}

// newTestSettings builds a settings.Store seeded with the given values and forces
// those exact values via Set (the app_settings row survives TRUNCATE, so Set
// overrides whatever a prior test seeded).
func newTestSettings(t *testing.T, ctx context.Context, pool *pgxpool.Pool, g settings.Game) *settings.Store {
	t.Helper()
	st, err := settings.NewStore(ctx, pool, g)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if err := st.Set(ctx, g); err != nil {
		t.Fatalf("settings set: %v", err)
	}
	return st
}

func register(t *testing.T, base, host, uid string) playerResp {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"instance_host": host, "remote_user_id": uid})
	resp, err := http.Post(base+"/api/v1/players", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register status: %d", resp.StatusCode)
	}
	var p playerResp
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return p
}

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

func TestRegistrationAndLedger(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	// 最初のプレイヤーは管理者ロール + 初期所持金50万円。
	first := register(t, srv.URL, "misskey.example", "alice")
	if !hasRole(first.Roles, "admin") {
		t.Errorf("first player roles = %v, want admin", first.Roles)
	}
	if first.Money != 500000 {
		t.Errorf("first player money = %d, want 500000", first.Money)
	}
	if first.Status.Job != "学生" {
		t.Errorf("first player job = %q, want 学生", first.Status.Job)
	}

	// 2人目は一般ユーザー。
	second := register(t, srv.URL, "misskey.example", "bob")
	if hasRole(second.Roles, "admin") {
		t.Errorf("second player should not be admin: %v", second.Roles)
	}
	if !hasRole(second.Roles, "user") {
		t.Errorf("second player roles = %v, want user", second.Roles)
	}

	// 同一identityの再登録は冪等: 同じID・所持金は二重付与されない。
	again := register(t, srv.URL, "misskey.example", "alice")
	if again.ID != first.ID {
		t.Errorf("re-register id = %d, want %d", again.ID, first.ID)
	}
	if again.Money != 500000 {
		t.Errorf("re-register money = %d, want 500000 (no double grant)", again.Money)
	}

	// GET でも同じ値が読める。
	resp, err := http.Get(srv.URL + "/api/v1/players/" + strconv.FormatInt(first.ID, 10))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status: %d", resp.StatusCode)
	}

	// 台帳の全entry合計は常に0(複式の不変条件)。
	led := ledger.New(pool)
	sum, err := led.AuditZeroSum(ctx)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if sum != 0 {
		t.Errorf("ledger zero-sum invariant broken: sum = %d", sum)
	}

	// 流通マネー総量 = 2人 × 50万円。
	total, err := led.TotalPlayerMoney(ctx)
	if err != nil {
		t.Fatalf("total: %v", err)
	}
	if total != 1000000 {
		t.Errorf("total player money = %d, want 1000000", total)
	}
}

func doWork(t *testing.T, base string, id int64, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(id, 10)+"/work",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("work post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("work decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

// 回帰: 別プレイヤーが同じ冪等キーを使っても、双方の入金が行われること。
// (以前は台帳refがグローバルで、2人目の入金が誤ってスキップされていた)
func TestCrossPlayerIdempotency(t *testing.T) {
	srv, _ := setup(t)

	p1 := register(t, srv.URL, "misskey.example", "one")
	p2 := register(t, srv.URL, "misskey.example", "two")
	changeJob(t, srv.URL, p1.ID, "アルバイト", "j-one")
	changeJob(t, srv.URL, p2.ID, "アルバイト", "j-two")

	const shared = "SHARED-KEY"
	r1, c1 := doWork(t, srv.URL, p1.ID, shared)
	r2, c2 := doWork(t, srv.URL, p2.ID, shared)
	if c1 != http.StatusOK || c2 != http.StatusOK {
		t.Fatalf("work status: %d %d", c1, c2)
	}
	// 給料1000+労働ボーナス40(消費2×20)=1040。
	if r1.Money != 501040 {
		t.Errorf("player1 money = %d, want 501040", r1.Money)
	}
	if r2.Money != 501040 {
		t.Errorf("player2 money = %d, want 501040 (同一キーでも別プレイヤーは独立)", r2.Money)
	}
}

func changeJob(t *testing.T, base string, id int64, jobName, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"job_name": jobName, "idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(id, 10)+"/job",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("job post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		json.NewDecoder(resp.Body).Decode(&p)
	}
	return p, resp.StatusCode
}

func TestWorkAction(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	alice := register(t, srv.URL, "misskey.example", "alice")
	// energy_maxは初期パラメータ(各5)由来で6(design 17.9、旧basic.cgi式)。
	if alice.Status.Energy != 6 || alice.Money != 500000 {
		t.Fatalf("initial state: energy=%d money=%d", alice.Status.Energy, alice.Money)
	}
	if alice.Status.Job != "学生" {
		t.Fatalf("initial job = %q, want 学生", alice.Status.Job)
	}

	// 学生は働けない -> 422。
	if _, code := doWork(t, srv.URL, alice.ID, "work-student"); code != http.StatusUnprocessableEntity {
		t.Errorf("student work status = %d, want 422", code)
	}

	// 職業安定所でアルバイトに転職。
	jp, code := changeJob(t, srv.URL, alice.ID, "アルバイト", "job-1")
	if code != http.StatusOK {
		t.Fatalf("change job status = %d", code)
	}
	if jp.Status.Job != "アルバイト" {
		t.Errorf("job after change = %q, want アルバイト", jp.Status.Job)
	}

	// 仕事1回: +1040円(給料1000+労働ボーナス40=消費2×20), 身体パワー-2(消費=基準1+基準1×係数1、初期上限6→4)。
	p, code := doWork(t, srv.URL, alice.ID, "work-1")
	if code != http.StatusOK {
		t.Fatalf("work status = %d", code)
	}
	if p.Money != 501040 || p.Status.Energy != 4 {
		t.Errorf("after work: money=%d energy=%d, want 501040/4", p.Money, p.Status.Energy)
	}

	// 同一idempotency_keyの再実行は冪等: 状態は変わらない。
	p2, code := doWork(t, srv.URL, alice.ID, "work-1")
	if code != http.StatusOK {
		t.Fatalf("idempotent work status = %d", code)
	}
	if p2.Money != 501040 || p2.Status.Energy != 4 {
		t.Errorf("idempotent work changed state: money=%d energy=%d", p2.Money, p2.Status.Energy)
	}

	// 別キーならもう一度適用される(4-2=2)。
	p3, _ := doWork(t, srv.URL, alice.ID, "work-2")
	if p3.Money != 502080 || p3.Status.Energy != 2 {
		t.Errorf("second work: money=%d energy=%d, want 502080/2", p3.Money, p3.Status.Energy)
	}

	// 身体パワー0では条件不成立 -> 422、状態は不変。
	if _, err := pool.Exec(ctx, `UPDATE player_status SET energy = 0 WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	_, code = doWork(t, srv.URL, alice.ID, "work-3")
	if code != http.StatusUnprocessableEntity {
		t.Errorf("work with 0 energy status = %d, want 422", code)
	}
	after, _ := http.Get(srv.URL + "/api/v1/players/" + strconv.FormatInt(alice.ID, 10))
	after.Body.Close()

	// 失敗したアクションのidempotency_keyは消費されていない(再試行可能)。
	var used int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM action_log WHERE idempotency_key = 'work-3'`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != 0 {
		t.Errorf("failed action consumed its idempotency key (count=%d)", used)
	}

	// 台帳ゼロ和は保たれる。
	led := ledger.New(pool)
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

func itemAction(t *testing.T, base, path string, playerID, itemID int64, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"item_id": itemID, "idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+path,
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s post: %v", path, err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("%s decode: %v", path, err)
		}
	}
	return p, resp.StatusCode
}

// buySets purchases `sets` sets of an item in one request.
func buySets(t *testing.T, base string, playerID, itemID int64, sets int, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"item_id": itemID, "sets": sets, "idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/buy",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("buy post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		json.NewDecoder(resp.Body).Decode(&p)
	}
	return p, resp.StatusCode
}

func TestBuyAndUseItem(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	// シードされた「栄養ドリンク」(500円, energy+3)のIDを引く。
	var drinkID int64
	if err := pool.QueryRow(ctx,
		`SELECT id FROM content_items WHERE name = '栄養ドリンク'`).Scan(&drinkID); err != nil {
		t.Fatalf("seed lookup: %v", err)
	}

	alice := register(t, srv.URL, "misskey.example", "alice")

	// 購入: 500円がplayerからsystem:shop_sinkへ抜け、所持品+1。
	p, code := itemAction(t, srv.URL, "/buy", alice.ID, drinkID, "buy-1")
	if code != http.StatusOK {
		t.Fatalf("buy status = %d", code)
	}
	if p.Money != 499500 {
		t.Errorf("money after buy = %d, want 499500", p.Money)
	}
	if p.itemQty(drinkID) != 1 {
		t.Errorf("drink qty after buy = %d, want 1", p.itemQty(drinkID))
	}

	// 購入は流通マネーを減らす(sink)。台帳ゼロ和は維持。
	led := ledger.New(pool)
	if total, _ := led.TotalPlayerMoney(ctx); total != 499500 {
		t.Errorf("circulating money = %d, want 499500", total)
	}
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken after buy: %d", sum)
	}

	// 購入の冪等性: 同一キーは二重課金しない。
	p2, _ := itemAction(t, srv.URL, "/buy", alice.ID, drinkID, "buy-1")
	if p2.Money != 499500 || p2.itemQty(drinkID) != 1 {
		t.Errorf("idempotent buy changed state: money=%d qty=%d", p2.Money, p2.itemQty(drinkID))
	}

	// 使用: 所持品-1、energy+3。上限(パラメータ由来=6)未満まで下げてから使い回復を観測する。
	if _, err := pool.Exec(ctx, `UPDATE player_status SET energy = 2 WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	p3, code := itemAction(t, srv.URL, "/use", alice.ID, drinkID, "use-1")
	if code != http.StatusOK {
		t.Fatalf("use status = %d", code)
	}
	if p3.Status.Energy != 5 {
		t.Errorf("energy after use = %d, want 5 (2+3)", p3.Status.Energy)
	}
	if p3.itemQty(drinkID) != 0 {
		t.Errorf("drink qty after use = %d, want 0", p3.itemQty(drinkID))
	}

	// 在庫が無い状態での使用は 422。
	_, code = itemAction(t, srv.URL, "/use", alice.ID, drinkID, "use-2")
	if code != http.StatusUnprocessableEntity {
		t.Errorf("use without stock status = %d, want 422", code)
	}

	// 所持金不足での購入は 422。所持金を0まで抜いて(複式でゼロ和維持)高額本を買う。
	var bookID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM content_items WHERE name = '参考書'`).Scan(&bookID); err != nil {
		t.Fatal(err)
	}
	if err := led.Post(ctx, "test_drain", "", []ledger.Entry{
		{Account: ledger.PlayerAccount(alice.ID), Delta: -p3.Money},
		{Account: ledger.SystemAccount("test_drain"), Delta: p3.Money},
	}); err != nil {
		t.Fatalf("drain: %v", err)
	}
	_, code = itemAction(t, srv.URL, "/buy", alice.ID, bookID, "buy-book")
	if code != http.StatusUnprocessableEntity {
		t.Errorf("buy with no money status = %d, want 422", code)
	}
}

func TestPowerRecovery(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	alice := register(t, srv.URL, "misskey.example", "alice")

	// energyを2(基準5分前)、nouを3(基準2分前)にする。rate60秒。
	// 回復メカニクスを上限式から切り離して検証するため energy_max/nou_energy_max は10に固定する。
	if _, err := pool.Exec(ctx,
		`UPDATE player_status
		 SET energy = 2, energy_max = 10, energy_recovered_at = now() - interval '5 minutes',
		     nou_energy = 3, nou_energy_max = 10, nou_recovered_at = now() - interval '2 minutes'
		 WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RecoverPower(ctx, pool, 60, 60); err != nil {
		t.Fatal(err)
	}
	var e, ne int
	if err := pool.QueryRow(ctx,
		`SELECT energy, nou_energy FROM player_status WHERE player_id = $1`, alice.ID).Scan(&e, &ne); err != nil {
		t.Fatal(err)
	}
	if e != 7 { // 2 + floor(300/60)=5
		t.Errorf("energy = %d, want 7", e)
	}
	if ne != 5 { // 3 + floor(120/60)=2
		t.Errorf("nou_energy = %d, want 5", ne)
	}

	// 上限クランプ: energy_max(10)を超えない。
	if _, err := pool.Exec(ctx,
		`UPDATE player_status SET energy = 9, energy_recovered_at = now() - interval '10 minutes'
		 WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RecoverPower(ctx, pool, 60, 60); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT energy FROM player_status WHERE player_id = $1`, alice.ID).Scan(&e); err != nil {
		t.Fatal(err)
	}
	if e != 10 {
		t.Errorf("energy after cap = %d, want 10", e)
	}
}

func TestSyokudou(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	// 食堂メニュー取得。
	resp, err := http.Get(srv.URL + "/api/v1/facilities/syokudou/menu")
	if err != nil {
		t.Fatal(err)
	}
	var menu []struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Price int64  `json:"price"`
	}
	json.NewDecoder(resp.Body).Decode(&menu)
	resp.Body.Close()
	if len(menu) < 3 {
		t.Fatalf("menu size = %d", len(menu))
	}
	var curryID, curryPrice int64
	for _, m := range menu {
		if m.Name == "カレー" {
			curryID, curryPrice = m.ID, m.Price
		}
	}
	if curryID == 0 {
		t.Fatal("カレー not in menu")
	}

	alice := register(t, srv.URL, "misskey.example", "alice") // satiety=100(満腹)

	eatOnce := func(key string) (playerResp, int) {
		body, _ := json.Marshal(map[string]any{"food_id": curryID, "idempotency_key": key})
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/eat",
			"application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("eat post: %v", err)
		}
		defer resp.Body.Close()
		var p playerResp
		if resp.StatusCode == http.StatusOK {
			json.NewDecoder(resp.Body).Decode(&p)
		}
		return p, resp.StatusCode
	}

	// 満腹(100)では食べられない -> 422。
	if _, code := eatOnce("eat-full"); code != http.StatusUnprocessableEntity {
		t.Errorf("eat while full status = %d, want 422", code)
	}

	// 満腹度を50に下げると食べられる。一律で満腹(100)になる。
	if _, err := pool.Exec(ctx, `UPDATE player_status SET satiety = 50 WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	p, code := eatOnce("eat-1")
	if code != http.StatusOK {
		t.Fatalf("eat status = %d", code)
	}
	if p.Money != 500000-curryPrice {
		t.Errorf("money = %d, want %d", p.Money, 500000-curryPrice)
	}
	if p.Status.Satiety != 100 {
		t.Errorf("satiety = %d, want 100 (一律で満腹)", p.Status.Satiety)
	}
	var tairyoku int
	pool.QueryRow(ctx, `SELECT tairyoku FROM player_status WHERE player_id = $1`, alice.ID).Scan(&tairyoku)
	if tairyoku != 7 { // 初期5 + 2
		t.Errorf("tairyoku = %d, want 7", tairyoku)
	}

	// 満腹になったので続けては食べられない -> 422。
	if _, code := eatOnce("eat-2"); code != http.StatusUnprocessableEntity {
		t.Errorf("second eat status = %d, want 422", code)
	}
}

func TestGym(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	resp, err := http.Get(srv.URL + "/api/v1/facilities/gym/menu")
	if err != nil {
		t.Fatal(err)
	}
	var menu []struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Price int64  `json:"price"`
	}
	json.NewDecoder(resp.Body).Decode(&menu)
	resp.Body.Close()
	var stretchID, stretchPrice int64
	for _, m := range menu {
		if m.Name == "ストレッチ" {
			stretchID, stretchPrice = m.ID, m.Price
		}
	}
	if stretchID == 0 {
		t.Fatal("ストレッチ not in gym menu")
	}

	alice := register(t, srv.URL, "misskey.example", "alice")

	// 鍛える: 代金-600, 身体パワー-1(初期上限6→6-1=5), 体力+1。
	body, _ := json.Marshal(map[string]any{"menu_id": stretchID, "idempotency_key": "gym-1"})
	resp, _ = http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/facilities/gym/use",
		"application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("train status = %d", resp.StatusCode)
	}
	var p playerResp
	json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()
	if p.Money != 500000-stretchPrice {
		t.Errorf("money = %d, want %d", p.Money, 500000-stretchPrice)
	}
	if p.Status.Energy != 5 {
		t.Errorf("energy = %d, want 5", p.Status.Energy)
	}
	var tairyoku int
	pool.QueryRow(ctx, `SELECT tairyoku FROM player_status WHERE player_id = $1`, alice.ID).Scan(&tairyoku)
	if tairyoku != 6 { // 初期5 + 1
		t.Errorf("tairyoku = %d, want 6", tairyoku)
	}

	// クールタイム内の再トレーニングは 422。
	body, _ = json.Marshal(map[string]any{"menu_id": stretchID, "idempotency_key": "gym-2"})
	resp, _ = http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/facilities/gym/use",
		"application/json", bytes.NewReader(body))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("second train status = %d, want 422", resp.StatusCode)
	}
}

func TestItemUseCooldown(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	// 栄養ドリンクは使用間隔30分。
	var drinkID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM content_items WHERE name = '栄養ドリンク'`).Scan(&drinkID); err != nil {
		t.Fatal(err)
	}
	alice := register(t, srv.URL, "misskey.example", "alice")

	// 2個購入。
	itemAction(t, srv.URL, "/buy", alice.ID, drinkID, "b1")
	itemAction(t, srv.URL, "/buy", alice.ID, drinkID, "b2")

	// 1回目の使用はOK。
	if _, code := itemAction(t, srv.URL, "/use", alice.ID, drinkID, "u1"); code != http.StatusOK {
		t.Fatalf("first use status = %d, want 200", code)
	}
	// 間隔内の2回目は 422(在庫はあるがクールタイム中)。
	if _, code := itemAction(t, srv.URL, "/use", alice.ID, drinkID, "u2"); code != http.StatusUnprocessableEntity {
		t.Errorf("second use within interval status = %d, want 422", code)
	}
	// 在庫は減っていない(2個購入-1使用=1個)。
	var qty int
	if err := pool.QueryRow(ctx,
		`SELECT quantity FROM player_items WHERE player_id = $1 AND item_id = $2`,
		alice.ID, drinkID).Scan(&qty); err != nil {
		t.Fatal(err)
	}
	if qty != 1 {
		t.Errorf("quantity = %d, want 1 (cooldownで2回目は消費されない)", qty)
	}
}

func bankAction(t *testing.T, base, path string, playerID, amount int64, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"amount": amount, "idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+path,
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s post: %v", path, err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("%s decode: %v", path, err)
		}
	}
	return p, resp.StatusCode
}

func TestBankAndInterest(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	alice := register(t, srv.URL, "misskey.example", "alice") // cash 500000, savings 0

	// 預金: cash 500000 -> 400000, savings 0 -> 100000。
	p, code := bankAction(t, srv.URL, "/bank/deposit", alice.ID, 100000, "dep-1")
	if code != http.StatusOK {
		t.Fatalf("deposit status = %d", code)
	}
	if p.Money != 400000 || p.Savings != 100000 {
		t.Errorf("after deposit: cash=%d savings=%d, want 400000/100000", p.Money, p.Savings)
	}

	// 引き出し: savings 100000 -> 60000, cash 400000 -> 440000。
	p, code = bankAction(t, srv.URL, "/bank/withdraw", alice.ID, 40000, "wd-1")
	if code != http.StatusOK {
		t.Fatalf("withdraw status = %d", code)
	}
	if p.Money != 440000 || p.Savings != 60000 {
		t.Errorf("after withdraw: cash=%d savings=%d, want 440000/60000", p.Money, p.Savings)
	}

	// 貯金超過の引き出しは 422。
	_, code = bankAction(t, srv.URL, "/bank/withdraw", alice.ID, 999999, "wd-2")
	if code != http.StatusUnprocessableEntity {
		t.Errorf("over-withdraw status = %d, want 422", code)
	}

	// 日次利息: savings 60000 の 0.5% = 300(切り捨て)。
	led := ledger.New(pool)
	accrue := func() int {
		var n int
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var e error
			n, e = bank.AccrueInterest(ctx, tx, led, "savings:", "interest:", 5)
			return e
		})
		if err != nil {
			t.Fatalf("accrue: %v", err)
		}
		return n
	}
	if n := accrue(); n != 1 {
		t.Errorf("interest accounts = %d, want 1", n)
	}
	sav, err := led.Balance(ctx, ledger.SavingsAccount(alice.ID))
	if err != nil {
		t.Fatal(err)
	}
	if sav != 60300 {
		t.Errorf("savings after interest = %d, want 60300 (60000 + floor(0.5%%))", sav)
	}

	// 台帳ゼロ和は利息(faucet)後も維持。
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken after interest: %d", sum)
	}
}

func adminPost(t *testing.T, base, path string, actingID int64, body any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if actingID > 0 {
		req.Header.Set("X-Acting-Player-Id", strconv.FormatInt(actingID, 10))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func TestAdminContentAndSimulate(t *testing.T) {
	srv, _ := setup(t)

	admin := register(t, srv.URL, "misskey.example", "root") // 最初=admin
	user := register(t, srv.URL, "misskey.example", "alice") // 一般

	// 認可: ヘッダ無し=401、非admin=403。
	if code, _ := adminPost(t, srv.URL, "/api/v1/admin/items", 0, map[string]any{"name": "x"}); code != http.StatusUnauthorized {
		t.Errorf("no-header status = %d, want 401", code)
	}
	if code, _ := adminPost(t, srv.URL, "/api/v1/admin/items", user.ID, map[string]any{"name": "x"}); code != http.StatusForbidden {
		t.Errorf("non-admin status = %d, want 403", code)
	}

	// アイテム作成(有効な効果)。
	code, body := adminPost(t, srv.URL, "/api/v1/admin/items", admin.ID, map[string]any{
		"name": "回復薬", "category": "薬", "price": 300,
		"effect": []map[string]any{{"op": "add_param", "param": "energy", "amount": 5}},
	})
	if code != http.StatusOK {
		t.Fatalf("create item status = %d, body = %s", code, body)
	}
	var created struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Name != "回復薬" {
		t.Errorf("created item = %+v", created)
	}

	// 不正な効果は 400 で拒否(未知op)。
	if code, _ := adminPost(t, srv.URL, "/api/v1/admin/items", admin.ID, map[string]any{
		"name": "bad", "effect": []map[string]any{{"op": "delete_universe"}},
	}); code != http.StatusBadRequest {
		t.Errorf("invalid effect status = %d, want 400", code)
	}

	// シミュレート: 所持金を増やす効果は faucet 警告を返す。
	code, body = adminPost(t, srv.URL, "/api/v1/admin/simulate", admin.ID, map[string]any{
		"effect": []map[string]any{
			{"op": "add_money", "amount": 1000},
			{"op": "add_param", "param": "energy", "amount": -2},
		},
		"state": map[string]any{
			"money":  500,
			"params": map[string]any{"energy": map[string]any{"value": 5, "max": 10}},
		},
	})
	if code != http.StatusOK {
		t.Fatalf("simulate status = %d, body = %s", code, body)
	}
	var sim struct {
		Plan struct {
			MoneyDelta int64 `json:"money_delta"`
			Params     []struct {
				Name     string `json:"name"`
				NewValue int    `json:"new_value"`
			} `json:"params"`
		} `json:"plan"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(body, &sim); err != nil {
		t.Fatal(err)
	}
	if sim.Plan.MoneyDelta != 1000 {
		t.Errorf("sim money_delta = %d, want 1000", sim.Plan.MoneyDelta)
	}
	if len(sim.Plan.Params) != 1 || sim.Plan.Params[0].NewValue != 3 {
		t.Errorf("sim params = %+v, want energy->3", sim.Plan.Params)
	}
	if len(sim.Warnings) == 0 {
		t.Error("expected a faucet warning for the money-adding effect")
	}

	// End-to-end: 管理者が作ったアイテムをプレイヤーが購入できる。
	p, buyCode := itemAction(t, srv.URL, "/buy", user.ID, created.ID, "buy-created")
	if buyCode != http.StatusOK {
		t.Fatalf("buy admin-created item status = %d", buyCode)
	}
	if p.Money != 499700 {
		t.Errorf("money after buying created item = %d, want 499700", p.Money)
	}
	if p.itemQty(created.ID) != 1 {
		t.Errorf("qty of created item = %d, want 1", p.itemQty(created.ID))
	}
}

// TestItemDurability: 耐久6回の参考書を1セット買い、6回使えて7回目は消えて使えないこと。
func TestItemDurability(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	var bookID int64 // 参考書: durability=6, use_interval=60分
	if err := pool.QueryRow(ctx, `SELECT id FROM content_items WHERE name = '参考書'`).Scan(&bookID); err != nil {
		t.Fatalf("seed lookup: %v", err)
	}
	alice := register(t, srv.URL, "misskey.example", "alice")

	p, code := itemAction(t, srv.URL, "/buy", alice.ID, bookID, "buy-book")
	if code != http.StatusOK {
		t.Fatalf("buy status = %d", code)
	}
	if got := p.itemRemaining(bookID); got != 6 {
		t.Errorf("remaining after buy = %d, want 6", got)
	}
	if got := p.itemQty(bookID); got != 1 {
		t.Errorf("sets after buy = %d, want 1", got)
	}

	// 6回使える(クールタイムはlast_used_atをクリアして回避)。
	for i := 1; i <= 6; i++ {
		if _, err := pool.Exec(ctx,
			`UPDATE player_items SET last_used_at = NULL WHERE player_id = $1 AND item_id = $2`,
			alice.ID, bookID); err != nil {
			t.Fatal(err)
		}
		if _, code := itemAction(t, srv.URL, "/use", alice.ID, bookID, fmt.Sprintf("use-%d", i)); code != http.StatusOK {
			t.Fatalf("use #%d status = %d, want 200", i, code)
		}
	}
	// 残量が尽きて7回目は所持していない(422)。
	if _, code := itemAction(t, srv.URL, "/use", alice.ID, bookID, "use-7"); code != http.StatusUnprocessableEntity {
		t.Errorf("7th use status = %d, want 422 (durability exhausted)", code)
	}
}

// TestPurchaseLimit: 同一アイテムは最大5セットまで、超過は422。
func TestPurchaseLimit(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	var drinkID int64 // 栄養ドリンク: durability=1, max_sets=5, stock=20
	if err := pool.QueryRow(ctx, `SELECT id FROM content_items WHERE name = '栄養ドリンク'`).Scan(&drinkID); err != nil {
		t.Fatalf("seed lookup: %v", err)
	}
	alice := register(t, srv.URL, "misskey.example", "alice")

	// 5セット一括はOK。
	p, code := buySets(t, srv.URL, alice.ID, drinkID, 5, "buy5")
	if code != http.StatusOK {
		t.Fatalf("buy 5 sets status = %d", code)
	}
	if p.itemQty(drinkID) != 5 {
		t.Errorf("sets after buy5 = %d, want 5", p.itemQty(drinkID))
	}
	// 6セット目は所持上限超過で422。
	if _, code := buySets(t, srv.URL, alice.ID, drinkID, 1, "buy6"); code != http.StatusUnprocessableEntity {
		t.Errorf("6th set status = %d, want 422", code)
	}
	// 一度に6セットも上限超過で422。
	bob := register(t, srv.URL, "misskey.example", "bob")
	if _, code := buySets(t, srv.URL, bob.ID, drinkID, 6, "buy6b"); code != http.StatusUnprocessableEntity {
		t.Errorf("buy 6 at once status = %d, want 422", code)
	}
}

// TestStockSoldOut: 在庫が尽きると購入不可(422)。
func TestStockSoldOut(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	var drinkID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM content_items WHERE name = '栄養ドリンク'`).Scan(&drinkID); err != nil {
		t.Fatalf("seed lookup: %v", err)
	}
	alice := register(t, srv.URL, "misskey.example", "alice")

	// 1セット購入で当日在庫行がlazy生成される。
	if _, code := itemAction(t, srv.URL, "/buy", alice.ID, drinkID, "b1"); code != http.StatusOK {
		t.Fatalf("buy status = %d", code)
	}
	// 在庫を0にして売り切れを再現。
	if _, err := pool.Exec(ctx, `UPDATE shop_daily_stock SET remaining = 0 WHERE item_id = $1`, drinkID); err != nil {
		t.Fatal(err)
	}
	if _, code := itemAction(t, srv.URL, "/buy", alice.ID, drinkID, "b2"); code != http.StatusUnprocessableEntity {
		t.Errorf("sold-out buy status = %d, want 422", code)
	}
}

// TestDayDurabilityItem: 日単位耐久は使用で減らず、日次デクリメントで失効する。
func TestDayDurabilityItem(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	var passID int64 // フィットネス会員証: durability=7, unit=day
	if err := pool.QueryRow(ctx, `SELECT id FROM content_items WHERE name = 'フィットネス会員証'`).Scan(&passID); err != nil {
		t.Fatalf("seed lookup: %v", err)
	}
	alice := register(t, srv.URL, "misskey.example", "alice")

	p, code := itemAction(t, srv.URL, "/buy", alice.ID, passID, "buy-pass")
	if code != http.StatusOK {
		t.Fatalf("buy status = %d", code)
	}
	if got := p.itemRemaining(passID); got != 7 {
		t.Errorf("remaining after buy = %d, want 7", got)
	}

	// 使用しても残日数は減らない(日単位)。
	p2, code := itemAction(t, srv.URL, "/use", alice.ID, passID, "use-pass")
	if code != http.StatusOK {
		t.Fatalf("use status = %d", code)
	}
	if got := p2.itemRemaining(passID); got != 7 {
		t.Errorf("remaining after use = %d, want 7 (day unit does not consume)", got)
	}

	// 日次デクリメントを7回で失効し持ち物から消える。
	for i := 0; i < 7; i++ {
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return worker.DecayDayItems(ctx, tx)
		}); err != nil {
			t.Fatalf("decay day items: %v", err)
		}
	}
	var cnt int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM player_items WHERE player_id = $1 AND item_id = $2`,
		alice.ID, passID).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Errorf("expired day item still held: count = %d, want 0", cnt)
	}
}

// TestBodyMetrics covers Phase C: initial height/weight from the server RNG,
// server-authoritative BMI/body-type derivation, and weight gain from eating.
func TestBodyMetrics(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	alice := register(t, srv.URL, "misskey.example", "alice")

	// 初期身長/体重はサーバRNGで女性テーブルの範囲に収まる。
	if alice.Status.HeightCm < 150 || alice.Status.HeightCm > 174 {
		t.Errorf("height_cm = %d, want 150..174", alice.Status.HeightCm)
	}
	if alice.Status.WeightG < 48000 || alice.Status.WeightG > 67000 {
		t.Errorf("weight_g = %d, want 48000..67000", alice.Status.WeightG)
	}

	// BMIはサーバ側でfloor算出。手計算と一致すること。
	h := float64(alice.Status.HeightCm) / 100.0
	wantBMI := int(float64(alice.Status.WeightG) / 1000.0 / (h * h))
	if alice.Status.BMI != wantBMI {
		t.Errorf("bmi = %d, want %d", alice.Status.BMI, wantBMI)
	}
	if alice.Status.BodyType == "" {
		t.Error("body_type is empty")
	}

	// カレー(calorie_g=1000)を食べると体重がちょうど1000g増える。
	resp, err := http.Get(srv.URL + "/api/v1/facilities/syokudou/menu")
	if err != nil {
		t.Fatal(err)
	}
	var menu []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	json.NewDecoder(resp.Body).Decode(&menu)
	resp.Body.Close()
	var curryID int64
	for _, m := range menu {
		if m.Name == "カレー" {
			curryID = m.ID
		}
	}
	if curryID == 0 {
		t.Fatal("カレー not in menu")
	}

	if _, err := pool.Exec(ctx, `UPDATE player_status SET satiety = 30 WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	before := alice.Status.WeightG
	body, _ := json.Marshal(map[string]any{"food_id": curryID, "idempotency_key": "body-eat-1"})
	eatResp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/eat",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if eatResp.StatusCode != http.StatusOK {
		t.Fatalf("eat status = %d", eatResp.StatusCode)
	}
	var after playerResp
	json.NewDecoder(eatResp.Body).Decode(&after)
	eatResp.Body.Close()

	if after.Status.WeightG != before+1000 {
		t.Errorf("weight after curry = %d, want %d (+1000 calorie_g)", after.Status.WeightG, before+1000)
	}
	// 体重変化に伴いBMI/体型も再算出される。
	h2 := float64(after.Status.HeightCm) / 100.0
	wantBMI2 := int(float64(after.Status.WeightG) / 1000.0 / (h2 * h2))
	if after.Status.BMI != wantBMI2 {
		t.Errorf("bmi after eat = %d, want %d", after.Status.BMI, wantBMI2)
	}
}

// TestHospitalTreat covers Phase D: the hospital charges the server-derived fee
// for the current disease and resets the disease index to the healthy baseline.
func TestHospitalTreat(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	alice := register(t, srv.URL, "misskey.example", "alice")

	// disease_index=-12 -> 病名「風邪」、治療費28000。
	if _, err := pool.Exec(ctx,
		`UPDATE player_status SET disease_index = -12 WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	before := register(t, srv.URL, "misskey.example", "alice") // 既存を引き直し(お金確認用)
	if before.Status.DiseaseName != "風邪" {
		t.Fatalf("disease_name = %q, want 風邪", before.Status.DiseaseName)
	}
	if before.Status.Condition != "風邪" {
		t.Errorf("condition = %q, want 風邪(病名で上書き)", before.Status.Condition)
	}

	body, _ := json.Marshal(map[string]any{"idempotency_key": "treat-1"})
	resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/hospital/treat",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("treat status = %d", resp.StatusCode)
	}
	var after playerResp
	json.NewDecoder(resp.Body).Decode(&after)
	resp.Body.Close()

	if after.Money != before.Money-28000 {
		t.Errorf("money after treat = %d, want %d (fee 28000)", after.Money, before.Money-28000)
	}
	if after.Status.DiseaseIndex != 50 {
		t.Errorf("disease_index after treat = %d, want 50", after.Status.DiseaseIndex)
	}
	if after.Status.DiseaseName != "" {
		t.Errorf("disease_name after treat = %q, want empty", after.Status.DiseaseName)
	}

	// お金不足なら422。重病(癌 index=-101, fee 88000)に所持金を0近くにして再現。
	if _, err := pool.Exec(ctx,
		`UPDATE player_status SET disease_index = -101 WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	// 所持金を1円に(貯金には移さずledgerで引き出す代わりにテスト用に直接不足を作る)。
	drain := after.Money - 1
	body2, _ := json.Marshal(map[string]any{"amount": drain, "idempotency_key": "drain-1"})
	dep, _ := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/bank/deposit",
		"application/json", bytes.NewReader(body2))
	dep.Body.Close()
	body3, _ := json.Marshal(map[string]any{"idempotency_key": "treat-poor"})
	poor, _ := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/hospital/treat",
		"application/json", bytes.NewReader(body3))
	if poor.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("treat while poor status = %d, want 422", poor.StatusCode)
	}
	poor.Body.Close()
}

// TestDiseaseDrift covers the worker's condition-based disease-index evaluation.
func TestDiseaseDrift(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	alice := register(t, srv.URL, "misskey.example", "alice")

	// 悪いコンディション(sisuu~25)を作る: energy50%/nou0%/標準体型/丁度いい満腹。
	// 病気指数=40、評価時刻を1時間前にしてdueにする。
	if _, err := pool.Exec(ctx, `
		UPDATE player_status SET energy=5, energy_max=10, nou_energy=0, nou_energy_max=10,
		    kenkou=5, satiety=70, height_cm=160, weight_g=56000,
		    disease_index=40, disease_evaled_at = now() - interval '1 hour'
		WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	// 悪い(delta -3) -> 40 - 3 = 37。
	n, err := worker.EvaluateDisease(ctx, pool, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("evaluated players = %d, want 1", n)
	}
	var idx int
	pool.QueryRow(ctx, `SELECT disease_index FROM player_status WHERE player_id=$1`, alice.ID).Scan(&idx)
	if idx != 37 {
		t.Errorf("disease_index after bad-condition eval = %d, want 37 (悪い -3)", idx)
	}

	// 直後は評価時刻が更新されており、続けて評価してもdueにならない。
	n2, _ := worker.EvaluateDisease(ctx, pool, 1)
	if n2 != 0 {
		t.Errorf("second eval players = %d, want 0 (評価時刻が更新済み)", n2)
	}

	// 最高コンディションは+2だが上限50でクリップ。
	if _, err := pool.Exec(ctx, `
		UPDATE player_status SET energy=10, nou_energy=10, satiety=70,
		    disease_index=50, disease_evaled_at = now() - interval '1 hour'
		WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	worker.EvaluateDisease(ctx, pool, 1)
	pool.QueryRow(ctx, `SELECT disease_index FROM player_status WHERE player_id=$1`, alice.ID).Scan(&idx)
	if idx != 50 {
		t.Errorf("disease_index after best-condition eval = %d, want 50 (上限クリップ)", idx)
	}
}

// TestJobEconomy covers Phase E: salary + condition-based experience on work,
// mastery at level 15, and the require_master gate on higher-tier jobs.
func TestJobEconomy(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	alice := register(t, srv.URL, "misskey.example", "alice")
	changeJob(t, srv.URL, alice.ID, "アルバイト", "j-baito")

	// 1回働く: 給料+1040(給料1000+労働ボーナス40)、勤務回数1、経験値が正方向に増える。
	p, code := doWork(t, srv.URL, alice.ID, "w1")
	if code != http.StatusOK {
		t.Fatalf("work status = %d", code)
	}
	if p.Money != 501040 {
		t.Errorf("money after work = %d, want 501040 (salary 1000 + labor bonus 40)", p.Money)
	}
	if p.Status.JobKaisuu != 1 {
		t.Errorf("job_kaisuu = %d, want 1", p.Status.JobKaisuu)
	}
	if p.Status.JobExp <= 0 {
		t.Errorf("job_exp = %d, want > 0", p.Status.JobExp)
	}

	// マスターしていないので正社員(require_master=アルバイト)には就けない。
	_, code = changeJob(t, srv.URL, alice.ID, "正社員", "j-seishain-early")
	if code != http.StatusUnprocessableEntity {
		t.Errorf("change to 正社員 without master status = %d, want 422", code)
	}

	// 就労条件を最高コンディションにし、経験値をレベル15手前に上げて1回働くとマスター認定。
	if _, err := pool.Exec(ctx, `
		UPDATE player_status SET job_exp = 1499, energy = 10, nou_energy = 10, satiety = 70,
		    height_cm = 160, weight_g = 56000, disease_index = 50
		WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	p, code = doWork(t, srv.URL, alice.ID, "w-master")
	if code != http.StatusOK {
		t.Fatalf("master work status = %d", code)
	}
	if p.Status.JobLevel < 15 {
		t.Fatalf("job_level = %d, want >= 15", p.Status.JobLevel)
	}
	mastered := false
	for _, m := range p.Status.MasteredJobs {
		if m == "アルバイト" {
			mastered = true
		}
	}
	if !mastered {
		t.Errorf("mastered_jobs = %v, want to contain アルバイト", p.Status.MasteredJobs)
	}

	// マスター後は正社員に就ける(転職で経験値/回数リセット)。
	p, code = changeJob(t, srv.URL, alice.ID, "正社員", "j-seishain")
	if code != http.StatusOK {
		t.Fatalf("change to 正社員 after master status = %d", code)
	}
	if p.Status.Job != "正社員" {
		t.Errorf("job = %q, want 正社員", p.Status.Job)
	}
	if p.Status.JobExp != 0 || p.Status.JobKaisuu != 0 {
		t.Errorf("after job change exp/kaisuu = %d/%d, want 0/0", p.Status.JobExp, p.Status.JobKaisuu)
	}
}

// TestWorkCannotWhenSick covers that severe illness blocks working.
func TestWorkCannotWhenSick(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	alice := register(t, srv.URL, "misskey.example", "alice")
	changeJob(t, srv.URL, alice.ID, "アルバイト", "j-baito")

	// 結核(disease_index=-50)は重病で就労不可。
	if _, err := pool.Exec(ctx,
		`UPDATE player_status SET disease_index = -50 WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	_, code := doWork(t, srv.URL, alice.ID, "w-sick")
	if code != http.StatusUnprocessableEntity {
		t.Errorf("work while gravely ill status = %d, want 422", code)
	}
}

// TestDerivedPowerMax covers Phase F (design 17.9): energy_max / nou_energy_max
// are derived from the player's parameters (legacy basic.cgi formula) rather than
// a fixed cap, and are recomputed when parameters change.
func TestDerivedPowerMax(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	alice := register(t, srv.URL, "misskey.example", "alice")
	// 初期パラメータ(各5)由来: energy_max = floor(5/12+5/4+5/4+5/8*3)+1 = 6、nou も 6。
	var emax, nmax int
	if err := pool.QueryRow(ctx,
		`SELECT energy_max, nou_energy_max FROM player_status WHERE player_id = $1`, alice.ID).
		Scan(&emax, &nmax); err != nil {
		t.Fatal(err)
	}
	if emax != 6 || nmax != 6 {
		t.Errorf("initial derived max = %d/%d, want 6/6", emax, nmax)
	}

	// 身体パラメータを鍛えた状態にし、パラメータ変化(アイテム使用)で上限が再計算されること。
	if _, err := pool.Exec(ctx,
		`UPDATE player_status SET tairyoku = 40, kenkou = 40, power = 40 WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	var drinkID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM content_items WHERE name = '栄養ドリンク'`).Scan(&drinkID); err != nil {
		t.Fatal(err)
	}
	itemAction(t, srv.URL, "/buy", alice.ID, drinkID, "pm-buy")
	itemAction(t, srv.URL, "/use", alice.ID, drinkID, "pm-use") // energy(パラメータ)変化 -> RefreshPowerMax

	if err := pool.QueryRow(ctx,
		`SELECT energy_max FROM player_status WHERE player_id = $1`, alice.ID).Scan(&emax); err != nil {
		t.Fatal(err)
	}
	// floor(5/12 + 40/4 + 40/4 + 5/8 + 40/8 + 5/8 + 5/8) + 1 = floor(27.29) + 1 = 28。
	if emax != 28 {
		t.Errorf("derived energy_max after training = %d, want 28", emax)
	}
}

// TestOnsen covers the reworked hot spring: bathing accelerates the player's
// natural power recovery by the bath's multiplier over elapsed time (not a fixed
// amount), charges the fee, and rejects bathing while already full.
func TestOnsen(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	alice := register(t, srv.URL, "misskey.example", "alice")

	var bathID int64
	var price int64
	if err := pool.QueryRow(ctx,
		`SELECT id, price FROM content_items WHERE name = '普通風呂' AND facility = 'onsen'`).
		Scan(&bathID, &price); err != nil {
		t.Fatal(err)
	}

	// パワーを1に下げ、回復基準時刻を5分前にして経過時間を作る(倍率10で一気にMAX回復)。
	if _, err := pool.Exec(ctx, `
		UPDATE player_status SET energy = 1, nou_energy = 1,
		    energy_recovered_at = now() - interval '5 minutes',
		    nou_recovered_at = now() - interval '5 minutes'
		WHERE player_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"bath_id": bathID, "idempotency_key": "onsen-1"})
	resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/onsen/bathe",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bathe status = %d", resp.StatusCode)
	}
	var after playerResp
	json.NewDecoder(resp.Body).Decode(&after)
	resp.Body.Close()

	// 倍率10 × 5分ぶんの加速回復で上限(6)までフル回復し、料金が引かれる。
	if after.Status.Energy != after.Status.EnergyMax {
		t.Errorf("energy after onsen = %d, want max %d", after.Status.Energy, after.Status.EnergyMax)
	}
	if after.Money != 500000-price {
		t.Errorf("money after onsen = %d, want %d", after.Money, 500000-price)
	}

	// 満タン状態でもう一度入ると無駄遣い防止で422。
	body2, _ := json.Marshal(map[string]any{"bath_id": bathID, "idempotency_key": "onsen-full"})
	full, _ := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/onsen/bathe",
		"application/json", bytes.NewReader(body2))
	if full.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("bathe while full status = %d, want 422", full.StatusCode)
	}
	full.Body.Close()
}

// TestDailyShop covers the daily shop rotation: the department store shows only a
// deterministic per-day subset of the item pool, and buying an item outside
// today's rotation is rejected (while an in-rotation item is not).
func TestDailyShop(t *testing.T) {
	_, pool := setup(t)
	ctx := context.Background()

	led := ledger.New(pool)
	rnd := rng.New(1)
	// デパートは1日3件だけ品揃えするサービスで検証する。
	const daily = 3
	st := newTestSettings(t, ctx, pool, settings.Game{
		InitialMoney:       500000,
		EnergyRecoverySec:  60,
		NouRecoverySec:     60,
		WorkIntervalMin:    0,
		DepartDailyCount:   daily,
		SyokudouDailyCount: daily,
	})
	psvc := player.New(pool, led, rnd, st)
	csvc := content.New(pool, time.UTC, 5, st)
	asvc := action.New(pool, led, psvc, rnd, time.UTC, 5, st)

	items, err := csvc.ListShopItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 || len(items) > daily {
		t.Fatalf("daily depart count = %d, want 1..%d", len(items), daily)
	}
	// 決定論: 再取得しても同じ集合。
	items2, _ := csvc.ListShopItems(ctx)
	if len(items2) != len(items) {
		t.Errorf("non-deterministic daily shop: %d vs %d", len(items), len(items2))
	}

	// 本日の品揃えに含まれないデパート商品IDを1つ特定する。
	daykey := gametime.DateKey(time.Now(), time.UTC, 5)
	var outID int64
	pool.QueryRow(ctx,
		`SELECT id FROM content_items
		 WHERE enabled AND facility = '' AND id NOT IN (SELECT id FROM daily_shop_ids('', $1, $2))
		 LIMIT 1`, daykey, daily).Scan(&outID)

	alice, err := psvc.Register(ctx, "misskey.example", "shopper", "shopper")
	if err != nil {
		t.Fatal(err)
	}

	// 本日の品揃え内の商品は「品揃えにない」で弾かれない(価格不足等は別問題)。
	if _, err := asvc.DoBuy(ctx, alice.ID, "", items[0].ID, 1, "buy-in"); errors.Is(err, action.ErrItemNotFound) {
		t.Errorf("in-rotation item rejected as not found")
	}
	// 本日の品揃え外の商品は購入不可(ErrItemNotFound)。
	if outID != 0 {
		if _, err := asvc.DoBuy(ctx, alice.ID, "", outID, 1, "buy-out"); !errors.Is(err, action.ErrItemNotFound) {
			t.Errorf("out-of-rotation buy err = %v, want ErrItemNotFound", err)
		}
	} else {
		t.Log("プールが小さく品揃え外の商品が無いためbuy-out検証はスキップ")
	}
}

func adminPut(t *testing.T, base, path string, actingID int64, body any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if actingID > 0 {
		req.Header.Set("X-Acting-Player-Id", strconv.FormatInt(actingID, 10))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// TestTownMap covers the town map API: GET is public, PUT is admin-only, updates
// persist and are validated (grid bounds / one-facility-per-cell).
func TestTownMap(t *testing.T) {
	srv, _ := setup(t)

	admin := register(t, srv.URL, "misskey.example", "root") // 最初=admin
	user := register(t, srv.URL, "misskey.example", "alice") // 一般

	// GETは公開。既定の施設配置(12件)が返る。
	resp, err := http.Get(srv.URL + "/api/v1/townmap")
	if err != nil {
		t.Fatal(err)
	}
	var got []townmap.Facility
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(got) != len(townmap.Default()) {
		t.Fatalf("default map size = %d, want %d", len(got), len(townmap.Default()))
	}

	// 認可: ヘッダ無し=401、非admin=403。
	if code, _ := adminPut(t, srv.URL, "/api/v1/admin/townmap", 0, got); code != http.StatusUnauthorized {
		t.Errorf("no-header PUT status = %d, want 401", code)
	}
	if code, _ := adminPut(t, srv.URL, "/api/v1/admin/townmap", user.ID, got); code != http.StatusForbidden {
		t.Errorf("non-admin PUT status = %d, want 403", code)
	}

	// adminが銀行を(1,0)へ移動して保存。
	updated := make([]townmap.Facility, len(got))
	copy(updated, got)
	for i := range updated {
		if updated[i].Key == "bank" {
			updated[i].Col = 1
			updated[i].Row = 0
		}
	}
	if code, body := adminPut(t, srv.URL, "/api/v1/admin/townmap", admin.ID, updated); code != http.StatusOK {
		t.Fatalf("admin PUT status = %d, body = %s", code, body)
	}

	// 永続化を確認: 再GETで銀行が(1,0)。
	resp2, err := http.Get(srv.URL + "/api/v1/townmap")
	if err != nil {
		t.Fatal(err)
	}
	var after []townmap.Facility
	if err := json.NewDecoder(resp2.Body).Decode(&after); err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	var bankOK bool
	for _, f := range after {
		if f.Key == "bank" {
			if f.Col != 1 || f.Row != 0 {
				t.Errorf("bank moved to (%d,%d), want (1,0)", f.Col, f.Row)
			}
			bankOK = true
		}
	}
	if !bankOK {
		t.Error("bank facility missing after update")
	}

	// 検証: 座標重複は400で拒否。
	dup := []townmap.Facility{
		{Key: "bank", Img: "bank", Alt: "銀行", Col: 5, Row: 5, Ready: true},
		{Key: "gym", Img: "gym", Alt: "ジム", Col: 5, Row: 5, Ready: true},
	}
	if code, _ := adminPut(t, srv.URL, "/api/v1/admin/townmap", admin.ID, dup); code != http.StatusBadRequest {
		t.Errorf("duplicate-cell PUT status = %d, want 400", code)
	}
	// 検証: 範囲外の列も400。
	oob := []townmap.Facility{{Key: "bank", Img: "bank", Col: 99, Row: 0}}
	if code, _ := adminPut(t, srv.URL, "/api/v1/admin/townmap", admin.ID, oob); code != http.StatusBadRequest {
		t.Errorf("out-of-range PUT status = %d, want 400", code)
	}
}

// TestSchool covers the school: attending a course raises brain params and
// consumes money + brain power, is limited to once per game day, and rejects
// insufficient brain power.
func TestSchool(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	// 学校メニューから日本語講座(国語+10 / 頭脳-7 / 14000円)を取得。
	resp, err := http.Get(srv.URL + "/api/v1/facilities/school/menu")
	if err != nil {
		t.Fatal(err)
	}
	var menu []struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Price int64  `json:"price"`
	}
	json.NewDecoder(resp.Body).Decode(&menu)
	resp.Body.Close()
	var courseID, coursePrice int64
	for _, m := range menu {
		if m.Name == "日本語講座" {
			courseID, coursePrice = m.ID, m.Price
		}
	}
	if courseID == 0 {
		t.Fatal("日本語講座 not in school menu")
	}

	alice := register(t, srv.URL, "misskey.example", "alice")
	// 頭脳科目と頭脳パワーを既知値に固定。nou_energy_maxも上げないと効果適用時に
	// 上限クランプされるため合わせて設定する。
	if _, err := pool.Exec(ctx,
		`UPDATE player_status SET kokugo=50,suugaku=50,rika=50,syakai=50,eigo=50,ongaku=50,bijutsu=50,nou_energy=50,nou_energy_max=100 WHERE player_id=$1`,
		alice.ID); err != nil {
		t.Fatal(err)
	}

	// 受講: 国語 50→60, 頭脳 50→43, 代金 -14000。
	body, _ := json.Marshal(map[string]any{"course_id": courseID, "idempotency_key": "sch-1"})
	resp, _ = http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/school/attend",
		"application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("attend status = %d", resp.StatusCode)
	}
	var p playerResp
	json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()
	if p.Money != 500000-coursePrice {
		t.Errorf("money = %d, want %d", p.Money, 500000-coursePrice)
	}
	var kokugo, nou int
	pool.QueryRow(ctx, `SELECT kokugo, nou_energy FROM player_status WHERE player_id=$1`, alice.ID).Scan(&kokugo, &nou)
	if kokugo != 60 {
		t.Errorf("kokugo = %d, want 60", kokugo)
	}
	if nou != 43 {
		t.Errorf("nou_energy = %d, want 43", nou)
	}

	// 同日2回目は 422(1日1回)。
	body, _ = json.Marshal(map[string]any{"course_id": courseID, "idempotency_key": "sch-2"})
	resp, _ = http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/school/attend",
		"application/json", bytes.NewReader(body))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("second attend status = %d, want 422", resp.StatusCode)
	}

	// 頭脳パワー不足は 422。
	bob := register(t, srv.URL, "misskey.example", "bob")
	if _, err := pool.Exec(ctx, `UPDATE player_status SET nou_energy=3 WHERE player_id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"course_id": courseID, "idempotency_key": "sch-3"})
	resp, _ = http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(bob.ID, 10)+"/school/attend",
		"application/json", bytes.NewReader(body))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("insufficient-brain status = %d, want 422", resp.StatusCode)
	}
}

// TestKyushitu covers the classroom: it uses the generic facility path with an
// all-parameter effect and a per-course cooldown. Guards the seed data + routing.
func TestKyushitu(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()

	// フラフラダンス(全パラメータ+1 / 身体-2 頭脳-2 / 10000円 / 間隔2分)を取得。
	resp, err := http.Get(srv.URL + "/api/v1/facilities/kyushitu/menu")
	if err != nil {
		t.Fatal(err)
	}
	var menu []struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Price int64  `json:"price"`
	}
	json.NewDecoder(resp.Body).Decode(&menu)
	resp.Body.Close()
	var courseID, coursePrice int64
	for _, m := range menu {
		if m.Name == "フラフラダンス" {
			courseID, coursePrice = m.ID, m.Price
		}
	}
	if courseID == 0 {
		t.Fatal("フラフラダンス not in kyushitu menu")
	}

	alice := register(t, srv.URL, "misskey.example", "alice")
	// 身体/頭脳パワーを既知の高値に固定(消費と上限クランプを回避)。
	if _, err := pool.Exec(ctx,
		`UPDATE player_status SET energy=50,energy_max=100,nou_energy=50,nou_energy_max=100,kokugo=10 WHERE player_id=$1`,
		alice.ID); err != nil {
		t.Fatal(err)
	}

	// 受講: 国語 10→11, 代金 -10000。
	body, _ := json.Marshal(map[string]any{"menu_id": courseID, "idempotency_key": "kyu-1"})
	resp, _ = http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/facilities/kyushitu/use",
		"application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("attend status = %d", resp.StatusCode)
	}
	var p playerResp
	json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()
	if p.Money != 500000-coursePrice {
		t.Errorf("money = %d, want %d", p.Money, 500000-coursePrice)
	}
	var kokugo int
	pool.QueryRow(ctx, `SELECT kokugo FROM player_status WHERE player_id=$1`, alice.ID).Scan(&kokugo)
	if kokugo != 11 {
		t.Errorf("kokugo = %d, want 11", kokugo)
	}

	// クールタイム内の再受講は 422。
	body, _ = json.Marshal(map[string]any{"menu_id": courseID, "idempotency_key": "kyu-2"})
	resp, _ = http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/facilities/kyushitu/use",
		"application/json", bytes.NewReader(body))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("second attend status = %d, want 422", resp.StatusCode)
	}
}

func stockPost(t *testing.T, base string, id int64, path string, body map[string]any) playerResp {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(id, 10)+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s status=%d body=%s", path, resp.StatusCode, data)
	}
	var p playerResp
	json.NewDecoder(resp.Body).Decode(&p)
	return p
}

func stockPostCode(t *testing.T, base string, id int64, path string, body map[string]any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(id, 10)+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestStock covers stock trading: buy/sell/settle move money via the ledger,
// the 200-share cap and insufficient-funds are rejected, and the ledger stays
// zero-sum. Prices are static here (no worker), so round-trips return to start.
func TestStock(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	led := ledger.New(pool)
	alice := register(t, srv.URL, "misskey.example", "alice") // 500000円

	// 公開の株価: 5銘柄すべて25000。
	resp, err := http.Get(srv.URL + "/api/v1/stocks")
	if err != nil {
		t.Fatal(err)
	}
	var sd struct {
		Prices []struct {
			Symbol string `json:"symbol"`
			Price  int64  `json:"price"`
		} `json:"prices"`
	}
	json.NewDecoder(resp.Body).Decode(&sd)
	resp.Body.Close()
	if len(sd.Prices) != 5 {
		t.Fatalf("prices = %d, want 5", len(sd.Prices))
	}
	for _, p := range sd.Prices {
		if p.Price != 25000 {
			t.Errorf("%s price = %d, want 25000", p.Symbol, p.Price)
		}
	}

	// A株を10株購入(250000円)。
	p := stockPost(t, srv.URL, alice.ID, "/stocks/buy", map[string]any{"symbol": "A", "quantity": 10, "idempotency_key": "sb1"})
	if p.Money != 250000 {
		t.Errorf("money after buy = %d, want 250000", p.Money)
	}

	// 保有: A=10株。
	resp, _ = http.Get(srv.URL + "/api/v1/players/" + strconv.FormatInt(alice.ID, 10) + "/stocks")
	var hd struct {
		Holdings []struct {
			Symbol string `json:"symbol"`
			Shares int    `json:"shares"`
		} `json:"holdings"`
	}
	json.NewDecoder(resp.Body).Decode(&hd)
	resp.Body.Close()
	var aShares int
	for _, h := range hd.Holdings {
		if h.Symbol == "A" {
			aShares = h.Shares
		}
	}
	if aShares != 10 {
		t.Errorf("A shares = %d, want 10", aShares)
	}

	// A株を4株売却(100000円)。250000+100000=350000。
	p = stockPost(t, srv.URL, alice.ID, "/stocks/sell", map[string]any{"symbol": "A", "quantity": 4, "idempotency_key": "ss1"})
	if p.Money != 350000 {
		t.Errorf("money after sell = %d, want 350000", p.Money)
	}

	// 保有上限: 残6株 +200 = 206 > 200 → 422。
	if code := stockPostCode(t, srv.URL, alice.ID, "/stocks/buy", map[string]any{"symbol": "A", "quantity": 200, "idempotency_key": "sb2"}); code != http.StatusUnprocessableEntity {
		t.Errorf("over-cap buy code = %d, want 422", code)
	}
	// 持ち金不足: B株100株=250万 > 350000 → 422。
	if code := stockPostCode(t, srv.URL, alice.ID, "/stocks/buy", map[string]any{"symbol": "B", "quantity": 100, "idempotency_key": "sb3"}); code != http.StatusUnprocessableEntity {
		t.Errorf("insufficient buy code = %d, want 422", code)
	}
	// 不正銘柄 → 400。
	if code := stockPostCode(t, srv.URL, alice.ID, "/stocks/buy", map[string]any{"symbol": "Z", "quantity": 1, "idempotency_key": "sb4"}); code != http.StatusBadRequest {
		t.Errorf("bad-symbol buy code = %d, want 400", code)
	}

	// 精算: 残A6株を時価清算(150000円)。350000+150000=500000で元に戻る。
	p = stockPost(t, srv.URL, alice.ID, "/stocks/settle", map[string]any{"idempotency_key": "settle1"})
	if p.Money != 500000 {
		t.Errorf("money after settle = %d, want 500000", p.Money)
	}
	// 精算後は保有ゼロ。
	var held int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM player_stock WHERE player_id=$1`, alice.ID).Scan(&held)
	if held != 0 {
		t.Errorf("holdings rows after settle = %d, want 0", held)
	}
	// 台帳ゼロ和を維持。
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

// TestKeiba covers horse racing: a race is generated, bets deduct/pay through the
// ledger (money reconciles to invested/payout), and validation rejects stale
// races, >2 horses, and >200 tickets. The ledger stays zero-sum.
func TestKeiba(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	led := ledger.New(pool)
	alice := register(t, srv.URL, "misskey.example", "alice") // 500000円

	// レース取得。
	resp, err := http.Get(srv.URL + "/api/v1/players/" + strconv.FormatInt(alice.ID, 10) + "/keiba")
	if err != nil {
		t.Fatal(err)
	}
	var rd struct {
		RaceID int64 `json:"race_id"`
		Lineup []struct {
			Name string `json:"name"`
			Odds int    `json:"odds"`
		} `json:"lineup"`
		Ranking []any `json:"ranking"`
	}
	json.NewDecoder(resp.Body).Decode(&rd)
	resp.Body.Close()
	if len(rd.Lineup) != 6 {
		t.Fatalf("lineup = %d, want 6", len(rd.Lineup))
	}

	betPost := func(raceID int64, bets []int, key string) (int, []byte) {
		b, _ := json.Marshal(map[string]any{"race_id": raceID, "bets": bets, "idempotency_key": key})
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/keiba/bet", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, data
	}

	// 1頭に1枚(500円)賭ける。所持金 = 500000 - invested + payout。
	code, body := betPost(rd.RaceID, []int{1, 0, 0, 0, 0, 0}, "kb1")
	if code != http.StatusOK {
		t.Fatalf("bet status = %d, body = %s", code, body)
	}
	var br struct {
		Player struct {
			Money int64 `json:"money"`
		} `json:"player"`
		Result struct {
			WinnerIndex int   `json:"winner_index"`
			Payout      int64 `json:"payout"`
			Invested    int64 `json:"invested"`
		} `json:"result"`
	}
	json.Unmarshal(body, &br)
	if br.Result.Invested != 500 {
		t.Errorf("invested = %d, want 500", br.Result.Invested)
	}
	if br.Result.WinnerIndex < 0 || br.Result.WinnerIndex >= 6 {
		t.Errorf("winner_index = %d out of range", br.Result.WinnerIndex)
	}
	if br.Player.Money != 500000-br.Result.Invested+br.Result.Payout {
		t.Errorf("money = %d, want %d", br.Player.Money, 500000-br.Result.Invested+br.Result.Payout)
	}

	// 検証: 古いrace_id → 422。
	if code, _ := betPost(rd.RaceID+9999, []int{1, 0, 0, 0, 0, 0}, "kb2"); code != http.StatusUnprocessableEntity {
		t.Errorf("stale race code = %d, want 422", code)
	}
	// 検証: 3頭賭け → 422。
	if code, _ := betPost(rd.RaceID, []int{1, 1, 1, 0, 0, 0}, "kb3"); code != http.StatusUnprocessableEntity {
		t.Errorf("3-horse bet code = %d, want 422", code)
	}
	// 検証: 201枚 → 422。
	if code, _ := betPost(rd.RaceID, []int{201, 0, 0, 0, 0, 0}, "kb4"); code != http.StatusUnprocessableEntity {
		t.Errorf("over-200 bet code = %d, want 422", code)
	}
	// 検証: bets長さ不正 → 400。
	if code, _ := betPost(rd.RaceID, []int{1, 0, 0}, "kb5"); code != http.StatusBadRequest {
		t.Errorf("bad-length bet code = %d, want 400", code)
	}

	// 台帳ゼロ和を維持。
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

// TestMail covers messaging: send delivers to both boxes, unread notification
// works, save/delete mutate the owner's box, and self-send/empty are rejected.
func TestMail(t *testing.T) {
	srv, _ := setup(t)
	alice := register(t, srv.URL, "misskey.example", "alice")
	bob := register(t, srv.URL, "misskey.example", "bob")

	post := func(id int64, path string, body map[string]any) int {
		b, _ := json.Marshal(body)
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(id, 10)+path, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	getMail := func(id int64) mail.Mailbox {
		resp, err := http.Get(srv.URL + "/api/v1/players/" + strconv.FormatInt(id, 10) + "/mail")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var mb mail.Mailbox
		json.NewDecoder(resp.Body).Decode(&mb)
		return mb
	}

	// alice→bob 送信。
	if code := post(alice.ID, "/mail/send", map[string]any{"recipient_id": bob.ID, "body": "やあボブ"}); code != http.StatusOK {
		t.Fatalf("send status = %d", code)
	}
	// 自分宛は 422。
	if code := post(alice.ID, "/mail/send", map[string]any{"recipient_id": alice.ID, "body": "x"}); code != http.StatusUnprocessableEntity {
		t.Errorf("self-send status = %d, want 422", code)
	}
	// 空本文は 422。
	if code := post(alice.ID, "/mail/send", map[string]any{"recipient_id": bob.ID, "body": "  "}); code != http.StatusUnprocessableEntity {
		t.Errorf("empty-body status = %d, want 422", code)
	}

	// bobの未読は1(mailを開く前)。
	resp, _ := http.Get(srv.URL + "/api/v1/players/" + strconv.FormatInt(bob.ID, 10) + "/mail/unread")
	var ur struct {
		Unread int `json:"unread"`
	}
	json.NewDecoder(resp.Body).Decode(&ur)
	resp.Body.Close()
	if ur.Unread != 1 {
		t.Errorf("bob unread = %d, want 1", ur.Unread)
	}

	// bobの受信箱: 1件、差出人alice、未読。
	bobMb := getMail(bob.ID)
	if len(bobMb.Received) != 1 || bobMb.Received[0].CounterpartName != "alice" || !bobMb.Received[0].Unread {
		t.Fatalf("bob inbox = %+v", bobMb.Received)
	}
	if bobMb.Unread != 1 {
		t.Errorf("bob mailbox unread = %d, want 1", bobMb.Unread)
	}
	// aliceの送信箱: 1件、宛先bob。
	aliceMb := getMail(alice.ID)
	if len(aliceMb.Sent) != 1 || aliceMb.Sent[0].CounterpartName != "bob" {
		t.Fatalf("alice sent = %+v", aliceMb.Sent)
	}

	// mailを開いた後は未読0(MarkChecked済み)。
	resp, _ = http.Get(srv.URL + "/api/v1/players/" + strconv.FormatInt(bob.ID, 10) + "/mail/unread")
	json.NewDecoder(resp.Body).Decode(&ur)
	resp.Body.Close()
	if ur.Unread != 0 {
		t.Errorf("bob unread after open = %d, want 0", ur.Unread)
	}

	// 保存トグル → 削除。
	msgID := bobMb.Received[0].ID
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/players/"+strconv.FormatInt(bob.ID, 10)+"/mail/"+strconv.FormatInt(msgID, 10)+"/save", bytes.NewReader([]byte(`{"saved":true}`)))
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("save status = %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/players/"+strconv.FormatInt(bob.ID, 10)+"/mail/"+strconv.FormatInt(msgID, 10), nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("delete status = %d", resp.StatusCode)
	}
	if len(getMail(bob.ID).Received) != 0 {
		t.Errorf("bob inbox not empty after delete")
	}
}

// TestGreeting covers the town chat: normal posts earn money (money reconciles to
// the reported reward), 宣伝 charges 20000, 管理人 is admin-only, NG words are
// fined and masked, over-length is rejected, and admin can delete. Ledger stays
// zero-sum.
func TestGreeting(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	led := ledger.New(pool)
	admin := register(t, srv.URL, "misskey.example", "root") // 最初=admin
	alice := register(t, srv.URL, "misskey.example", "alice")

	type greetResp struct {
		Player struct {
			Money int64 `json:"money"`
		} `json:"player"`
		Result struct {
			Reward  int64 `json:"reward"`
			Fine    bool  `json:"fine"`
			Jackpot bool  `json:"jackpot"`
		} `json:"result"`
	}
	post := func(id int64, body map[string]any) (int, greetResp) {
		b, _ := json.Marshal(body)
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(id, 10)+"/greetings", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var gr greetResp
		json.NewDecoder(resp.Body).Decode(&gr)
		return resp.StatusCode, gr
	}

	// 通常投稿: 報酬 > 0、所持金 = 500000 + reward。
	code, gr := post(alice.ID, map[string]any{"category": "あいさつ", "body": "こんにちは", "color": "#333333", "idempotency_key": "g1"})
	if code != http.StatusOK {
		t.Fatalf("post status = %d", code)
	}
	if gr.Result.Reward <= 0 {
		t.Errorf("reward = %d, want > 0", gr.Result.Reward)
	}
	if gr.Player.Money != 500000+gr.Result.Reward {
		t.Errorf("money = %d, want %d", gr.Player.Money, 500000+gr.Result.Reward)
	}

	// 宣伝: -20000円。
	_, gr = post(alice.ID, map[string]any{"category": "宣伝", "body": "私の店へどうぞ", "color": "#0000ff", "idempotency_key": "g2"})
	if gr.Result.Reward != -20000 {
		t.Errorf("ad reward = %d, want -20000", gr.Result.Reward)
	}

	// 管理人枠は非adminだと422。
	if code, _ := post(alice.ID, map[string]any{"category": "管理人", "body": "お知らせ", "idempotency_key": "g3"}); code != http.StatusUnprocessableEntity {
		t.Errorf("non-admin 管理人 status = %d, want 422", code)
	}
	// adminはOK。
	if code, _ := post(admin.ID, map[string]any{"category": "管理人", "body": "お知らせ", "idempotency_key": "g4"}); code != http.StatusOK {
		t.Errorf("admin 管理人 status = %d, want 200", code)
	}

	// NGワードは罰金(fine=true)。
	_, gr = post(alice.ID, map[string]any{"category": "雑談", "body": "このぼけ", "idempotency_key": "g5"})
	if !gr.Result.Fine {
		t.Errorf("NG word not fined")
	}

	// 60字超は400。
	long := ""
	for i := 0; i < 61; i++ {
		long += "あ"
	}
	if code, _ := post(alice.ID, map[string]any{"category": "雑談", "body": long, "idempotency_key": "g6"}); code != http.StatusBadRequest {
		t.Errorf("over-length status = %d, want 400", code)
	}

	// 一覧取得。
	resp, _ := http.Get(srv.URL + "/api/v1/greetings")
	var list []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) < 4 {
		t.Errorf("greetings = %d, want >= 4", len(list))
	}
	// NG本文がマスクされている。
	var ngMasked bool
	for _, g := range list {
		if g.Body == "このNG" {
			ngMasked = true
		}
	}
	if !ngMasked {
		t.Errorf("NG word not masked in stored body")
	}

	// admin削除。
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/admin/greetings/"+strconv.FormatInt(list[0].ID, 10), nil)
	req.Header.Set("X-Acting-Player-Id", strconv.FormatInt(admin.ID, 10))
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin delete status = %d, want 200", resp.StatusCode)
	}

	// 台帳ゼロ和。
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

// TestAttendance covers the visitor book: check-in is once per game day, the
// board marks present/absent/blank correctly relative to registration, and the
// ranking computes the attendance rate.
func TestAttendance(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	alice := register(t, srv.URL, "misskey.example", "alice")

	checkin := func() bool {
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/attendance/checkin", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var r struct {
			Recorded bool `json:"recorded"`
		}
		json.NewDecoder(resp.Body).Decode(&r)
		return r.Recorded
	}

	// 当日初回=記帳、2回目=記帳なし。
	if !checkin() {
		t.Errorf("first checkin: recorded = false, want true")
	}
	if checkin() {
		t.Errorf("second checkin: recorded = true, want false")
	}

	// 登録日を5ゲーム日前へ遡らせ、過去2日分の出席を投入(setupと同じ UTC/境界5)。
	// created_atは正午にして、gametime.Dateが境界(5時)で前日へ食い込まないようにする。
	today := gametime.Date(time.Now(), time.UTC, 5)
	if _, err := pool.Exec(ctx, `UPDATE players SET created_at = $1 WHERE id = $2`, today.AddDate(0, 0, -5).Add(12*time.Hour), alice.ID); err != nil {
		t.Fatal(err)
	}
	for _, off := range []int{1, 2} {
		if _, err := pool.Exec(ctx, `INSERT INTO attendance (player_id, day) VALUES ($1, $2) ON CONFLICT DO NOTHING`, alice.ID, today.AddDate(0, 0, -off)); err != nil {
			t.Fatal(err)
		}
	}

	// ボード取得。
	resp, err := http.Get(srv.URL + "/api/v1/attendance")
	if err != nil {
		t.Fatal(err)
	}
	var board struct {
		Dates   []string `json:"dates"`
		Members []struct {
			ID    int64    `json:"id"`
			Name  string   `json:"name"`
			Cells []string `json:"cells"`
		} `json:"members"`
		Ranking []struct {
			Name    string `json:"name"`
			Present int    `json:"present"`
			Days    int    `json:"days"`
			Rate    int    `json:"rate"`
		} `json:"ranking"`
	}
	json.NewDecoder(resp.Body).Decode(&board)
	resp.Body.Close()

	if len(board.Dates) != 14 {
		t.Fatalf("dates = %d, want 14", len(board.Dates))
	}
	var am *struct {
		ID    int64    `json:"id"`
		Name  string   `json:"name"`
		Cells []string `json:"cells"`
	}
	for i := range board.Members {
		if board.Members[i].ID == alice.ID {
			am = &board.Members[i]
		}
	}
	if am == nil {
		t.Fatal("alice not in board")
	}
	// i=0,1,2 出席 / i=3,4,5 欠席(登録日以降で未出席) / i>=6 未登録(登録日より前)。
	want := map[int]string{0: "present", 1: "present", 2: "present", 3: "absent", 5: "absent", 6: "blank", 13: "blank"}
	for i, w := range want {
		if am.Cells[i] != w {
			t.Errorf("cell[%d] = %q, want %q", i, am.Cells[i], w)
		}
	}
	// ランキング: alice applicable=6, present=3, rate=50。
	var found bool
	for _, r := range board.Ranking {
		if r.Name == "alice" {
			found = true
			if r.Present != 3 || r.Days != 6 || r.Rate != 50 {
				t.Errorf("rank alice = present %d days %d rate %d, want 3/6/50", r.Present, r.Days, r.Rate)
			}
		}
	}
	if !found {
		t.Errorf("alice not in ranking")
	}
}

// TestEvent covers random events: rolling repeatedly fires events, effects apply
// through the ledger (zero-sum preserved), and rolls are rate-limited.
func TestEvent(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	led := ledger.New(pool)
	alice := register(t, srv.URL, "misskey.example", "alice")
	// 慈善イベントの受け皿として別プレイヤーも用意。
	register(t, srv.URL, "misskey.example", "bob")

	roll := func(key string) (int, bool, string) {
		b, _ := json.Marshal(map[string]any{"idempotency_key": key})
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/events/roll", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var r struct {
			Event *struct {
				Name string `json:"name"`
			} `json:"event"`
		}
		json.NewDecoder(resp.Body).Decode(&r)
		name := ""
		if r.Event != nil {
			name = r.Event.Name
		}
		return resp.StatusCode, r.Event != nil, name
	}

	events := 0
	for i := 0; i < 180; i++ {
		// レート制限を解除して毎回抽選させる。健康の貯金を積んでおき、
		// 体調不良イベントが「代入」で即風邪ぎみにすることを検証する。
		if _, err := pool.Exec(ctx, `DELETE FROM player_facility_cooldowns WHERE player_id=$1 AND facility='event_roll'`, alice.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE player_status SET disease_index=50 WHERE player_id=$1`, alice.ID); err != nil {
			t.Fatal(err)
		}
		code, fired, name := roll(fmt.Sprintf("ev-%d", i))
		if code != http.StatusOK {
			t.Fatalf("roll status = %d", code)
		}
		if fired {
			events++
		}
		if name == "体調不良" {
			var idx int
			pool.QueryRow(ctx, `SELECT disease_index FROM player_status WHERE player_id=$1`, alice.ID).Scan(&idx)
			if idx != -8 {
				t.Errorf("体調不良後のdisease_index = %d, want -8 (代入されていない)", idx)
			}
		}
	}
	if events == 0 {
		t.Errorf("no events fired in 180 rolls")
	}
	// 台帳ゼロ和(お金系イベント・慈善を含む)を維持。
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}

	// レート制限: cooldownを解除して1回roll(cooldownセット)→直後のrollは抽選されない。
	if _, err := pool.Exec(ctx, `DELETE FROM player_facility_cooldowns WHERE player_id=$1 AND facility='event_roll'`, alice.ID); err != nil {
		t.Fatal(err)
	}
	roll("rate-1")
	if _, fired, _ := roll("rate-2"); fired {
		t.Errorf("second roll within interval fired an event (rate limit not applied)")
	}
}

// TestCustomEvent verifies an admin-defined event joins the roll pool and its
// outcome (money/params) is applied.
func TestCustomEvent(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	alice := register(t, srv.URL, "misskey.example", "cevalice")
	// 前回実行の残留イベントを消してから投入する(content_eventsはsetupで消えない)。
	if _, err := pool.Exec(ctx, `DELETE FROM content_events`); err != nil {
		t.Fatal(err)
	}
	// 重み100のカスタムイベント(+100円, kokugo+2)を投入(組み込み21種に対し圧倒的に出やすい)。
	// メッセージはプレースホルダー({money}/{name})の展開も検証する。
	if _, err := pool.Exec(ctx,
		`INSERT INTO content_events (name, message, good, money_min, money_max, params, weight, enabled)
		 VALUES ('検証入金', '{name}は検証で{money}円拾いました。', true, 100, 100, '{"kokugo": 2}', 100, true)`); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	roll := func(key string) (string, string) {
		b, _ := json.Marshal(map[string]any{"idempotency_key": key})
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/events/roll", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var r struct {
			Event *struct {
				Name    string `json:"name"`
				Message string `json:"message"`
			} `json:"event"`
		}
		json.NewDecoder(resp.Body).Decode(&r)
		if r.Event != nil {
			return r.Event.Name, r.Event.Message
		}
		return "", ""
	}
	fired := false
	for i := 0; i < 300 && !fired; i++ {
		if _, err := pool.Exec(ctx, `DELETE FROM player_facility_cooldowns WHERE player_id=$1 AND facility='event_roll'`, alice.ID); err != nil {
			t.Fatal(err)
		}
		var before int64
		pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
			fmt.Sprintf("player:%d", alice.ID)).Scan(&before)
		var kokugoBefore int
		pool.QueryRow(ctx, `SELECT kokugo FROM player_status WHERE player_id=$1`, alice.ID).Scan(&kokugoBefore)
		if name, msg := roll(fmt.Sprintf("cev-%d", i)); name == "検証入金" {
			fired = true
			if msg != "cevaliceは検証で100円拾いました。" {
				t.Errorf("message = %q, want placeholder-expanded", msg)
			}
			var after int64
			pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
				fmt.Sprintf("player:%d", alice.ID)).Scan(&after)
			if after-before != 100 {
				t.Errorf("money delta = %d, want 100", after-before)
			}
			var kokugoAfter int
			pool.QueryRow(ctx, `SELECT kokugo FROM player_status WHERE player_id=$1`, alice.ID).Scan(&kokugoAfter)
			if kokugoAfter-kokugoBefore != 2 {
				t.Errorf("kokugo delta = %d, want 2", kokugoAfter-kokugoBefore)
			}
		}
	}
	if !fired {
		t.Error("custom event never fired in 300 rolls")
	}
}

// TestCustomEventConditions verifies eligibility conditions: a condition-gated
// event never fires for a player who fails the condition, and can fire once the
// condition is met.
func TestCustomEventConditions(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	alice := register(t, srv.URL, "misskey.example", "cevcond")
	if _, err := pool.Exec(ctx, `DELETE FROM content_events`); err != nil {
		t.Fatal(err)
	}
	// 条件: kokugo>=9999。重み100で、条件さえ満たせば高確率で出る。
	if _, err := pool.Exec(ctx,
		`INSERT INTO content_events (name, message, good, money_min, money_max, params, weight, enabled, conditions)
		 VALUES ('秀才の幸運', '勉強のご褒美に10円拾いました。', true, 10, 10, '{}', 100, true,
		         '[{"pred":"param_gte","param":"kokugo","value":9999}]')`); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	roll := func(key string) string {
		b, _ := json.Marshal(map[string]any{"idempotency_key": key})
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(alice.ID, 10)+"/events/roll", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var r struct {
			Event *struct {
				Name string `json:"name"`
			} `json:"event"`
		}
		json.NewDecoder(resp.Body).Decode(&r)
		if r.Event != nil {
			return r.Event.Name
		}
		return ""
	}
	// 条件未達(kokugo初期値)では絶対に出ない。
	for i := 0; i < 120; i++ {
		if _, err := pool.Exec(ctx, `DELETE FROM player_facility_cooldowns WHERE player_id=$1 AND facility='event_roll'`, alice.ID); err != nil {
			t.Fatal(err)
		}
		if roll(fmt.Sprintf("cc-a%d", i)) == "秀才の幸運" {
			t.Fatal("gated event fired for unqualified player")
		}
	}
	// 条件を満たすと出る。
	if _, err := pool.Exec(ctx, `UPDATE player_status SET kokugo=9999 WHERE player_id=$1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	fired := false
	for i := 0; i < 300 && !fired; i++ {
		if _, err := pool.Exec(ctx, `DELETE FROM player_facility_cooldowns WHERE player_id=$1 AND facility='event_roll'`, alice.ID); err != nil {
			t.Fatal(err)
		}
		fired = roll(fmt.Sprintf("cc-b%d", i)) == "秀才の幸運"
	}
	if !fired {
		t.Error("gated event never fired for qualified player in 300 rolls")
	}
}

// grantMoney credits a player via a balanced ledger tx (test setup helper).
func grantMoney(t *testing.T, pool *pgxpool.Pool, playerID, amount int64) {
	t.Helper()
	ctx := context.Background()
	var txid int64
	if err := pool.QueryRow(ctx, `INSERT INTO ledger_tx (reason) VALUES ('test_grant') RETURNING id`).Scan(&txid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO ledger_entry (tx_id, account, delta) VALUES ($1, $2, $3), ($1, $4, $5)`,
		txid, "player:"+strconv.FormatInt(playerID, 10), amount, "system:test_grant", -amount); err != nil {
		t.Fatal(err)
	}
}

// TestCLeague covers battle characters: creation, growth (transfers owner params
// + charges money), battling (records a result), and validation. Ledger zero-sum.
func TestCLeague(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	led := ledger.New(pool)
	alice := register(t, srv.URL, "misskey.example", "alice")
	bob := register(t, srv.URL, "misskey.example", "bob")
	grantMoney(t, pool, alice.ID, 2000000)
	// 育成できるよう本人パラメータを高くしておく。
	for _, id := range []int64{alice.ID, bob.ID} {
		if _, err := pool.Exec(ctx, `UPDATE player_status SET kokugo=1000, power=1000, looks=1000 WHERE player_id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}

	post := func(id int64, path string, body map[string]any) (int, []byte) {
		b, _ := json.Marshal(body)
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(id, 10)+path, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, data
	}

	// キャラ作成。
	if code, body := post(alice.ID, "/character", map[string]any{"name": "アリスの戦士", "idempotency_key": "n1"}); code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", code, body)
	}
	post(bob.ID, "/character", map[string]any{"name": "ボブの戦士", "idempotency_key": "n2"})

	// 育成: kokugo50 + power50 → cost 100万円、本人-50ずつ。
	code, body := post(alice.ID, "/character/grow", map[string]any{"inputs": map[string]int{"kokugo": 50, "power": 50}, "idempotency_key": "g1"})
	if code != http.StatusOK {
		t.Fatalf("grow status = %d, body = %s", code, body)
	}
	var pr struct {
		Money  int64 `json:"money"`
		Params struct {
			Kokugo int `json:"kokugo"`
		} `json:"params"`
	}
	json.Unmarshal(body, &pr)
	if pr.Money != 2500000-1000000 {
		t.Errorf("money after grow = %d, want %d", pr.Money, 2500000-1000000)
	}
	if pr.Params.Kokugo != 950 {
		t.Errorf("alice kokugo = %d, want 950", pr.Params.Kokugo)
	}
	// キャラ能力が反映されている。
	resp, _ := http.Get(srv.URL + "/api/v1/players/" + strconv.FormatInt(alice.ID, 10) + "/character")
	var ch struct {
		Abilities map[string]int `json:"abilities"`
	}
	json.NewDecoder(resp.Body).Decode(&ch)
	resp.Body.Close()
	if ch.Abilities["kokugo"] != 50 || ch.Abilities["power"] != 50 {
		t.Errorf("character abilities = %+v", ch.Abilities)
	}

	// 育成の検証: 本人値以上 → 422。
	if code, _ := post(alice.ID, "/character/grow", map[string]any{"inputs": map[string]int{"kokugo": 99999}, "idempotency_key": "g2"}); code != http.StatusUnprocessableEntity {
		t.Errorf("over-param grow status = %d, want 422", code)
	}

	// bobも少し育成(対戦相手にする)。
	post(bob.ID, "/character/grow", map[string]any{"inputs": map[string]int{"kokugo": 5}, "idempotency_key": "gb"})

	// 対戦: alice vs bob。結果が返る。
	code, body = post(alice.ID, "/character/battle", map[string]any{"opponent_id": bob.ID, "idempotency_key": "b1"})
	if code != http.StatusOK {
		t.Fatalf("battle status = %d, body = %s", code, body)
	}
	var br struct {
		Result struct {
			Winner string `json:"winner"`
			Rounds []any  `json:"rounds"`
		} `json:"result"`
	}
	json.Unmarshal(body, &br)
	if br.Result.Winner != "a" && br.Result.Winner != "b" && br.Result.Winner != "draw" {
		t.Errorf("winner = %q", br.Result.Winner)
	}
	if len(br.Result.Rounds) != 5 {
		t.Errorf("rounds = %d, want 5", len(br.Result.Rounds))
	}

	// クールタイム中の連戦 → 422(DebugNoCooldown=false)。
	if code, _ := post(alice.ID, "/character/battle", map[string]any{"opponent_id": bob.ID, "idempotency_key": "b2"}); code != http.StatusUnprocessableEntity {
		t.Errorf("cooldown battle status = %d, want 422", code)
	}
	// 自分対戦 → 422。
	if code, _ := post(alice.ID, "/character/battle", map[string]any{"opponent_id": alice.ID, "idempotency_key": "b3"}); code != http.StatusUnprocessableEntity {
		t.Errorf("self-battle status = %d, want 422", code)
	}

	// リーグ順位に2キャラ。
	resp, _ = http.Get(srv.URL + "/api/v1/cleague")
	var rank []any
	json.NewDecoder(resp.Body).Decode(&rank)
	resp.Body.Close()
	if len(rank) != 2 {
		t.Errorf("ranking = %d, want 2", len(rank))
	}

	// 台帳ゼロ和。
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

// buildHouse posts a build request to the construction company and returns the
// updated player and HTTP status.
func buildHouse(t *testing.T, base string, playerID int64, town, row, col int, exterior string, interiorRank int, idemKey string) (playerResp, int) {
	t.Helper()
	return buildHouseTuika(t, base, playerID, town, row, col, exterior, interiorRank, 0, idemKey)
}

// buildHouseTuika builds with an explicit 追加種別 (2軒目以降).
func buildHouseTuika(t *testing.T, base string, playerID int64, town, row, col int, exterior string, interiorRank, tuika int, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"town": town, "row": row, "col": col,
		"exterior": exterior, "interior_rank": interiorRank, "tuika": tuika,
		"idempotency_key": idemKey,
	})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/build",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

// creditSavings tops up a player's bank savings via the ledger (test faucet),
// preserving the double-entry zero-sum invariant.
func creditSavings(t *testing.T, pool *pgxpool.Pool, playerID, amount int64) {
	t.Helper()
	ctx := context.Background()
	led := ledger.New(pool)
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return led.PostTx(ctx, tx, "test_credit", "", []ledger.Entry{
			{Account: ledger.SavingsAccount(playerID), Delta: amount},
			{Account: ledger.SystemAccount("test_faucet"), Delta: -amount},
		})
	})
	if err != nil {
		t.Fatalf("credit savings: %v", err)
	}
}

// seedPlots designates buildable empty plots for tests by adding akichi
// facilities to the town map (空き地は施設に統合済み)。各要素は{town, row, col}。
func seedPlots(t *testing.T, pool *pgxpool.Pool, plots [][3]int) {
	t.Helper()
	ctx := context.Background()
	for _, p := range plots {
		if _, err := pool.Exec(ctx,
			`UPDATE town_map SET facilities = facilities || jsonb_build_object(
				'key', 'akichi', 'img', 'akiti', 'alt', '空き地',
				'town', $1::int, 'row', $2::int, 'col', $3::int, 'dest', 0, 'ready', false)
			 WHERE id = 1`,
			p[0], p[1], p[2]); err != nil {
			t.Fatalf("seed plot: %v", err)
		}
	}
}

func moveTown(t *testing.T, base string, playerID int64, dest int, means, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"dest": dest, "means": means, "idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/move",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("move post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func TestMoveTown(t *testing.T) {
	srv, pool := setup(t)
	p := register(t, srv.URL, "misskey.example", "traveler")

	// 徒歩で街2へ移動(無料)。current_townが変わる。
	got, code := moveTown(t, srv.URL, p.ID, 2, "walk", "mv1")
	if code != http.StatusOK {
		t.Fatalf("walk move: status=%d", code)
	}
	if got.CurrentTown != 2 {
		t.Errorf("current_town after walk = %d, want 2", got.CurrentTown)
	}
	if got.Money != 500_000 {
		t.Errorf("money after walk = %d, want 500000 (無料)", got.Money)
	}

	// 移動直後は移動時間(クールタイム)中で再移動不可。
	if _, c := moveTown(t, srv.URL, p.ID, 3, "walk", "mv2"); c != http.StatusUnprocessableEntity {
		t.Errorf("move during cooldown: status=%d, want 422", c)
	}

	// クールタイムを解除し、バスで街0へ移動(500円課金)。
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM player_facility_cooldowns WHERE player_id=$1 AND facility='move'`, p.ID); err != nil {
		t.Fatalf("clear cooldown: %v", err)
	}
	got, code = moveTown(t, srv.URL, p.ID, 0, "bus", "mv3")
	if code != http.StatusOK {
		t.Fatalf("bus move: status=%d", code)
	}
	if got.CurrentTown != 0 {
		t.Errorf("current_town after bus = %d, want 0", got.CurrentTown)
	}
	if got.Money != 499_500 {
		t.Errorf("money after bus = %d, want 499500 (500円課金)", got.Money)
	}

	// 同じ街への移動は拒否。
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM player_facility_cooldowns WHERE player_id=$1 AND facility='move'`, p.ID); err != nil {
		t.Fatalf("clear cooldown: %v", err)
	}
	if _, c := moveTown(t, srv.URL, p.ID, 0, "walk", "mv4"); c != http.StatusUnprocessableEntity {
		t.Errorf("move to same town: status=%d, want 422", c)
	}

	// 不正な手段は拒否。
	if _, c := moveTown(t, srv.URL, p.ID, 1, "teleport", "mv5"); c != http.StatusUnprocessableEntity {
		t.Errorf("invalid means: status=%d, want 422", c)
	}

	// 徒歩の能力上昇: 何度か徒歩移動すると身体5能力の合計が増える(各50%, +1〜5)。
	sumStats := func() int {
		var tairyoku, kenkou, speed, wanryoku, kyakuryoku int
		if err := pool.QueryRow(context.Background(),
			`SELECT tairyoku, kenkou, speed, wanryoku, kyakuryoku FROM player_status WHERE player_id=$1`,
			p.ID).Scan(&tairyoku, &kenkou, &speed, &wanryoku, &kyakuryoku); err != nil {
			t.Fatalf("read stats: %v", err)
		}
		return tairyoku + kenkou + speed + wanryoku + kyakuryoku
	}
	before := sumStats()
	for i := 0; i < 15; i++ {
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM player_facility_cooldowns WHERE player_id=$1 AND facility='move'`, p.ID); err != nil {
			t.Fatalf("clear cooldown: %v", err)
		}
		dest := 1 + i%2 // 街1と街2を交互に(同一街回避)
		if _, c := moveTown(t, srv.URL, p.ID, dest, "walk", fmt.Sprintf("wgain-%d", i)); c != http.StatusOK {
			t.Fatalf("walk gain move %d: status=%d", i, c)
		}
	}
	if after := sumStats(); after <= before {
		t.Errorf("walk stat gains: sum before=%d after=%d, want after>before", before, after)
	}
}

// giveItem inserts a catalog item into a player's inventory for tests.
func giveItem(t *testing.T, pool *pgxpool.Pool, playerID int64, name string, qty int) {
	t.Helper()
	ctx := context.Background()
	var id int64
	var durability int
	if err := pool.QueryRow(ctx,
		`SELECT id, GREATEST(durability, 1) FROM content_items WHERE name = $1`, name).Scan(&id, &durability); err != nil {
		t.Fatalf("find item %s: %v", name, err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (player_id, item_id) DO UPDATE SET
		   quantity = player_items.quantity + $3, remaining_uses = player_items.remaining_uses + $4`,
		playerID, id, qty, durability*qty); err != nil {
		t.Fatalf("give item %s: %v", name, err)
	}
}

type moveResultT struct {
	ArrivedTown int            `json:"arrived_town"`
	Vehicle     string         `json:"vehicle"`
	StatGains   map[string]int `json:"stat_gains"`
	Accident    bool           `json:"accident"`
	Lost        bool           `json:"lost"`
}

func moveTownR(t *testing.T, base string, playerID int64, dest int, means, idemKey string) (moveResultT, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"dest": dest, "means": means, "idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/move",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("move post: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		MoveResult moveResultT `json:"move_result"`
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return out.MoveResult, resp.StatusCode
}

func TestMoveVehicle(t *testing.T) {
	srv, pool := setup(t)
	p := register(t, srv.URL, "misskey.example", "driver")

	// ベンツ(乗り物, 移動5秒)を持って徒歩移動 → ベンツを使い、車なので能力は上がらない。
	giveItem(t, pool, p.ID, "ベンツ", 1)
	mr, code := moveTownR(t, srv.URL, p.ID, 1, "walk", "veh1")
	if code != http.StatusOK {
		t.Fatalf("move with benz: status=%d", code)
	}
	if mr.Vehicle != "ベンツ" {
		t.Errorf("vehicle = %q, want ベンツ", mr.Vehicle)
	}
	if len(mr.StatGains) != 0 {
		t.Errorf("car should not raise stats, got %v", mr.StatGains)
	}
	if mr.ArrivedTown != 1 {
		t.Errorf("arrived = %d, want 1", mr.ArrivedTown)
	}

	// 自転車(移動20秒)も持つと、最速の乗り物(ベンツ5秒)が選ばれる。
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM player_facility_cooldowns WHERE player_id=$1 AND facility='move'`, p.ID); err != nil {
		t.Fatalf("clear cooldown: %v", err)
	}
	giveItem(t, pool, p.ID, "自転車", 1)
	mr, code = moveTownR(t, srv.URL, p.ID, 2, "walk", "veh2")
	if code != http.StatusOK {
		t.Fatalf("move with benz+bike: status=%d", code)
	}
	if mr.Vehicle != "ベンツ" {
		t.Errorf("fastest vehicle = %q, want ベンツ(5秒 < 自転車20秒)", mr.Vehicle)
	}
}

func warp(t *testing.T, base string, playerID int64, dest int, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"dest": dest, "idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/warp",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("warp post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func TestWarp(t *testing.T) {
	srv, pool := setup(t)
	p := register(t, srv.URL, "misskey.example", "warper") // 現金500000

	// 街3へワープ → 100000円課金、即時到着(クールタイム無し)。
	got, code := warp(t, srv.URL, p.ID, 3, "w1")
	if code != http.StatusOK {
		t.Fatalf("warp: status=%d", code)
	}
	if got.CurrentTown != 3 {
		t.Errorf("current_town after warp = %d, want 3", got.CurrentTown)
	}
	if got.Money != 400_000 {
		t.Errorf("money after warp = %d, want 400000 (100000課金)", got.Money)
	}

	// 同じ街へのワープは拒否。
	if _, c := warp(t, srv.URL, p.ID, 3, "w2"); c != http.StatusUnprocessableEntity {
		t.Errorf("warp to same town: status=%d, want 422", c)
	}

	// 現金が料金未満だと拒否。
	creditCash(t, pool, p.ID, -350_000) // 400000-350000=50000 < 100000
	if _, c := warp(t, srv.URL, p.ID, 0, "w3"); c != http.StatusUnprocessableEntity {
		t.Errorf("warp with insufficient cash: status=%d, want 422", c)
	}
}

// TestTownMapHouseGuard verifies that saving the town map cannot remove the
// akichi under a built house (家が孤立する不整合を防ぐガード)。
func TestTownMapHouseGuard(t *testing.T) {
	srv, pool := setup(t)
	admin := register(t, srv.URL, "misskey.example", "root") // 最初=admin
	creditSavings(t, pool, admin.ID, 30_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}}) // 街4 A1 を空き地(akichi)に
	if _, c := buildHouse(t, srv.URL, admin.ID, 4, 0, 1, "house1", 3, "hg1"); c != http.StatusOK {
		t.Fatalf("build: status=%d", c)
	}
	akichi := townmap.Facility{Key: "akichi", Img: "akiti", Alt: "空き地", Town: 4, Row: 0, Col: 1}

	// 家のあるマスの akichi を外して保存 → 拒否(422)。
	if code, _ := adminPut(t, srv.URL, "/api/v1/admin/townmap", admin.ID, townmap.Default()); code != http.StatusUnprocessableEntity {
		t.Errorf("remove akichi under house: status=%d, want 422", code)
	}
	// akichi を含めて保存 → 成功(200)。
	withAkichi := append(townmap.Default(), akichi)
	if code, body := adminPut(t, srv.URL, "/api/v1/admin/townmap", admin.ID, withAkichi); code != http.StatusOK {
		t.Errorf("keep akichi: status=%d, body=%s", code, body)
	}
}

// TestUploadAsset covers the background-asset image upload: admin-only, stored,
// served, listed, and validated.
func TestUploadAsset(t *testing.T) {
	srv, _ := setup(t)
	admin := register(t, srv.URL, "misskey.example", "root")
	user := register(t, srv.URL, "misskey.example", "alice")
	// 1x1 red PNG。
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	good := map[string]any{"name": "tile1", "mime": "image/png", "data": png}

	// 認可: 非adminは403。
	if code, _ := adminPost(t, srv.URL, "/api/v1/admin/assets", user.ID, good); code != http.StatusForbidden {
		t.Errorf("non-admin upload: status=%d, want 403", code)
	}
	// adminはアップロード成功。
	if code, body := adminPost(t, srv.URL, "/api/v1/admin/assets", admin.ID, good); code != http.StatusOK {
		t.Fatalf("upload: status=%d, body=%s", code, body)
	}
	// 公開配信で取得でき、Content-Typeが画像。
	resp, err := http.Get(srv.URL + "/api/v1/assets/tile1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("serve: status=%d, content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp.Body.Close()
	// 一覧に含まれる。
	lreq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/admin/assets", nil)
	lreq.Header.Set("X-Acting-Player-Id", strconv.FormatInt(admin.ID, 10))
	lresp, err := http.DefaultClient.Do(lreq)
	if err != nil {
		t.Fatal(err)
	}
	lbody, _ := io.ReadAll(lresp.Body)
	lresp.Body.Close()
	if lresp.StatusCode != http.StatusOK || !bytes.Contains(lbody, []byte("tile1")) {
		t.Errorf("list: status=%d, body=%s", lresp.StatusCode, lbody)
	}
	// 不正な名前・形式は400。
	if code, _ := adminPost(t, srv.URL, "/api/v1/admin/assets", admin.ID,
		map[string]any{"name": "bad name!", "mime": "image/png", "data": png}); code != http.StatusBadRequest {
		t.Errorf("bad name: status=%d, want 400", code)
	}
	if code, _ := adminPost(t, srv.URL, "/api/v1/admin/assets", admin.ID,
		map[string]any{"name": "tile2", "mime": "application/pdf", "data": png}); code != http.StatusBadRequest {
		t.Errorf("bad mime: status=%d, want 400", code)
	}

	// 配置中の画像は削除できない(422)。配置を外せば削除できる(200)。
	delTile1 := func() int {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/admin/assets/tile1", nil)
		req.Header.Set("X-Acting-Player-Id", strconv.FormatInt(admin.ID, 10))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	place := []map[string]any{{"img": "u:tile1", "town": 0, "col": 1, "row": 0}}
	if code, _ := adminPut(t, srv.URL, "/api/v1/admin/townassets", admin.ID, place); code != http.StatusOK {
		t.Fatalf("place asset: status=%d", code)
	}
	if c := delTile1(); c != http.StatusUnprocessableEntity {
		t.Errorf("delete in-use asset: status=%d, want 422", c)
	}
	if code, _ := adminPut(t, srv.URL, "/api/v1/admin/townassets", admin.ID, []map[string]any{}); code != http.StatusOK {
		t.Fatalf("clear townassets: status=%d", code)
	}
	if c := delTile1(); c != http.StatusOK {
		t.Errorf("delete unused asset: status=%d, want 200", c)
	}
}

// TestTowns covers the town config API: GET is public, PUT is admin-only,
// updates the count/names/land price and reflects in move bounds.
func TestTowns(t *testing.T) {
	srv, _ := setup(t)
	admin := register(t, srv.URL, "misskey.example", "root")
	user := register(t, srv.URL, "misskey.example", "alice")

	// GETは公開。既定の5街。
	resp, err := http.Get(srv.URL + "/api/v1/towns")
	if err != nil {
		t.Fatal(err)
	}
	var got []struct {
		No        int    `json:"no"`
		Name      string `json:"name"`
		LandPrice int    `json:"land_price"`
	}
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if len(got) != 5 || got[0].Name != "公園" {
		t.Fatalf("default towns = %v", got)
	}

	// 非adminのPUTは403。
	if code, _ := adminPut(t, srv.URL, "/api/v1/admin/towns", user.ID,
		[]map[string]any{{"name": "x", "land_price": 1}}); code != http.StatusForbidden {
		t.Errorf("non-admin update towns: status=%d, want 403", code)
	}
	// 空名は400。
	if code, _ := adminPut(t, srv.URL, "/api/v1/admin/towns", admin.ID,
		[]map[string]any{{"name": "", "land_price": 1}}); code != http.StatusBadRequest {
		t.Errorf("empty name: status=%d, want 400", code)
	}

	// adminが6街に更新(6番目を追加)。
	six := []map[string]any{
		{"name": "A", "land_price": 100}, {"name": "B", "land_price": 100},
		{"name": "C", "land_price": 100}, {"name": "D", "land_price": 100},
		{"name": "E", "land_price": 100}, {"name": "F", "land_price": 100},
	}
	if code, body := adminPut(t, srv.URL, "/api/v1/admin/towns", admin.ID, six); code != http.StatusOK {
		t.Fatalf("update towns: status=%d, body=%s", code, body)
	}
	// 移動先の上限が6街に広がる: dest=5 が通る(以前は422)。
	if _, c := moveTown(t, srv.URL, admin.ID, 5, "walk", "tw6"); c != http.StatusOK {
		t.Errorf("move to town 5 (6 towns): status=%d, want 200", c)
	}

	// 隠し町(hidden)にはワープできない。街2を隠し町にする。
	hidden := []map[string]any{
		{"name": "A", "land_price": 100}, {"name": "B", "land_price": 100},
		{"name": "C", "land_price": 100, "hidden": true}, {"name": "D", "land_price": 100},
		{"name": "E", "land_price": 100}, {"name": "F", "land_price": 100},
	}
	if code, _ := adminPut(t, srv.URL, "/api/v1/admin/towns", admin.ID, hidden); code != http.StatusOK {
		t.Fatalf("set hidden town: status=%d", code)
	}
	if _, c := warp(t, srv.URL, admin.ID, 2, "wh1"); c != http.StatusUnprocessableEntity {
		t.Errorf("warp to hidden town: status=%d, want 422", c)
	}
	if _, c := warp(t, srv.URL, admin.ID, 1, "wh2"); c != http.StatusOK {
		t.Errorf("warp to normal town: status=%d, want 200", c)
	}
}

func TestBuildHouse(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	p := register(t, srv.URL, "misskey.example", "builder")
	creditSavings(t, pool, p.ID, 30_000_000) // 3000万円を普通口座へ
	// 謎の街(4)のA1-A5を空地(akichi施設)に指定する。街0の施設セル(建設会社: row4,col13)は
	// 施設が既にあるため akichi を置けず、そこには建てられない(下でテスト)。
	seedPlots(t, pool, [][3]int{{4, 0, 1}, {4, 0, 2}, {4, 0, 3}, {4, 0, 4}, {4, 0, 5}})

	// 空地に指定されていないマスには建てられない。
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 5, 5, "house1", 0, "bn"); c != http.StatusUnprocessableEntity {
		t.Errorf("non-plot: status=%d, want 422", c)
	}

	// 1軒目: 謎の街(4) の A1 に house1 + D内装。費用=(250+150)×1×10000=4,000,000。
	got, code := buildHouse(t, srv.URL, p.ID, 4, 0, 1, "house1", 3, "b1")
	if code != http.StatusOK {
		t.Fatalf("build 1st: status=%d", code)
	}
	if got.Savings != 26_000_000 {
		t.Errorf("savings after 1st = %d, want 26000000", got.Savings)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM player_houses WHERE owner_id=$1`, p.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("house count = %d, want 1", count)
	}

	// 同じマスには二重に建てられない(空地でない)。
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 0, 1, "house1", 3, "b2"); c != http.StatusUnprocessableEntity {
		t.Errorf("duplicate plot: status=%d, want 422", c)
	}

	// 街0(メイン街)の施設セル(建設会社: col13,row4)には建てられない。
	if _, c := buildHouse(t, srv.URL, p.ID, 0, 4, 13, "house1", 0, "bf"); c != http.StatusUnprocessableEntity {
		t.Errorf("facility cell: status=%d, want 422", c)
	}

	// 2軒目以降: 費用=(250+150*2)*10000=5,500,000。内装は無視される。
	got, code = buildHouse(t, srv.URL, p.ID, 4, 0, 2, "house1", 0, "b3")
	if code != http.StatusOK {
		t.Fatalf("build 2nd: status=%d", code)
	}
	if got.Savings != 20_500_000 {
		t.Errorf("savings after 2nd = %d, want 20500000", got.Savings)
	}

	// 3・4軒目まで建てられる。
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 0, 3, "house1", 0, "b4"); c != http.StatusOK {
		t.Fatalf("build 3rd: status=%d", c)
	}
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 0, 4, "house1", 0, "b5"); c != http.StatusOK {
		t.Fatalf("build 4th: status=%d", c)
	}

	// 5軒目は上限(mochiie_max=4)で拒否される。
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 0, 5, "house1", 0, "b6"); c != http.StatusUnprocessableEntity {
		t.Errorf("5th house: status=%d, want 422", c)
	}

	// 台帳ゼロ和は保たれる。
	led := ledger.New(pool)
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

// TestBuildHouseTuika covers 2軒目以降の追加種別: tuika費の加算、同種1軒制限、
// 能力審査(総資産1億+全パラ1万)、tuika家のコンテンツ枠拒否。
func TestBuildHouseTuika(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	p := register(t, srv.URL, "misskey.example", "tuikabuilder")
	creditSavings(t, pool, p.ID, 20_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}, {4, 0, 2}, {4, 0, 3}, {4, 0, 4}})

	// 1軒目(D内装)。tuika指定は無視され家のみになる。費用=(250+150)×1=400万。
	if _, c := buildHouseTuika(t, srv.URL, p.ID, 4, 0, 1, "house1", 3, 3, "t1"); c != http.StatusOK {
		t.Fatalf("build 1st: %d", c)
	}
	var tuika0 int
	pool.QueryRow(ctx, `SELECT tuika FROM player_houses WHERE owner_id=$1`, p.ID).Scan(&tuika0)
	if tuika0 != 0 {
		t.Errorf("1st house tuika = %d, want 0", tuika0)
	}

	// 2軒目の持ち物販売店(tuika=3)は能力審査に落ちる(総資産1600万<1億)。
	if _, c := buildHouseTuika(t, srv.URL, p.ID, 4, 0, 2, "house1", 0, 3, "t2"); c != http.StatusUnprocessableEntity {
		t.Errorf("shinsa fail: %d, want 422", c)
	}
	// 運営(tuika=1)は審査なし。費用=250+150×2+100=650万。
	got, c := buildHouseTuika(t, srv.URL, p.ID, 4, 0, 2, "house1", 0, 1, "t3")
	if c != http.StatusOK {
		t.Fatalf("build unei: %d", c)
	}
	if got.Savings != 20_000_000-4_000_000-6_500_000 {
		t.Errorf("savings after unei = %d, want 9500000", got.Savings)
	}
	// 同種(運営)は2軒持てない。
	if _, c := buildHouseTuika(t, srv.URL, p.ID, 4, 0, 3, "house1", 0, 1, "t4"); c != http.StatusUnprocessableEntity {
		t.Errorf("dup unei: %d, want 422", c)
	}

	// 資産を1億超に増やしてもパラメータ不足なら審査に落ちる。
	creditSavings(t, pool, p.ID, 100_000_000)
	if _, c := buildHouseTuika(t, srv.URL, p.ID, 4, 0, 3, "house1", 0, 3, "t5"); c != http.StatusUnprocessableEntity {
		t.Errorf("shinsa param fail: %d, want 422", c)
	}
	// 全パラ1万で審査合格。費用=250+300+500=1050万。
	if _, err := pool.Exec(ctx, `UPDATE player_status SET kokugo=10000, suugaku=10000, rika=10000,
		syakai=10000, eigo=10000, ongaku=10000, bijutsu=10000, looks=10000, tairyoku=10000,
		kenkou=10000, speed=10000, power=10000, wanryoku=10000, kyakuryoku=10000, love=10000,
		omoshirosa=10000 WHERE player_id=$1`, p.ID); err != nil {
		t.Fatalf("set params: %v", err)
	}
	if _, c := buildHouseTuika(t, srv.URL, p.ID, 4, 0, 3, "house1", 0, 3, "t6"); c != http.StatusOK {
		t.Fatalf("build yami: %d", c)
	}
	// tuika家にはコンテンツ枠を設定できない。
	var yamiID int64
	pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1 AND tuika=3`, p.ID).Scan(&yamiID)
	if _, c := setContents(t, srv.URL, p.ID, yamiID,
		[]map[string]any{{"slot": 0, "kind": "bbs"}}, "t7"); c != http.StatusUnprocessableEntity {
		t.Errorf("contents on tuika house: %d, want 422", c)
	}
}

// TestCompanyUnei covers 運営(tuika=1): 社員追加(上限5)、社員教育(パラ移転1/10・
// 養育費2万/pt・1時間間隔・非オーナー拒否)、職判定と日次収入(worker)。
func TestCompanyUnei(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	owner := register(t, srv.URL, "misskey.example", "uneiowner")
	other := register(t, srv.URL, "misskey.example", "uneiother")
	creditSavings(t, pool, owner.ID, 20_000_000)
	creditCash(t, pool, owner.ID, 1_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}, {4, 0, 2}})
	if _, c := buildHouse(t, srv.URL, owner.ID, 4, 0, 1, "house1", 3, "u1"); c != http.StatusOK {
		t.Fatalf("build 1st: %d", c)
	}
	if _, c := buildHouseTuika(t, srv.URL, owner.ID, 4, 0, 2, "house1", 0, 1, "u2"); c != http.StatusOK {
		t.Fatalf("build unei: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1 AND tuika=1`, owner.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}
	post := func(who int64, path string, body map[string]any) int {
		t.Helper()
		b, _ := json.Marshal(body)
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(who, 10)+path,
			"application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("post %s: %v", path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// 社員追加は5人まで。非オーナーは追加できない。
	for i := 0; i < 5; i++ {
		if c := post(owner.ID, "/building/company/staff", map[string]any{
			"house_id": houseID, "idempotency_key": fmt.Sprintf("sa%d", i)}); c != http.StatusOK {
			t.Fatalf("staff add %d: %d", i, c)
		}
	}
	if c := post(owner.ID, "/building/company/staff", map[string]any{
		"house_id": houseID, "idempotency_key": "sa5"}); c != http.StatusUnprocessableEntity {
		t.Errorf("6th staff: %d, want 422", c)
	}
	if c := post(other.ID, "/building/company/staff", map[string]any{
		"house_id": houseID, "idempotency_key": "sa6"}); c != http.StatusUnprocessableEntity {
		t.Errorf("other staff add: %d, want 422", c)
	}

	// 社員教育: 国語500持ちのオーナーが100を与える → 社員+10、養育費20万円(現金)。
	if _, err := pool.Exec(ctx, `UPDATE player_status SET kokugo=500 WHERE player_id=$1`, owner.ID); err != nil {
		t.Fatalf("set kokugo: %v", err)
	}
	var staffID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM company_staff WHERE house_id=$1 AND idx=1`, houseID).Scan(&staffID); err != nil {
		t.Fatalf("staff id: %v", err)
	}
	if c := post(owner.ID, "/building/company/educate", map[string]any{
		"house_id": houseID, "staff_id": staffID, "param": "kokugo", "amount": 100,
		"idempotency_key": "e1"}); c != http.StatusOK {
		t.Fatalf("educate: %d", c)
	}
	var ownerKokugo int
	pool.QueryRow(ctx, `SELECT kokugo FROM player_status WHERE player_id=$1`, owner.ID).Scan(&ownerKokugo)
	if ownerKokugo != 400 {
		t.Errorf("owner kokugo = %d, want 400", ownerKokugo)
	}
	var staffKokugo int
	pool.QueryRow(ctx, `SELECT (params->>'kokugo')::int FROM company_staff WHERE id=$1`, staffID).Scan(&staffKokugo)
	if staffKokugo != 10 {
		t.Errorf("staff kokugo = %d, want 10", staffKokugo)
	}
	var feeSum int64
	pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account='system:company_edu'`).Scan(&feeSum)
	if feeSum != 200_000 {
		t.Errorf("edu fee = %d, want 200000", feeSum)
	}
	// 同じ社員は1時間待たないと教育できない。
	if c := post(owner.ID, "/building/company/educate", map[string]any{
		"house_id": houseID, "staff_id": staffID, "param": "kokugo", "amount": 10,
		"idempotency_key": "e2"}); c != http.StatusUnprocessableEntity {
		t.Errorf("educate again: %d, want 422", c)
	}
	// 非オーナー(運営)は教育できない。
	if c := post(other.ID, "/building/company/educate", map[string]any{
		"house_id": houseID, "staff_id": staffID, "param": "kokugo", "amount": 10,
		"idempotency_key": "e3"}); c != http.StatusUnprocessableEntity {
		t.Errorf("other educate: %d, want 422", c)
	}

	// 日次収入: kokugo10の社員はアルバイト(給与1000)に就ける。
	// 収入 = (1000×10 + (10×235/20)×10)/4 = (10000+1170)/4 = 2792円。
	// 残り4人は無資格でも給与0職として (0+0)/4=0…ではなくアルバイト(無条件)が
	// 就けるため同額の基礎収入が入る点に注意して合計を検証する。
	savBefore := int64(0)
	pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		fmt.Sprintf("savings:%d", owner.ID)).Scan(&savBefore)
	led := ledger.New(pool)
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := worker.PayCompanyIncome(ctx, tx, led)
		return err
	}); err != nil {
		t.Fatalf("pay income: %v", err)
	}
	var savAfter int64
	pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		fmt.Sprintf("savings:%d", owner.ID)).Scan(&savAfter)
	// 教育済み社員2792 + 無資格(アルバイト1000×10/4=2500)×4 = 12792円。
	if savAfter-savBefore != 2792+2500*4 {
		t.Errorf("income = %d, want %d", savAfter-savBefore, 2792+2500*4)
	}

	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

// TestCompanyKaisha covers 株式会社(tuika=2): 会社BBSと入退会フロー、役員による
// 社員教育と上限拡張、食材購入、製造(原料上限・1日1回・店頭陳列)、収益の役員分配。
func TestCompanyKaisha(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	owner := register(t, srv.URL, "misskey.example", "kaishaowner")
	member := register(t, srv.URL, "misskey.example", "kaishamember")
	creditSavings(t, pool, owner.ID, 200_000_000)
	creditCash(t, pool, owner.ID, 5_000_000)
	creditCash(t, pool, member.ID, 1_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}, {4, 0, 2}})
	if _, err := pool.Exec(ctx, `UPDATE player_status SET kokugo=10000, suugaku=10000, rika=10000,
		syakai=10000, eigo=10000, ongaku=10000, bijutsu=10000, looks=10000, tairyoku=10000,
		kenkou=10000, speed=10000, power=10000, wanryoku=10000, kyakuryoku=10000, love=10000,
		omoshirosa=10000 WHERE player_id=$1`, owner.ID); err != nil {
		t.Fatalf("set params: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE player_status SET suugaku=500 WHERE player_id=$1`, member.ID); err != nil {
		t.Fatalf("set member params: %v", err)
	}
	if _, c := buildHouse(t, srv.URL, owner.ID, 4, 0, 1, "house1", 3, "k1"); c != http.StatusOK {
		t.Fatalf("build 1st: %d", c)
	}
	if _, c := buildHouseTuika(t, srv.URL, owner.ID, 4, 0, 2, "house1", 0, 2, "k2"); c != http.StatusOK {
		t.Fatalf("build kaisha: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1 AND tuika=2`, owner.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}
	post := func(who int64, path string, body map[string]any) int {
		t.Helper()
		b, _ := json.Marshal(body)
		resp, err := http.Post(srv.URL+"/api/v1/players/"+strconv.FormatInt(who, 10)+path,
			"application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("post %s: %v", path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// 部外者はメンバー板に書けない。オープン板に入会希望を投稿できる。
	if c := post(member.ID, "/building/company/bbs", map[string]any{
		"house_id": houseID, "board": "member", "body": "だめ", "idempotency_key": "b0"}); c != http.StatusUnprocessableEntity {
		t.Errorf("outsider member post: %d, want 422", c)
	}
	if c := post(member.ID, "/building/company/bbs", map[string]any{
		"house_id": houseID, "board": "open", "body": "入りたいです", "want_join": true,
		"idempotency_key": "b1"}); c != http.StatusOK {
		t.Fatalf("join request: %d", c)
	}
	var joinPostID int64
	pool.QueryRow(ctx, `SELECT id FROM company_bbs WHERE house_id=$1 AND status='in'`, houseID).Scan(&joinPostID)
	// 非オーナーは許可できない。オーナーが許可すると役員になる。
	if c := post(member.ID, "/building/company/approve", map[string]any{
		"house_id": houseID, "post_id": joinPostID, "idempotency_key": "a1"}); c != http.StatusUnprocessableEntity {
		t.Errorf("member approve: %d, want 422", c)
	}
	if c := post(owner.ID, "/building/company/approve", map[string]any{
		"house_id": houseID, "post_id": joinPostID, "idempotency_key": "a2"}); c != http.StatusOK {
		t.Fatalf("approve join: %d", c)
	}
	var isOfficer bool
	pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM company_officers WHERE house_id=$1 AND player_id=$2)`,
		houseID, member.ID).Scan(&isOfficer)
	if !isOfficer {
		t.Fatal("member not officer after approve")
	}

	// 役員が増えたので社員上限は3×(役員1+オーナー1)=6。
	for i := 0; i < 6; i++ {
		if c := post(owner.ID, "/building/company/staff", map[string]any{
			"house_id": houseID, "idempotency_key": fmt.Sprintf("ks%d", i)}); c != http.StatusOK {
			t.Fatalf("staff add %d: %d", i, c)
		}
	}
	if c := post(owner.ID, "/building/company/staff", map[string]any{
		"house_id": houseID, "idempotency_key": "ks6"}); c != http.StatusUnprocessableEntity {
		t.Errorf("7th staff: %d, want 422", c)
	}

	// 役員(member)も社員教育できる。数学100 → 社員+10。
	var staffID int64
	pool.QueryRow(ctx, `SELECT id FROM company_staff WHERE house_id=$1 AND idx=1`, houseID).Scan(&staffID)
	if c := post(member.ID, "/building/company/educate", map[string]any{
		"house_id": houseID, "staff_id": staffID, "param": "suugaku", "amount": 100,
		"idempotency_key": "ke1"}); c != http.StatusOK {
		t.Fatalf("officer educate: %d", c)
	}
	// 食材購入(株式会社のみ、パラメータ消費なし): 100 → +1.0kcal。
	var staffID2 int64
	pool.QueryRow(ctx, `SELECT id FROM company_staff WHERE house_id=$1 AND idx=2`, houseID).Scan(&staffID2)
	if c := post(owner.ID, "/building/company/educate", map[string]any{
		"house_id": houseID, "staff_id": staffID2, "param": "syoku", "amount": 100,
		"idempotency_key": "ke2"}); c != http.StatusOK {
		t.Fatalf("syoku buy: %d", c)
	}
	var syoku float64
	pool.QueryRow(ctx, `SELECT syoku::float8 FROM company_materials WHERE house_id=$1`, houseID).Scan(&syoku)
	if syoku != 1.0 {
		t.Errorf("syoku = %v, want 1.0", syoku)
	}

	// 製造: 店が必要。開店してから、数学up=5(原料max=10/10=1? → 社員のsuugaku=10なので原料1)
	// 原料max=1のため設定値は1まで。
	if _, c := openShop(t, srv.URL, owner.ID, func() int64 {
		var h1 int64
		pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1 AND tuika=0`, owner.ID).Scan(&h1)
		return h1
	}(), "会社の店", "書籍", 2.0, "ko1"); c != http.StatusOK {
		t.Fatalf("open shop: %d", c)
	}
	// コンテンツ枠にshopが無くても店開店APIが通る前提(既存仕様)。原料超過は拒否。
	if c := post(owner.ID, "/building/company/seizou", map[string]any{
		"house_id": houseID, "input": map[string]any{
			"name": "テスト商品", "params": map[string]any{"suugaku": 5}, "zaiko": 1, "taikyuu": 1, "price": 10000},
		"idempotency_key": "sz1"}); c != http.StatusUnprocessableEntity {
		t.Errorf("seizou over material: %d, want 422", c)
	}
	if c := post(owner.ID, "/building/company/seizou", map[string]any{
		"house_id": houseID, "input": map[string]any{
			"name": "テスト商品", "params": map[string]any{"suugaku": 1}, "zaiko": 1, "taikyuu": 1, "price": 10000},
		"idempotency_key": "sz2"}); c != http.StatusOK {
		t.Fatalf("seizou: %d", c)
	}
	// 1日1回。
	if c := post(owner.ID, "/building/company/seizou", map[string]any{
		"house_id": houseID, "input": map[string]any{
			"name": "テスト商品", "params": map[string]any{"suugaku": 1}, "zaiko": 1, "taikyuu": 1, "price": 10000},
		"idempotency_key": "sz3"}); c != http.StatusUnprocessableEntity {
		t.Errorf("seizou twice: %d, want 422", c)
	}
	// 商品が店頭に並ぶ(facility='company'で問屋の品揃えからは除外)。
	var prodCnt int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM house_shop_stock ss JOIN content_items ci ON ci.id=ss.item_id
		WHERE ci.facility='company' AND ci.name LIKE 'テスト商品%'`).Scan(&prodCnt)
	if prodCnt != 1 {
		t.Errorf("product on shelf = %d, want 1", prodCnt)
	}

	// 収益分配: オーナーと役員の両方に同額が入る。
	ownerSav, memberSav := int64(0), int64(0)
	pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		fmt.Sprintf("savings:%d", owner.ID)).Scan(&ownerSav)
	pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		fmt.Sprintf("savings:%d", member.ID)).Scan(&memberSav)
	led := ledger.New(pool)
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := worker.PayCompanyIncome(ctx, tx, led)
		return err
	}); err != nil {
		t.Fatalf("pay income: %v", err)
	}
	var ownerSav2, memberSav2 int64
	pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		fmt.Sprintf("savings:%d", owner.ID)).Scan(&ownerSav2)
	pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		fmt.Sprintf("savings:%d", member.ID)).Scan(&memberSav2)
	ownerGain := ownerSav2 - ownerSav
	memberGain := memberSav2 - memberSav
	if ownerGain <= 0 || ownerGain != memberGain {
		t.Errorf("income owner=%d member=%d, want equal positive", ownerGain, memberGain)
	}

	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

// TestYamiShop covers the 持ち物販売店(闇市): listing from inventory (販売/預ける),
// visitor purchase (売上→家主普通口座), owner buyback (500円手数料), and item
// return on house sale.
func TestYamiShop(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	owner := register(t, srv.URL, "misskey.example", "yamiowner")
	visitor := register(t, srv.URL, "misskey.example", "yamivisitor")
	creditSavings(t, pool, owner.ID, 200_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}, {4, 0, 2}})
	if _, err := pool.Exec(ctx, `UPDATE player_status SET kokugo=10000, suugaku=10000, rika=10000,
		syakai=10000, eigo=10000, ongaku=10000, bijutsu=10000, looks=10000, tairyoku=10000,
		kenkou=10000, speed=10000, power=10000, wanryoku=10000, kyakuryoku=10000, love=10000,
		omoshirosa=10000 WHERE player_id=$1`, owner.ID); err != nil {
		t.Fatalf("set params: %v", err)
	}
	if _, c := buildHouse(t, srv.URL, owner.ID, 4, 0, 1, "house1", 3, "y1"); c != http.StatusOK {
		t.Fatalf("build 1st: %d", c)
	}
	if _, c := buildHouseTuika(t, srv.URL, owner.ID, 4, 0, 2, "house1", 0, 3, "y2"); c != http.StatusOK {
		t.Fatalf("build yami: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1 AND tuika=3`, owner.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}
	// 出品用アイテム: 耐久>0の商品を2個持たせる。
	var itemID int64
	var durability int
	var masterPrice int64
	if err := pool.QueryRow(ctx,
		`SELECT id, GREATEST(durability,1), price FROM content_items
		 WHERE enabled AND durability > 0 AND (max_sets = 0 OR max_sets >= 2) ORDER BY id LIMIT 1`).
		Scan(&itemID, &durability, &masterPrice); err != nil {
		t.Fatalf("pick item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO player_items (player_id, item_id, quantity, remaining_uses) VALUES ($1, $2, 2, $3)`,
		owner.ID, itemID, durability*2); err != nil {
		t.Fatalf("grant items: %v", err)
	}

	post := func(path string, body map[string]any) int {
		t.Helper()
		b, _ := json.Marshal(body)
		resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("post %s: %v", path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	ownerPath := "/api/v1/players/" + strconv.FormatInt(owner.ID, 10)
	visitorPath := "/api/v1/players/" + strconv.FormatInt(visitor.ID, 10)

	// 1個目を1000円で販売、2個目を倉庫に預ける。
	if c := post(ownerPath+"/building/yami/list", map[string]any{
		"house_id": houseID, "item_id": itemID, "price": 1000, "idempotency_key": "yl1"}); c != http.StatusOK {
		t.Fatalf("list sale: %d", c)
	}
	if c := post(ownerPath+"/building/yami/list", map[string]any{
		"house_id": houseID, "item_id": itemID, "warehouse": true, "idempotency_key": "yl2"}); c != http.StatusOK {
		t.Fatalf("list warehouse: %d", c)
	}
	var invCnt int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM player_items WHERE player_id=$1 AND item_id=$2`, owner.ID, itemID).Scan(&invCnt)
	if invCnt != 0 {
		t.Errorf("owner inventory rows = %d, want 0", invCnt)
	}
	var saleID, whID int64
	pool.QueryRow(ctx, `SELECT id FROM yami_items WHERE house_id=$1 AND zokusei=0`, houseID).Scan(&saleID)
	pool.QueryRow(ctx, `SELECT id FROM yami_items WHERE house_id=$1 AND zokusei=1`, houseID).Scan(&whID)
	if saleID == 0 || whID == 0 {
		t.Fatalf("listings not created: sale=%d wh=%d", saleID, whID)
	}

	// 訪問者は販売品を買える(現金1000円→家主の普通口座)。
	ownerSavBefore := int64(0)
	pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		fmt.Sprintf("savings:%d", owner.ID)).Scan(&ownerSavBefore)
	if c := post(visitorPath+"/building/yami/buy", map[string]any{
		"house_id": houseID, "listing_id": saleID, "idempotency_key": "yb1"}); c != http.StatusOK {
		t.Fatalf("visitor buy: %d", c)
	}
	var ownerSavAfter int64
	pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		fmt.Sprintf("savings:%d", owner.ID)).Scan(&ownerSavAfter)
	if ownerSavAfter-ownerSavBefore != 1000 {
		t.Errorf("owner savings delta = %d, want 1000", ownerSavAfter-ownerSavBefore)
	}
	var vQty int
	pool.QueryRow(ctx, `SELECT quantity FROM player_items WHERE player_id=$1 AND item_id=$2`, visitor.ID, itemID).Scan(&vQty)
	if vQty != 1 {
		t.Errorf("visitor qty = %d, want 1", vQty)
	}
	// 倉庫品は訪問者から買えない。
	if c := post(visitorPath+"/building/yami/buy", map[string]any{
		"house_id": houseID, "listing_id": whID, "idempotency_key": "yb2"}); c != http.StatusUnprocessableEntity {
		t.Errorf("visitor buy warehouse: %d, want 422", c)
	}
	// 家主は倉庫品を500円で回収できる(売上なし)。
	if c := post(ownerPath+"/building/yami/buy", map[string]any{
		"house_id": houseID, "listing_id": whID, "idempotency_key": "yb3"}); c != http.StatusOK {
		t.Fatalf("owner buyback: %d", c)
	}
	var feeSum int64
	pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account='system:yami_fee'`).Scan(&feeSum)
	if feeSum != 500 {
		t.Errorf("yami fee = %d, want 500", feeSum)
	}
	var oQty int
	pool.QueryRow(ctx, `SELECT quantity FROM player_items WHERE player_id=$1 AND item_id=$2`, owner.ID, itemID).Scan(&oQty)
	if oQty != 1 {
		t.Errorf("owner qty after buyback = %d, want 1", oQty)
	}

	// 売却時は残りの出品が持ち物へ戻る。
	if c := post(ownerPath+"/building/yami/list", map[string]any{
		"house_id": houseID, "item_id": itemID, "price": 2000, "idempotency_key": "yl3"}); c != http.StatusOK {
		t.Fatalf("relist: %d", c)
	}
	if _, c := sellHouse(t, srv.URL, owner.ID, houseID, "ys1"); c != http.StatusOK {
		t.Fatalf("sell: %d", c)
	}
	pool.QueryRow(ctx, `SELECT COALESCE(quantity,0) FROM player_items WHERE player_id=$1 AND item_id=$2`, owner.ID, itemID).Scan(&oQty)
	if oQty != 1 {
		t.Errorf("owner qty after sell = %d, want 1", oQty)
	}
	var leftover int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM yami_items WHERE house_id=$1`, houseID).Scan(&leftover)
	if leftover != 0 {
		t.Errorf("leftover listings = %d, want 0", leftover)
	}

	led := ledger.New(pool)
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

// TestBuildHouseHiddenTown covers the hidden-town build rules: building into a
// hidden town from outside is rejected, but a player currently in the hidden
// town may build there (街マップの空き地クリック導線).
func TestBuildHouseHiddenTown(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	p := register(t, srv.URL, "misskey.example", "hiddenbuilder")
	creditSavings(t, pool, p.ID, 30_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}})
	ts := building.DefaultTowns()
	ts[4].Hidden = true
	building.SetTowns(ts)
	defer building.SetTowns(building.DefaultTowns())

	// 隠し町の外からは建てられない。
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 0, 1, "house1", 3, "hb1"); c != http.StatusUnprocessableEntity {
		t.Errorf("hidden from outside: status=%d, want 422", c)
	}
	// 隠し町に現在いる場合は建てられる。
	if _, err := pool.Exec(ctx, `UPDATE players SET current_town=4 WHERE id=$1`, p.ID); err != nil {
		t.Fatalf("move player: %v", err)
	}
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 0, 1, "house1", 3, "hb2"); c != http.StatusOK {
		t.Errorf("hidden inside: status=%d, want 200", c)
	}
}

// creditCash tops up a player's cash via the ledger for tests.
func creditCash(t *testing.T, pool *pgxpool.Pool, playerID, amount int64) {
	t.Helper()
	ctx := context.Background()
	led := ledger.New(pool)
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return led.PostTx(ctx, tx, "test_cash", "", []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: amount},
			{Account: ledger.SystemAccount("test_faucet"), Delta: -amount},
		})
	})
	if err != nil {
		t.Fatalf("credit cash: %v", err)
	}
}

func rebuildHouse(t *testing.T, base string, playerID, houseID int64, exterior string, interiorRank int, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"house_id": houseID, "exterior": exterior, "interior_rank": interiorRank, "idempotency_key": idemKey,
	})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/rebuild",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("rebuild post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func sellHouse(t *testing.T, base string, playerID, houseID int64, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"house_id": houseID, "idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/sell",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("sell post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func TestSellRebuildHouse(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	register(t, srv.URL, "misskey.example", "admin0") // 1人目=admin
	p := register(t, srv.URL, "misskey.example", "builder2")
	creditSavings(t, pool, p.ID, 10_000_000)
	creditCash(t, pool, p.ID, 40_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}})

	// 建築(謎の街 A1, house1, D内装)。savings 10M-4M=6M。
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 0, 1, "house1", 3, "s1"); c != http.StatusOK {
		t.Fatalf("build: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1`, p.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}

	// 建て替え(house4, A内装)。費用=800×4×10000=32,000,000を現金から。
	got, c := rebuildHouse(t, srv.URL, p.ID, houseID, "house4", 0, "s2")
	if c != http.StatusOK {
		t.Fatalf("rebuild: %d", c)
	}
	// 現金 = 初期50万 + 4000万 - 3200万 = 8,500,000
	if got.Money != 8_500_000 {
		t.Errorf("cash after rebuild = %d, want 8500000", got.Money)
	}
	var (
		ext string
		ir  int
	)
	if err := pool.QueryRow(ctx, `SELECT exterior, interior_rank FROM player_houses WHERE id=$1`, houseID).Scan(&ext, &ir); err != nil {
		t.Fatalf("read house: %v", err)
	}
	if ext != "house4" || ir != 0 {
		t.Errorf("after rebuild: exterior=%q interior=%d, want house4/0", ext, ir)
	}

	// 売却。返金=謎の街地価250万×10000=2,500,000を現金へ。
	got2, c := sellHouse(t, srv.URL, p.ID, houseID, "s3")
	if c != http.StatusOK {
		t.Fatalf("sell: %d", c)
	}
	if got2.Money != 11_000_000 { // 8,500,000 + 2,500,000
		t.Errorf("cash after sell = %d, want 11000000", got2.Money)
	}
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM player_houses WHERE owner_id=$1`, p.ID).Scan(&cnt); err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Errorf("houses after sell = %d, want 0", cnt)
	}

	// 売却でマスは空地に戻り、同じ場所に再度建てられる。
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 0, 1, "house1", 3, "s4"); c != http.StatusOK {
		t.Errorf("rebuild on freed plot: %d", c)
	}

	led := ledger.New(pool)
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

func setHouseComment(t *testing.T, base string, playerID, houseID int64, setumei, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"house_id": houseID, "setumei": setumei, "idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/comment",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("comment post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func saisen(t *testing.T, base string, playerID, houseID, amount int64, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"house_id": houseID, "amount": amount, "idempotency_key": idemKey})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/saisen",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("saisen post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func TestSaisenAndComment(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	register(t, srv.URL, "misskey.example", "admin0")
	owner := register(t, srv.URL, "misskey.example", "owner1")
	visitor := register(t, srv.URL, "misskey.example", "visitor1")
	creditSavings(t, pool, owner.ID, 10_000_000)
	creditCash(t, pool, visitor.ID, 100_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}})

	// ownerが謎の街A1に家を建てる(savings 10M-5M=5M)。
	if _, c := buildHouse(t, srv.URL, owner.ID, 4, 0, 1, "house1", 3, "o1"); c != http.StatusOK {
		t.Fatalf("build: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1`, owner.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}

	// 家主がマウスオーバーコメントを設定。
	if _, c := setHouseComment(t, srv.URL, owner.ID, houseID, "いらっしゃい", "c1"); c != http.StatusOK {
		t.Fatalf("comment: %d", c)
	}
	var setumei string
	if err := pool.QueryRow(ctx, `SELECT setumei FROM player_houses WHERE id=$1`, houseID).Scan(&setumei); err != nil {
		t.Fatalf("read setumei: %v", err)
	}
	if setumei != "いらっしゃい" {
		t.Errorf("setumei = %q, want いらっしゃい", setumei)
	}

	// 自分の家へのさい銭も可(レガシー同様。現金→自分の普通口座)。
	if _, c := saisen(t, srv.URL, owner.ID, houseID, 100, "s0"); c != http.StatusOK {
		t.Errorf("self saisen: %d, want 200", c)
	}

	// 訪問者がさい銭(5000円): 現金-5000、家主の普通口座+5000。
	got, c := saisen(t, srv.URL, visitor.ID, houseID, 5000, "s1")
	if c != http.StatusOK {
		t.Fatalf("saisen: %d", c)
	}
	if got.Money != 595_000 { // 初期50万 + creditCash10万 - 5000
		t.Errorf("visitor cash = %d, want 595000", got.Money)
	}
	var ownerSavings int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		"savings:"+strconv.FormatInt(owner.ID, 10)).Scan(&ownerSavings); err != nil {
		t.Fatalf("owner savings: %v", err)
	}
	if ownerSavings != 6_005_100 { // 建築後6M + 自分さい銭100 + 5000
		t.Errorf("owner savings = %d, want 6005100", ownerSavings)
	}

	// 同一相手への上限(20000円/日)。5000×3をさらに積んで計20000、次の100は拒否。
	saisen(t, srv.URL, visitor.ID, houseID, 5000, "s2")
	saisen(t, srv.URL, visitor.ID, houseID, 5000, "s3")
	got4, c := saisen(t, srv.URL, visitor.ID, houseID, 5000, "s4")
	if c != http.StatusOK {
		t.Fatalf("saisen s4: %d", c)
	}
	if got4.Money != 580_000 { // 600000 - 20000
		t.Errorf("visitor cash after 20000 = %d, want 580000", got4.Money)
	}
	if _, c := saisen(t, srv.URL, visitor.ID, houseID, 100, "s5"); c != http.StatusUnprocessableEntity {
		t.Errorf("over per-target cap: %d, want 422", c)
	}

	led := ledger.New(pool)
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

func openShop(t *testing.T, base string, playerID, houseID int64, title, syubetu string, markup float64, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"house_id": houseID, "title": title, "syubetu": syubetu, "markup": markup, "idempotency_key": idemKey,
	})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/shop/open",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("open shop post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func TestOpenHouseShop(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	register(t, srv.URL, "misskey.example", "admin0")
	p := register(t, srv.URL, "misskey.example", "shopowner")
	creditSavings(t, pool, p.ID, 10_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}})
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 0, 1, "house1", 3, "b1"); c != http.StatusOK {
		t.Fatalf("build: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1`, p.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}

	// 店を開く(食料品, 掛け率2)。
	if _, c := openShop(t, srv.URL, p.ID, houseID, "駄菓子屋", "食料品", 2.0, "o1"); c != http.StatusOK {
		t.Fatalf("open shop: %d", c)
	}
	var (
		syubetu string
		markup  float64
	)
	if err := pool.QueryRow(ctx, `SELECT syubetu, markup FROM house_shops WHERE house_id=$1`, houseID).Scan(&syubetu, &markup); err != nil {
		t.Fatalf("read shop: %v", err)
	}
	if syubetu != "食料品" || markup != 2.0 {
		t.Errorf("shop = %q/%v, want 食料品/2", syubetu, markup)
	}

	// 掛け率の制約(0.3<率<=3)。
	if _, c := openShop(t, srv.URL, p.ID, houseID, "", "食料品", 3.5, "o2"); c != http.StatusUnprocessableEntity {
		t.Errorf("markup>3: %d, want 422", c)
	}
	if _, c := openShop(t, srv.URL, p.ID, houseID, "", "食料品", 0.2, "o3"); c != http.StatusUnprocessableEntity {
		t.Errorf("markup<=0.3: %d, want 422", c)
	}
	// 無効な種類。
	if _, c := openShop(t, srv.URL, p.ID, houseID, "", "存在しない種類", 2, "o4"); c != http.StatusUnprocessableEntity {
		t.Errorf("invalid syubetu: %d, want 422", c)
	}
	// 他人は所有していない家に店を開けない。
	visitor := register(t, srv.URL, "misskey.example", "shopvisitor")
	if _, c := openShop(t, srv.URL, visitor.ID, houseID, "", "食料品", 2, "o5"); c != http.StatusUnprocessableEntity {
		t.Errorf("non-owner: %d, want 422", c)
	}
}

func shiire(t *testing.T, base string, playerID, houseID, itemID int64, qty int, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"house_id": houseID, "item_id": itemID, "qty": qty, "idempotency_key": idemKey,
	})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/shiire",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("shiire post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func TestShiire(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	register(t, srv.URL, "misskey.example", "admin0")
	p := register(t, srv.URL, "misskey.example", "shopowner2")
	creditSavings(t, pool, p.ID, 20_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}})
	if _, c := buildHouse(t, srv.URL, p.ID, 4, 0, 1, "house1", 3, "b1"); c != http.StatusOK {
		t.Fatalf("build: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1`, p.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}
	if _, c := openShop(t, srv.URL, p.ID, houseID, "本屋", "書籍", 2.0, "o1"); c != http.StatusOK {
		t.Fatalf("open shop: %d", c)
	}

	var (
		itemID int64
		price  int64
	)
	if err := pool.QueryRow(ctx,
		`SELECT id, price FROM content_items WHERE category='書籍' AND facility='' AND enabled ORDER BY price LIMIT 1`).
		Scan(&itemID, &price); err != nil {
		t.Fatalf("pick book: %v", err)
	}

	// 書籍を3個仕入れる。普通口座 -= price*3。
	got, c := shiire(t, srv.URL, p.ID, houseID, itemID, 3, "s1")
	if c != http.StatusOK {
		t.Fatalf("shiire: %d", c)
	}
	if got.Savings != 20_000_000-4_000_000-price*3 { // 建築後16M - 仕入れ
		t.Errorf("savings = %d, want %d", got.Savings, 16_000_000-price*3)
	}
	var (
		stock    int
		buyPrice int64
	)
	if err := pool.QueryRow(ctx,
		`SELECT stock, buy_price FROM house_shop_stock WHERE house_id=$1 AND item_id=$2`, houseID, itemID).
		Scan(&stock, &buyPrice); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if stock != 3 || buyPrice != price {
		t.Errorf("stock=%d buy_price=%d, want 3/%d", stock, buyPrice, price)
	}

	// 書籍店に食料品は仕入れられない。
	var foodID int64
	if err := pool.QueryRow(ctx,
		`SELECT id FROM content_items WHERE category='食料品' AND facility='' AND enabled LIMIT 1`).Scan(&foodID); err != nil {
		t.Fatalf("pick food: %v", err)
	}
	if _, c := shiire(t, srv.URL, p.ID, houseID, foodID, 1, "s2"); c != http.StatusUnprocessableEntity {
		t.Errorf("wrong category: %d, want 422", c)
	}

	// 在庫上限80超過(3+79=82)。
	if _, c := shiire(t, srv.URL, p.ID, houseID, itemID, 79, "s3"); c != http.StatusUnprocessableEntity {
		t.Errorf("over stock limit: %d, want 422", c)
	}

	led := ledger.New(pool)
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

func buyFromShop(t *testing.T, base string, playerID, houseID, itemID int64, qty int, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"house_id": houseID, "item_id": itemID, "qty": qty, "idempotency_key": idemKey,
	})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/shop/buy",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("buy post: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func TestBuyFromHouseShop(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	register(t, srv.URL, "misskey.example", "admin0")
	owner := register(t, srv.URL, "misskey.example", "shopowner3")
	visitor := register(t, srv.URL, "misskey.example", "buyer1")
	creditSavings(t, pool, owner.ID, 20_000_000)
	creditCash(t, pool, visitor.ID, 1_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}})
	if _, c := buildHouse(t, srv.URL, owner.ID, 4, 0, 1, "house1", 3, "b1"); c != http.StatusOK {
		t.Fatalf("build: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1`, owner.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}
	if _, c := openShop(t, srv.URL, owner.ID, houseID, "本屋", "書籍", 2.0, "o1"); c != http.StatusOK {
		t.Fatalf("open shop: %d", c)
	}
	var (
		itemID int64
		price  int64
	)
	if err := pool.QueryRow(ctx,
		`SELECT id, price FROM content_items WHERE category='書籍' AND facility='' AND enabled ORDER BY price LIMIT 1`).
		Scan(&itemID, &price); err != nil {
		t.Fatalf("pick book: %v", err)
	}
	if _, c := shiire(t, srv.URL, owner.ID, houseID, itemID, 3, "s1"); c != http.StatusOK {
		t.Fatalf("shiire: %d", c)
	}

	// コンテンツ枠に「お店」を設定していない家では買えない。
	if _, c := buyFromShop(t, srv.URL, visitor.ID, houseID, itemID, 1, "buy0"); c != http.StatusUnprocessableEntity {
		t.Errorf("buy before shop content: %d, want 422", c)
	}
	if _, c := setContents(t, srv.URL, owner.ID, houseID,
		[]map[string]any{{"slot": 0, "kind": "shop"}}, "c1"); c != http.StatusOK {
		t.Fatalf("set shop content: %d", c)
	}

	// 一度に買えるのは4個まで(レガシー item_kosuuseigen)。
	if _, c := buyFromShop(t, srv.URL, visitor.ID, houseID, itemID, 5, "buy5"); c != http.StatusUnprocessableEntity {
		t.Errorf("buy over qty limit: %d, want 422", c)
	}

	// 訪問者が2個購入。店頭価格 = 仕入れ値×掛け率2。
	shelf := price * 2
	got, c := buyFromShop(t, srv.URL, visitor.ID, houseID, itemID, 2, "buy1")
	if c != http.StatusOK {
		t.Fatalf("buy: %d", c)
	}
	if got.Money != 1_500_000-shelf*2 { // 初期50万 + credit100万 - 店頭価格×2
		t.Errorf("buyer cash = %d, want %d", got.Money, 1_500_000-shelf*2)
	}
	var stock int
	if err := pool.QueryRow(ctx, `SELECT stock FROM house_shop_stock WHERE house_id=$1 AND item_id=$2`, houseID, itemID).Scan(&stock); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if stock != 1 { // 3 - 2
		t.Errorf("shop stock = %d, want 1", stock)
	}
	var qty int
	if err := pool.QueryRow(ctx, `SELECT quantity FROM player_items WHERE player_id=$1 AND item_id=$2`, visitor.ID, itemID).Scan(&qty); err != nil {
		t.Fatalf("read buyer items: %v", err)
	}
	if qty != 2 {
		t.Errorf("buyer items = %d, want 2", qty)
	}
	// 家主の売上(普通口座)= 建築後15M - 仕入れ + 売上。
	var ownerSav int64
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		"savings:"+strconv.FormatInt(owner.ID, 10)).Scan(&ownerSav); err != nil {
		t.Fatalf("owner savings: %v", err)
	}
	if ownerSav != 16_000_000-price*3+shelf*2 {
		t.Errorf("owner savings = %d, want %d", ownerSav, 16_000_000-price*3+shelf*2)
	}

	// 自分の店では買えない。
	if _, c := buyFromShop(t, srv.URL, owner.ID, houseID, itemID, 1, "buy2"); c != http.StatusUnprocessableEntity {
		t.Errorf("self buy: %d, want 422", c)
	}
	// 在庫不足(残1に4個)。
	if _, c := buyFromShop(t, srv.URL, visitor.ID, houseID, itemID, 4, "buy3"); c != http.StatusUnprocessableEntity {
		t.Errorf("over stock: %d, want 422", c)
	}

	// ご近所キャッシュバック: 買い手が店と同じ街(4)に家を建てると単価10%引き。
	seedPlots(t, pool, [][3]int{{4, 1, 5}})
	creditSavings(t, pool, visitor.ID, 6_000_000)
	if _, c := buildHouse(t, srv.URL, visitor.ID, 4, 1, 5, "house1", 3, "vb1"); c != http.StatusOK {
		t.Fatalf("visitor build: %d", c)
	}
	moneyBefore := 1_500_000 - shelf*2
	cashback := (shelf / 10) * 1
	br, got2, c := buyFromShopFull(t, srv.URL, visitor.ID, houseID, itemID, 1, "cash", "buy4")
	if c != http.StatusOK {
		t.Fatalf("neighbor buy: %d", c)
	}
	if br.Cashback != cashback {
		t.Errorf("cashback = %d, want %d", br.Cashback, cashback)
	}
	if got2.Money != moneyBefore-(shelf-cashback) {
		t.Errorf("neighbor buyer money = %d, want %d", got2.Money, moneyBefore-(shelf-cashback))
	}

	// クレジット払い: クレジットカード所持で普通口座から支払える。
	if _, c := shiire(t, srv.URL, owner.ID, houseID, itemID, 2, "s2"); c != http.StatusOK {
		t.Fatalf("restock: %d", c)
	}
	// カード無しはエラー。
	if _, _, c := buyFromShopFull(t, srv.URL, visitor.ID, houseID, itemID, 1, "credit", "buy5c"); c != http.StatusUnprocessableEntity {
		t.Errorf("credit without card: %d, want 422", c)
	}
	giveItem(t, pool, visitor.ID, "クレジットカード", 1)
	var savBefore int64
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account=$1`,
		"savings:"+strconv.FormatInt(visitor.ID, 10)).Scan(&savBefore); err != nil {
		t.Fatalf("visitor savings: %v", err)
	}
	br2, got3, c := buyFromShopFull(t, srv.URL, visitor.ID, houseID, itemID, 1, "credit", "buy6")
	if c != http.StatusOK {
		t.Fatalf("credit buy: %d", c)
	}
	if br2.Method != "credit" {
		t.Errorf("method = %s, want credit", br2.Method)
	}
	if got3.Savings != savBefore-br2.Paid {
		t.Errorf("savings after credit = %d, want %d", got3.Savings, savBefore-br2.Paid)
	}

	led := ledger.New(pool)
	if sum, _ := led.AuditZeroSum(ctx); sum != 0 {
		t.Errorf("ledger zero-sum broken: %d", sum)
	}
}

// buyResultT mirrors the buy_result payload of the shop-buy response.
type buyResultT struct {
	Total    int64  `json:"total"`
	Cashback int64  `json:"cashback"`
	Paid     int64  `json:"paid"`
	Method   string `json:"method"`
}

// buyFromShopFull buys with an explicit pay method and returns the buy_result.
func buyFromShopFull(t *testing.T, base string, playerID, houseID, itemID int64, qty int, method, idemKey string) (buyResultT, playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"house_id": houseID, "item_id": itemID, "qty": qty, "pay_method": method, "idempotency_key": idemKey,
	})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/shop/buy",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("buy post: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		playerResp
		BuyResult buyResultT `json:"buy_result"`
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return out.BuyResult, out.playerResp, resp.StatusCode
}

func postBbs(t *testing.T, base string, playerID, houseID int64, kind, body, idemKey string) (playerResp, int) {
	t.Helper()
	return postBbsReply(t, base, playerID, houseID, kind, body, 0, idemKey)
}

// postBbsReply posts with an explicit parent thread NO (通常掲示板のレス).
func postBbsReply(t *testing.T, base string, playerID, houseID int64, kind, body string, parentNo int, idemKey string) (playerResp, int) {
	t.Helper()
	reqBody, _ := json.Marshal(map[string]any{
		"house_id": houseID, "kind": kind, "body": body, "parent_no": parentNo, "idempotency_key": idemKey,
	})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/bbs/post",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post bbs: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func deleteBbs(t *testing.T, base string, playerID, houseID int64, kind string, articleNo int64, idemKey string) (playerResp, int) {
	t.Helper()
	return deleteBbsFull(t, base, playerID, houseID, kind, articleNo, 0, false, idemKey)
}

// deleteBbsFull covers 親記事no.(スレッドごと)と全記事削除も指定できる版。
func deleteBbsFull(t *testing.T, base string, playerID, houseID int64, kind string, articleNo, threadNo int64, all bool, idemKey string) (playerResp, int) {
	t.Helper()
	reqBody, _ := json.Marshal(map[string]any{
		"house_id": houseID, "kind": kind, "article_no": articleNo, "thread_no": threadNo,
		"all": all, "idempotency_key": idemKey,
	})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/bbs/delete",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("delete bbs: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

// setContents replaces a house's content slots (コンテンツ枠設定).
func setContents(t *testing.T, base string, playerID, houseID int64, contents []map[string]any, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"house_id": houseID, "contents": contents, "idempotency_key": idemKey,
	})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/contents",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("set contents: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func TestHouseContents(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	register(t, srv.URL, "misskey.example", "admin0")
	owner := register(t, srv.URL, "misskey.example", "contentowner")
	other := register(t, srv.URL, "misskey.example", "contentother")
	creditSavings(t, pool, owner.ID, 10_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}})
	// Dランク(3)=1枠の家。
	if _, c := buildHouse(t, srv.URL, owner.ID, 4, 0, 1, "house1", 3, "b1"); c != http.StatusOK {
		t.Fatalf("build: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1`, owner.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}

	// 枠0に掲示板を設定できる。
	if _, c := setContents(t, srv.URL, owner.ID, houseID,
		[]map[string]any{{"slot": 0, "kind": "bbs", "title": "みんなの板"}}, "c1"); c != http.StatusOK {
		t.Fatalf("set slot0: %d", c)
	}
	// Dランクは1枠なので枠1は指定できない。
	if _, c := setContents(t, srv.URL, owner.ID, houseID,
		[]map[string]any{{"slot": 1, "kind": "bbs"}}, "c2"); c != http.StatusUnprocessableEntity {
		t.Errorf("slot over limit: %d, want 422", c)
	}
	// 不正な種類は拒否。
	if _, c := setContents(t, srv.URL, owner.ID, houseID,
		[]map[string]any{{"slot": 0, "kind": "casino"}}, "c3"); c != http.StatusUnprocessableEntity {
		t.Errorf("bad kind: %d, want 422", c)
	}
	// 他人の家は設定できない。
	if _, c := setContents(t, srv.URL, other.ID, houseID,
		[]map[string]any{{"slot": 0, "kind": "bbs"}}, "c4"); c != http.StatusUnprocessableEntity {
		t.Errorf("non-owner: %d, want 422", c)
	}
	// 独自URLはhttp/https以外を拒否。
	if _, c := setContents(t, srv.URL, owner.ID, houseID,
		[]map[string]any{{"slot": 0, "kind": "url", "url": "javascript:alert(1)"}}, "c5"); c != http.StatusUnprocessableEntity {
		t.Errorf("bad url scheme: %d, want 422", c)
	}
	if _, c := setContents(t, srv.URL, owner.ID, houseID,
		[]map[string]any{{"slot": 0, "kind": "url", "title": "リンク", "url": "https://example.com/"}}, "c6"); c != http.StatusOK {
		t.Errorf("good url: %d, want 200", c)
	}
	// 最後に掲示板へ戻す(後段の検証用)。
	if _, c := setContents(t, srv.URL, owner.ID, houseID,
		[]map[string]any{{"slot": 0, "kind": "bbs", "title": "みんなの板"}}, "c7"); c != http.StatusOK {
		t.Fatalf("restore bbs: %d", c)
	}
	// 設定はbuilding APIのcontentsに反映される(掲示板のみ)。
	var kind, title string
	if err := pool.QueryRow(ctx,
		`SELECT kind, title FROM house_contents WHERE house_id=$1 AND slot=0`, houseID).Scan(&kind, &title); err != nil {
		t.Fatalf("read content: %v", err)
	}
	if kind != "bbs" || title != "みんなの板" {
		t.Errorf("content = %s/%s, want bbs/みんなの板", kind, title)
	}
}

func TestHouseBbs(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	register(t, srv.URL, "misskey.example", "admin0")
	owner := register(t, srv.URL, "misskey.example", "bbsowner")
	visitor := register(t, srv.URL, "misskey.example", "bbsvisitor")
	creditSavings(t, pool, owner.ID, 10_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}})
	// Cランク(2)=2枠(掲示板+家主板を公開するため)。
	if _, c := buildHouse(t, srv.URL, owner.ID, 4, 0, 1, "house1", 2, "b1"); c != http.StatusOK {
		t.Fatalf("build: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1`, owner.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}

	// コンテンツ枠未設定の掲示板には書き込めない(新築時は非公開)。
	if _, c := postBbs(t, srv.URL, visitor.ID, houseID, "normal", "まだ無い", "p0"); c != http.StatusUnprocessableEntity {
		t.Errorf("post before contents: %d, want 422", c)
	}
	// 枠に掲示板と家主板を設定。
	if _, c := setContents(t, srv.URL, owner.ID, houseID,
		[]map[string]any{{"slot": 0, "kind": "bbs"}, {"slot": 1, "kind": "nushi"}}, "c1"); c != http.StatusOK {
		t.Fatalf("set contents: %d", c)
	}

	// 訪問者が通常掲示板に投稿。
	if _, c := postBbs(t, srv.URL, visitor.ID, houseID, "normal", "こんにちは", "p1"); c != http.StatusOK {
		t.Fatalf("post normal: %d", c)
	}
	// 家主が家主板に投稿。
	if _, c := postBbs(t, srv.URL, owner.ID, houseID, "nushi", "家主です", "p2"); c != http.StatusOK {
		t.Fatalf("post nushi: %d", c)
	}
	// 非家主は家主板に書けない。
	if _, c := postBbs(t, srv.URL, visitor.ID, houseID, "nushi", "だめ", "p3"); c != http.StatusUnprocessableEntity {
		t.Errorf("visitor nushi: %d, want 422", c)
	}

	var normalCnt, nushiCnt int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM house_bbs WHERE house_id=$1 AND kind='normal'`, houseID).Scan(&normalCnt)
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM house_bbs WHERE house_id=$1 AND kind='nushi'`, houseID).Scan(&nushiCnt)
	if normalCnt != 1 || nushiCnt != 1 {
		t.Errorf("counts = %d/%d, want 1/1", normalCnt, nushiCnt)
	}

	// visitorの投稿を取得。
	var postID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM house_bbs WHERE author_id=$1`, visitor.ID).Scan(&postID); err != nil {
		t.Fatalf("post id: %v", err)
	}
	// 無関係の第三者は削除できない(家主・管理者のみ)。
	other := register(t, srv.URL, "misskey.example", "bbsother")
	if _, c := deleteBbs(t, srv.URL, other.ID, houseID, "normal", postID, "d1"); c != http.StatusUnprocessableEntity {
		t.Errorf("other delete: %d, want 422", c)
	}
	// 投稿した本人でも通常掲示板は削除できない(レガシー: 家主・管理者のみ)。
	if _, c := deleteBbs(t, srv.URL, visitor.ID, houseID, "normal", postID, "d1b"); c != http.StatusUnprocessableEntity {
		t.Errorf("author delete: %d, want 422", c)
	}
	// 家主は記事no.指定で訪問者の投稿を削除できる。
	if _, c := deleteBbs(t, srv.URL, owner.ID, houseID, "normal", postID, "d2"); c != http.StatusOK {
		t.Fatalf("owner delete: %d", c)
	}
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM house_bbs WHERE house_id=$1 AND kind='normal'`, houseID).Scan(&normalCnt)
	if normalCnt != 0 {
		t.Errorf("normal after delete = %d, want 0", normalCnt)
	}
}

// TestHouseBbsThreads covers the legacy normal_bbs spec: threaded replies with
// NO.x numbering, the per-post money reward, 二重投稿 rejection, the weighted
// length limit, and deletion by 記事no./親記事no./全記事削除.
func TestHouseBbsThreads(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	register(t, srv.URL, "misskey.example", "thadmin")
	owner := register(t, srv.URL, "misskey.example", "throwner")
	visitor := register(t, srv.URL, "misskey.example", "thvisitor")
	creditSavings(t, pool, owner.ID, 10_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}})
	if _, c := buildHouse(t, srv.URL, owner.ID, 4, 0, 1, "house1", 2, "b1"); c != http.StatusOK {
		t.Fatalf("build: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1`, owner.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}
	if _, c := setContents(t, srv.URL, owner.ID, houseID,
		[]map[string]any{{"slot": 0, "kind": "bbs"}, {"slot": 1, "kind": "nushi"}}, "c1"); c != http.StatusOK {
		t.Fatalf("set contents: %d", c)
	}
	cashOf := func(id int64) int64 {
		var m int64
		if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger_entry WHERE account = $1`,
			fmt.Sprintf("player:%d", id)).Scan(&m); err != nil {
			t.Fatalf("cash of %d: %v", id, err)
		}
		return m
	}

	// 投稿でお金がもらえる(通常1001〜3009円、ボーナス時最大15006円)。
	before := cashOf(visitor.ID)
	if _, c := postBbs(t, srv.URL, visitor.ID, houseID, "normal", "スレ1本文", "t1"); c != http.StatusOK {
		t.Fatalf("post thread1: %d", c)
	}
	reward := cashOf(visitor.ID) - before
	if reward < 1001 || reward > 15006 {
		t.Errorf("reward = %d, want 1001..15006", reward)
	}
	// 二重投稿(同一人物・同一本文)は拒否。
	if _, c := postBbs(t, srv.URL, visitor.ID, houseID, "normal", "スレ1本文", "t1b"); c != http.StatusUnprocessableEntity {
		t.Errorf("duplicate post: %d, want 422", c)
	}
	// 全角100字超は拒否(重み付き200)。
	long := ""
	for i := 0; i < 101; i++ {
		long += "あ"
	}
	if _, c := postBbs(t, srv.URL, visitor.ID, houseID, "normal", long, "t1c"); c != http.StatusUnprocessableEntity {
		t.Errorf("long post: %d, want 422", c)
	}
	if _, c := postBbs(t, srv.URL, visitor.ID, houseID, "normal", "スレ2本文", "t2"); c != http.StatusOK {
		t.Fatalf("post thread2: %d", c)
	}
	// スレッドNO.1へのレス。存在しないスレッドへのレスは拒否。
	if _, c := postBbsReply(t, srv.URL, owner.ID, houseID, "normal", "レスです", 1, "r1"); c != http.StatusOK {
		t.Fatalf("reply: %d", c)
	}
	if _, c := postBbsReply(t, srv.URL, owner.ID, houseID, "normal", "宛先なし", 99, "r2"); c != http.StatusUnprocessableEntity {
		t.Errorf("reply to missing thread: %d, want 422", c)
	}
	var parents, replies int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM house_bbs WHERE house_id=$1 AND kind='normal' AND thread_no IS NOT NULL`, houseID).Scan(&parents)
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM house_bbs WHERE house_id=$1 AND kind='normal' AND parent_no=1`, houseID).Scan(&replies)
	if parents != 2 || replies != 1 {
		t.Fatalf("parents/replies = %d/%d, want 2/1", parents, replies)
	}

	// レスが付いた親記事は記事no.指定では削除できない。
	var parent1ID int64
	pool.QueryRow(ctx, `SELECT id FROM house_bbs WHERE house_id=$1 AND kind='normal' AND thread_no=1`, houseID).Scan(&parent1ID)
	if _, c := deleteBbs(t, srv.URL, owner.ID, houseID, "normal", parent1ID, "d1"); c != http.StatusUnprocessableEntity {
		t.Errorf("delete parent with reply: %d, want 422", c)
	}
	// 親記事no.指定でスレッドごと削除できる。
	if _, c := deleteBbsFull(t, srv.URL, owner.ID, houseID, "normal", 0, 1, false, "d2"); c != http.StatusOK {
		t.Fatalf("delete thread: %d", c)
	}
	var cnt int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM house_bbs WHERE house_id=$1 AND kind='normal'`, houseID).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("after thread delete = %d, want 1", cnt)
	}
	// 通常掲示板は書いた本人でも削除できない(家主・管理者のみ)。
	var parent2ID int64
	pool.QueryRow(ctx, `SELECT id FROM house_bbs WHERE house_id=$1 AND kind='normal' AND thread_no=2`, houseID).Scan(&parent2ID)
	if _, c := deleteBbs(t, srv.URL, visitor.ID, houseID, "normal", parent2ID, "d3"); c != http.StatusUnprocessableEntity {
		t.Errorf("author delete: %d, want 422", c)
	}
	// 全記事削除。
	if _, c := deleteBbsFull(t, srv.URL, owner.ID, houseID, "normal", 0, 0, true, "d4"); c != http.StatusOK {
		t.Fatalf("delete all: %d", c)
	}
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM house_bbs WHERE house_id=$1 AND kind='normal'`, houseID).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("after all delete = %d, want 0", cnt)
	}

	// 家主板: 家主が投稿し、記事no.指定で本人(家主)が削除できる。他人は不可。
	if _, c := postBbs(t, srv.URL, owner.ID, houseID, "nushi", "家主記事", "n1"); c != http.StatusOK {
		t.Fatalf("post nushi: %d", c)
	}
	var nushiID int64
	pool.QueryRow(ctx, `SELECT id FROM house_bbs WHERE house_id=$1 AND kind='nushi'`, houseID).Scan(&nushiID)
	if _, c := deleteBbs(t, srv.URL, visitor.ID, houseID, "nushi", nushiID, "n2"); c != http.StatusUnprocessableEntity {
		t.Errorf("visitor nushi delete: %d, want 422", c)
	}
	if _, c := deleteBbs(t, srv.URL, owner.ID, houseID, "nushi", nushiID, "n3"); c != http.StatusOK {
		t.Fatalf("owner nushi delete: %d", c)
	}
}

func setPrice(t *testing.T, base string, playerID, houseID, itemID, sellPrice int64, idemKey string) (playerResp, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"house_id": houseID, "item_id": itemID, "sell_price": sellPrice, "idempotency_key": idemKey,
	})
	resp, err := http.Post(base+"/api/v1/players/"+strconv.FormatInt(playerID, 10)+"/building/shop/price",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("set price: %v", err)
	}
	defer resp.Body.Close()
	var p playerResp
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return p, resp.StatusCode
}

func TestSetShopPrice(t *testing.T) {
	srv, pool := setup(t)
	ctx := context.Background()
	register(t, srv.URL, "misskey.example", "admin0")
	owner := register(t, srv.URL, "misskey.example", "priceowner")
	creditSavings(t, pool, owner.ID, 10_000_000)
	seedPlots(t, pool, [][3]int{{4, 0, 1}})
	if _, c := buildHouse(t, srv.URL, owner.ID, 4, 0, 1, "house1", 3, "b1"); c != http.StatusOK {
		t.Fatalf("build: %d", c)
	}
	var houseID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM player_houses WHERE owner_id=$1`, owner.ID).Scan(&houseID); err != nil {
		t.Fatalf("house id: %v", err)
	}
	if _, c := openShop(t, srv.URL, owner.ID, houseID, "本屋", "書籍", 2.0, "o1"); c != http.StatusOK {
		t.Fatalf("open shop: %d", c)
	}
	var (
		itemID int64
		price  int64
	)
	if err := pool.QueryRow(ctx,
		`SELECT id, price FROM content_items WHERE category='書籍' AND facility='' AND enabled ORDER BY price LIMIT 1`).
		Scan(&itemID, &price); err != nil {
		t.Fatalf("pick book: %v", err)
	}
	if _, c := shiire(t, srv.URL, owner.ID, houseID, itemID, 3, "s1"); c != http.StatusOK {
		t.Fatalf("shiire: %d", c)
	}

	// 個別価格を仕入れ値×2.5に設定(×3以内)。
	newPrice := price * 5 / 2
	if _, c := setPrice(t, srv.URL, owner.ID, houseID, itemID, newPrice, "sp1"); c != http.StatusOK {
		t.Fatalf("set price: %d", c)
	}
	var sellPrice *int64
	if err := pool.QueryRow(ctx, `SELECT sell_price FROM house_shop_stock WHERE house_id=$1 AND item_id=$2`, houseID, itemID).Scan(&sellPrice); err != nil {
		t.Fatalf("read sell_price: %v", err)
	}
	if sellPrice == nil || *sellPrice != newPrice {
		t.Errorf("sell_price = %v, want %d", sellPrice, newPrice)
	}

	// 仕入れ値×3超過は拒否。
	if _, c := setPrice(t, srv.URL, owner.ID, houseID, itemID, price*4, "sp2"); c != http.StatusUnprocessableEntity {
		t.Errorf("over max: %d, want 422", c)
	}

	// 0で掛け率に戻す(sell_price=NULL)。
	if _, c := setPrice(t, srv.URL, owner.ID, houseID, itemID, 0, "sp3"); c != http.StatusOK {
		t.Fatalf("clear price: %d", c)
	}
	if err := pool.QueryRow(ctx, `SELECT sell_price FROM house_shop_stock WHERE house_id=$1 AND item_id=$2`, houseID, itemID).Scan(&sellPrice); err != nil {
		t.Fatalf("read sell_price: %v", err)
	}
	if sellPrice != nil {
		t.Errorf("sell_price after clear = %d, want nil", *sellPrice)
	}

	// 他人は設定できない。
	other := register(t, srv.URL, "misskey.example", "priceother")
	if _, c := setPrice(t, srv.URL, other.ID, houseID, itemID, 100, "sp4"); c != http.StatusUnprocessableEntity {
		t.Errorf("other set: %d, want 422", c)
	}
}
