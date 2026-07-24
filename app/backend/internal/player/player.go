// Package player implements player registration and status reads. Money is
// always read through the ledger; it is never stored as a mutable column.
package player

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/condition"
	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/news"
	"github.com/shiroha-a/town/internal/rng"
	"github.com/shiroha-a/town/internal/settings"
)

// Player is the aggregate returned to callers.
type Player struct {
	ID           int64
	InstanceHost string
	RemoteUserID string
	DisplayName  string
	Roles        []string
	Money        int64
	Savings      int64
	SuperSavings int64
	LoanDaily    int64 // 住宅ローンの日額返済(なければ0)
	LoanCount    int   // 住宅ローンの残り返済回数(なければ0)
	CurrentTown   int       // 現在いる街(0=公園..4=謎の街)。街移動で変化
	CreatedAt     time.Time // 入居日(役場の名鑑/プロフィールで在住日数を出す)
	Status        Status
	Params        Params
	Items         []ItemStack
	ItemKindLimit int // 所持できるアイテムの種類上限(0=無制限。表示用)
}

// Params holds the detailed player parameters shown on the main screen.
// アダルト系(エッチ)はリライトで排除済み。
type Params struct {
	// 頭脳(教科)
	Kokugo  int
	Suugaku int
	Rika    int
	Syakai  int
	Eigo    int
	Ongaku  int
	Bijutsu int
	// 身体
	Looks      int
	Tairyoku   int
	Kenkou     int
	Speed      int
	Power      int
	Wanryoku   int
	Kyakuryoku int
	// その他
	Love       int
	Omoshirosa int
}

// ItemStack is one held item with its quantity and use-effect summary.
type ItemStack struct {
	ItemID          int64
	Name            string
	Category        string // 一覧のカテゴリ見出し(未分類は空)
	Quantity        int
	RemainingUses   int            // 残量('use'=残り総使用回数, 'day'=残日数)
	Sets            int            // 表示セット数 = ceil(remaining_uses/durability)
	DurabilityUnit  string         // 'use'(回) or 'day'(日)
	Money           int64          // 使用時のお金増減
	Params          map[string]int // 使用時の上昇パラメータ
	IntervalMin     int            // 使用間隔(分)
	CalorieG        int            // 摂取カロリー(食べると体重+calorie_g g)
	NextAvailableAt *time.Time     // クールタイム中の再使用可能時刻(未使用/経過済みはnil)
}

// Status holds the current gameplay stats.
type Status struct {
	Energy          int
	EnergyMax       int
	NouEnergy       int
	NouEnergyMax    int
	Job             string
	JobLevel        int
	JobExp          int        // 現在の職業の総経験値
	JobKaisuu       int        // 現在の職業での累計勤務回数
	MasteredJobs    []string   // マスター済み職業(レベル15到達で追加)
	Satiety         int        // 空腹値(満腹度 0-100)
	HeightCm        int        // 身長cm
	WeightG         int        // 体重g(表示はweight_g/1000でkg)
	BMI             int        // 体格指数(weight/height^2の整数切り捨て、旧check_BMI準拠)
	BodyType        string     // 体型ラベル(BMIからの派生)
	DiseaseIndex    int        // 病気指数(基準50、0未満で発病)
	DiseaseName     string     // 病名(病気指数からの派生、健康なら空)
	Condition       string     // コンディション表示ラベル(病名があれば病名、なければ体調ラベル)
	WorkAvailableAt *time.Time // 就労クールタイム中の再就労可能時刻(可能ならnil)
	EnergyFullAt    *time.Time // 身体パワーが満タンになる時刻(満タン時はnil)
	NouEnergyFullAt *time.Time // 頭脳パワーが満タンになる時刻(満タン時はnil)
	OnsenMultiplier int        // 入浴中の回復倍率(1=入浴していない)
}

// Service is the player domain service.
type Service struct {
	pool     *pgxpool.Pool
	ledger   *ledger.Repo
	rng      *rng.Rand
	settings *settings.Store
}

