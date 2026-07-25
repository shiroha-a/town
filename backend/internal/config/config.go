// Package config loads the backend infrastructure configuration: the DB and
// Redis addresses, the listen address and the public base URL. ゲームの設定は
// ここではなくDBにあり、管理画面から編集する(internal/settings)。
//
// 設定ファイル(既定 default.yml)は任意で、環境変数だけでも起動できる。
// リポジトリには default.yml.example だけを置き、実ファイルは環境ごとに作る。
package config

import (
	"errors"
	"fmt"
	"io/fs"
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
	Worker   WorkerConfig   `yaml:"worker"`
}

type ServerConfig struct {
	HTTPAddr string `yaml:"http_addr"`
	// BaseURL is the public address of the site (例 https://town.example.com)。
	// MiAuth のコールバックURLはここから組み立てる。
	BaseURL string `yaml:"base_url"`
	// ExtraOrigins lists additional origins the SPA may be served from, for
	// access paths that are not the base URL (Tailscale や localhost など、
	// テスト用の経路)。特別値 "all" は全許可で**テスト環境専用**。
	//
	// コールバックURLは許可されたオリジンからしか組み立てない。そうしないと
	// Hostを詐称してコールバックを攻撃者のサイトへ向けられ、MiAuthのsessionを
	// 奪われる。
	ExtraOrigins []string `yaml:"extra_origins"`
	// AppName は MiAuth の同意画面に出るアプリ名。
	AppName string `yaml:"app_name"`
}

// AllowedOrigins returns the origins a login may come from: the base URL plus
// any extras. 空の項目は落とす。
func (c ServerConfig) AllowedOrigins() []string {
	out := make([]string, 0, len(c.ExtraOrigins)+1)
	if o := strings.TrimSuffix(strings.TrimSpace(c.BaseURL), "/"); o != "" {
		out = append(out, o)
	}
	for _, e := range c.ExtraOrigins {
		if o := strings.TrimSuffix(strings.TrimSpace(e), "/"); o != "" {
			out = append(out, o)
		}
	}
	return out
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type RedisConfig struct {
	Addr string `yaml:"addr"`
	DB   int    `yaml:"db"`
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
	var c Config
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	case errors.Is(err, fs.ErrNotExist) && os.Getenv("TOWN_CONFIG") == "":
		// 設定ファイルは任意。コンテナのように環境変数だけで動かす場合は置かない。
		// TOWN_CONFIG で明示されたパスが無い場合だけはエラーにする(打ち間違い対策)。
	default:
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	applyDefaults(&c)
	if v := os.Getenv("TOWN_HTTP_ADDR"); v != "" {
		c.Server.HTTPAddr = v
	}
	if v := os.Getenv("TOWN_DATABASE_URL"); v != "" {
		c.Database.URL = v
	}
	if v := os.Getenv("TOWN_REDIS_ADDR"); v != "" {
		c.Redis.Addr = v
	}
	if v := os.Getenv("TOWN_BASE_URL"); v != "" {
		c.Server.BaseURL = v
	}
	if v := os.Getenv("TOWN_EXTRA_ORIGINS"); v != "" {
		c.Server.ExtraOrigins = splitList(v)
	}
	if v := os.Getenv("TOWN_APP_NAME"); v != "" {
		c.Server.AppName = v
	}
	if c.Server.AppName == "" {
		c.Server.AppName = "TOWN"
	}
	return &c, nil
}

// applyDefaults fills the values a bare environment needs to boot. 設定ファイルを
// 置かずに環境変数だけで動かせるよう、ここに開発用の既定を持つ。
func applyDefaults(c *Config) {
	if c.Server.HTTPAddr == "" {
		c.Server.HTTPAddr = ":8090"
	}
	if c.Server.BaseURL == "" {
		c.Server.BaseURL = "http://localhost:5173"
	}
	if c.Database.URL == "" {
		c.Database.URL = "postgres://town:town@localhost:55432/town?sslmode=disable"
	}
	if c.Redis.Addr == "" {
		c.Redis.Addr = "localhost:56379"
	}
	if c.Worker.TickInterval.Std() == 0 {
		c.Worker.TickInterval = Duration(10 * time.Second)
	}
	if c.Worker.LeaderLockTTL.Std() == 0 {
		c.Worker.LeaderLockTTL = Duration(30 * time.Second)
	}
}
