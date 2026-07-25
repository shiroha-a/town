package casino

import "github.com/shiroha-a/town/internal/rng"

// ブラックジャックのカード計算ヘルパー(状態管理・台帳はaction層が行う)。
// カードは0..51。値=(card%13)+1、10超は10。Aは11だがバスト時に1として数え直す。

// BJValue returns the base value of a card (A=1, J/Q/K=10).
func BJValue(card int) int {
	v := card%13 + 1
	if v > 10 {
		v = 10
	}
	return v
}

// BJScore totals a hand, counting aces as 11 then dropping to 1 while busted.
func BJScore(cards []int) int {
	score, aces := 0, 0
	for _, c := range cards {
		v := BJValue(c)
		if v == 1 {
			aces++
			v = 11
		}
		score += v
	}
	for score > 21 && aces > 0 {
		score -= 10
		aces--
	}
	return score
}

// BJDraw draws a card not already in used (親子の既出), by rejection sampling.
func BJDraw(r *rng.Rand, used []int) int {
	for {
		c := r.IntN(52)
		dup := false
		for _, u := range used {
			if u == c {
				dup = true
				break
			}
		}
		if !dup {
			return c
		}
	}
}