func New(pool *pgxpool.Pool, l *ledger.Repo, r *rng.Rand, st *settings.Store) *Service {
	return &Service{pool: pool, ledger: l, rng: r, settings: st}
}

// ErrNotFound is returned when a player does not exist.
var ErrNotFound = errors.New("player not found")

// Register creates a new player, or returns the existing one if the identity
// (instance_host, remote_user_id) is already registered. The very first player
// in the world is granted the admin role. New players receive the initial money
// grant through the ledger (idempotent by ref).
func (s *Service) Register(ctx context.Context, instanceHost, remoteUserID, displayName string) (*Player, error) {
	var (
		id      int64
		created bool
	)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var count int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM players`).Scan(&count); err != nil {
			return fmt.Errorf("count players: %w", err)
		}

		err := tx.QueryRow(ctx,
			`INSERT INTO players (instance_host, remote_user_id, display_name)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (instance_host, remote_user_id) DO NOTHING
			 RETURNING id`, instanceHost, remoteUserID, displayName).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			// 既存プレイヤー: 既存IDを引く
			return tx.QueryRow(ctx,
				`SELECT id FROM players WHERE instance_host = $1 AND remote_user_id = $2`,
				instanceHost, remoteUserID).Scan(&id)
		}
		if err != nil {
			return fmt.Errorf("insert player: %w", err)
		}
		created = true

		// 初期身長/体重はサーバRNGで生成する(design 17.3。性別未実装のため旧の女性テーブル)。
		heightCm := 150 + s.rng.IntN(25)        // 150〜174cm
		weightG := (48 + s.rng.IntN(20)) * 1000 // 48〜67kg
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_status (player_id, height_cm, weight_g) VALUES ($1, $2, $3)`,
			id, heightCm, weightG); err != nil {
			return fmt.Errorf("insert player_status: %w", err)
		}
		// パワー上限を初期パラメータから導出し、現在値を上限にクランプする。
		if err := RefreshPowerMax(ctx, tx, id); err != nil {
			return err
		}

		// 最初のプレイヤーは管理者ロールを付与
		role := "user"
		if count == 0 {
			role = "admin"
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_roles (player_id, role) VALUES ($1, $2)`, id, role); err != nil {
			return fmt.Errorf("insert player_roles: %w", err)
		}
		// 街のニュース(レガシー game.cgi の news_kiroku("入居", ...))。
		return news.RecordFor(ctx, tx, news.KindMoveIn, id, displayName,
			fmt.Sprintf("%sさんが新しい住民になりました。", displayName), nil, true)
	})
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}

	if created {
		ref := fmt.Sprintf("initial_grant:%d", id)
		initialMoney := s.settings.Get().InitialMoney
		if err := s.ledger.Post(ctx, "initial_grant", ref, []ledger.Entry{
			{Account: ledger.SystemAccount("initial_grant"), Delta: -initialMoney},
			{Account: ledger.PlayerAccount(id), Delta: initialMoney},
		}); err != nil {
			return nil, fmt.Errorf("initial grant: %w", err)
		}
	}

	return s.Get(ctx, id)
}

// RefreshPowerMax recomputes energy_max / nou_energy_max from the player's
// parameters using the legacy basic.cgi formula and clamps the current
// energy / nou_energy to the new maxima. It must run at registration and after
// any parameter change so the derived maxima (and the condition percentages that
// use them) stay in sync. energy_max/nou_energy_max are thus parameter-derived
// rather than a fixed cap (design 17.9).
func RefreshPowerMax(ctx context.Context, tx pgx.Tx, playerID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE player_status ps SET
			energy_max = m.emax, nou_energy_max = m.nmax,
			energy = LEAST(ps.energy, m.emax),
			nou_energy = LEAST(ps.nou_energy, m.nmax),
			updated_at = now()
		FROM (
			SELECT player_id,
				(FLOOR(looks/12.0 + tairyoku/4.0 + kenkou/4.0 + speed/8.0
				       + power/8.0 + wanryoku/8.0 + kyakuryoku/8.0) + 1)::bigint AS emax,
				(FLOOR(kokugo/6.0 + suugaku/6.0 + rika/6.0 + syakai/6.0
				       + eigo/6.0 + ongaku/6.0 + bijutsu/6.0) + 1)::bigint AS nmax
			FROM player_status WHERE player_id = $1
		) m
		WHERE ps.player_id = m.player_id`, playerID); err != nil {
		return fmt.Errorf("refresh power max: %w", err)
	}
	return nil
}

