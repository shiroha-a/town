// Package bingo implements the 5x5 bingo card and its line rules (legacy
// bingo.cgi). A card is 25 cells in row-major order with a free centre; a card
// is "up" once at least LinesToWin of the 12 lines (5 rows + 5 columns +
// 2 diagonals) are fully covered by the numbers drawn so far.
package bingo

import "github.com/shiroha-a/town/internal/rng"

// Card geometry.
const (
	Size  = 5
	Cells = Size * Size
	// Centre is the free square (レガシー $senter='yes' の "00")。
	Centre = Cells / 2
	// FreeCell marks the centre in a stored card.
	FreeCell = 0
)

// Defaults mirror the legacy town_ini settings in bingo.cgi.
const (
	DefaultMaxNumber  = 60 // $kazu
	DefaultPerDay     = 20 // $hikeru_bi
	DefaultDays       = 3  // $kaisaibi_bi
	DefaultLinesToWin = 2  // $atarilain
	// MaxCardsPerPlayer is how many cards one player may hold ($b_set_mai).
	MaxCardsPerPlayer = 3
)

// Prize returns the payout for finishing in the given place (1-based),
// mirroring bingo.cgi:437-449.
func Prize(rank int) int64 {
	switch {
	case rank <= 1:
		return 500_000
	case rank <= 5:
		return 250_000
	default:
		return 150_000
	}
}

// Lines lists the 12 winning lines as cell indices (5 rows, 5 columns,
// 2 diagonals).
var Lines = buildLines()

func buildLines() [][Size]int {
	out := make([][Size]int, 0, 12)
	for r := range Size {
		var line [Size]int
		for c := range Size {
			line[c] = r*Size + c
		}
		out = append(out, line)
	}
	for c := range Size {
		var line [Size]int
		for r := range Size {
			line[r] = r*Size + c
		}
		out = append(out, line)
	}
	var d1, d2 [Size]int
	for i := range Size {
		d1[i] = i*Size + i
		d2[i] = i*Size + (Size - 1 - i)
	}
	return append(out, d1, d2)
}

// NewCard draws Cells-1 distinct numbers from 1..maxNumber and places them
// around the free centre.
func NewCard(r *rng.Rand, maxNumber int) []int {
	if maxNumber < Cells {
		maxNumber = Cells
	}
	pool := make([]int, maxNumber)
	for i := range pool {
		pool[i] = i + 1
	}
	// 先頭 Cells-1 個だけ確定すればよい(部分シャッフル)。
	for i := range Cells - 1 {
		j := i + r.IntN(maxNumber-i)
		pool[i], pool[j] = pool[j], pool[i]
	}
	card := make([]int, Cells)
	k := 0
	for i := range Cells {
		if i == Centre {
			card[i] = FreeCell
			continue
		}
		card[i] = pool[k]
		k++
	}
	return card
}

// Sequence returns 1..maxNumber in a random draw order.
func Sequence(r *rng.Rand, maxNumber int) []int {
	seq := make([]int, maxNumber)
	for i := range seq {
		seq[i] = i + 1
	}
	for i := maxNumber - 1; i > 0; i-- {
		j := r.IntN(i + 1)
		seq[i], seq[j] = seq[j], seq[i]
	}
	return seq
}

// Marks reports, per cell, whether it is covered by the drawn numbers. The free
// centre always counts as covered.
func Marks(card []int, drawn []int) []bool {
	hit := make(map[int]bool, len(drawn))
	for _, n := range drawn {
		hit[n] = true
	}
	out := make([]bool, len(card))
	for i, n := range card {
		out[i] = n == FreeCell || hit[n]
	}
	return out
}

// CompletedLines counts how many of the 12 lines are fully covered.
func CompletedLines(card []int, drawn []int) int {
	marks := Marks(card, drawn)
	count := 0
	for _, line := range Lines {
		full := true
		for _, idx := range line {
			if idx >= len(marks) || !marks[idx] {
				full = false
				break
			}
		}
		if full {
			count++
		}
	}
	return count
}

// DrawnCount returns how many numbers of the sequence are public after
// elapsedDays whole days (day 0 = the first day). It never exceeds the
// sequence length or perDay*days.
func DrawnCount(total, perDay, days, elapsedDays int) int {
	if elapsedDays < 0 {
		elapsedDays = 0
	}
	n := perDay * (elapsedDays + 1)
	if cap := perDay * days; n > cap {
		n = cap
	}
	return min(n, total)
}
