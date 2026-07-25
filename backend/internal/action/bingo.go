package action

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/bingo"
	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/player"
)

// BingoCard is one of the player's cards, with the marks derived from the
// numbers drawn so far.
type BingoCard struct {
	ID      int64  `json:"id"`
	Numbers []int  `json:"numbers"`
	Marks   []bool `json:"marks"`
	Lines   int    `json:"lines"`
	Rank    *int   `json:"rank"`  // 上がった着順(未上がりはnull)
	Prize   int64  `json:"prize"` // 受け取った賞金
	CanWin  bool   `json:"can_win"`
}

// BingoState is the 大会 view for one player.
type BingoState struct {
	Active      bool        `json:"active"`
	EventID     int64       `json:"event_id"`
	Drawn       []int       `json:"drawn"`        // 公開済みの抽選番号(引かれた順)
	Total       int         `json:"total"`        // 全部で何個引くか
	Day         int         `json:"day"`          // 何日目(1始まり)
	Days        int         `json:"days"`         // 開催日数
	LinesToWin  int         `json:"lines_to_win"` //ビンゴ成立に必要なライン数
	Cards       []BingoCard `json:"cards"`
	MaxCards    int         `json:"max_cards"`
	FinishedCnt int         `json:"finished_count"` // 上がった人数
	NextPrize   int64       `json:"next_prize"`     // 次に上がった人がもらえる賞金
}

// bingoEvent is the loaded event row plus the derived draw progress.
type bingoEvent struct {
	id         int64
	numbers    []int
	perDay     int
	days       int
	linesToWin int
	startedAt  time.Time
	finished   int
}

// drawn returns the numbers made public so far, and which day it is (1-based).
func (e bingoEvent) drawn() ([]int, int) {
	elapsed := int(time.Since(e.startedAt).Hours() / 24)
	n := bingo.DrawnCount(len(e.numbers), e.perDay, e.days, elapsed)
	day := min(elapsed+1, e.days)
	return e.numbers[:n], day
}

// loadBingoEvent returns the newest event, or nil when none has been held.
func loadBingoEvent(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (*bingoEvent, error) {
	var e bingoEvent
	err := q.QueryRow(ctx,
		`SELECT id, numbers, per_day, days, lines_to_win, started_at, finished_count
		 FROM bingo_events ORDER BY id DESC LIMIT 1`).
		Scan(&e.id, &e.numbers, &e.perDay, &e.days, &e.linesToWin, &e.startedAt, &e.finished)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load bingo event: %w", err)
	}
	return &e, nil
}

// BingoState returns the current event and the player's cards.
func (s *Service) BingoState(ctx context.Context, playerID int64) (*BingoState, error) {
	e, err := loadBingoEvent(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	out := &BingoState{Cards: []BingoCard{}, MaxCards: bingo.MaxCardsPerPlayer}
	if e == nil {
		return out, nil
	}
	drawn, day := e.drawn()
	out.Active = true
	out.EventID, out.Drawn, out.Total = e.id, drawn, len(e.numbers)
	out.Day, out.Days, out.LinesToWin = day, e.days, e.linesToWin
	out.FinishedCnt = e.finished
	out.NextPrize = bingo.Prize(e.finished + 1)

	rows, err := s.pool.Query(ctx,
		`SELECT id, numbers, rank, prize FROM bingo_cards
		 WHERE event_id = $1 AND player_id = $2 ORDER BY id`, e.id, playerID)
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c BingoCard
		if err := rows.Scan(&c.ID, &c.Numbers, &c.Rank, &c.Prize); err != nil {
			return nil, fmt.Errorf("scan card: %w", err)
		}
		c.Marks = bingo.Marks(c.Numbers, drawn)
		c.Lines = bingo.CompletedLines(c.Numbers, drawn)
		c.CanWin = c.Rank == nil && c.Lines >= e.linesToWin
		out.Cards = append(out.Cards, c)
	}
	return out, rows.Err()
}