// PublicSummary is the public listing view of a player (住民名鑑).
type PublicSummary struct {
	ID          int64
	DisplayName string
	Job         string
	JobLevel    int
	CreatedAt   time.Time // 入居日(役場の「新着順」並び替えに使う)
}

// ListPublic returns all active players with public summary fields, for the
// profile/roster screen. Private fields (money, identity) are never included.
func (s *Service) ListPublic(ctx context.Context) ([]PublicSummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT p.id, p.display_name, ps.job, ps.job_level, p.created_at
		 FROM players p JOIN player_status ps ON ps.player_id = p.id
		 WHERE p.deleted_at IS NULL ORDER BY p.id`)
	if err != nil {
		return nil, fmt.Errorf("list players: %w", err)
	}
	defer rows.Close()
	out := []PublicSummary{}
	for rows.Next() {
		var ps PublicSummary
		if err := rows.Scan(&ps.ID, &ps.DisplayName, &ps.Job, &ps.JobLevel, &ps.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan player summary: %w", err)
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// AdminPlayerSummary is the admin listing view of a player (含む非公開情報)。
type AdminPlayerSummary struct {
	ID          int64
	DisplayName string
	Roles       []string
	Money       int64
	Job         string
	JobLevel    int
}

// AdminList returns all active players with admin-relevant fields.
func (s *Service) AdminList(ctx context.Context) ([]AdminPlayerSummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT p.id, p.display_name, ps.job, ps.job_level,
		        COALESCE((SELECT array_agg(role ORDER BY role) FROM player_roles WHERE player_id = p.id), '{}')
		 FROM players p JOIN player_status ps ON ps.player_id = p.id
		 WHERE p.deleted_at IS NULL ORDER BY p.id`)
	if err != nil {
		return nil, fmt.Errorf("admin list players: %w", err)
	}
	out := []AdminPlayerSummary{}
	for rows.Next() {
		var a AdminPlayerSummary
		if err := rows.Scan(&a.ID, &a.DisplayName, &a.Job, &a.JobLevel, &a.Roles); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan admin player: %w", err)
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		bal, err := s.ledger.Balance(ctx, ledger.PlayerAccount(out[i].ID))
		if err != nil {
			return nil, err
		}
		out[i].Money = bal
	}
	return out, nil
}

// AdminPlayerUpdate is the admin-editable player state. Money is a target value
// applied through the ledger (not a mutable column); IsAdmin toggles the role.
type AdminPlayerUpdate struct {
	DisplayName  string
	Money        int64
	IsAdmin      bool
	Params       Params
	Energy       int
	NouEnergy    int
	Satiety      int
	Job          string
	JobLevel     int
	JobExp       int
	DiseaseIndex int
	HeightCm     int
	WeightG      int
}

