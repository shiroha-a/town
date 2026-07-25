package worker

import (
	"testing"
	"time"

	"github.com/shiroha-a/town/internal/config"
)

func TestGameDateBoundary(t *testing.T) {
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{
		loc: jst,
		cfg: &config.Config{Game: config.GameConfig{DayBoundaryHour: 5}},
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"just before 5am is previous day", "2026-07-11T04:59:59+09:00", "2026-07-10"},
		{"exactly 5am is same day", "2026-07-11T05:00:00+09:00", "2026-07-11"},
		{"midday is same day", "2026-07-11T13:00:00+09:00", "2026-07-11"},
		{"late night is same day", "2026-07-11T23:30:00+09:00", "2026-07-11"},
		{"just after midnight is previous day", "2026-07-11T00:30:00+09:00", "2026-07-10"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, err := time.Parse(time.RFC3339, c.in)
			if err != nil {
				t.Fatal(err)
			}
			got := w.gameDate(in).Format("2006-01-02")
			if got != c.want {
				t.Errorf("gameDate(%s) = %s, want %s", c.in, got, c.want)
			}
		})
	}
}
