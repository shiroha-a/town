package casino

import (
	"testing"

	"github.com/shiroha-a/town/internal/rng"
)

func TestKaburiSize(t *testing.T) {
	cases := map[int]int{0: 10, 1: 9, 5: 5, 7: 3, 8: 3, 100: 3, -1: 10}
	for streak, want := range cases {
		if got := KaburiSize(streak); got != want {
			t.Errorf("KaburiSize(%d) = %d, want %d", streak, got, want)
		}
	}
}

// 伏せ札が場に無いと、どのカードを出しても勝ててしまう。配る枚数がいくつでも
// 伏せ札が必ず含まれ、重複せず昇順であることを確かめる。
func TestKaburiDeal(t *testing.T) {
	r := rng.New(42)
	for size := KaburiMinSize; size <= KaburiMaxSize; size++ {
		for hidden := 1; hidden <= KaburiPool; hidden++ {
			cards := KaburiDeal(r, hidden, size)
			if len(cards) != size {
				t.Fatalf("size=%d hidden=%d: %d枚配られた", size, hidden, len(cards))
			}
			seen := map[int]bool{}
			found := false
			for i, c := range cards {
				if c < 1 || c > KaburiPool {
					t.Fatalf("場に出ないカード %d", c)
				}
				if seen[c] {
					t.Fatalf("同じカードが2枚 (%d)", c)
				}
				seen[c] = true
				if c == hidden {
					found = true
				}
				if i > 0 && cards[i-1] > c {
					t.Fatalf("昇順になっていない: %v", cards)
				}
			}
			if !found {
				t.Fatalf("size=%d: 伏せ札 %d が場に無い (%v)", size, hidden, cards)
			}
		}
	}
}

// 配当は「枚数が減るほど高い」。控除率が負(＝店が損をする)にならないことも見る。
func TestKaburiPayoutPercent(t *testing.T) {
	prev := int64(0)
	for size := KaburiMaxSize; size >= KaburiMinSize; size-- {
		p := KaburiPayoutPercent(size)
		if p <= prev {
			t.Errorf("枚数%dの配当 %d%% が %d 枚のときより高くない", size, p, size+1)
		}
		prev = p
		// 期待値 = (1-1/N)*p - 1/N が0未満(店の取り分がある)。
		win := float64(size-1) / float64(size)
		ev := win*float64(p)/100 - (1 - win)
		if ev >= 0 {
			t.Errorf("枚数%d: 期待値が %.4f で店が損をする", size, ev)
		}
	}
}
