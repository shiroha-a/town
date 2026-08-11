package casino

import "github.com/shiroha-a/town/internal/rng"

// カード引き: 場から1枚引き、前の人が引いたカード(伏せ札)とかぶらなければ勝ち。
// 場は全員で1つを共有し、かぶらずに通るたびに1枚減る(＝かぶりやすくなり配当が上がる)。
// 1回の勝負に共有卓の読み書きが要るので、他のゲームと違い純粋関数にはできない。
// ここには卓の規則(枚数・配当・場の配り方)だけを置き、台帳と卓の更新は action 側で行う。

const (
	// KaburiPool is the highest card face; the deck is 1..KaburiPool.
	KaburiPool = 10
	// KaburiMaxSize is the number of cards on a fresh table.
	KaburiMaxSize = 10
	// KaburiMinSize is how far the table shrinks (連鎖の上限でもある)。
	KaburiMinSize = 3
)

// KaburiBets are the allowed stakes (サイコロと同じ刻み)。
func KaburiBets() []int64 { return []int64{10000, 100000, 500000, 1000000} }

// KaburiSize returns how many cards are on the table after `streak` consecutive
// misses (かぶらなかった回数)。1回通るごとに1枚減り、KaburiMinSizeで止まる。
func KaburiSize(streak int) int {
	size := KaburiMaxSize - streak
	if size < KaburiMinSize {
		return KaburiMinSize
	}
	if size > KaburiMaxSize {
		return KaburiMaxSize
	}
	return size
}

// kaburiPayout is the win payout as a percentage of the stake, per table size.
// 枚数が減るほどかぶりやすくなるので配当を上げる。どの枚数でも控除率が5〜7%に
// 収まる値にしてある(公平配当 = (1/N)/(1-1/N) から少し引いた値)。
var kaburiPayout = map[int]int64{
	10: 5, 9: 6, 8: 8, 7: 10, 6: 13, 5: 18, 4: 25, 3: 40,
}

// KaburiPayoutPercent returns the win payout (% of the stake) for a table size.
func KaburiPayoutPercent(size int) int64 {
	if p, ok := kaburiPayout[size]; ok {
		return p
	}
	return kaburiPayout[KaburiMaxSize]
}

// KaburiDeal lays out `size` cards that always include the hidden one, so the
// previous player's card is never impossible to hit (伏せ札が場に無いと必勝になる)。
// The rest are drawn from the remaining faces; the result is ascending.
func KaburiDeal(r *rng.Rand, hidden, size int) []int {
	if size < 1 {
		size = 1
	}
	if size > KaburiPool {
		size = KaburiPool
	}
	rest := make([]int, 0, KaburiPool-1)
	for c := 1; c <= KaburiPool; c++ {
		if c != hidden {
			rest = append(rest, c)
		}
	}
	// 部分Fisher-Yatesで先頭 size-1 枚を選ぶ。
	for i := 0; i < size-1 && i < len(rest); i++ {
		j := i + r.IntN(len(rest)-i)
		rest[i], rest[j] = rest[j], rest[i]
	}
	out := append([]int{hidden}, rest[:size-1]...)
	// 昇順に並べる(場の見た目を安定させ、伏せ札の位置を隠す)。
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
