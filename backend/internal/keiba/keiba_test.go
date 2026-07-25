package keiba

import (
	"testing"

	"github.com/shiroha-a/town/internal/rng"
)

func TestGenerateRace(t *testing.T) {
	r := rng.New(2)
	race := GenerateRace(r)
	if len(race) != Runners {
		t.Fatalf("runners = %d, want %d", len(race), Runners)
	}
	for _, h := range race {
		if h.Name == "" || h.Img == "" {
			t.Errorf("empty horse: %+v", h)
		}
		ok := false
		for _, o := range oddsValues {
			if h.Odds == o {
				ok = true
			}
		}
		if !ok {
			t.Errorf("odds %d not in pool", h.Odds)
		}
	}
}

// TestSimulateFavoriteWinsMore checks the winner is in range, steps are recorded,
// and the low-odds favorite wins more often than the high-odds longshot.
func TestSimulateFavoriteWinsMore(t *testing.T) {
	r := rng.New(1)
	lineup := []Horse{{Odds: 2}, {Odds: 30}, {Odds: 8}, {Odds: 12}, {Odds: 16}, {Odds: 5}}
	favWins, longWins := 0, 0
	for i := 0; i < 2000; i++ {
		res := Simulate(lineup, r)
		if res.WinnerIndex < 0 || res.WinnerIndex >= Runners {
			t.Fatalf("winner out of range: %d", res.WinnerIndex)
		}
		if len(res.Steps) != Runners {
			t.Fatalf("steps len = %d, want %d", len(res.Steps), Runners)
		}
		switch res.WinnerIndex {
		case 0:
			favWins++
		case 1:
			longWins++
		}
	}
	if favWins <= longWins {
		t.Errorf("favorite(odds2) won %d, longshot(odds30) won %d; expected favorite to win more", favWins, longWins)
	}
}
