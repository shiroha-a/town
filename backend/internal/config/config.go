// Package config loads the backend configuration from a YAML file
// (default.yml) with environment-variable overrides for containers.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level backend configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	Game     GameConfig     `yaml:"game"`
	Worker   WorkerConfig   `yaml:"worker"`
}

type ServerConfig struct {
	HTTPAddr string `yaml:"http_addr"`
	// AllowedOrigins lists the origins the SPA may be served from. MiAuth の
	// コールバックURLはここに載っているオリジンからしか組み立てない(そうしないと
	// Hostを詐称されてコールバックを攻撃者のサイトへ向けられ、session を奪われる)。
	// 特別値 "all" は全許可。**テスト環境専用**で、本番では必ず列挙すること。
	AllowedOrigins []string `yaml:"allowed_origins"`
	// AppName は MiAuth の同意画面に出るアプリ名。
	AppName string `yaml:"app_name"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type RedisConfig struct {
	Addr string `yaml:"addr"`
	DB   int    `yaml:"db"`
}

// GameConfig holds gameplay-wide parameters. Money values are integer yen.
type GameConfig struct {
	Timezone        string `yaml:"timezone"`
	DayBoundaryHour int    `yaml:"day_boundary_hour"`
	InitialMoney    int64  `yaml:"initial_money"`
	MoneyMax        int64  `yaml:"money_max"`
	// RNGSeed fixes the random seed for deterministic runs/tests. 0 = time-based.
	RNGSeed int64 `yaml:"rng_seed"`
	// DailyInterestPermille is the savings interest rate per mille per game day
	// (5 = 0.5%). Interest is floored to an integer, matching the legacy game.
	DailyInterestPermille int `yaml:"daily_interest_permille"`
	// EnergyRecoverySec / NouRecoverySec: seconds required to recover 1 point of
	// 身体パワー / 頭脳パワー. The worker recovers power on this cadence.
	EnergyRecoverySec int `yaml:"energy_recovery_sec"`
	NouRecoverySec    int `yaml:"nou_recovery_sec"`
	// MealIntervalMin: 食事のクールタイム(分)。前回食事からこの時間は次の食事不可。
	MealIntervalMin int `yaml:"meal_interval_min"`
	// SatietyDecaySec: 空腹値(満腹度)が1減るのに要する秒数。workerが減少させる。
	SatietyDecaySec int `yaml:"satiety_decay_sec"`
	// ConditionEvalIntervalMin: 病気指数のコンディション評価間隔(分)。この間隔ごとに
	// workerがコンディションに応じて病気指数を増減する。
	ConditionEvalIntervalMin int `yaml:"condition_eval_interval_min"`
	// WorkIntervalMin: 就労のクールタイム(分)。前回出勤からこの時間は再出勤できない。
	WorkIntervalMin int `yaml:"work_interval_min"`
	// DebugNoCooldown: trueにすると仕事・アイテム使用・施設・食事の各間隔(クールタイム)を
	// すべて無効化する。デバッグ用。本番では false にすること。
	DebugNoCooldown bool `yaml:"debug_no_cooldown"`
	// DepartDailyCount / SyokudouDailyCount: デパート/食堂で毎日表示する品数(旧100/9)。
	// 商品プールから game_date をシードに決定論的に選ぶ。0以下は全件表示。
	DepartDailyCount   int `yaml:"depart_daily_count"`
	SyokudouDailyCount int `yaml:"syokudou_daily_count"`
	// HanbaiDailyCount: 自販機で毎日陳列する品数(旧仕様は3。0以下=全件)。
	HanbaiDailyCount int `yaml:"hanbai_daily_count"`
	// ItemKindLimit: 所持できるアイテムの種類上限(旧TOWN 25品目)。0以下で無制限。
	ItemKindLimit int `yaml:"item_kind_limit"`
	// StockAdjust: 店頭在庫の割り算倍率(旧 zaiko_tyousetuti)。実在庫=ceil(標準在庫/倍率)。
	StockAdjust int `yaml:"stock_adjust"`
	// MoveWalkSecs/MoveBusSecs: 街移動にかかる秒数(旧 matiidou_time)。徒歩/バス。
	MoveWalkSecs int `yaml:"move_walk_secs"`
	MoveBusSecs  int `yaml:"move_bus_secs"`
}

type WorkerConfig struct {
	TickInterval  Duration `yaml:"tick_interval"`
	LeaderLockTTL Duration `yaml:"leader_lock_ttl"`
}

// Duration is a time.Duration that unmarshals from a Go duration string
// (e.g. "10s", "5m") in YAML.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Std returns the underlying time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// splitList parses a comma-separated env value into a trimmed list.
func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load reads the config file (path from TOWN_CONFIG, default "default.yml")
// and applies environment overrides used in containerized deployments.
func Load() (*Config, error) {
	path := os.Getenv("TOWN_CONFIG")
	if path == "" {
		path = "default.yml"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if v := os.Getenv("TOWN_HTTP_ADDR"); v != "" {
		c.Server.HTTPAddr = v
	}
	if v := os.Getenv("TOWN_DATABASE_URL"); v != "" {
		c.Database.URL = v
	}
	if v := os.Getenv("TOWN_REDIS_ADDR"); v != "" {
		c.Redis.Addr = v
	}
	if v := os.Getenv("TOWN_ALLOWED_ORIGINS"); v != "" {
		c.Server.AllowedOrigins = splitList(v)
	}
	if v := os.Getenv("TOWN_APP_NAME"); v != "" {
		c.Server.AppName = v
	}
	if c.Server.AppName == "" {
		c.Server.AppName = "TOWN"
	}
	return &c, nil
}
