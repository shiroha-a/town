// Package fishing implements the 釣りゲーム (legacy tsuri.cgi): spend a bait
// item, then pick face-down cards. Hitting the winning card lands a fish;
// hitting a "引いてる" card lets you keep going with a fresh (2-4 card) round
// and raises the rank of the fish you will eventually land. Anything else lets
// the fish escape.
//
// The card layout lives on the server only — the client is told how many cards
// are on the table and nothing else.
package fishing

import (
	"strings"

	"github.com/shiroha-a/town/internal/rng"
)

// Fee-free game: the cost is one bait item, not money.

// BaitRank maps a bait item name to the starting rank (レガシー tsuri.cgi:342-346).
// The rank feeds into the final fish tier, so a better bait lands a better fish.
func BaitRank(name string) int {
	switch {
	case strings.Contains(name, "初"):
		return 1
	case strings.Contains(name, "小"):
		return 2
	case strings.Contains(name, "中"):
		return 3
	case strings.Contains(name, "大"):
		return 4
	default:
		return 1
	}
}

// FishName maps the accumulated rank to the fish that is landed
// (レガシー tsuri.cgi:120-126)。
func FishName(rank int) string {
	switch {
	case rank <= 1:
		return "小魚(小)"
	case rank == 2:
		return "中魚(中)"
	case rank == 3:
		return "大魚(大)"
	case rank == 4:
		return "特魚(特)"
	default:
		return "幻魚(幻)"
	}
}

// Layout is one round's card arrangement. Positions are 1-based; a zero
// continue slot means that slot does not exist for this round.
type Layout struct {
	Cards int
	Win   int
	Cont1 int
	Cont2 int
}

// InitialCards is the number of cards in the first round (レガシー $kado_kazu=3).
const InitialCards = 3

// Deal lays out one round of n cards: one winner, plus up to two "引いてる"
// slots (the 2nd exists from 3 cards, the 3rd only at 4 cards). Mirrors the
// legacy randcheck(n, n) permutation.
func Deal(r *rng.Rand, n int) Layout {
	if n < 2 {
		n = 2
	}
	if n > 4 {
		n = 4
	}
	// 1..n をシャッフルして先頭から 当たり/継続1/継続2 に割り当てる
	// (レガシー randcheck は重複しない乱数を n 個引くのと同じ)。
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i + 1
	}
	for i := n - 1; i > 0; i-- {
		j := r.IntN(i + 1)
		perm[i], perm[j] = perm[j], perm[i]
	}
	l := Layout{Cards: n, Win: perm[0]}
	if n >= 3 {
		l.Cont1 = perm[1]
	}
	if n == 4 {
		l.Cont2 = perm[2]
	}
	return l
}

// NextCards is the size of the round that follows a "引いてる" card
// (レガシー int(rand(3))+2 → 2..4)。
func NextCards(r *rng.Rand) int {
	return r.IntN(3) + 2
}

// Result is the outcome of picking one card.
type Result string

const (
	// Win: the fish is landed and the session ends.
	Win Result = "win"
	// Continue: still hooked; a new round is dealt.
	Continue Result = "continue"
	// Lose: the fish got away and the session ends.
	Lose Result = "lose"
)

// Pick classifies a chosen card against the layout.
func (l Layout) Pick(card int) Result {
	switch card {
	case l.Win:
		return Win
	case l.Cont1, l.Cont2:
		if card == 0 {
			return Lose // 存在しない継続スロットとの誤一致を防ぐ
		}
		return Continue
	default:
		return Lose
	}
}

// InitialRankB is the second rank component when a round shows 4 cards
// (レガシー tsuri.cgi:366-372)。
func InitialRankB(cards, rankA int) int {
	if cards != 4 {
		return 0
	}
	if rankA == 4 {
		return 2
	}
	return 1
}
