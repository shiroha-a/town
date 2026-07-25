package casino

import (
	"encoding/json"
	"errors"

	"github.com/shiroha-a/town/internal/rng"
)

// ドーナツ(donuts.cgi): 1..5のカードを引き、前のカードよりHi/Loを当てるHi&Lo。
// 当たると累積枚数×1万円を得てカードが1枚積まれ、外れると同額を失いテーブルは1枚に戻る。
// 同じ数字はセーフ(損得なし)で、doroubai倍を累積に適用して継続する。
// レガシーはサブ名不一致や$kihongaku構文誤りで壊れていたため、カタログの「意図された仕様」で再構築する。
func init() { register("donuts", donuts{}) }

type donuts struct{}

const (
	// donutsUnit is the base stake per stacked card (1万円). The actual bet of one
	// draw is Count * donutsUnit.
	donutsUnit = 10000
	// donutsDoroubai is the multiplier applied to the stack when the drawn card
	// matches the previous one (セーフ, $doroubai=2).
	donutsDoroubai = 2
	// donutsMaxStack bounds the accumulated card count so that Count*donutsUnit
	// cannot overflow int64 for any client-supplied state.
	donutsMaxStack = 1 << 40
)

// donutsParams is the client-held game state for one draw. The previous card and
// the accumulated stack are carried by the client (the server keeps no state),
// matching the spec that ドーナツの前カードはparamsで受け取る.
type donutsParams struct {
	Choice string `json:"choice"` // "hi" | "low"
	Prev   int    `json:"prev"`   // 直前のカード(1..5)
	Count  int64  `json:"count"`  // 累積カード枚数(>=1)。掛け金 = Count*1万
}

type donutsDetail struct {
	Prev      int    `json:"prev"`       // 比較対象の前カード
	Card      int    `json:"card"`       // 引いたカード(1..5)
	Choice    string `json:"choice"`     // "hi" | "low"
	Count     int64  `json:"count"`      // このプレイ時点の累積枚数
	Outcome   string `json:"outcome"`    // "win" | "lose" | "safe"
	NextPrev  int    `json:"next_prev"`  // 次ラウンドの前カード(=引いたカード)
	NextCount int64  `json:"next_count"` // 次ラウンドの累積枚数
}

func (donuts) Bets() []int64 { return nil } // 掛け金は累積枚数で変動するため任意額を許容

func (donuts) Play(r *rng.Rand, bet int64, params json.RawMessage) (Result, error) {
	var p donutsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{}, errors.New("賭け方が正しくありません。")
	}
	if p.Choice != "hi" && p.Choice != "low" {
		return Result{}, errors.New("ハイかローを選んでください。")
	}
	if p.Prev < 1 || p.Prev > 5 {
		return Result{}, errors.New("前のカードが正しくありません。")
	}
	if p.Count < 1 || p.Count > donutsMaxStack {
		return Result{}, errors.New("累積枚数が正しくありません。")
	}
	// 掛け金は「累積枚数×1万円」で確定する。クライアントが送るbetがこれと不一致なら不正操作。
	if bet != p.Count*donutsUnit {
		return Result{}, errors.New("掛け金が累積枚数と一致しません。")
	}

	// rand(5)+1 → 1..5一様。
	card := r.IntN(5) + 1

	var outcome string
	var payout int64
	var nextCount int64
	switch {
	case card == p.Prev:
		// 同じ数字はセーフ。損得なし(payout=bet)で、累積にdoroubai倍を適用して継続。
		outcome = "safe"
		payout = bet
		nextCount = p.Count * donutsDoroubai
		if nextCount > donutsMaxStack {
			nextCount = donutsMaxStack
		}
	case (p.Choice == "hi" && card > p.Prev) || (p.Choice == "low" && card < p.Prev):
		// 当たり: 累積枚数×1万を得る(1:1)。カードが1枚積まれる。
		outcome = "win"
		payout = bet * 2 // 掛け金の返却+勝ち分
		nextCount = p.Count + 1
	default:
		// 外れ: 同額を失い、テーブルは1枚に戻る。
		outcome = "lose"
		payout = 0
		nextCount = 1
	}

	return Result{
		Payout: payout,
		Win:    outcome == "win",
		Detail: donutsDetail{
			Prev:      p.Prev,
			Card:      card,
			Choice:    p.Choice,
			Count:     p.Count,
			Outcome:   outcome,
			NextPrev:  card,
			NextCount: nextCount,
		},
	}, nil
}
