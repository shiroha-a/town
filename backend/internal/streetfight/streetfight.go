// Package streetfight implements ストリートファイト (legacy game.cgi mode=battle):
// the player walks into a random monster and the two trade blows until one side
// runs out of 身体パワー or 頭脳パワー. Every attack picks one of the 16
// parameters; the damage is simply the attacker's value minus the defender's, so
// a fight is won by being better than the opponent at the drawn ability.
//
// The battle itself is a pure function of (player stats, monster, rng) so it can
// be tested without a database.
package streetfight

import (
	"fmt"
	"strings"

	"github.com/shiroha-a/town/internal/rng"
)

// Abilities are the 16 parameters an attack may draw, in legacy attack order
// (1..16 of kougekinaiyou). The first 8 wear down 頭脳パワー, the rest 身体パワー.
var Abilities = []string{
	"kokugo", "suugaku", "rika", "syakai", "eigo", "ongaku", "bijutsu", "looks",
	"tairyoku", "kenkou", "speed", "power", "wanryoku", "kyakuryoku", "love", "omoshirosa",
}

// brainAttacks is how many of the leading abilities hit 頭脳パワー.
//
// レガシーは 9〜16 のうち 14(脚力) だけを頭脳側に振っていたが、その攻撃の文面は
// 「蹴りを浴び◯◯の肉体的ダメージ」で、ソースにも「以下番号ずれ」と書かれている。
// 表示と処理が食い違うだけなので、ここでは素直に前半8つを頭脳にする。
const brainAttacks = 8

// attack is one of the 16 attack flavours (legacy kougekinaiyou の文面)。
type attack struct {
	Ability string
	// Line is what the attacker does.
	Line string
	// Miss is shown when the defender is at least as good at that ability.
	Miss string
	// Hit is shown with the damage; %d is the damage.
	Hit string
}

var attacks = []attack{
	{"kokugo", "この漢字が読めるか？と歩み寄った！", "%sは漢字が得意だったのであっさり答えた。",
		"「よ、読めない。。」%sは%dの精神的ダメージを受けた！"},
	{"suugaku", "難しい数学の問題で困らせようとした！", "%sは数学が得意だったので効果が無かった。",
		"ちんぷんかんぷんだった%sは%dの精神的ダメージを受けた！"},
	{"rika", "理科の公式を唱えて動揺を誘った！", "%sは理科が得意だったので効果が無かった。",
		"頭が混乱した%sは%dの精神的ダメージを受けた！"},
	{"syakai", "有名な歴史の事件の年号を質問攻めした！", "%sは歴史が大得意だった。",
		"焦った%sは%dの精神的ダメージを受けた！"},
	{"eigo", "突然英語をしゃべり出した！", "%sも負けじと英語で答えた。",
		"「もっと英語を勉強せねば。。」と%sは%dの精神的ダメージを受けた！"},
	{"ongaku", "ピアノの鍵盤を叩き、この音階は何？と質問した！", "%sは簡単に質問に答えた。",
		"「わ、わからん。。」%sは%dの精神的ダメージを受けた！"},
	{"bijutsu", "すらすらと景色を描いて見せた！", "%sは「この下手くそが！」と吐き捨てた。",
		"「う、うまい。。」%sは%dの精神的ダメージを受けた！"},
	{"looks", "ルックスで勝負しろ！と迫った！", "%sは「勝ったね」とつぶやいた。",
		"「う。。」%sは%dの精神的ダメージを受けた！"},
	{"tairyoku", "体力勝負に持ち込んだ！", "%sは体力に自信があった。",
		"%sは%dの肉体的疲労を受けた！"},
	{"kenkou", "健康だけが自慢！と動揺させようとした。", "%sの方が若々しかった。",
		"%sは動揺し、%dの肉体的疲労を受けた！"},
	{"speed", "素早いフットワークで翻弄しようとした！", "%sの方が素早かった。",
		"%sは翻弄され%dの肉体的疲労を受けた！"},
	{"power", "パワーで投げ飛ばそうとした！", "%sはもっとパワーがあった。",
		"%sは投げ飛ばされ%dの肉体的ダメージを受けた！"},
	{"wanryoku", "パンチを浴びせた！", "しかし%sは軽くよけた。",
		"%sはパンチを浴び%dの肉体的ダメージを受けた！"},
	{"kyakuryoku", "蹴りかかった！", "しかし%sはさっとよけた。",
		"%sは蹴りを浴び%dの肉体的ダメージを受けた！"},
	{"love", "愛している恋人の自慢を始めた！", "%sはもっと恋人を愛していると言った。",
		"「う、うらやましい。。」%sは%dの精神的ダメージを受けた！"},
	{"omoshirosa", "笑わせ、そのスキに一発おみまいしようとした！", "「つまらん！」と%sは一蹴した。",
		"%sは笑い転げ、その隙に%dのパンチをくらった！"},
}

