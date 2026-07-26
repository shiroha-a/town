package casino

import (
	"encoding/json"
	"errors"

	"github.com/shiroha-a/town/internal/rng"
)

// ロト(loto.cgi): 6桁の各位置に0-9を予想し、抽選6桁と位置ごとに一致した個数で
// 倍率が決まる即時くじ。ロト6(loto6.cgi)とは別物の旧型。
func init() { register("loto", loto{}) }

type loto struct{}

// lotoDigits is the number of predicted positions (旧loto.cgiのloto1..loto6で6桁固定)。
const lotoDigits = 6

// lotoBai maps the number of position matches (0..6) to the payout multiplier
// ($bai in loto.cgi)。プレイヤー有利だが仕様の倍率をそのまま使いリバランスしない。
var lotoBai = [lotoDigits + 1]int64{0, 2, 5, 20, 100, 500, 1000}

// Bets returns the allowed stake amounts (@loto_set3)。
func (loto) Bets() []int64 { return []int64{500, 1000, 5000, 10000, 50000, 100000} }

type lotoParams struct {
	Digits []int `json:"digits"` // 各位置の予想(0-9)、6桁
}

type lotoDetail struct {
	Picks      []int `json:"picks"`      // プレイヤーの予想6桁
	Draw       []int `json:"draw"`       // 抽選された6桁
	Matches    int   `json:"matches"`    // 位置ごとの一致数
	Multiplier int64 `json:"multiplier"` // 一致数に対応する倍率($bai)
}

// Play draws six independent digits (0-9) and pays by the count of positions
// that match the player's prediction.
func (loto) Play(r *rng.Rand, bet int64, params json.RawMessage) (Result, error) {
	var p lotoParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{}, errors.New("予想が正しくありません。")
	}
	if len(p.Digits) != lotoDigits {
		return Result{}, errors.New("6桁の数字を選んでください。")
	}
	for _, d := range p.Digits {
		if d < 0 || d > 9 {
			return Result{}, errors.New("各桁は0から9で選んでください。")
		}
	}
	// 各位置 int(rand(10))=0..9 を独立に抽選し、位置ごとの一致数を数える。
	draw := make([]int, lotoDigits)
	matches := 0
	for i := 0; i < lotoDigits; i++ {
		draw[i] = r.IntN(10)
		if draw[i] == p.Digits[i] {
			matches++
		}
	}
	mult := lotoBai[matches]
	// 現金型: 配当=掛け金×倍率(旧loto.cgi: money = money - rate + rate*bai)。
	// ネット損益はPayout-bet。一致0(倍率0)は総没収でPayout=0。
	payout := bet * mult
	return Result{
		Payout: payout,
		Win:    mult > 0,
		Detail: lotoDetail{
			Picks:      p.Digits,
			Draw:       draw,
			Matches:    matches,
			Multiplier: mult,
		},
	}, nil
}
