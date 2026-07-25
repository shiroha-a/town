package cleague

import (
	"testing"

	"github.com/shiroha-a/town/internal/rng"
)

func abilitiesAll(v int) map[string]int {
	m := map[string]int{}
	for _, k := range AbilityKeys {
		m[k] = v
	}
	return m
}

// TestBattleStrongerWinsMore checks the stronger character wins the majority of
// matches (with upsets allowed).
func TestBattleStrongerWinsMore(t *testing.T) {
	r := rng.New(1)
	strong := abilitiesAll(300)
	weak := abilitiesAll(30)
	sWins := 0
	for i := 0; i < 600; i++ {
		if Battle(strong, weak, r).Winner == "a" {
			sWins++
		}
	}
	if sWins < 400 {
		t.Errorf("strong won %d/600, expected a clear majority", sWins)
	}
}

// TestDerived checks the derived sintai/zunou aggregates.
func TestDerived(t *testing.T) {
	ab := abilitiesAll(100)
	// body 7項 ×100 /10 = 70
	if got := Sintai(ab); got != 70 {
		t.Errorf("sintai = %d, want 70", got)
	}
	// brain 9項 ×100 /10 = 90
	if got := Zunou(ab); got != 90 {
		t.Errorf("zunou = %d, want 90", got)
	}
}
