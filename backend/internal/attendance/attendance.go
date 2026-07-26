// Package attendance implements 足あと, the visitor attendance book. Each town
// access records the player's presence for the current game day (once per day);
// the board is a members×dates matrix (present/absent/未登録) with an
// attendance-rate ranking. Legacy: ashiato1/2.cgi (normalized per the rewrite
// notes to one row per present day).
package attendance

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/gametime"
)

// cell states.
const (
	Present = "present" // 出席(足跡)
	Absent  = "absent"  // 欠席(×)
	Blank   = "blank"   // 未登録期間
)

// rankMinDays is the minimum applicable days to appear in the ranking.
const rankMinDays = 3

// Member is one row of the attendance matrix.
type Member struct {
	ID    int64    `json:"id"`
	Name  string   `json:"name"`
	Cells []string `json:"cells"` // dates と同順(newest first)
}

// RankEntry is one attendance-rate ranking row.
type RankEntry struct {
	Name    string `json:"name"`
	Present int    `json:"present"`
	Days    int    `json:"days"`
	Rate    int    `json:"rate"` // 出席率(%)
}

// Board is the attendance view: date columns (newest first), member rows and a
// rate ranking.
type Board struct {
	Dates   []string    `json:"dates"`
	Members []Member    `json:"members"`
	Ranking []RankEntry `json:"ranking"`
}

// Service records and reads attendance.
type Service struct {
	pool            *pgxpool.Pool
	loc             *time.Location
	dayBoundaryHour int
}

// New builds the service. loc/dayBoundaryHour define the game day.
func New(pool *pgxpool.Pool, loc *time.Location, dayBoundaryHour int) *Service {
	return &Service{pool: pool, loc: loc, dayBoundaryHour: dayBoundaryHour}
}

func (s *Service) today() time.Time {
	return gametime.Date(time.Now(), s.loc, s.dayBoundaryHour)
}

// Checkin records the player as present for the current game day, returning
// whether this was the first check-in today.
func (s *Service) Checkin(ctx context.Context, playerID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO attendance (player_id, day) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		playerID, s.today())
	if err != nil {
		return false, fmt.Errorf("checkin: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Board builds the attendance matrix for the last `days` game days plus the
// attendance-rate ranking.
func (s *Service) Board(ctx context.Context, days int) (Board, error) {
	var b Board
	if days < 1 {
		days = 14
	}
	today := s.today()
	dates := make([]time.Time, days)
	for i := 0; i < days; i++ {
		dates[i] = today.AddDate(0, 0, -i) // newest first
	}
	oldest := dates[days-1]

	// プレイヤー(登録日=created_atのゲーム日)。
	type pl struct {
		id     int64
		name   string
		regDay time.Time
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, display_name, created_at FROM players WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return b, fmt.Errorf("players: %w", err)
	}
	var players []pl
	for rows.Next() {
		var p pl
		var created time.Time
		if err := rows.Scan(&p.id, &p.name, &created); err != nil {
			rows.Close()
			return b, err
		}
		p.regDay = gametime.Date(created, s.loc, s.dayBoundaryHour)
		players = append(players, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return b, err
	}

	// 出席行を集合化: player_id -> set(dateKey)。
	present := map[int64]map[string]bool{}
	arows, err := s.pool.Query(ctx,
		`SELECT player_id, day FROM attendance WHERE day >= $1`, oldest)
	if err != nil {
		return b, fmt.Errorf("attendance: %w", err)
	}
	for arows.Next() {
		var pid int64
		var d time.Time
		if err := arows.Scan(&pid, &d); err != nil {
			arows.Close()
			return b, err
		}
		if present[pid] == nil {
			present[pid] = map[string]bool{}
		}
		present[pid][d.Format("2006-01-02")] = true
	}
	arows.Close()
	if err := arows.Err(); err != nil {
		return b, err
	}

	b.Dates = make([]string, days)
	for i, d := range dates {
		b.Dates[i] = d.Format("01/02")
	}
	b.Members = make([]Member, 0, len(players))
	b.Ranking = []RankEntry{}
	for _, p := range players {
		cells := make([]string, days)
		presentCount, applicable := 0, 0
		for i, d := range dates {
			key := d.Format("2006-01-02")
			switch {
			case d.Before(p.regDay):
				cells[i] = Blank
			case present[p.id][key]:
				cells[i] = Present
				presentCount++
				applicable++
			default:
				cells[i] = Absent
				applicable++
			}
		}
		b.Members = append(b.Members, Member{ID: p.id, Name: p.name, Cells: cells})
		if applicable >= rankMinDays {
			b.Ranking = append(b.Ranking, RankEntry{
				Name: p.name, Present: presentCount, Days: applicable,
				Rate: presentCount * 100 / applicable,
			})
		}
	}
	// 出席率降順 → 出席数降順(簡易ソート)。
	for i := 0; i < len(b.Ranking); i++ {
		for j := i + 1; j < len(b.Ranking); j++ {
			if b.Ranking[j].Rate > b.Ranking[i].Rate ||
				(b.Ranking[j].Rate == b.Ranking[i].Rate && b.Ranking[j].Present > b.Ranking[i].Present) {
				b.Ranking[i], b.Ranking[j] = b.Ranking[j], b.Ranking[i]
			}
		}
	}
	if len(b.Ranking) > 10 {
		b.Ranking = b.Ranking[:10]
	}
	return b, nil
}
