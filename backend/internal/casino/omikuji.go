package casino

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shiroha-a/town/internal/rng"
)

// おみくじ(omikuji.cgi): お賽銭額(saisengaku 1-7)と占う項目(kibou)を選び、8段階の運勢を引く。
// 運勢に応じて対象カテゴリの能力が増減し、金運では金銭が増減する。賽銭を「ちょろまかす」
// 選択は所持金が増えるが凶寄りの運になる。掛け金は取らず(bet=0)、賽銭・金運の金銭変動は
// MoneyDelta、運勢のステータス増減はParams、超大吉の破魔矢はItemsで表現する。
func init() { register("omikuji", omikuji{}) }

type omikuji struct{}

// Bets returns nil: おみくじは掛け金を取らない(所持金の変動は賽銭=MoneyDeltaで扱う)。
func (omikuji) Bets() []int64 { return nil }

// omikujiSaisen maps saisengaku(1..7) to the amount SUBTRACTED from cash in the
// legacy code ($saisen)。負値は「ちょろまかし」で所持金が増える。プレイヤーの所持金
// 変動(MoneyDelta)は -$saisen になる。
var omikujiSaisen = [7]int64{-100000, -10000, -100, 100, 1000, 10000, 100000}

// omikujiSoutaiun/omikujiSokoage are indexed by kihon_kuji(=saisengaku-1)。
// 各項目の運勢値は v = int(rand(soutaiun)) + sokoage で引く。賽銭が高いほど底上げ
// (sokoage)が上がり大吉寄りになり、ちょろまかしは範囲(soutaiun)が狭く凶寄りになる。
var omikujiSoutaiun = [7]int{15, 48, 182, 1041, 1027, 994, 859}
var omikujiSokoage = [7]int{0, 0, 0, 0, 14, 47, 182}

// omikujiUnseiName maps unsei(1..8) to its fortune name(index0 is unused)。
var omikujiUnseiName = [9]string{"", "超大吉", "大凶", "凶", "末吉", "吉", "小吉", "中吉", "大吉"}

// omikujiStatCount/omikujiStatAmount map unsei to (能力を変える個数, 各増減量) for the
// stat categories(学問/健康/恋愛)。unsei1(超大吉)は破魔矢、unsei4(末吉)は無効果。
// unsei8(大吉)はcount=4だが、恋愛のように能力数が4未満のカテゴリでは能力数にクランプ
// され全項目が対象になる(レガシーの恋愛大吉=全項目+8と一致)。
var omikujiStatCount = [9]int{0, 0, 2, 1, 0, 1, 2, 3, 4}
var omikujiStatAmount = [9]int64{0, 0, -10, -5, 0, 1, 2, 4, 8}

// omikujiKinnMoney maps unsei to the MoneyDelta contribution for 金運(賽銭は含まない)。
// レガシーの大凶/凶は「+20万/+2万」の現金と同時にbank-20万/-2万と借金(loan日額増)を
// 発生させ、差し引きの即時純資産変動は0になる。casino.Resultにはbank/loanを操作する術が
// 無く借金の継続利息も表現できないため、即時純資産に忠実な0で表現する。
var omikujiKinnMoney = [9]int64{0, 0, 0, 0, -100, 0, 1000, 10000, 100000}

// 各カテゴリの対象能力。恋愛はレガシーでは[looks,love,面白さ,エッチ]だが、エッチは
// 本システムの16スキルに存在しないため除外し、[looks,love,omoshirosa]の3項目で再現する。
var (
	omikujiGakuAbilities = []string{"kokugo", "suugaku", "rika", "syakai", "eigo", "ongaku", "bijutsu"}
	omikujiKennAbilities = []string{"tairyoku", "kenkou", "speed", "power", "wanryoku", "kyakuryoku"}
	omikujiRennAbilities = []string{"looks", "love", "omoshirosa"}
)

// omikujiParamLabel gives the Japanese display name for each affected skill.
var omikujiParamLabel = map[string]string{
	"kokugo": "国語", "suugaku": "数学", "rika": "理科", "syakai": "社会",
	"eigo": "英語", "ongaku": "音楽", "bijutsu": "美術",
	"tairyoku": "体力", "kenkou": "健康", "speed": "スピード", "power": "パワー",
	"wanryoku": "腕力", "kyakuryoku": "脚力",
	"looks": "ルックス", "love": "ラブ度", "omoshirosa": "面白さ",
}

