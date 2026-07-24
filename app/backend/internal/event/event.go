// Package event implements the random events (レガシー event.pl event_happen).
// On a town visit there is a 1/12 chance a random event fires, mutating the
// player's money, parameters, disease index or weight. This package is the pure
// selection/computation; the action service applies the outcome (ledger, params,
// special events like charity and confiscation). Item-dependent branches use the
// legacy's non-item outcome; adult (etti) and teleport events are excluded.
package event

import (
	"fmt"

	"github.com/shiroha-a/town/internal/rng"
)

// Outcome is a computed event result. Money-relative events (halve/double/…)
// are already resolved to an absolute MoneyDelta from the player's current money.
type Outcome struct {
	Name       string         `json:"name"`
	Message    string         `json:"message"`
	Good       bool           `json:"good"`
	MoneyDelta int64          `json:"money_delta"`
	Params     map[string]int `json:"params"`
	// DiseaseSet は disease_index への直接代入(レガシー$byouki_sisuu = N)。
	// 健康の貯金(プラス指数)に関係なく即その病状になる。nilで変化なし。
	DiseaseSet *int   `json:"disease_set"`
	WeightG    int    `json:"weight_g"`
	Special    string `json:"special"` // "charity" | "confiscate" | ""
}

func good(name, msg string, o Outcome) Outcome { o.Name, o.Message, o.Good = name, msg, true; return o }
func bad(name, msg string, o Outcome) Outcome  { o.Name, o.Message, o.Good = name, msg, false; return o }
func param(k string, v int) map[string]int     { return map[string]int{k: v} }

// subject builds a ±3 study-parameter event (rand 1/2 good or bad).
func subject(r *rng.Rand, key, name, upMsg, downMsg string) Outcome {
	if r.IntN(2) == 0 {
		return good(name, upMsg, Outcome{Params: param(key, 3)})
	}
	return bad(name, downMsg, Outcome{Params: param(key, -3)})
}

