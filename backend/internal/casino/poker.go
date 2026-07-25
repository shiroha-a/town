package casino

import "sort"

// ポーカーの役判定。カードは0..51、rank=card%13(0=A,1=2,..,9=10,10=J,11=Q,12=K)、
// suit=card/13。役の強さ(result)は0..9で、ポイント増減は result-1(ノーペア=-1)。

var pokerHandNames = []string{
	"ノーペア", "ワンペア", "ツーペア", "スリーカード", "ストレート",
	"フラッシュ", "フルハウス", "フォーカード", "ストレートフラッシュ",
	"ロイヤルストレートフラッシュ",
}

// PokerHandName returns the Japanese name of a hand rank (0..9).
func PokerHandName(result int) string {
	if result < 0 || result >= len(pokerHandNames) {
		return ""
	}
	return pokerHandNames[result]
}

// PokerEval evaluates a 5-card hand and returns its rank 0..9.
func PokerEval(hand []int) int {
	rankCount := map[int]int{}
	suitCount := map[int]int{}
	for _, c := range hand {
		rankCount[c%13]++
		suitCount[c/13]++
	}
	pairs, threes, fours := 0, 0, 0
	for _, cnt := range rankCount {
		switch cnt {
		case 2:
			pairs++
		case 3:
			threes++
		case 4:
			fours++
		}
	}
	flush := len(suitCount) == 1
	straight := pokerStraight(rankCount)
	switch {
	case straight && flush && pokerRoyal(rankCount):
		return 9
	case straight && flush:
		return 8
	case fours == 1:
		return 7
	case threes == 1 && pairs == 1:
		return 6
	case flush:
		return 5
	case straight:
		return 4
	case threes == 1:
		return 3
	case pairs == 2:
		return 2
	case pairs == 1:
		return 1
	default:
		return 0
	}
}

// pokerStraight reports whether 5 distinct ranks form a run (A-2-3-4-5 …
// 10-J-Q-K-A). A is low(0) normally, and also allowed high in 10-J-Q-K-A.
func pokerStraight(rankCount map[int]int) bool {
	if len(rankCount) != 5 {
		return false
	}
	ranks := make([]int, 0, 5)
	for r := range rankCount {
		ranks = append(ranks, r)
	}
	sort.Ints(ranks)
	if ranks[4]-ranks[0] == 4 {
		return true
	}
	// A(0)を最高位に置く 10-J-Q-K-A = {0,9,10,11,12}
	return ranks[0] == 0 && ranks[1] == 9 && ranks[2] == 10 && ranks[3] == 11 && ranks[4] == 12
}

// pokerRoyal reports whether the hand contains exactly 10-J-Q-K-A.
func pokerRoyal(rankCount map[int]int) bool {
	for _, r := range []int{0, 9, 10, 11, 12} {
		if rankCount[r] == 0 {
			return false
		}
	}
	return true
}
