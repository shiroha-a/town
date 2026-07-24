package event

import (
	"testing"

	"github.com/shiroha-a/town/internal/rng"
)

// TestRoll checks events fire at roughly 1/12, have valid names, and that the
// theft loss never exceeds the player's money.
func TestRoll(t *testing.T) {
	r := rng.New(1)
	const n = 6000
	fired := 0
	for i := 0; i < n; i++ {
		occ, o := Roll(r, 1000, 100)
		if !occ {
			continue
		}
		fired++
		if o.Name == "" {
			t.Fatalf("event with empty name: %+v", o)
		}
		// 泥棒は持ち金(1000)を超えて奪わない。
		if o.Name == "泥棒" && -o.MoneyDelta > 1000 {
			t.Errorf("theft %d exceeds money 1000", -o.MoneyDelta)
		}
	}
	if fired == 0 {
		t.Fatal("no events fired")
	}
	// 期待値 ~ n/12 = 500。広めの範囲で確認。
	if fired < 300 || fired > 750 {
		t.Errorf("fired = %d, expected ~%d", fired, n/12)
	}
}

// TestSickEventSetsIndex verifies the 体調不良 event assigns the disease index
// directly (レガシー$byouki_sisuu = -8), rather than decrementing it.
func TestSickEventSetsIndex(t *testing.T) {
	r := rng.New(1)
	found := false
	for _, fn := range events {
		o := fn(r, 1000, 100)
		if o.Name != "体調不良" {
			continue
		}
		found = true
		if o.DiseaseSet == nil || *o.DiseaseSet != -8 {
			t.Errorf("体調不良 DiseaseSet = %v, want -8", o.DiseaseSet)
		}
	}
	if !found {
		t.Fatal("体調不良 event not in pool")
	}
}