// events is the pool drawn from when an event fires (non-item branches).
var events = []func(r *rng.Rand, money int64, speed int) Outcome{
	// 1 泥棒
	func(r *rng.Rand, money int64, _ int) Outcome {
		if money <= 0 {
			return bad("泥棒", "泥棒に入られましたが、盗まれるお金がありませんでした。", Outcome{})
		}
		steal := min(int64(r.IntN(10000)+1), money)
		return bad("泥棒", fmt.Sprintf("泥棒に入られ、%d円盗まれました。", steal), Outcome{MoneyDelta: -steal})
	},
	// 2 財布拾い
	func(r *rng.Rand, _ int64, _ int) Outcome {
		g := int64(r.IntN(10000) + 1)
		return good("財布拾い", fmt.Sprintf("道でお財布を拾い、%d円ねこばばしました。", g), Outcome{MoneyDelta: g})
	},
	// 3 英語
	func(r *rng.Rand, _ int64, _ int) Outcome {
		return subject(r, "eigo", "英語", "宣教師に習い英語力が3アップしました。", "英語の先生に見放され3ダウンしました。")
	},
	// 5 NHK集金
	func(_ *rng.Rand, _ int64, _ int) Outcome {
		return bad("NHK集金", "NHKの集金が来て1000円払いました。", Outcome{MoneyDelta: -1000})
	},
	// 6 やけ食い(体重+1kg)
	func(_ *rng.Rand, _ int64, _ int) Outcome {
		return bad("やけ食い", "失恋のやけ食いで体重が1kg増えました。", Outcome{WeightG: 1000})
	},
	// 7-12 科目
	func(r *rng.Rand, _ int64, _ int) Outcome {
		return subject(r, "kokugo", "国語", "文学に目覚め国語が3アップしました。", "漢字を忘れ国語が3ダウンしました。")
	},
	func(r *rng.Rand, _ int64, _ int) Outcome {
		return subject(r, "suugaku", "数学", "数学パズルにハマり数学が3アップしました。", "数字恐怖症で数学が3ダウンしました。")
	},
	func(r *rng.Rand, _ int64, _ int) Outcome {
		return subject(r, "rika", "理科", "科学番組を見て理科が3アップしました。", "試験管を割り理科が3ダウンしました。")
	},
	func(r *rng.Rand, _ int64, _ int) Outcome {
		return subject(r, "syakai", "社会", "歴史の本を読み社会が3アップしました。", "社会への関心が薄れ社会が3ダウンしました。")
	},
	func(r *rng.Rand, _ int64, _ int) Outcome {
		return subject(r, "ongaku", "音楽", "ピアノを練習し音楽が3アップしました。", "ガラガラ声になり音楽が3ダウンしました。")
	},
	func(r *rng.Rand, _ int64, _ int) Outcome {
		return subject(r, "bijutsu", "美術", "芸術に目覚め美術が3アップしました。", "居眠りして美術が3ダウンしました。")
	},
	// 13 ラッキーくじ
	func(r *rng.Rand, _ int64, _ int) Outcome {
		k := int64(r.IntN(15) + 1)
		return good("ラッキーくじ", fmt.Sprintf("ラッキーくじが当選し%d万円ゲットしました。", k), Outcome{MoneyDelta: k * 10000})
	},
	// 14 優しい気持ち(LOVE+5)
	func(_ *rng.Rand, _ int64, _ int) Outcome {
		return good("優しい気持ち", "優しい気持ちになりLOVE度が5アップしました。", Outcome{Params: param("love", 5)})
	},
	// 15 風邪ぎみ(病気指数=-8の直接代入。レガシー$byouki_sisuu = -8)
	func(_ *rng.Rand, _ int64, _ int) Outcome {
		idx := -8
		return bad("体調不良", "裸で寝ていて体調を崩しました(風邪ぎみ)。", Outcome{DiseaseSet: &idx})
	},
	// 16 スリ(持ち金半減)
	func(_ *rng.Rand, money int64, _ int) Outcome {
		if money <= 0 {
			return bad("スリ", "スリに遭いましたが、盗まれるお金がありませんでした。", Outcome{})
		}
		return bad("スリ", "スリに遭い、持ち金が半分になりました。", Outcome{MoneyDelta: -(money / 2)})
	},
	// 17 本の印税
	func(r *rng.Rand, _ int64, _ int) Outcome {
		g := int64(1000)
		if r.IntN(2) == 1 {
			g = 500
		}
		return good("印税", fmt.Sprintf("出した本の印税が%d円入りました。", g), Outcome{MoneyDelta: g})
	},
	// 18 フラフープ
	func(r *rng.Rand, _ int64, _ int) Outcome {
		if r.IntN(2) == 0 {
			return good("フラフープ", "フラフープ大会で100円もらいました。", Outcome{MoneyDelta: 100})
		}
		return bad("フラフープ", "フラフープを壊し500円払いました。", Outcome{MoneyDelta: -500})
	},
	// 19 車にひかれる(speed判定)
	func(_ *rng.Rand, _ int64, speed int) Outcome {
		if speed > 180 {
			return good("交通事故", "フットワークで車をかわしました。", Outcome{})
		}
		return bad("交通事故", "車にひかれ入院費3万円かかりました。", Outcome{MoneyDelta: -30000})
	},
	// 20 香水
	func(r *rng.Rand, _ int64, _ int) Outcome {
		if r.IntN(2) == 0 {
			return good("香水", "良い香りがして300円拾いました。", Outcome{MoneyDelta: 300})
		}
		return bad("香水", "パチンコで1万円すりました。", Outcome{MoneyDelta: -10000})
	},
	// 22 地震 or 資産運用
	func(r *rng.Rand, money int64, _ int) Outcome {
		if r.IntN(2) == 0 {
			if money <= 0 {
				return bad("地震", "地震がありましたが、失うお金はありませんでした。", Outcome{})
			}
			return bad("地震", "地震で持ち金が3分の1になりました。", Outcome{MoneyDelta: -(money - money/3)})
		}
		if money <= 0 {
			return good("資産運用", "資産運用に成功しましたが、元手がありませんでした。", Outcome{})
		}
		return good("資産運用", "資産運用に成功し持ち金が倍になりました。", Outcome{MoneyDelta: money})
	},
	// 23 税務署(納税=持ち金の1%)
	func(_ *rng.Rand, money int64, _ int) Outcome {
		if money <= 0 {
			return good("税務署", "税務署が来ましたが、納税は免除されました。", Outcome{})
		}
		tax := money / 100
		return bad("税務署", fmt.Sprintf("持ち金の1%%(%d円)を納税しました。", tax), Outcome{MoneyDelta: -tax})
	},
	// 24 宝くじ(1/10当選)
	func(r *rng.Rand, _ int64, _ int) Outcome {
		if r.IntN(10) == 0 {
			win := int64(r.IntN(10)+1) * 1000000
			return good("宝くじ", fmt.Sprintf("宝くじが当選し%d円ゲットしました！", win), Outcome{MoneyDelta: win})
		}
		return bad("宝くじ", "宝くじは外れ、3000円損しました。", Outcome{MoneyDelta: -3000})
	},
	// 25 ノイローゼ(体重-1kg)
	func(_ *rng.Rand, _ int64, _ int) Outcome {
		return good("ノイローゼ", "ゲームのやりすぎで体重が1kg減りました。", Outcome{WeightG: -1000})
	},
	// 26 ストリートファイト巻き添え
	func(r *rng.Rand, _ int64, _ int) Outcome {
		switch r.IntN(5) {
		case 0:
			return bad("巻き添え", "喧嘩の巻き添えで身体パワーを消耗しました。", Outcome{Params: param("energy", -3)})
		case 1:
			return bad("巻き添え", "喧嘩の巻き添えで頭脳パワーを消耗しました。", Outcome{Params: param("nou_energy", -3)})
		case 2:
			return bad("巻き添え", "喧嘩の巻き添えで身体・頭脳パワーを消耗しました。", Outcome{Params: map[string]int{"energy": -2, "nou_energy": -2}})
		default:
			return good("巻き添え", "喧嘩から逃げ出して事なきを得ました。", Outcome{})
		}
	},
	// 27 温泉入浴券
	func(_ *rng.Rand, _ int64, _ int) Outcome {
		return good("温泉入浴券", "温泉入浴券で身体・頭脳パワーが回復しました。", Outcome{Params: map[string]int{"energy": 1000, "nou_energy": 1000}})
	},
	// 28 慈善(誰かに1万円プレゼント)
	func(_ *rng.Rand, _ int64, _ int) Outcome {
		return good("プレゼント", "街の誰かに10000円プレゼントしました。", Outcome{MoneyDelta: -10000, Special: "charity"})
	},
	// 29 幸せの星砂(非所持=10円)
	func(_ *rng.Rand, _ int64, _ int) Outcome {
		return good("星砂", "道に10円落ちていました。", Outcome{MoneyDelta: 10})
	},
	// 32 必要な品物(非所持=100円)
	func(_ *rng.Rand, _ int64, _ int) Outcome {
		return good("落とし物", "道で100円拾いました。", Outcome{MoneyDelta: 100})
	},
	// 34 差し押さえ(所持品を1つ没収)
	func(_ *rng.Rand, _ int64, _ int) Outcome {
		return bad("差し押さえ", "持ち物が1つ差し押さえられました。", Outcome{Special: "confiscate"})
	},
}

