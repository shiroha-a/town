package casino

import (
	"encoding/json"
	"errors"

	"github.com/shiroha-a/town/internal/rng"
)

// サイコロ(saikoro.cgi): 2個のサイコロの合計が偶数か奇数かを当てる。1:1のフェア。
func init() { register("saikoro", dice{}) }

type dice struct{}

func (dice) Bets() []int64 { return []int64{10000, 100000, 500000, 1000000} }

type diceParams struct {
	Choice string `json:"choice"` // "even" | "odd"
}

type diceDetail struct {
	Dice1  int    `json:"dice1"`
	Dice2  int    `json:"dice2"`
	Sum    int    `json:"sum"`
	Result string `json:"result"` // "even" | "odd"
}

func (dice) Play(r *rng.Rand, bet int64, params json.RawMessage) (Result, error) {
	var p diceParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{}, errors.New("賭け方が正しくありません。")
	}
	if p.Choice != "even" && p.Choice != "odd" {
		return Result{}, errors.New("偶数か奇数を選んでください。")
	}
	// rand(36)=0..35 → 2d6一様。偶奇は各1/2(完全フェア)。
	n := r.IntN(36)
	d1 := n/6 + 1
	d2 := n%6 + 1
	sum := d1 + d2
	res := "odd"
	if sum%2 == 0 {
		res = "even"
	}
	win := res == p.Choice
	var payout int64
	if win {
		payout = bet * 2 // 掛け金の返却+勝ち分
	}
	return Result{
		Payout: payout,
		Win:    win,
		Detail: diceDetail{Dice1: d1, Dice2: d2, Sum: sum, Result: res},
	}, nil
}
