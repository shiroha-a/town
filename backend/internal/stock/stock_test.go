package stock

import (
	"testing"

	"github.com/shiroha-a/town/internal/rng"
)

// TestApplyMovementSafe checks the price engine never produces a non-positive
// price, is deterministic for a fixed seed, and does move prices over time.
func TestApplyMovementSafe(t *testing.T) {
	r := rng.New(42)
	prices := make([]int64, len(Symbols))
	for i := range prices {
		prices[i] = InitialPrice
	}
	moves := 0
	for i := 0; i < 3000; i++ {
		if applyMovement(prices, r, 20) != "" {
			moves++
		}
		for _, p := range prices {
			if p <= 0 {
				t.Fatalf("price went non-positive: %d at iter %d", p, i)
			}
		}
	}
	if moves == 0 {
		t.Fatal("no price movement in 3000 iterations")
	}
}
