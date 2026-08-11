package action

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/casino"
	"github.com/shiroha-a/town/internal/effects"
	"github.com/shiroha-a/town/internal/ledger"
	"github.com/shiroha-a/town/internal/player"
)

// カード引き: 街にひとつの卓を全員で使う。場から1枚引き、前の人が引いたカード
// (伏せ札)とかぶらなければ勝ち。通るたびに場のカードが1枚減ってかぶりやすくなり、
// そのぶん配当が上がる。かぶりが出ると場は10枚に戻る。
// 引くのはサーバーなので、伏せ札を人づてに知っても有利にはならない。

// KaburiEntry is one line of the table's recent history. 引いた数字は出さない:
// 場の緊張感(次にかぶるのは何か)を残すため、勝敗と枚数だけ見せる。
type KaburiEntry struct {
	Name string    `json:"name"`
	Size int       `json:"size"`
	Win  bool      `json:"win"`
	At   time.Time `json:"at"`
}

// KaburiState is the shared table as one player sees it.
type KaburiState struct {
	Cards         []int         `json:"cards"`          // 場に並んでいるカード(昇順)
	Size          int           `json:"size"`           // 場の枚数
	Streak        int           `json:"streak"`         // かぶらずに続いた回数
	PayoutPercent int64         `json:"payout_percent"` // 勝ったときの配当(掛け金比%)
	Bets          []int64       `json:"bets"`
	Recent        []KaburiEntry `json:"recent"`
}

// KaburiPlayResult is the outcome of one round.
type KaburiPlayResult struct {
	State  *KaburiState   `json:"state"`
	Player *player.Player `json:"-"`
	Card   int            `json:"card"`   // 自分が引いたカード
	Hidden int            `json:"hidden"` // 前の人のカード(勝負がついたので見せる)
	Win    bool           `json:"win"`
	Payout int64          `json:"payout"` // 戻ってきた額(掛け金を含む。負けは0)
}

const kaburiRecentLimit = 10

// kaburiFreshTable is the SQL that puts a new table out. 住民データの全消しで
// 卓ごと消えるので、読むときも引くときも無ければここから作り直す。
const kaburiFreshTable = `INSERT INTO kaburi_table (id, hidden_card, cards, streak)
	 VALUES (1, 1, '{1,2,3,4,5,6,7,8,9,10}', 0) ON CONFLICT (id) DO NOTHING`

