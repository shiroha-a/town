// Package keiba implements the 競馬場 (horse racing): a 6-horse race with random
// odds, server-authoritative simulation, betting and a profit ranking. The race
// lineup is generated and stored server-side so the client only sends bets (by
// horse index), removing the legacy's odds-tampering surface.
package keiba

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/rng"
)

const (
	// TicketPrice is the yen cost of one betting ticket (legacy hardcoded 500).
	TicketPrice = 500
	// MaxTickets is the total tickets buyable in one race (legacy $keiba_gendomaisuu).
	MaxTickets = 200
	// MaxHorsesBet is the number of horses a player may bet on at once.
	MaxHorsesBet = 2
	// Runners is the number of horses per race.
	Runners = 6
	// goalDistance is the accumulated distance that ends the race.
	goalDistance = 910
	// maxTicks guards the simulation loop.
	maxTicks = 200
	// rankInactiveDays drops ranking rows unplayed for this long.
	rankInactiveDays = 90
)

// horse is one master horse (name + sprite).
type horse struct {
	name string
	img  string
}

// horses is the ver.1.40 master field (15 horses).
var horses = []horse{
	{"◇ダンスパートナー", "danpa"},
	{"◇エアグルーヴ", "groove"},
	{"◇ビワハヤヒデ", "hayahide"},
	{"◇イシノサンデー", "ishisun"},
	{"◇ジャングルポケット", "junpoke"},
	{"◇マンハッタンカフェ", "manhattan"},
	{"◇ノーリーズン", "noreason"},
	{"◇オグリキャップ", "oguri"},
	{"◇サクラプレジデント", "sakura_predi"},
	{"◇エアシャカール", "shakur"},
	{"◇ビワハイジ", "heidi"},
	{"◇ナリタブライアン", "brian"},
	{"◇テイエムオーシャン", "tm_ocean"},
	{"◇トウカイテイオー", "tokai_teio"},
	{"◇ローマンエンパイア", "roman"},
}

// oddsValues are the ver.1.40 odds pool (9 values); six are drawn per race.
var oddsValues = []int{8, 28, 2, 12, 16, 4, 30, 3, 5}

// Horse is one runner in a generated race.
type Horse struct {
	Name string `json:"name"`
	Img  string `json:"img"`
	Odds int    `json:"odds"`
}

// RaceResult is the outcome of one simulated race.
type RaceResult struct {
	WinnerIndex int     `json:"winner_index"`
	Steps       [][]int `json:"steps"` // 各馬のtick毎の歩幅(アニメ用)
}

// GenerateRace shuffles the horse and odds pools independently and pairs the
// first Runners of each (mirroring keiba.cgi's per-load shuffle).
func GenerateRace(r *rng.Rand) []Horse {
	hi := shuffledIndexes(r, len(horses))
	oi := shuffledIndexes(r, len(oddsValues))
	out := make([]Horse, Runners)
	for i := 0; i < Runners; i++ {
		h := horses[hi[i]]
		out[i] = Horse{Name: h.name, Img: h.img, Odds: oddsValues[oi[i]]}
	}
	return out
}

// shuffledIndexes returns 0..n-1 in random order (Fisher-Yates).
func shuffledIndexes(r *rng.Rand, n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := r.IntN(i + 1)
		idx[i], idx[j] = idx[j], idx[i]
	}
	return idx
}

// Simulate runs the race: each tick every horse advances by
// int((rand(60)+10) / (1 + odds/70)), so higher-odds horses are slower. The
// race ends when a horse reaches goalDistance; the furthest horse wins (ties
// broken randomly). It returns the winner index and the per-tick step lists.
func Simulate(lineup []Horse, r *rng.Rand) RaceResult {
	dist := make([]int, len(lineup))
	steps := make([][]int, len(lineup))
	for tick := 0; tick < maxTicks; tick++ {
		maxDist := 0
		for i, h := range lineup {
			step := int(float64(r.IntN(60)+10) / (1.0 + float64(h.Odds)/70.0))
			dist[i] += step
			steps[i] = append(steps[i], step)
			if dist[i] > maxDist {
				maxDist = dist[i]
			}
		}
		if maxDist >= goalDistance {
			break
		}
	}
	// 勝ち馬 = 最大距離。同着はランダム選択。
	maxDist := 0
	for _, d := range dist {
		if d > maxDist {
			maxDist = d
		}
	}
	var tied []int
	for i, d := range dist {
		if d == maxDist {
			tied = append(tied, i)
		}
	}
	return RaceResult{WinnerIndex: tied[r.IntN(len(tied))], Steps: steps}
}

// RankEntry is one row of the profit ranking.
type RankEntry struct {
	Name     string `json:"name"`
	Profit   int64  `json:"profit"`
	Invested int64  `json:"invested"`
	Won      int64  `json:"won"`
}

// Service handles race generation/storage and ranking reads.
type Service struct {
	pool *pgxpool.Pool
	rng  *rng.Rand
}

// New builds the service. rng is used to generate race lineups.
func New(pool *pgxpool.Pool, r *rng.Rand) *Service {
	return &Service{pool: pool, rng: r}
}

// GetOrCreateRace generates a fresh race for the player, stores it (overwriting
// any pending race), and returns its id and lineup.
func (s *Service) GetOrCreateRace(ctx context.Context, playerID int64) (int64, []Horse, error) {
	lineup := GenerateRace(s.rng)
	b, err := json.Marshal(lineup)
	if err != nil {
		return 0, nil, err
	}
	var raceID int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO keiba_race (player_id, lineup) VALUES ($1, $2)
		 ON CONFLICT (player_id) DO UPDATE SET
		   lineup = $2, race_id = nextval('keiba_race_race_id_seq'), created_at = now()
		 RETURNING race_id`, playerID, b).Scan(&raceID); err != nil {
		return 0, nil, fmt.Errorf("store race: %w", err)
	}
	return raceID, lineup, nil
}

// Ranking returns the top profit-makers active within rankInactiveDays.
func (s *Service) Ranking(ctx context.Context) ([]RankEntry, error) {
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`SELECT p.display_name, kr.won - kr.invested AS profit, kr.invested, kr.won
		 FROM keiba_ranking kr JOIN players p ON p.id = kr.player_id
		 WHERE kr.last_played >= now() - interval '%d days' AND p.deleted_at IS NULL
		 ORDER BY profit DESC LIMIT 10`, rankInactiveDays))
	if err != nil {
		return nil, fmt.Errorf("query ranking: %w", err)
	}
	defer rows.Close()
	out := []RankEntry{}
	for rows.Next() {
		var e RankEntry
		if err := rows.Scan(&e.Name, &e.Profit, &e.Invested, &e.Won); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LoadRace reads a player's pending race lineup and id (for the bet handler).
func LoadRace(ctx context.Context, tx pgx.Tx, playerID int64) (int64, []Horse, error) {
	var raceID int64
	var b []byte
	err := tx.QueryRow(ctx,
		`SELECT race_id, lineup FROM keiba_race WHERE player_id = $1`, playerID).Scan(&raceID, &b)
	if err != nil {
		return 0, nil, err
	}
	var lineup []Horse
	if err := json.Unmarshal(b, &lineup); err != nil {
		return 0, nil, err
	}
	return raceID, lineup, nil
}
