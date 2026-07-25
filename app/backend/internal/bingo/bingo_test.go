package bingo

import (
	"testing"

	"github.com/shiroha-a/town/internal/rng"
)

func TestLines(t *testing.T) {
	if len(Lines) != 12 {
		t.Fatalf("lines = %d, want 12 (5行+5列+斜め2)", len(Lines))
	}
	// 各セルが少なくとも1本のラインに含まれること。
	seen := map[int]bool{}
	for _, l := range Lines {
		for _, idx := range l {
			if idx < 0 || idx >= Cells {
				t.Fatalf("cell index %d out of range", idx)
			}
			seen[idx] = true
		}
	}
	if len(seen) != Cells {
		t.Errorf("covered cells = %d, want %d", len(seen), Cells)
	}
}

func TestNewCard(t *testing.T) {
	r := rng.New(1)
	for range 30 {
		c := NewCard(r, DefaultMaxNumber)
		if len(c) != Cells {
			t.Fatalf("cells = %d", len(c))
		}
		if c[Centre] != FreeCell {
			t.Fatalf("centre = %d, want free", c[Centre])
		}
		seen := map[int]bool{}
		for i, n := range c {
			if i == Centre {
				continue
			}
			if n < 1 || n > DefaultMaxNumber {
				t.Fatalf("number %d out of range", n)
			}
			if seen[n] {
				t.Fatalf("duplicate number %d", n)
			}
			seen[n] = true
		}
	}
}

func TestSequenceIsPermutation(t *testing.T) {
	r := rng.New(2)
	seq := Sequence(r, DefaultMaxNumber)
	if len(seq) != DefaultMaxNumber {
		t.Fatalf("len = %d", len(seq))
	}
	seen := map[int]bool{}
	for _, n := range seq {
		if n < 1 || n > DefaultMaxNumber || seen[n] {
			t.Fatalf("bad sequence value %d", n)
		}
		seen[n] = true
	}
}

func TestCompletedLines(t *testing.T) {
	// 1..25 を並べたカード(中央だけフリー)。
	card := make([]int, Cells)
	for i := range card {
		card[i] = i + 1
	}
	card[Centre] = FreeCell

	// 何も引いていない: フリー中央だけではどのラインも揃わない。
	if got := CompletedLines(card, nil); got != 0 {
		t.Errorf("no draw = %d lines, want 0", got)
	}
	// 1行目(1..5)を引く → 1ライン。
	if got := CompletedLines(card, []int{1, 2, 3, 4, 5}); got != 1 {
		t.Errorf("row = %d lines, want 1", got)
	}
	// 中央を通る列(3,8,18,23 + フリー) → 1ライン。
	if got := CompletedLines(card, []int{3, 8, 18, 23}); got != 1 {
		t.Errorf("centre column = %d lines, want 1", got)
	}
	// 斜め(1,7,19,25 + フリー) → 1ライン。
	if got := CompletedLines(card, []int{1, 7, 19, 25}); got != 1 {
		t.Errorf("diagonal = %d lines, want 1", got)
	}
	// 1行目と中央列 → 2ライン。
	if got := CompletedLines(card, []int{1, 2, 3, 4, 5, 8, 18, 23}); got != 2 {
		t.Errorf("row+col = %d lines, want 2", got)
	}
}

func TestMarksFreeCentre(t *testing.T) {
	card := NewCard(rng.New(3), DefaultMaxNumber)
	m := Marks(card, nil)
	if !m[Centre] {
		t.Error("centre should always be marked")
	}
	for i, v := range m {
		if i != Centre && v {
			t.Fatalf("cell %d marked with no draws", i)
		}
	}
}

func TestDrawnCount(t *testing.T) {
	// 60個/20個ずつ/3日
	cases := []struct{ elapsed, want int }{
		{0, 20}, {1, 40}, {2, 60}, {3, 60}, {99, 60}, {-1, 20},
	}
	for _, c := range cases {
		if got := DrawnCount(60, 20, 3, c.elapsed); got != c.want {
			t.Errorf("DrawnCount(elapsed=%d) = %d, want %d", c.elapsed, got, c.want)
		}
	}
	// 数列が短ければそれが上限。
	if got := DrawnCount(10, 20, 3, 0); got != 10 {
		t.Errorf("short sequence = %d, want 10", got)
	}
}

func TestPrize(t *testing.T) {
	cases := map[int]int64{1: 500_000, 2: 250_000, 5: 250_000, 6: 150_000, 99: 150_000}
	for rank, want := range cases {
		if got := Prize(rank); got != want {
			t.Errorf("Prize(%d) = %d, want %d", rank, got, want)
		}
	}
}