// Roll returns whether an event fired (1/12) and, if so, its computed outcome.
func Roll(r *rng.Rand, money int64, speed int) (bool, Outcome) {
	if r.IntN(12) != 0 {
		return false, Outcome{}
	}
	return true, events[r.IntN(len(events))](r, money, speed)
}

// Custom is an admin-defined event (content_events)。発生すると金額は
// [MoneyMin, MoneyMax]の一様乱数、パラメータ/病気/体重は固定値が適用される。
type Custom struct {
	Name       string
	Message    string
	Good       bool
	MoneyMin   int64
	MoneyMax   int64
	Params     map[string]int
	DiseaseSet *int
	WeightG    int
	Weight     int // 抽選の重み(組み込みイベントは各1)
}

// RollAll rolls the 1/12 occurrence and then picks from the built-in pool and
// the enabled custom events, weighted (built-ins weigh 1 each).
func RollAll(r *rng.Rand, money int64, speed int, customs []Custom) (bool, Outcome) {
	if r.IntN(12) != 0 {
		return false, Outcome{}
	}
	total := len(events)
	for _, c := range customs {
		total += max(1, c.Weight)
	}
	n := r.IntN(total)
	if n < len(events) {
		return true, events[n](r, money, speed)
	}
	n -= len(events)
	for _, c := range customs {
		w := max(1, c.Weight)
		if n < w {
			delta := c.MoneyMin
			if c.MoneyMax > c.MoneyMin {
				delta += int64(r.IntN(int(c.MoneyMax-c.MoneyMin) + 1))
			}
			o := Outcome{
				Name:       c.Name,
				Message:    c.Message,
				Good:       c.Good,
				MoneyDelta: delta,
				DiseaseSet: c.DiseaseSet,
				WeightG:    c.WeightG,
			}
			if len(c.Params) > 0 {
				o.Params = c.Params
			}
			return true, o
		}
		n -= w
	}
	// customsが並行更新で空になった場合の保険。
	return true, events[r.IntN(len(events))](r, money, speed)
}
