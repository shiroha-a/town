package casino

import (
	"encoding/json"
	"errors"

	"github.com/shiroha-a/town/internal/rng"
)

// 福引き(fukubiki.cgi): 複数枚のカードから1枚を選び、当たり/並/はずれのランクに
// 応じた景品(アイテム)を得る。参加費は無料でbet=0で呼ばれる。日次1回制限は基盤側
// で後付けするため、ここでは実装しない。
func init() { register("fukubiki", fukubiki{}) }

type fukubiki struct{}

// fukubikiCards is the number of cards laid out ($kado_kazu, legacy default 3)。
// 3枚のうち1枚が当たり・1枚が並・1枚がはずれの役割を無作為に割り当てる。
const fukubikiCards = 3

// 各ランクの景品プール(content_itemsの既存アイテム名)。当たり=高額、並=中位、
// はずれ=参加賞のデザート。未知名は基盤側(grantItem)で自動スキップされる。
var (
	fukubikiAtari  = []string{"キティのぬいぐるみ", "シャネルのバッグ", "100本のバラの花束", "アンティーク時計", "キャビア"}
	fukubikiNami   = []string{"スッポン", "ゴディバチョコレート", "高級弁当", "どくだみ茶", "参考書", "栄養ドリンク"}
	fukubikiHazure = []string{"あんみつ", "だいふく", "かき氷", "たいやき", "うまい棒（タコヤキ味）"}
)

// Bets returns no fixed stakes: 福引きは無料(bet=0で呼ばれる)。
func (fukubiki) Bets() []int64 { return nil }

type fukubikiParams struct {
	Card int `json:"card"` // 選んだカード番号(1..fukubikiCards)
}

// fukubikiCardInfo reveals one card's hidden rank for presentation.
type fukubikiCardInfo struct {
	Card int    `json:"card"` // カード番号(1..N)
	Rank string `json:"rank"` // "atari" | "nami" | "hazure"
}

// fukubikiDetail is the per-play result serialized to the frontend.
type fukubikiDetail struct {
	Card  int                `json:"card"`  // プレイヤーが選んだカード
	Rank  string             `json:"rank"`  // 選んだカードのランク
	Prize string             `json:"prize"` // 当たった景品名
	Cards []fukubikiCardInfo `json:"cards"` // 全カードの正体(公開用)
}

// Play assigns a random atari/nami/hazure role to each card (legacy randcheck
// permutation), then awards one prize from the pool matching the chosen card's
// rank. The game is free (bet is always 0), so the prize is delivered as an item
// grant rather than a cash payout.
func (fukubiki) Play(r *rng.Rand, bet int64, params json.RawMessage) (Result, error) {
	var p fukubikiParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{}, errors.New("カードの選択が正しくありません。")
	}
	if p.Card < 1 || p.Card > fukubikiCards {
		return Result{}, errors.New("カードを選んでください。")
	}
	// 1..N の順列(randcheck)を作り、先頭を当たり・2番目を並・残りをはずれの位置とする。
	perm := make([]int, fukubikiCards)
	for i := range perm {
		perm[i] = i + 1
	}
	// 部分Fisher-Yatesで無置換に並べ替える(重複なし抽選)。
	for i := 0; i < fukubikiCards; i++ {
		j := i + r.IntN(fukubikiCards-i)
		perm[i], perm[j] = perm[j], perm[i]
	}
	rankOf := func(card int) string {
		switch {
		case card == perm[0]:
			return "atari"
		case fukubikiCards >= 3 && card == perm[1]:
			return "nami"
		default:
			return "hazure"
		}
	}
	cards := make([]fukubikiCardInfo, fukubikiCards)
	for i := 0; i < fukubikiCards; i++ {
		c := i + 1
		cards[i] = fukubikiCardInfo{Card: c, Rank: rankOf(c)}
	}
	rank := rankOf(p.Card)
	pool := fukubikiHazure
	switch rank {
	case "atari":
		pool = fukubikiAtari
	case "nami":
		pool = fukubikiNami
	}
	prize := pick(r, pool)
	return Result{
		Payout: 0, // 無料(掛け金なし)。景品はアイテム付与で渡す。
		Win:    rank != "hazure",
		Items:  []ItemGrant{{Name: prize, Qty: 1}},
		Detail: fukubikiDetail{Card: p.Card, Rank: rank, Prize: prize, Cards: cards},
	}, nil
}
