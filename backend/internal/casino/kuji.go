package casino

import (
	"encoding/json"
	"errors"
	"math"

	"github.com/shiroha-a/town/internal/rng"
)

// くじ(kuzi.cgi): 2枚のカードから1枚を選ぶ2択ダブルアップ。当たれば連勝段数(dabulu)が
// 上がり、賞金は倍率2^nで育つ。精算でその時点の賞金を受け取り、外れると掛け金は没収。
// 連勝段数はサーバ状態を持たず、クライアントがparams(Stage)で持ち回る。
func init() { register("kuji", kuji{}) }

type kuji struct{}

// Bets mirrors the legacy @kuzi_set3 bet candidates.
func (kuji) Bets() []int64 {
	return []int64{500, 1000, 5000, 10000, 50000, 100000, 500000, 1000000}
}

type kujiParams struct {
	Stage   int  `json:"stage"`   // current win streak (dabulu); 0 = fresh session
	Choice  int  `json:"choice"`  // chosen card (1 or 2); draw only
	Cashout bool `json:"cashout"` // true = settle current winnings without drawing
}

type kujiDetail struct {
	Action  string `json:"action"`  // "draw" | "settle"
	Card    int    `json:"card"`    // revealed winning card (1|2); 0 for settle
	Choice  int    `json:"choice"`  // player's picked card (1|2); 0 for settle
	Win     bool   `json:"win"`     // whether the draw won
	Stage   int    `json:"stage"`   // win streak after this play (0 = reset)
	Bairitu int64  `json:"bairitu"` // settle multiplier 2^(stage-1) at the current stage
	Syoukin int64  `json:"syoukin"` // settle value at the current stage / amount paid on settle
}

// mulPow2 returns bet * 2^e, saturating at math.MaxInt64. The legacy game caps
// neither the streak nor the bet, so this guards against an int64 overflow on an
// (astronomically unlikely) long winning streak that would otherwise wrap
// negative and corrupt the payout sign in the ledger.
func mulPow2(bet int64, e int) int64 {
	v := bet
	for i := 0; i < e; i++ {
		if v > math.MaxInt64/2 {
			return math.MaxInt64
		}
		v *= 2
	}
	return v
}

func (kuji) Play(r *rng.Rand, bet int64, params json.RawMessage) (Result, error) {
	var p kujiParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{}, errors.New("賭け方が正しくありません。")
	}
	if p.Stage < 0 {
		return Result{}, errors.New("連勝段数が正しくありません。")
	}

	// 精算(command2=seisan): カードを引かずに現在の賞金を確定して受け取る。
	// atri_moto_hiku='yes'相当: Payoutに掛け金の返却分を含めた総額(rate*2^(dabulu-1))を返す。
	if p.Cashout {
		if p.Stage < 1 {
			return Result{}, errors.New("精算できる賞金がありません。")
		}
		syoukin := mulPow2(bet, p.Stage-1) // rate * 2^(dabulu-1)
		return Result{
			Payout: syoukin,
			Win:    true,
			Detail: kujiDetail{
				Action:  "settle",
				Win:     true,
				Stage:   0,
				Bairitu: mulPow2(1, p.Stage-1),
				Syoukin: syoukin,
			},
		}, nil
	}

	// 抽選(command=hiku): randed = int(rand(2))+1 → 1 or 2、当選確率1/2。
	if p.Choice != 1 && p.Choice != 2 {
		return Result{}, errors.New("カードを選んでください。")
	}
	randed := r.IntN(2) + 1
	win := p.Choice == randed
	if !win {
		// 外れ: 掛け金没収(Payout=0でネット-bet)、段数リセット。
		return Result{
			Payout: 0,
			Win:    false,
			Detail: kujiDetail{Action: "draw", Card: randed, Choice: p.Choice, Win: false, Stage: 0},
		}, nil
	}
	// 当たり: 段数++。勝ち分はまだ確定させず、Payout=bet(ネット0)で掛け金をエスクロー継続。
	// レガシー同様、実際の増減は精算(settle)か次の外れまで発生しない。
	stage := p.Stage + 1
	return Result{
		Payout: bet, // 掛け金の返却のみ(ネット0)。勝ち分は精算で確定する。
		Win:    true,
		Detail: kujiDetail{
			Action:  "draw",
			Card:    randed,
			Choice:  p.Choice,
			Win:     true,
			Stage:   stage,
			Bairitu: mulPow2(1, stage-1),
			Syoukin: mulPow2(bet, stage-1),
		},
	}, nil
}
