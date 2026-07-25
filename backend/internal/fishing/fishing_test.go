package fishing

import (
	"testing"

	"github.com/shiroha-a/town/internal/rng"
)

func TestBaitRank(t *testing.T) {
	cases := map[string]int{
		"釣りの餌(初)": 1,
		"小魚(小)":   2,
		"中魚(中)":   3,
		"大魚(大)":   4,
		"謎の餌":     1,
	}
	for name, want := range cases {
		if got := BaitRank(name); got != want {
			t.Errorf("BaitRank(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestFishName(t *testing.T) {
	cases := map[int]string{
		0: "小魚(小)", 1: "小魚(小)", 2: "中魚(中)", 3: "大魚(大)", 4: "特魚(特)", 5: "幻魚(幻)", 9: "幻魚(幻)",
	}
	for rank, want := range cases {
		if got := FishName(rank); got != want {
			t.Errorf("FishName(%d) = %q, want %q", rank, got, want)
		}
	}
}

// Deal must place the winner and the continue slots on distinct cards, and only
// hand out the slots that exist for that round size.
func TestDealDistinctSlots(t *testing.T) {
	r := rng.New(1)
	for _, n := range []int{2, 3, 4} {
		for range 50 {
			l := Deal(r, n)
			if l.Cards != n {
				t.Fatalf("cards = %d, want %d", l.Cards, n)
			}
			if l.Win < 1 || l.Win > n {
				t.Fatalf("win %d out of range for %d cards", l.Win, n)
			}
			if n < 3 && l.Cont1 != 0 {
				t.Fatalf("%d cards should have no cont1, got %d", n, l.Cont1)
			}
			if n < 4 && l.Cont2 != 0 {
				t.Fatalf("%d cards should have no cont2, got %d", n, l.Cont2)
			}
			if l.Cont1 != 0 && l.Cont1 == l.Win {
				t.Fatalf("cont1 collides with win at %d", l.Win)
			}
			if l.Cont2 != 0 && (l.Cont2 == l.Win || l.Cont2 == l.Cont1) {
				t.Fatalf("cont2 collides: %+v", l)
			}
		}
	}
}

func TestPick(t *testing.T) {
	l := Layout{Cards: 4, Win: 2, Cont1: 3, Cont2: 1}
	if got := l.Pick(2); got != Win {
		t.Errorf("pick win = %v", got)
	}
	if got := l.Pick(3); got != Continue {
		t.Errorf("pick cont1 = %v", got)
	}
	if got := l.Pick(1); got != Continue {
		t.Errorf("pick cont2 = %v", got)
	}
	if got := l.Pick(4); got != Lose {
		t.Errorf("pick blank = %v", got)
	}
	// 2枚(継続スロット無し)で、存在しないスロットに誤ってマッチしないこと。
	l2 := Layout{Cards: 2, Win: 1}
	if got := l2.Pick(2); got != Lose {
		t.Errorf("2-card blank = %v, want lose", got)
	}
}

func TestNextCardsRange(t *testing.T) {
	r := rng.New(7)
	for range 100 {
		if n := NextCards(r); n < 2 || n > 4 {
			t.Fatalf("NextCards = %d, want 2..4", n)
		}
	}
}

func TestInitialRankB(t *testing.T) {
	if got := InitialRankB(3, 2); got != 0 {
		t.Errorf("3 cards should give 0, got %d", got)
	}
	if got := InitialRankB(4, 4); got != 2 {
		t.Errorf("4 cards with rankA=4 should give 2, got %d", got)
	}
	if got := InitialRankB(4, 1); got != 1 {
		t.Errorf("4 cards with rankA=1 should give 1, got %d", got)
	}
}
