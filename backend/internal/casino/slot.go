package casino

import (
	"encoding/json"

	"github.com/shiroha-a/town/internal/rng"
)

// スロット(slot.cgi): 3リール×3段(3×3グリッド)を回し、8本のライン上に同一絵柄が
// 3つ揃うと「掛け金×絵柄倍率」を払い戻す。複数ライン成立は合算する。
// レガシーは絵柄0(最弱)が揃うと現金でなくステータス還元だが、Playは純粋関数で
// プレイヤーを変更できないため、仕様の倍率表どおり絵柄0を×1の現金配当として扱う
// (付録の「8ライン合算」に準拠。EVが有利側でもリバランスしない)。
func init() { register("slot", slot{}) }

type slot struct{}

// Bets returns the allowed stakes (@l1_stt3 / @l1_set3 in slot.cgi).
func (slot) Bets() []int64 { return []int64{500, 1000, 5000, 10000, 50000, 100000} }

// 絵柄0..6の配当倍率(@l1_bauritu)。絵柄0=×1, 6=×7777。
var slotMultipliers = [7]int64{1, 20, 80, 200, 800, 2000, 7777}

// 各リールの絵柄別ストップ数(絵柄0..6)。総数はリール1=37, リール2=48, リール3=53。
// 絵柄0が最頻で、番号が大きいほど希少。
var slotReels = [3][7]int{
	{16, 6, 5, 4, 3, 2, 1},
	{27, 6, 5, 4, 3, 2, 1},
	{32, 6, 5, 4, 3, 2, 1},
}

// 大当たり閾値: 合計倍率が200倍以上のとき強調表示する(l1_log.cgiの大当たり判定)。
const slotBigMultiplier = 200

// slotLineCells lists the (row, reel) cells of each of the 8 paylines in the
// legacy order: center/left/right columns, both diagonals, then top/middle/
// bottom rows. Row 0 is the top row; reel 0 is the leftmost reel.
var slotLineCells = [8][3][2]int{
	{{0, 1}, {1, 1}, {2, 1}}, // 1: 中列
	{{0, 0}, {1, 0}, {2, 0}}, // 2: 左列
	{{0, 2}, {1, 2}, {2, 2}}, // 3: 右列
	{{0, 0}, {1, 1}, {2, 2}}, // 4: 対角＼
	{{0, 2}, {1, 1}, {2, 0}}, // 5: 対角／
	{{0, 0}, {0, 1}, {0, 2}}, // 6: 上段行
	{{1, 0}, {1, 1}, {1, 2}}, // 7: 中段行
	{{2, 0}, {2, 1}, {2, 2}}, // 8: 下段行
}

// slotLine is one matched payline in the result.
type slotLine struct {
	Line   int   `json:"line"`   // ライン番号(1..8)
	Symbol int   `json:"symbol"` // 揃った絵柄(0..6)
	Mult   int64 `json:"mult"`   // その絵柄の倍率
}

// slotDetail is the per-play result serialized to the frontend.
type slotDetail struct {
	Grid       [3][3]int  `json:"grid"`       // grid[row][reel], row 0 = 上段
	Lines      []slotLine `json:"lines"`      // 成立したライン(0本ならはずれ)
	Multiplier int64      `json:"multiplier"` // 合計倍率(payout/bet)
	Big        bool       `json:"big"`        // 200倍以上の大当たり
}

// Play spins the three reels and pays out the sum over all 8 winning paylines.
// Slot takes no player choice or carried state, so params is ignored.
func (slot) Play(r *rng.Rand, bet int64, params json.RawMessage) (Result, error) {
	// grid[row][reel] を各リールの無置換抽選から組み立てる。
	var grid [3][3]int
	for reel := 0; reel < 3; reel++ {
		cells := drawReel(r, slotReels[reel])
		for row := 0; row < 3; row++ {
			grid[row][reel] = cells[row]
		}
	}

	lines := []slotLine{}
	var payout int64
	for i, cells := range slotLineCells {
		s0 := grid[cells[0][0]][cells[0][1]]
		s1 := grid[cells[1][0]][cells[1][1]]
		s2 := grid[cells[2][0]][cells[2][1]]
		if s0 == s1 && s1 == s2 {
			mult := slotMultipliers[s0]
			payout += bet * mult
			lines = append(lines, slotLine{Line: i + 1, Symbol: s0, Mult: mult})
		}
	}

	var totalMult int64
	if bet > 0 {
		totalMult = payout / bet
	}
	return Result{
		Payout: payout, // 元金の返却分を含む(motokin_hiku='yes'の net = payout - bet)
		Win:    payout > 0,
		Detail: slotDetail{
			Grid:       grid,
			Lines:      lines,
			Multiplier: totalMult,
			Big:        totalMult >= slotBigMultiplier,
		},
	}, nil
}

// drawReel builds a reel strip from the per-symbol stop counts, then draws three
// distinct stop positions without replacement (legacy randcheck) and returns the
// symbols shown in the top/middle/bottom cells of that reel. Because the draw is
// uniform without replacement, only the stop counts (not the strip order) affect
// the distribution.
func drawReel(r *rng.Rand, counts [7]int) [3]int {
	strip := make([]int, 0, 64)
	for sym, c := range counts {
		for i := 0; i < c; i++ {
			strip = append(strip, sym)
		}
	}
	// 部分Fisher-Yatesで先頭3要素を無置換抽選する。
	var cells [3]int
	for i := 0; i < 3; i++ {
		j := i + r.IntN(len(strip)-i)
		strip[i], strip[j] = strip[j], strip[i]
		cells[i] = strip[i]
	}
	return cells
}