// kaburiReadState loads the shared table plus the recent history.
func (s *Service) kaburiReadState(ctx context.Context, playerID int64) (*KaburiState, error) {
	if _, err := s.pool.Exec(ctx, kaburiFreshTable); err != nil {
		return nil, fmt.Errorf("ensure kaburi table: %w", err)
	}
	var (
		cards  []int16
		streak int
	)
	if err := s.pool.QueryRow(ctx,
		`SELECT cards, streak FROM kaburi_table WHERE id = 1`).
		Scan(&cards, &streak); err != nil {
		return nil, fmt.Errorf("read kaburi table: %w", err)
	}
	st := &KaburiState{
		Cards:         make([]int, 0, len(cards)),
		Size:          len(cards),
		Streak:        streak,
		PayoutPercent: casino.KaburiPayoutPercent(len(cards)),
		Bets:          casino.KaburiBets(),
		Recent:        []KaburiEntry{},
	}
	for _, c := range cards {
		st.Cards = append(st.Cards, int(c))
	}
	rows, err := s.pool.Query(ctx,
		`SELECT COALESCE(p.display_name, '(退会)'), g.payout > 0,
		        COALESCE((g.detail->>'size')::int, 0), g.created_at
		 FROM game_plays g LEFT JOIN players p ON p.id = g.player_id
		 WHERE g.game = 'kaburi' ORDER BY g.id DESC LIMIT $1`, kaburiRecentLimit)
	if err != nil {
		return nil, fmt.Errorf("read kaburi history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e KaburiEntry
		if err := rows.Scan(&e.Name, &e.Win, &e.Size, &e.At); err != nil {
			return nil, fmt.Errorf("scan kaburi history: %w", err)
		}
		st.Recent = append(st.Recent, e)
	}
	return st, rows.Err()
}

// KaburiGetState returns the shared table for the 画面表示.
func (s *Service) KaburiGetState(ctx context.Context, playerID int64) (*KaburiState, error) {
	return s.kaburiReadState(ctx, playerID)
}

// KaburiPlay draws one card from the table and compares it with the hidden one.
// Missing it pays out and shrinks the table by a card; matching it loses the
// stake and resets the table.
func (s *Service) KaburiPlay(ctx context.Context, playerID, bet int64, idempotencyKey string) (*KaburiPlayResult, error) {
	okBet := false
	for _, b := range casino.KaburiBets() {
		if b == bet {
			okBet = true
		}
	}
	if !okBet {
		return nil, &ConditionError{Message: "掛け金の指定が正しくありません。"}
	}
	out := &KaburiPlayResult{}
	played := false
	p, err := s.runAction(ctx, playerID, "kaburi", idempotencyKey, func(ctx context.Context, tx pgx.Tx, state effects.State) error {
		if err := s.checkGameLimit(ctx, tx, playerID, "kaburi"); err != nil {
			return err
		}
		played = true
		if _, err := tx.Exec(ctx, kaburiFreshTable); err != nil {
			return fmt.Errorf("ensure kaburi table: %w", err)
		}
		// 卓は全員で1つなので、同時に出されても順番に処理されるよう行を押さえる。
		var (
			hidden int16
			cards  []int16
			streak int
		)
		if err := tx.QueryRow(ctx,
			`SELECT hidden_card, cards, streak FROM kaburi_table WHERE id = 1 FOR UPDATE`).
			Scan(&hidden, &cards, &streak); err != nil {
			return fmt.Errorf("lock kaburi table: %w", err)
		}
		if state.Money < bet {
			return &ConditionError{Message: "お金が足りません。"}
		}
		// 引くのはサーバー。場のN枚から一様に1枚。かぶる確率は 1/N。
		card := int(cards[s.rng.IntN(len(cards))])
		out.Card = card
		out.Hidden = int(hidden)
		out.Win = card != int(hidden)

		size := len(cards)
		entries := []ledger.Entry{
			{Account: ledger.PlayerAccount(playerID), Delta: -bet},
			{Account: ledger.SystemAccount("casino"), Delta: bet},
		}
		if out.Win {
			// 掛け金 + 配当を戻す。
			out.Payout = bet + bet*casino.KaburiPayoutPercent(size)/100
			entries = append(entries,
				ledger.Entry{Account: ledger.SystemAccount("casino"), Delta: -out.Payout},
				ledger.Entry{Account: ledger.PlayerAccount(playerID), Delta: out.Payout},
			)
		}
		if err := s.ledger.PostTx(ctx, tx, "casino:kaburi", "", entries); err != nil {
			return err
		}
		// 次の場: 引いたカードが新しい伏せ札。通れば1枚減り、かぶれば10枚に戻る。
		nextStreak := 0
		if out.Win {
			nextStreak = streak + 1
		}
		nextCards := casino.KaburiDeal(s.rng, card, casino.KaburiSize(nextStreak))
		next := make([]int16, 0, len(nextCards))
		for _, c := range nextCards {
			next = append(next, int16(c))
		}
		if _, err := tx.Exec(ctx,
			`UPDATE kaburi_table SET hidden_card = $1, cards = $2, streak = $3,
			 last_player = $4, updated_at = now() WHERE id = 1`,
			int16(card), next, nextStreak, playerID); err != nil {
			return fmt.Errorf("update kaburi table: %w", err)
		}
		// 記録。引いた数字は画面に出さないので、枚数と勝敗だけ持つ。
		detail, _ := json.Marshal(map[string]any{"size": size, "streak": streak, "win": out.Win})
		if _, err := tx.Exec(ctx,
			`INSERT INTO game_plays (game, player_id, bet, payout, detail) VALUES ('kaburi', $1, $2, $3, $4)`,
			playerID, bet, out.Payout, detail); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 同じ冪等キーの二重送信。勝負は済んでいるので、負けたように見せない。
	if !played {
		return nil, &ConditionError{Message: "この勝負はすでに終わっています。"}
	}
	st, err := s.kaburiReadState(ctx, playerID)
	if err != nil {
		return nil, err
	}
	out.Player, out.State = p, st
	return out, nil
}