// AdminUpdate applies admin edits to a player: status/params/name/role and a
// money target (via a ledger adjustment to keep the double-entry invariant).
func (s *Service) AdminUpdate(ctx context.Context, id int64, u AdminPlayerUpdate) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM players WHERE id = $1 AND deleted_at IS NULL)`, id).Scan(&exists); err != nil {
			return fmt.Errorf("check player: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
		if u.DisplayName != "" {
			if _, err := tx.Exec(ctx, `UPDATE players SET display_name = $2 WHERE id = $1`, id, u.DisplayName); err != nil {
				return fmt.Errorf("update name: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE player_status SET
				energy = $2, nou_energy = $3, satiety = $4, job = $5, job_level = $6, job_exp = $7,
				disease_index = $8, height_cm = $9, weight_g = $10,
				kokugo = $11, suugaku = $12, rika = $13, syakai = $14, eigo = $15, ongaku = $16, bijutsu = $17,
				looks = $18, tairyoku = $19, kenkou = $20, speed = $21, power = $22, wanryoku = $23, kyakuryoku = $24,
				love = $25, omoshirosa = $26, updated_at = now()
			WHERE player_id = $1`,
			id, u.Energy, u.NouEnergy, u.Satiety, u.Job, u.JobLevel, u.JobExp, u.DiseaseIndex, u.HeightCm, u.WeightG,
			u.Params.Kokugo, u.Params.Suugaku, u.Params.Rika, u.Params.Syakai, u.Params.Eigo, u.Params.Ongaku, u.Params.Bijutsu,
			u.Params.Looks, u.Params.Tairyoku, u.Params.Kenkou, u.Params.Speed, u.Params.Power, u.Params.Wanryoku, u.Params.Kyakuryoku,
			u.Params.Love, u.Params.Omoshirosa); err != nil {
			return fmt.Errorf("update status: %w", err)
		}
		// パラメータ変化に伴いパワー上限を再計算(energy/nouは上限にクランプ)。
		if err := RefreshPowerMax(ctx, tx, id); err != nil {
			return err
		}
		// admin ロールの付与/剥奪。
		if u.IsAdmin {
			if _, err := tx.Exec(ctx,
				`INSERT INTO player_roles (player_id, role) VALUES ($1, 'admin') ON CONFLICT DO NOTHING`, id); err != nil {
				return fmt.Errorf("grant admin: %w", err)
			}
		} else {
			if _, err := tx.Exec(ctx,
				`DELETE FROM player_roles WHERE player_id = $1 AND role = 'admin'`, id); err != nil {
				return fmt.Errorf("revoke admin: %w", err)
			}
		}
		// お金は台帳で目標値に調整(system:admin_adjust との複式)。
		var current int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
			ledger.PlayerAccount(id)).Scan(&current); err != nil {
			return fmt.Errorf("read money: %w", err)
		}
		if delta := u.Money - current; delta != 0 {
			if err := s.ledger.PostTx(ctx, tx, "admin_adjust", "", []ledger.Entry{
				{Account: ledger.PlayerAccount(id), Delta: delta},
				{Account: ledger.SystemAccount("admin_adjust"), Delta: -delta},
			}); err != nil {
				return fmt.Errorf("adjust money: %w", err)
			}
		}
		return nil
	})
}

