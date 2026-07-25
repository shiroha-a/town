package casino

import (
	"encoding/json"
	"errors"

	"github.com/shiroha-a/town/internal/rng"
)

// お宝(otakara.cgi): 4種の宝箱(銅/銀/金/スペシャル)から1つ選んで代金(箱代)を払い、
// その箱に対応する宝をプール内一様(int(rand(#プール)))で1つ得る。宝は通常アイテム
// (所持品追加=Items)・ステータス系(即時適用=Params)・金銭当たり(Payout)のいずれか。
// スペシャルは全宝(銅+銀+金の和集合)から抽選する(価格逆転はレガシー準拠)。
// レガシーの宝データ(dat_dir/otakara.cgi)は現存しないため、既存のcontent_items名と
// 16スキルで各箱のプールを再構成する。分布は付録Aの「プール内一様」に従う。
func init() { register("otakara", otakara{}) }

type otakara struct{}

// otakaraPrize is one entry in a box's prize pool. Exactly one of item / params /
// money is set: an item grant, a stat change, or a cash win (金銭当たり).
type otakaraPrize struct {
	name   string       // 賞品名(表示用)
	item   string       // content_items名(アイテム賞のとき非空)
	params []ParamDelta // ステータス賞のときの増減
	money  int64        // 金銭当たり額(金銭賞のとき>0)
}

// 銅の箱(500円): 安価な食べ物・飲み物中心に、わずかなステ上昇と小銭当たり。
var otakaraCopper = []otakaraPrize{
	{name: "だいふく", item: "だいふく"},
	{name: "たいやき", item: "たいやき"},
	{name: "あんみつ", item: "あんみつ"},
	{name: "青汁", item: "青汁"},
	{name: "ミルク", item: "ミルク"},
	{name: "ちょっと元気", params: []ParamDelta{{Param: "tairyoku", Amount: 2}}},
	{name: "豆知識", params: []ParamDelta{{Param: "kokugo", Amount: 1}}},
	{name: "小銭", money: 1000},
	{name: "はずれ(小銭)", money: 100},
}

// 銀の箱(1,000円): 栄養ドリンクや上位デザート、中程度のステ上昇とへそくり。
var otakaraSilver = []otakaraPrize{
	{name: "パフェ", item: "パフェ"},
	{name: "杏仁豆腐", item: "杏仁豆腐"},
	{name: "栄養ドリンク", item: "栄養ドリンク"},
	{name: "リポビタンD", item: "リポビタンD"},
	{name: "美肌", params: []ParamDelta{{Param: "looks", Amount: 5}}},
	{name: "ときめき", params: []ParamDelta{{Param: "love", Amount: 3}}},
	{name: "体力増強", params: []ParamDelta{{Param: "tairyoku", Amount: 5}, {Param: "power", Amount: 3}}},
	{name: "へそくり", money: 5000},
	{name: "はずれ(小銭)", money: 500},
}

// 金の箱(1,000,000円): 高級車やブランド品、大幅なステ上昇と大金の当たり。
var otakaraGold = []otakaraPrize{
	{name: "ロールスロイス", item: "ロールスロイス"},
	{name: "ポルシェ", item: "ポルシェ"},
	{name: "ベンツ", item: "ベンツ"},
	{name: "ミグ25", item: "ミグ25"},
	{name: "シャネルのバッグ", item: "シャネルのバッグ"},
	{name: "天才のひらめき", params: []ParamDelta{
		{Param: "kokugo", Amount: 8}, {Param: "suugaku", Amount: 8}, {Param: "rika", Amount: 8},
		{Param: "syakai", Amount: 8}, {Param: "eigo", Amount: 8},
	}},
	{name: "鋼の肉体", params: []ParamDelta{
		{Param: "power", Amount: 20}, {Param: "wanryoku", Amount: 20},
		{Param: "kyakuryoku", Amount: 20}, {Param: "tairyoku", Amount: 20},
	}},
	{name: "金塊", money: 2000000},
	{name: "大判小判", money: 5000000},
}

// スペシャル(500,000円): 全宝(銅+銀+金)の和集合から一様抽選する。安宝も高級宝も
// 等確率で出るため一発逆転もあれば大損もある。
var otakaraSpecial = func() []otakaraPrize {
	all := make([]otakaraPrize, 0, len(otakaraCopper)+len(otakaraSilver)+len(otakaraGold))
	all = append(all, otakaraCopper...)
	all = append(all, otakaraSilver...)
	all = append(all, otakaraGold...)
	return all
}()

// otakaraBox pairs a box's key with its cost (箱代=bet) and prize pool.
type otakaraBox struct {
	cost int64
	pool []otakaraPrize
}

// otakaraBoxes maps the box key received via params to its cost and pool.
var otakaraBoxes = map[string]otakaraBox{
	"copper":  {cost: 500, pool: otakaraCopper},
	"silver":  {cost: 1000, pool: otakaraSilver},
	"special": {cost: 500000, pool: otakaraSpecial},
	"gold":    {cost: 1000000, pool: otakaraGold},
}

// Bets returns the allowed box costs (銅=500, 銀=1,000, スペシャル=500,000, 金=1,000,000).
func (otakara) Bets() []int64 { return []int64{500, 1000, 500000, 1000000} }

type otakaraParams struct {
	Box string `json:"box"` // "copper" | "silver" | "gold" | "special"
}

// otakaraDetail is the per-play result serialized to the frontend.
type otakaraDetail struct {
	Box    string       `json:"box"`              // 選んだ箱のkey
	Cost   int64        `json:"cost"`             // 箱代(bet)
	Prize  string       `json:"prize"`            // 当たった宝の名前
	Kind   string       `json:"kind"`             // "item" | "param" | "money"
	Item   string       `json:"item,omitempty"`   // アイテム賞のcontent_items名
	Params []ParamDelta `json:"params,omitempty"` // ステータス賞の増減
	Money  int64        `json:"money,omitempty"`  // 金銭当たり額
}

// Play opens the chosen box: it draws one prize uniformly from that box's pool and
// returns it as an item grant, a stat change, or a cash win. The box cost is the
// bet; item/stat prizes leave Payout 0 (stake consumed) while a cash prize pays out
// via Payout so the net over the bet channel is prize - cost.
func (otakara) Play(r *rng.Rand, bet int64, params json.RawMessage) (Result, error) {
	var p otakaraParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{}, errors.New("箱の選択が正しくありません。")
	}
	box, ok := otakaraBoxes[p.Box]
	if !ok {
		return Result{}, errors.New("箱の種類を選んでください。")
	}
	// 箱代(bet)は選んだ箱の代金と一致していなければならない。
	if bet != box.cost {
		return Result{}, errors.New("箱代が正しくありません。")
	}

	prize := box.pool[r.IntN(len(box.pool))]

	detail := otakaraDetail{Box: p.Box, Cost: box.cost, Prize: prize.name}
	var res Result
	switch {
	case prize.item != "":
		detail.Kind = "item"
		detail.Item = prize.item
		res.Items = []ItemGrant{{Name: prize.item, Qty: 1}}
		res.Win = true // 宝(アイテム)を入手できたら当たり扱い
	case len(prize.params) > 0:
		detail.Kind = "param"
		detail.Params = prize.params
		res.Params = prize.params
		res.Win = true // ステータス上昇も当たり扱い
	default:
		detail.Kind = "money"
		detail.Money = prize.money
		res.Payout = prize.money // 金銭当たりはPayout(net = money - cost)
		res.Win = prize.money > box.cost
	}
	res.Detail = detail
	return res, nil
}