type omikujiParams struct {
	Saisengaku int    `json:"saisengaku"` // 1..7
	Kibou      string `json:"kibou"`      // "gaku" | "kenn" | "renn" | "kinn"
}

// omikujiFortune is one drawn fortune, kept for display.
type omikujiFortune struct {
	Kibou string `json:"kibou"` // "gaku"|"kenn"|"renn"|"kinn"|"zentai"
	Unsei int    `json:"unsei"` // 1..8
	Name  string `json:"name"`  // 運勢名(超大吉..大吉)
}

// omikujiChange is one applied stat change.
type omikujiChange struct {
	Param  string `json:"param"`  // 16スキル名
	Label  string `json:"label"`  // 日本語表示名
	Amount int64  `json:"amount"` // 増減量
}

// omikujiDetail is the per-play result serialized to the frontend.
type omikujiDetail struct {
	Saisengaku  int              `json:"saisengaku"`
	SaisenMoney int64            `json:"saisen_money"` // 賽銭による所持金変動(+ちょろまかし/-納付)
	KinnMoney   int64            `json:"kinn_money"`   // 金運の運勢による所持金変動(賽銭を除く)
	MoneyDelta  int64            `json:"money_delta"`  // 合計所持金変動(賽銭+金運)
	Kibou       string           `json:"kibou"`
	KibouName   string           `json:"kibou_name"` // 学問/健康/恋愛/金運
	Fortunes    []omikujiFortune `json:"fortunes"`   // 4項目の運勢(表示用)
	Overall     omikujiFortune   `json:"overall"`    // 全体運(表示用)
	Result      omikujiFortune   `json:"result"`     // 選んだkibouの運勢(効果対象)
	Changes     []omikujiChange  `json:"changes"`    // 適用されたステータス変化
	Hamaya      bool             `json:"hamaya"`     // 超大吉で破魔矢を授かったか
	Message     string           `json:"message"`    // 結果コメント(mse3相当)
}

// Play draws the fortunes for all four categories (for display), then applies the
// effect of the chosen category's fortune. お賽銭・金運はMoneyDelta、ステータスは
// Params、超大吉の破魔矢はItemsに載せる。掛け金は取らないためbetは0でなければならない。
func (omikuji) Play(r *rng.Rand, bet int64, params json.RawMessage) (Result, error) {
	if bet != 0 {
		return Result{}, errors.New("おみくじに掛け金は不要です。")
	}
	var p omikujiParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{}, errors.New("おみくじの指定が正しくありません。")
	}
	if p.Saisengaku < 1 || p.Saisengaku > 7 {
		return Result{}, errors.New("お賽銭を選んでください。")
	}
	abilities, kibouName, ok := omikujiCategory(p.Kibou)
	if !ok {
		return Result{}, errors.New("占う項目を選んでください。")
	}

	idx := p.Saisengaku - 1
	soutaiun := omikujiSoutaiun[idx]
	sokoage := omikujiSokoage[idx]

	// 学問/健康/恋愛/金運の順に各運勢値を独立に引く(表示用に4項目すべて保持する)。
	kibous := [4]string{"gaku", "kenn", "renn", "kinn"}
	var raw [4]int
	fortunes := make([]omikujiFortune, 4)
	for i, k := range kibous {
		v := r.IntN(soutaiun) + sokoage
		raw[i] = v
		u := omikujiUnsei(v)
		fortunes[i] = omikujiFortune{Kibou: k, Unsei: u, Name: omikujiUnseiName[u]}
	}

	// 全体運: 特定運(4項目からランダムに1つ)を加えた5値平均。表示専用でステには影響しない。
	tokutei := raw[r.IntN(4)]
	zentaiV := (raw[0] + raw[1] + raw[2] + raw[3] + tokutei) / 5
	zu := omikujiUnsei(zentaiV)
	overall := omikujiFortune{Kibou: "zentai", Unsei: zu, Name: omikujiUnseiName[zu]}

	// 選んだkibouの運勢を効果対象にする。
	var chosen omikujiFortune
	for _, f := range fortunes {
		if f.Kibou == p.Kibou {
			chosen = f
		}
	}
	unsei := chosen.Unsei

	detail := omikujiDetail{
		Saisengaku: p.Saisengaku,
		Kibou:      p.Kibou,
		KibouName:  kibouName,
		Fortunes:   fortunes,
		Overall:    overall,
		Result:     chosen,
	}
	var res Result

	// 賽銭による所持金変動($money -= $saisen → プレイヤーのMoneyDelta = -$saisen)。
	saisenMoney := -omikujiSaisen[idx]
	detail.SaisenMoney = saisenMoney
	res.MoneyDelta += saisenMoney

	switch {
	case unsei == 1:
		// 超大吉は全カテゴリ共通で破魔矢を授かる。
		detail.Hamaya = true
		detail.Message = "破魔矢を授かった。"
		res.Items = append(res.Items, ItemGrant{Name: "破魔矢", Qty: 1})
	case p.Kibou == "kinn":
		// 金運は金銭効果(MoneyDelta)。
		km := omikujiKinnMoney[unsei]
		detail.KinnMoney = km
		res.MoneyDelta += km
		detail.Message = omikujiKinnMessage(unsei)
	default:
		// 学問/健康/恋愛は運勢に応じて対象能力を増減する(splice相当の無置換抽選)。
		count := omikujiStatCount[unsei]
		amount := omikujiStatAmount[unsei]
		for _, ab := range omikujiPick(r, abilities, count) {
			detail.Changes = append(detail.Changes, omikujiChange{Param: ab, Label: omikujiParamLabel[ab], Amount: amount})
			res.Params = append(res.Params, ParamDelta{Param: ab, Amount: amount})
		}
		detail.Message = omikujiStatMessage(detail.Changes)
	}

	detail.MoneyDelta = res.MoneyDelta
	// Win: 超大吉(1)または吉以上(5..8)を良い結果とみなす(表示用)。
	res.Win = unsei == 1 || unsei >= 5
	res.Detail = detail
	return res, nil
}