// Monster is one entry of the monster master.
type Monster struct {
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	Level        int            `json:"level"`
	WinMoney     int64          `json:"win_money"`
	LoseMoney    int64          `json:"lose_money"`
	Params       map[string]int `json:"params"`
	ItemRate     int            `json:"item_rate"`
	RewardItemID *int64         `json:"reward_item_id"`
	RewardName   string         `json:"reward_name"`
	Icon         string         `json:"icon"`
	Enabled      bool           `json:"enabled"`
}

// EnergyMax is the 身体パワー a set of parameters supports. Same formula as the
// player's (player.RefreshPowerMax), so a monster with a player's stats is as
// tough as that player.
func EnergyMax(p map[string]int) int {
	v := float64(p["looks"])/12 + float64(p["tairyoku"])/4 + float64(p["kenkou"])/4 +
		float64(p["speed"])/8 + float64(p["power"])/8 + float64(p["wanryoku"])/8 +
		float64(p["kyakuryoku"])/8
	return int(v) + 1
}

// NouEnergyMax is the 頭脳パワー a set of parameters supports.
func NouEnergyMax(p map[string]int) int {
	v := float64(p["kokugo"])/6 + float64(p["suugaku"])/6 + float64(p["rika"])/6 +
		float64(p["syakai"])/6 + float64(p["eigo"])/6 + float64(p["ongaku"])/6 +
		float64(p["bijutsu"])/6
	return int(v) + 1
}

// Turn is one attack and its aftermath.
type Turn struct {
	// Attacker is "player" or "monster".
	Attacker string `json:"attacker"`
	Ability  string `json:"ability"`
	Line     string `json:"line"`
	Text     string `json:"text"`
	// Damage is 0 when the attack did not land.
	Damage int `json:"damage"`
	// Target is "nou"(頭脳) or "energy"(身体); empty when nothing landed.
	Target        string `json:"target"`
	PlayerEnergy  int    `json:"player_energy"`
	PlayerNou     int    `json:"player_nou"`
	MonsterEnergy int    `json:"monster_energy"`
	MonsterNou    int    `json:"monster_nou"`
}

// Result is a whole fight.
type Result struct {
	Monster Monster `json:"monster"`
	// MonsterEnergyMax / MonsterNouMax are the monster's starting power.
	MonsterEnergyMax int    `json:"monster_energy_max"`
	MonsterNouMax    int    `json:"monster_nou_max"`
	Turns            []Turn `json:"turns"`
	// Outcome is "win", "lose" or "draw".
	Outcome string `json:"outcome"`
	// Reason names what ran out: "monster_energy", "monster_nou",
	// "player_energy", "player_nou" or "" for a draw.
	Reason string `json:"reason"`
	// Energy / Nou are the player's power after the fight.
	Energy int `json:"energy"`
	Nou    int `json:"nou"`
	// Money is the player's money delta (positive when they won).
	Money int64 `json:"money"`
	// RewardItemID is set when the win dropped the monster's prize.
	RewardItemID *int64 `json:"reward_item_id"`
	RewardName   string `json:"reward_name"`
}

