package session

import (
	"testing"
	"time"

	"github.com/shiroha-a/town/internal/settings"
)

// TestTTLFromSettings covers 有効期限の取得: 管理画面の設定を読み、値が無い/
// 壊れているときは既定に落ちる。0日のまま使うと発行した瞬間に切れてしまう。
func TestTTLFromSettings(t *testing.T) {
	tests := []struct {
		name string
		days int
		want time.Duration
	}{
		{"設定どおり", 7, 7 * 24 * time.Hour},
		{"1日", 1, 24 * time.Hour},
		{"未設定(0)は既定", 0, defaultTTL},
		{"負の値は既定", -3, defaultTTL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := settings.Defaults()
			g.SessionTTLDays = tt.days
			s := New(nil, false, settings.NewStatic(g))
			if got := s.TTL(); got != tt.want {
				t.Errorf("TTL() = %v, want %v", got, tt.want)
			}
		})
	}

	// 設定ストアを渡していないとき(テストや単発の計算)は既定。
	if got := New(nil, false, nil).TTL(); got != defaultTTL {
		t.Errorf("設定なしのTTL() = %v, want %v", got, defaultTTL)
	}
}