// omikujiUnsei bands a drawn luck value v into unsei(1..8):
// 1=超大吉, 2=大凶, 3=凶, 4=末吉, 5=吉, 6=小吉, 7=中吉, 8=大吉。
// レガシーはelsif連鎖で吉(183-368)が小吉の重複範囲を先取りするため、実効バンドは
// 以下の連続区間になる(spec: 吉優先で実質369-530が小吉)。
func omikujiUnsei(v int) int {
	switch {
	case v == 0:
		return 1
	case v <= 14:
		return 2
	case v <= 47:
		return 3
	case v <= 182:
		return 4
	case v <= 368:
		return 5
	case v <= 530:
		return 6
	case v <= 697:
		return 7
	default:
		return 8
	}
}

// omikujiCategory returns the target ability set and Japanese label for a kibou.
// 金運(kinn)は金銭効果のため能力集合を持たない。
func omikujiCategory(kibou string) (abilities []string, name string, ok bool) {
	switch kibou {
	case "gaku":
		return omikujiGakuAbilities, "学問", true
	case "kenn":
		return omikujiKennAbilities, "健康", true
	case "renn":
		return omikujiRennAbilities, "恋愛", true
	case "kinn":
		return nil, "金運", true
	default:
		return nil, "", false
	}
}

// omikujiPick draws n distinct abilities without replacement (レガシーのspliceに相当)。
// nが能力数を超える場合は能力数にクランプする(恋愛の中吉/大吉などで全項目が対象になる)。
func omikujiPick(r *rng.Rand, abilities []string, n int) []string {
	if n <= 0 {
		return nil
	}
	pool := make([]string, len(abilities))
	copy(pool, abilities)
	if n > len(pool) {
		n = len(pool)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		j := i + r.IntN(len(pool)-i)
		pool[i], pool[j] = pool[j], pool[i]
		out = append(out, pool[i])
	}
	return out
}

// omikujiStatMessage builds the result comment (mse3相当) for a stat category.
func omikujiStatMessage(changes []omikujiChange) string {
	if len(changes) == 0 {
		return "何も起こらなかった。"
	}
	msg := ""
	for _, c := range changes {
		if c.Amount < 0 {
			msg += fmt.Sprintf("%sが%d下がった。", c.Label, -c.Amount)
		} else {
			msg += fmt.Sprintf("%sが%d上がった。", c.Label, c.Amount)
		}
	}
	return msg
}

// omikujiKinnMessage builds the result comment for the 金運 category.
func omikujiKinnMessage(unsei int) string {
	switch unsei {
	case 2:
		return "20万円が当たったが、同額の借金を背負い差し引きゼロだった。"
	case 3:
		return "2万円が当たったが、同額の借金を背負い差し引きゼロだった。"
	case 4:
		return "100円を落とした。"
	case 5:
		return "何も起こらなかった。"
	case 6:
		return "ゲームで千円儲かった。"
	case 7:
		return "ゲームで一万円儲かった。"
	case 8:
		return "ゲームで十万円儲かった。"
	default:
		return ""
	}
}
