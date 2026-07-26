package player

import (
	"testing"
	"time"
)

func TestProjectPower(t *testing.T) {
	base := time.Now().Add(-70 * time.Second)
	cases := []struct {
		name      string
		value     int
		max       int
		sec       int
		mult      float64
		wantValue int
	}{
		{"経過ぶんだけ増える", 10, 100, 60, 1, 11},
		{"満タンは増えない", 100, 100, 60, 1, 100},
		{"上限を超えない", 99, 100, 10, 1, 100},
		{"入浴中は倍率ぶん速い", 10, 100, 60, 2, 12},
		{"回復秒数0なら据え置き", 10, 100, 0, 1, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, at := projectPower(c.value, c.max, base, c.sec, c.mult)
			if got != c.wantValue {
				t.Errorf("value = %d, want %d", got, c.wantValue)
			}
			// 進めた時刻は現在を超えない(次の回復時刻の計算が狂うため)。
			if at.After(time.Now()) {
				t.Errorf("recoveredAt が未来になっている: %v", at)
			}
		})
	}
}