// DoBingoTakeCard hands the player one more card for the running event.
func (s *Service) DoBingoTakeCard(ctx context.Context, playerID int64, idempotencyKey string) (*player.Player, error) {
	return s.runAction(ctx, playerID, "bingo_card", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		e, err := loadBingoEvent(ctx, tx)
		if err != nil {
			return err
		}
		if e == nil {
			return &ConditionError{Message: "いまビンゴ大会は開かれていません。"}
		}
		var held int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM bingo_cards WHERE event_id = $1 AND player_id = $2`,
			e.id, playerID).Scan(&held); err != nil {
			return fmt.Errorf("count cards: %w", err)
		}
		if held >= bingo.MaxCardsPerPlayer {
			return &ConditionError{Message: fmt.Sprintf("カードは%d枚までです。", bingo.MaxCardsPerPlayer)}
		}
		card := bingo.NewCard(s.rng, len(e.numbers))
		if _, err := tx.Exec(ctx,
			`INSERT INTO bingo_cards (event_id, player_id, numbers) VALUES ($1, $2, $3)`,
			e.id, playerID, card); err != nil {
			return fmt.Errorf("insert card: %w", err)
		}
		return nil
	})
}

// BingoClaimResult reports a successful claim.
type BingoClaimResult struct {
	Rank  int   `json:"rank"`
	Prize int64 `json:"prize"`
}

// DoBingoClaim finalises a completed card: it takes the next finishing place
// and pays the prize into the player's 普通口座 (レガシー bingo.cgi:437-449)。
func (s *Service) DoBingoClaim(ctx context.Context, playerID, cardID int64, idempotencyKey string) (*player.Player, *BingoClaimResult, error) {
	var res *BingoClaimResult
	p, err := s.runAction(ctx, playerID, "bingo_claim", idempotencyKey, func(ctx context.Context, tx pgx.Tx, _ effects.State) error {
		e, err := loadBingoEvent(ctx, tx)
		if err != nil {
			return err
		}
		if e == nil {
			return &ConditionError{Message: "いまビンゴ大会は開かれていません。"}
		}
		// 着順の採番が競合しないようイベント行をロックする。
		if err := tx.QueryRow(ctx,
			`SELECT finished_count FROM bingo_events WHERE id = $1 FOR UPDATE`, e.id).
			Scan(&e.finished); err != nil {
			return fmt.Errorf("lock event: %w", err)
		}

		var numbers []int
		var rank *int
		err = tx.QueryRow(ctx,
			`SELECT numbers, rank FROM bingo_cards
			 WHERE id = $1 AND player_id = $2 AND event_id = $3 FOR UPDATE`,
			cardID, playerID, e.id).Scan(&numbers, &rank)
		if errors.Is(err, pgx.ErrNoRows) {
			return &ConditionError{Message: "そのカードは持っていません。"}
		}
		if err != nil {
			return fmt.Errorf("load card: %w", err)
		}
		if rank != nil {
			return &ConditionError{Message: "そのカードはもう上がっています。"}
		}
		drawn, _ := e.drawn()
		if lines := bingo.CompletedLines(numbers, drawn); lines < e.linesToWin {
			return &ConditionError{Message: fmt.Sprintf("まだ%dラインです。%dライン揃うと上がりです。", lines, e.linesToWin)}
		}

		place := e.finished + 1
		prize := bingo.Prize(place)
		if _, err := tx.Exec(ctx,
			`UPDATE bingo_events SET finished_count = $2 WHERE id = $1`, e.id, place); err != nil {
			return fmt.Errorf("bump finished: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE bingo_cards SET rank = $2, prize = $3, claimed_at = now() WHERE id = $1`,
			cardID, place, prize); err != nil {
			return fmt.Errorf("mark card: %w", err)
		}
		// 賞金は普通口座へ(レガシー $bank += $bingo_syoukin)。
		if err := s.ledger.PostTx(ctx, tx, "bingo_prize", "", []ledger.Entry{
			{Account: ledger.SystemAccount("bingo"), Delta: -prize},
			{Account: ledger.SavingsAccount(playerID), Delta: prize},
		}); err != nil {
			return fmt.Errorf("pay prize: %w", err)
		}
		res = &BingoClaimResult{Rank: place, Prize: prize}
		return nil
	})
	return p, res, err
}

// StartBingo opens a new bingo event (admin). Any running event is left in
// place historically; the newest event is the active one.
func (s *Service) StartBingo(ctx context.Context, maxNumber, perDay, days, linesToWin int) error {
	if maxNumber < bingo.Cells {
		maxNumber = bingo.DefaultMaxNumber
	}
	if perDay <= 0 {
		perDay = bingo.DefaultPerDay
	}
	if days <= 0 {
		days = bingo.DefaultDays
	}
	if linesToWin <= 0 {
		linesToWin = bingo.DefaultLinesToWin
	}
	seq := bingo.Sequence(s.rng, maxNumber)
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO bingo_events (numbers, per_day, days, lines_to_win) VALUES ($1, $2, $3, $4)`,
		seq, perDay, days, linesToWin); err != nil {
		return fmt.Errorf("start bingo: %w", err)
	}
	return nil
}