// AdminSoftDelete marks a player as deleted (論理削除). Ledger history is kept.
func (s *Service) AdminSoftDelete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE players SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete player: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// HasRole reports whether the player holds the given role.
func (s *Service) HasRole(ctx context.Context, id int64, role string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM player_roles WHERE player_id = $1 AND role = $2)`,
		id, role).Scan(&exists); err != nil {
		return false, fmt.Errorf("has role: %w", err)
	}
	return exists, nil
}

// Get returns a player aggregate including current money (from the ledger).
func (s *Service) Get(ctx context.Context, id int64) (*Player, error) {
	p := &Player{ID: id}
	err := s.pool.QueryRow(ctx,
		`SELECT instance_host, remote_user_id, display_name, current_town, created_at
		 FROM players WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&p.InstanceHost, &p.RemoteUserID, &p.DisplayName, &p.CurrentTown, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get player: %w", err)
	}

	var energyRecoveredAt, nouRecoveredAt time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT energy, energy_max, nou_energy, nou_energy_max, job, job_level, satiety,
		        job_exp, job_kaisuu, mastered_jobs,
		        height_cm, weight_g, disease_index,
		        kokugo, suugaku, rika, syakai, eigo, ongaku, bijutsu,
		        looks, tairyoku, kenkou, speed, power, wanryoku, kyakuryoku,
		        love, omoshirosa,
		        energy_recovered_at, nou_recovered_at, onsen_multiplier
		 FROM player_status WHERE player_id = $1`, id).
		Scan(&p.Status.Energy, &p.Status.EnergyMax, &p.Status.NouEnergy,
			&p.Status.NouEnergyMax, &p.Status.Job, &p.Status.JobLevel, &p.Status.Satiety,
			&p.Status.JobExp, &p.Status.JobKaisuu, &p.Status.MasteredJobs,
			&p.Status.HeightCm, &p.Status.WeightG, &p.Status.DiseaseIndex,
			&p.Params.Kokugo, &p.Params.Suugaku, &p.Params.Rika, &p.Params.Syakai,
			&p.Params.Eigo, &p.Params.Ongaku, &p.Params.Bijutsu,
			&p.Params.Looks, &p.Params.Tairyoku, &p.Params.Kenkou, &p.Params.Speed,
			&p.Params.Power, &p.Params.Wanryoku, &p.Params.Kyakuryoku,
			&p.Params.Love, &p.Params.Omoshirosa,
			&energyRecoveredAt, &nouRecoveredAt, &p.Status.OnsenMultiplier); err != nil {
		return nil, fmt.Errorf("get status: %w", err)
	}
	// 満タンまでの残り時間表示用に、満タンになる時刻を算出する(満タン中はnil)。
	// 満タン時刻 = recovered_at + (recovery_sec / 入浴倍率) × (max - current)。
	// 入浴中(onsen_multiplier>1)は回復が倍率ぶん速いので満タンも早くなる。
	cfg := s.settings.Get()
	mult := p.Status.OnsenMultiplier
	if mult < 1 {
		mult = 1
	}
	if sec := cfg.EnergyRecoverySec; sec > 0 && p.Status.Energy < p.Status.EnergyMax {
		d := time.Duration(float64(sec)/float64(mult)*float64(p.Status.EnergyMax-p.Status.Energy)) * time.Second
		full := energyRecoveredAt.Add(d)
		p.Status.EnergyFullAt = &full
	}
	if sec := cfg.NouRecoverySec; sec > 0 && p.Status.NouEnergy < p.Status.NouEnergyMax {
		d := time.Duration(float64(sec)/float64(mult)*float64(p.Status.NouEnergyMax-p.Status.NouEnergy)) * time.Second
		full := nouRecoveredAt.Add(d)
		p.Status.NouEnergyFullAt = &full
	}
	// BMI・体型・コンディションはサーバ側で権威的に算出する(副作用なし。旧check_BMIと同一)。
	p.Status.BMI = condition.BMI(p.Status.HeightCm, p.Status.WeightG)
	p.Status.BodyType = condition.BodyType(p.Status.BMI)
	cond := condition.Compute(condition.Input{
		Energy: p.Status.Energy, EnergyMax: p.Status.EnergyMax,
		NouEnergy: p.Status.NouEnergy, NouEnergyMax: p.Status.NouEnergyMax,
		Kenkou: p.Params.Kenkou, Satiety: p.Status.Satiety,
		BMI: p.Status.BMI, DiseaseIndex: p.Status.DiseaseIndex,
	})
	p.Status.DiseaseName = cond.DiseaseName
	p.Status.Condition = cond.Display

	// 就労クールタイム中なら再就労可能時刻を返す(経過済み/未就労はnil)。デバッグ時は常にnil。
	debugNoCd := s.settings.Get().DebugNoCooldown
	if !debugNoCd {
		var workAt *time.Time
		if err := s.pool.QueryRow(ctx,
			`SELECT next_available_at FROM player_facility_cooldowns
			 WHERE player_id = $1 AND facility = 'work' AND next_available_at > now()`,
			id).Scan(&workAt); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("get work cooldown: %w", err)
		}
		p.Status.WorkAvailableAt = workAt
	}

	rows, err := s.pool.Query(ctx,
		`SELECT role FROM player_roles WHERE player_id = $1 ORDER BY role`, id)
	if err != nil {
		return nil, fmt.Errorf("get roles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		p.Roles = append(p.Roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("roles rows: %w", err)
	}

	bal, err := s.ledger.Balance(ctx, ledger.PlayerAccount(id))
	if err != nil {
		return nil, err
	}
	p.Money = bal

	savings, err := s.ledger.Balance(ctx, ledger.SavingsAccount(id))
	if err != nil {
		return nil, err
	}
	p.Savings = savings

	superSavings, err := s.ledger.Balance(ctx, ledger.SuperSavingsAccount(id))
	if err != nil {
		return nil, err
	}
	p.SuperSavings = superSavings

	// 住宅ローン(なければ0,0)。サブクエリのCOALESCEで常に1行を返す。
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE((SELECT nitigaku FROM player_loans WHERE player_id = $1), 0),
		        COALESCE((SELECT kaisuu FROM player_loans WHERE player_id = $1), 0)`,
		id).Scan(&p.LoanDaily, &p.LoanCount); err != nil {
		return nil, err
	}

	items, err := s.pool.Query(ctx,
		`SELECT ci.id, ci.name, COALESCE(ci.category, ''), pi.quantity, pi.remaining_uses,
		        CEIL(pi.remaining_uses::numeric / ci.durability)::int AS sets,
		        ci.durability_unit, ci.effect, ci.use_interval_min, ci.calorie_g,
		        CASE WHEN pi.last_used_at IS NOT NULL
		                  AND pi.last_used_at + make_interval(mins => ci.use_interval_min) > now()
		             THEN pi.last_used_at + make_interval(mins => ci.use_interval_min)
		             ELSE NULL END AS next_available_at
		 FROM player_items pi
		 JOIN content_items ci ON ci.id = pi.item_id
		 WHERE pi.player_id = $1 AND pi.remaining_uses > 0
		 ORDER BY ci.id`, id)
	if err != nil {
		return nil, fmt.Errorf("get items: %w", err)
	}
	defer items.Close()
	for items.Next() {
		var (
			it      ItemStack
			effJSON []byte
		)
		if err := items.Scan(&it.ItemID, &it.Name, &it.Category, &it.Quantity, &it.RemainingUses, &it.Sets, &it.DurabilityUnit, &effJSON, &it.IntervalMin, &it.CalorieG, &it.NextAvailableAt); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		if debugNoCd {
			it.NextAvailableAt = nil // デバッグ: クールタイム表示を無効化
		}
		if eff, err := effects.ParseEffect(effJSON); err == nil {
			it.Money = eff.MoneySum()
			it.Params = map[string]int{}
			for k, v := range eff.ParamSum() {
				if v != 0 {
					it.Params[k] = v
				}
			}
		}
		p.Items = append(p.Items, it)
	}
	if err := items.Err(); err != nil {
		return nil, fmt.Errorf("items rows: %w", err)
	}
	// 所持アイテムの種類上限(表示用)。バックエンドの購入チェックと同じ設定値。
	p.ItemKindLimit = cfg.ItemKindLimit
	return p, nil
}

// Participant is one currently-active player for the town-top participant list.
type Participant struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
}

// TouchLastSeen records player activity for the participant list. Throttled to
// one write per 30 seconds; errors are ignored (表示用の心拍であり本処理を
// 妨げない)。
func (s *Service) TouchLastSeen(ctx context.Context, id int64) {
	_, _ = s.pool.Exec(ctx,
		`UPDATE players SET last_seen_at = now()
		 WHERE id = $1 AND (last_seen_at IS NULL OR last_seen_at < now() - interval '30 seconds')`, id)
}

// Participants lists players active within the last 20 minutes
// (レガシー$logout_time=1200秒の参加者リスト相当)。
func (s *Service) Participants(ctx context.Context) ([]Participant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, display_name FROM players
		 WHERE deleted_at IS NULL AND last_seen_at > now() - interval '20 minutes'
		 ORDER BY last_seen_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()
	out := []Participant{}
	for rows.Next() {
		var p Participant
		if err := rows.Scan(&p.ID, &p.DisplayName); err != nil {
			return nil, fmt.Errorf("scan participant: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