// maxTurns caps a fight so two invincible sides cannot loop forever (legacy 50).
const maxTurns = 50

// Fight runs the whole battle. energy/nou are the player's current power; the
// player's remaining power is returned in Result. Money and the prize are decided
// here but applied by the caller.
func Fight(params map[string]int, energy, nou int, m Monster, r *rng.Rand) Result {
	res := Result{
		Monster:          m,
		MonsterEnergyMax: EnergyMax(m.Params),
		MonsterNouMax:    NouEnergyMax(m.Params),
		Turns:            []Turn{},
	}
	mEnergy, mNou := res.MonsterEnergyMax, res.MonsterNouMax
	// 速いほうが先攻(レガシー: 自分のスピードが上回ったときだけ自分から)。
	playerTurn := params["speed"] > m.Params["speed"]

	for range maxTurns {
		a := attacks[r.IntN(len(attacks))]
		var atk, def map[string]int
		t := Turn{Ability: a.Ability, Line: a.Line}
		if playerTurn {
			t.Attacker, atk, def = "player", params, m.Params
		} else {
			t.Attacker, atk, def = "monster", m.Params, params
		}
		// ダメージは「攻撃側の能力 − 防御側の同じ能力」。上回っていなければ効かない。
		dmg := atk[a.Ability] - def[a.Ability]
		defender := m.Name
		if !playerTurn {
			defender = "あなた"
		}
		if dmg <= 0 {
			t.Text = sprintf(a.Miss, defender, 0)
		} else {
			t.Damage = dmg
			t.Text = sprintf(a.Hit, defender, dmg)
			brain := indexOf(a.Ability) < brainAttacks
			if brain {
				t.Target = "nou"
			} else {
				t.Target = "energy"
			}
			if playerTurn {
				if brain {
					mNou -= dmg
				} else {
					mEnergy -= dmg
				}
			} else {
				if brain {
					nou -= dmg
				} else {
					energy -= dmg
				}
			}
		}
		t.PlayerEnergy, t.PlayerNou = energy, nou
		t.MonsterEnergy, t.MonsterNou = mEnergy, mNou
		res.Turns = append(res.Turns, t)

		switch {
		case mEnergy <= 0:
			res.Outcome, res.Reason = "win", "monster_energy"
		case mNou <= 0:
			res.Outcome, res.Reason = "win", "monster_nou"
		case energy <= 0:
			res.Outcome, res.Reason = "lose", "player_energy"
		case nou <= 0:
			res.Outcome, res.Reason = "lose", "player_nou"
		}
		if res.Outcome != "" {
			break
		}
		playerTurn = !playerTurn
	}
	if res.Outcome == "" {
		res.Outcome = "draw"
	}
	// パワーは0未満にしない(そのまま自分のパワーとして保存される)。
	res.Energy, res.Nou = max(0, energy), max(0, nou)

	switch res.Outcome {
	case "win":
		res.Money = m.WinMoney
		// 景品は 1/ItemRate で当たる(0=景品なし)。
		if m.RewardItemID != nil && m.ItemRate > 0 && r.IntN(m.ItemRate) == 0 {
			res.RewardItemID = m.RewardItemID
			res.RewardName = m.RewardName
		}
	case "lose":
		res.Money = -m.LoseMoney
	}
	return res
}

// sprintf fills an attack line: the miss lines only name the defender, the hit
// lines also carry the damage.
func sprintf(format, name string, dmg int) string {
	if strings.Contains(format, "%d") {
		return fmt.Sprintf(format, name, dmg)
	}
	return fmt.Sprintf(format, name)
}

// indexOf returns the attack order (0-based) of an ability.
func indexOf(ability string) int {
	for i, a := range Abilities {
		if a == ability {
			return i
		}
	}
	return len(Abilities)
}
