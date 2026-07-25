package casino

import "github.com/shiroha-a/town/internal/rng"

// ロト6(loto6.cgi): 1..36から6個を選び、日次抽選の当選6個との一致数で賞金が決まる。
// 賞金は銀行普通口座へ振り込む。EVは仕様上プレイヤー有利だが倍率は原典どおりとする。

// Loto6Prizes maps the match count (0..6) to the prize in yen.
var Loto6Prizes = [7]int64{0, 2000, 10000, 100000, 1000000, 10000000, 100000000}

const Loto6Cost int64 = 1000 // 1口の値段

const (
	Loto6DailyLimit = 20 // 1人1日あたりの上限口数
	Loto6N          = 36 // 番号の範囲(1..36)
	Loto6Pick       = 6  // 選ぶ個数
)

// GenLoto6 draws Loto6Pick distinct numbers in 1..Loto6N, sorted ascending.
func GenLoto6(r *rng.Rand) []int {
	picked := map[int]bool{}
	nums := make([]int, 0, Loto6Pick)
	for len(nums) < Loto6Pick {
		n := r.IntN(Loto6N) + 1
		if !picked[n] {
			picked[n] = true
			nums = append(nums, n)
		}
	}
	for i := 1; i < len(nums); i++ {
		for j := i; j > 0 && nums[j-1] > nums[j]; j-- {
			nums[j-1], nums[j] = nums[j], nums[j-1]
		}
	}
	return nums
}

// Loto6Matches counts how many of nums appear in winning.
func Loto6Matches(nums, winning []int) int {
	set := make(map[int]bool, len(winning))
	for _, w := range winning {
		set[w] = true
	}
	m := 0
	for _, n := range nums {
		if set[n] {
			m++
		}
	}
	return m
}

// ValidLoto6Pick reports whether nums is a valid selection (Loto6Pick distinct
// numbers within 1..Loto6N).
func ValidLoto6Pick(nums []int) bool {
	if len(nums) != Loto6Pick {
		return false
	}
	seen := map[int]bool{}
	for _, n := range nums {
		if n < 1 || n > Loto6N || seen[n] {
			return false
		}
		seen[n] = true
	}
	return true
}
