// Package cleague implements the C-League battle-character system (レガシー
// game.cgi sub doukyo/c_league/battle): each player grows one character by
// feeding their own parameters and money into it, then battles other players'
// characters. This is a game mechanic and is unrelated to romance/marriage.
package cleague

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/rng"
)

// AbilityKeys are the 16 growable abilities (etti excluded per the rewrite).
var AbilityKeys = []string{
	"kokugo", "suugaku", "rika", "syakai", "eigo", "ongaku", "bijutsu",
	"looks", "tairyoku", "kenkou", "speed", "power", "wanryoku", "kyakuryoku",
	"love", "omoshirosa",
}

var bodyKeys = []string{"looks", "tairyoku", "kenkou", "speed", "power", "wanryoku", "kyakuryoku"}
var brainKeys = []string{"kokugo", "suugaku", "rika", "syakai", "eigo", "ongaku", "bijutsu", "love", "omoshirosa"}

// IsAbility reports whether k is a valid ability key.
func IsAbility(k string) bool {
	for _, a := range AbilityKeys {
		if a == k {
			return true
		}
	}
	return false
}

// comments are the default battle lines per ability (レガシー com_henkou 既定).
var comments = map[string]string{
	"kokugo":     "この漢字が読めるか？",
	"suugaku":    "この数学の答えがわかるか？",
	"rika":       "この公式を考えたのは誰か知ってるか？",
	"syakai":     "この事件は何年に起きたか知ってるか？",
	"eigo":       "この英単語の意味を知ってるか？",
	"ongaku":     "この曲を作曲したのは誰か知ってるか？",
	"bijutsu":    "この絵を見てみろ！",
	"looks":      "ルックスで勝負だ！",
	"tairyoku":   "体力で勝負しろ！",
	"kenkou":     "健康には自信があるぞ！",
	"speed":      "このスピードについてこられるかな？",
	"power":      "タックルで勝負だ！",
	"wanryoku":   "腕相撲で勝負だ！",
	"kyakuryoku": "キックをお見舞いしてやる！",
	"love":       "愛の深さでは負けないぞ！",
	"omoshirosa": "このギャグどう？",
}

// Character is a player's battle character with derived stats.
type Character struct {
	OwnerID   int64          `json:"owner_id"`
	Name      string         `json:"name"`
	Abilities map[string]int `json:"abilities"`
	Sintai    int            `json:"sintai"` // 身体能力(導出)
	Zunou     int            `json:"zunou"`  // 頭の良さ(導出)
	Wins      int            `json:"wins"`
	Losses    int            `json:"losses"`
	Draws     int            `json:"draws"`
}

func sumKeys(a map[string]int, keys []string) int {
	total := 0
	for _, k := range keys {
		total += a[k]
	}
	return total
}

// Sintai / Zunou are the derived body/brain scores (legacy /10 aggregate).
func Sintai(a map[string]int) int { return sumKeys(a, bodyKeys) / 10 }
func Zunou(a map[string]int) int  { return sumKeys(a, brainKeys) / 10 }

// BattleRound is one duel round.
type BattleRound struct {
	Ability string `json:"ability"`
	Comment string `json:"comment"`
	AScore  int    `json:"a_score"`
	BScore  int    `json:"b_score"`
	Winner  string `json:"winner"` // a | b | draw
}

// BattleResult is a full match outcome from A's perspective.
type BattleResult struct {
	Winner string        `json:"winner"` // a | b | draw
	Rounds []BattleRound `json:"rounds"`
}

// battleRounds is the number of duels per match (best-of).
const battleRounds = 5

// Battle simulates a best-of match between two characters' abilities. Each round
// draws a random ability; each side scores its ability value plus a random
// bonus, so the stronger character is favored but upsets happen.
func Battle(a, b map[string]int, r *rng.Rand) BattleResult {
	var res BattleResult
	aw, bw := 0, 0
	for i := 0; i < battleRounds; i++ {
		key := AbilityKeys[r.IntN(len(AbilityKeys))]
		as := a[key] + r.IntN(a[key]/2+10)
		bs := b[key] + r.IntN(b[key]/2+10)
		round := BattleRound{Ability: key, Comment: comments[key], AScore: as, BScore: bs}
		switch {
		case as > bs:
			round.Winner = "a"
			aw++
		case bs > as:
			round.Winner = "b"
			bw++
		default:
			round.Winner = "draw"
		}
		res.Rounds = append(res.Rounds, round)
	}
	switch {
	case aw > bw:
		res.Winner = "a"
	case bw > aw:
		res.Winner = "b"
	default:
		res.Winner = "draw"
	}
	return res
}

// Service reads C-League state.
type Service struct {
	pool *pgxpool.Pool
}

// New builds the service.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// GetCharacter returns a player's character (nil if none).
func (s *Service) GetCharacter(ctx context.Context, ownerID int64) (*Character, error) {
	var c Character
	var abJSON []byte
	err := s.pool.QueryRow(ctx,
		`SELECT owner_id, name, abilities, wins, losses, draws FROM battle_characters WHERE owner_id = $1`,
		ownerID).Scan(&c.OwnerID, &c.Name, &abJSON, &c.Wins, &c.Losses, &c.Draws)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("character: %w", err)
	}
	c.Abilities = map[string]int{}
	_ = json.Unmarshal(abJSON, &c.Abilities)
	for _, k := range AbilityKeys {
		if _, ok := c.Abilities[k]; !ok {
			c.Abilities[k] = 0
		}
	}
	c.Sintai = Sintai(c.Abilities)
	c.Zunou = Zunou(c.Abilities)
	return &c, nil
}

// RankEntry is one league-ranking row.
type RankEntry struct {
	OwnerName string `json:"owner_name"`
	CharName  string `json:"char_name"`
	OwnerID   int64  `json:"owner_id"`
	Wins      int    `json:"wins"`
	Losses    int    `json:"losses"`
	Draws     int    `json:"draws"`
	Sintai    int    `json:"sintai"`
	Zunou     int    `json:"zunou"`
}

// Ranking returns all characters ordered by wins (then draws), for the league.
func (s *Service) Ranking(ctx context.Context) ([]RankEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT bc.owner_id, bc.name, p.display_name, bc.abilities, bc.wins, bc.losses, bc.draws
		 FROM battle_characters bc JOIN players p ON p.id = bc.owner_id
		 WHERE p.deleted_at IS NULL
		 ORDER BY bc.wins DESC, bc.draws DESC, bc.name`)
	if err != nil {
		return nil, fmt.Errorf("ranking: %w", err)
	}
	defer rows.Close()
	out := []RankEntry{}
	for rows.Next() {
		var e RankEntry
		var abJSON []byte
		if err := rows.Scan(&e.OwnerID, &e.CharName, &e.OwnerName, &abJSON, &e.Wins, &e.Losses, &e.Draws); err != nil {
			return nil, err
		}
		ab := map[string]int{}
		_ = json.Unmarshal(abJSON, &ab)
		e.Sintai = Sintai(ab)
		e.Zunou = Zunou(ab)
		out = append(out, e)
	}
	return out, rows.Err()
}
