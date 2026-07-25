package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadWithEnvOverrideAndDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yml")
	yaml := "server:\n  http_addr: \":1\"\n" +
		"worker:\n  tick_interval: \"15s\"\n  leader_lock_ttl: \"45s\"\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOWN_CONFIG", path)
	t.Setenv("TOWN_HTTP_ADDR", ":9999")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Server.HTTPAddr != ":9999" {
		t.Errorf("env override: got %q want :9999", c.Server.HTTPAddr)
	}
	if c.Worker.TickInterval.Std() != 15*time.Second {
		t.Errorf("tick_interval: got %v want 15s", c.Worker.TickInterval.Std())
	}
	if c.Worker.LeaderLockTTL.Std() != 45*time.Second {
		t.Errorf("leader_lock_ttl: got %v want 45s", c.Worker.LeaderLockTTL.Std())
	}
}
