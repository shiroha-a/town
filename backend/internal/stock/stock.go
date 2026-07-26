// Package stock implements the 株取引場 (stock exchange): five shared-price
// symbols A-E, per-player holdings, a background price-movement engine that
// mirrors the legacy event.pl economy/new-product events, and read helpers for
// prices, holdings and logs. Trades themselves live in the action service so
// they reuse its ledger + idempotency machinery.
package stock

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/rng"
)

// Symbols are the five tradable stocks, in display order.
var Symbols = []string{"A", "B", "C", "D", "E"}

// ValidSymbol reports whether s is a tradable symbol.
func ValidSymbol(s string) bool {
	for _, sym := range Symbols {
		if s == sym {
			return true
		}
	}
	return false
}

// TradeLogKeep bounds the per-player trade history length.
const TradeLogKeep = tradeLogKeep

const (
	// MaxShares is the per-symbol holding cap (legacy $konu_jogen).
	MaxShares = 200
	// InitialPrice is the seed/reset baseline (legacy 25000).
	InitialPrice = 25000
	// eventLogKeep / tradeLogKeep bound the visible log lengths.
	eventLogKeep = 30
	tradeLogKeep = 10
)

// StockPrice is one symbol's current price.
type StockPrice struct {
	Symbol string `json:"symbol"`
	Price  int64  `json:"price"`
}

// Holding is a player's position in one symbol plus derived display values.
type Holding struct {
	Symbol     string `json:"symbol"`
	Price      int64  `json:"price"`      // 現在株価
	Shares     int    `json:"shares"`     // 保有株数
	Value      int64  `json:"value"`      // 時価(price*shares)
	CostTotal  int64  `json:"cost_total"` // 保有分の取得原価
	AvgCost    int64  `json:"avg_cost"`   // 平均取得単価
	Unrealized int64  `json:"unrealized"` // 含み損益(value-cost_total)
	InvTotal   int64  `json:"inv_total"`  // 累計投資
	RetTotal   int64  `json:"ret_total"`  // 累計回収
	Net        int64  `json:"net"`        // 累計収支(ret-inv)
}

// Service provides read access to stock state.
type Service struct {
	pool *pgxpool.Pool
}

// New builds a read service.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Prices returns the current price of all symbols in display order.
func (s *Service) Prices(ctx context.Context) ([]StockPrice, error) {
	rows, err := s.pool.Query(ctx, `SELECT symbol, price FROM stock_price ORDER BY symbol`)
	if err != nil {
		return nil, fmt.Errorf("query prices: %w", err)
	}
	defer rows.Close()
	out := make([]StockPrice, 0, len(Symbols))
	for rows.Next() {
		var p StockPrice
		if err := rows.Scan(&p.Symbol, &p.Price); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Holdings returns the player's position in every symbol (0-share included) with
// current price and derived P&L values.
func (s *Service) Holdings(ctx context.Context, playerID int64) ([]Holding, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT sp.symbol, sp.price,
		        COALESCE(ps.shares, 0), COALESCE(ps.cost_total, 0),
		        COALESCE(ps.inv_total, 0), COALESCE(ps.ret_total, 0)
		 FROM stock_price sp
		 LEFT JOIN player_stock ps ON ps.symbol = sp.symbol AND ps.player_id = $1
		 ORDER BY sp.symbol`, playerID)
	if err != nil {
		return nil, fmt.Errorf("query holdings: %w", err)
	}
	defer rows.Close()
	out := make([]Holding, 0, len(Symbols))
	for rows.Next() {
		var h Holding
		if err := rows.Scan(&h.Symbol, &h.Price, &h.Shares, &h.CostTotal, &h.InvTotal, &h.RetTotal); err != nil {
			return nil, err
		}
		h.Value = h.Price * int64(h.Shares)
		if h.Shares > 0 {
			h.AvgCost = h.CostTotal / int64(h.Shares)
			h.Unrealized = h.Value - h.CostTotal
		}
		h.Net = h.RetTotal - h.InvTotal
		out = append(out, h)
	}
	return out, rows.Err()
}

// EventLog returns the most recent price-movement messages, newest first.
func (s *Service) EventLog(ctx context.Context, limit int) ([]string, error) {
	return queryMessages(ctx, s.pool,
		`SELECT message FROM stock_event_log ORDER BY id DESC LIMIT $1`, limit)
}

// History returns the player's most recent trade messages, newest first.
func (s *Service) History(ctx context.Context, playerID int64, limit int) ([]string, error) {
	return queryMessages(ctx, s.pool,
		`SELECT message FROM stock_trade_log WHERE player_id = $2 ORDER BY id DESC LIMIT $1`,
		limit, playerID)
}

func queryMessages(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) ([]string, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{} // 空でもJSON nullでなく[]を返す(フロントの.length対策)。
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MovePrices runs one iteration of the legacy event.pl price movement against
// the shared prices: a chance of an economy event (all symbols) or, failing
// that, a single-symbol new-product event. g is the volatility divisor
// (legacy: online players + 1); higher g means rarer, gentler moves. It writes
// updated prices and an event-log line inside one transaction, and reports
// whether prices moved.
func MovePrices(ctx context.Context, pool *pgxpool.Pool, r *rng.Rand, g int) (bool, error) {
	if g <= 0 {
		g = 20
	}
	var moved bool
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		prices, err := lockPrices(ctx, tx)
		if err != nil {
			return err
		}
		msg := applyMovement(prices, r, g)
		if msg == "" {
			return nil
		}
		moved = true
		for i, sym := range Symbols {
			if _, err := tx.Exec(ctx,
				`UPDATE stock_price SET price = $1, updated_at = now() WHERE symbol = $2`,
				prices[i], sym); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO stock_event_log (message) VALUES ($1)`, msg); err != nil {
			return err
		}
		// 動向ログを最新30件に切り詰める。
		_, err = tx.Exec(ctx,
			`DELETE FROM stock_event_log WHERE id NOT IN (
			   SELECT id FROM stock_event_log ORDER BY id DESC LIMIT $1)`, eventLogKeep)
		return err
	})
	return moved, err
}

func lockPrices(ctx context.Context, tx pgx.Tx) ([]int64, error) {
	prices := make([]int64, len(Symbols))
	bySym := map[string]int{}
	for i, s := range Symbols {
		bySym[s] = i
	}
	rows, err := tx.Query(ctx, `SELECT symbol, price FROM stock_price FOR UPDATE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sym string
		var p int64
		if err := rows.Scan(&sym, &p); err != nil {
			return nil, err
		}
		if i, ok := bySym[sym]; ok {
			prices[i] = p
		}
	}
	return prices, rows.Err()
}

// applyMovement mutates prices in place and returns a log message (empty if no
// movement). Formulas and constants are transcribed from event.pl.
func applyMovement(prices []int64, r *rng.Rand, g int) string {
	saikoro0 := r.IntN(10 * g)
	switch saikoro0 {
	case 1: // 景気よし: 全銘柄上昇(上限+2500)
		saikoro1 := r.IntN(100) + 1
		var parts []string
		for i := range prices {
			agene := int64(float64(prices[i]) * (float64(saikoro1) / 1000.0) * 1.11111)
			if agene > 2500 {
				agene = 2500
			}
			prices[i] += agene
			parts = append(parts, fmt.Sprintf("%s+%d", Symbols[i], agene))
		}
		return "景気よし " + strings.Join(parts, " ")
	case 2: // 景気わる: 全銘柄下落。250円未満は10000円へ買収統合
		saikoro1 := r.IntN(100) + 1
		var parts []string
		for i := range prices {
			agene := int64(float64(prices[i]) * (float64(saikoro1) / 1000.0))
			prices[i] -= agene
			part := fmt.Sprintf("%s-%d", Symbols[i], agene)
			if prices[i] < 250 {
				prices[i] = 10000
				part += "(買収統合)"
			}
			parts = append(parts, part)
		}
		return "景気わる " + strings.Join(parts, " ")
	}

	// 景気イベント不発時のみ新商品イベント。saikoro2=1..10で銘柄と成否が決まる。
	saikoro2 := r.IntN(20 * g)
	if saikoro2 < 1 || saikoro2 > 10 {
		return ""
	}
	idx := (saikoro2 - 1) / 2 // 0..4 → A..E
	success := saikoro2%2 == 1
	saikoro3 := float64(r.IntN(50)+1) / 10.0 // 0.1..5.0
	rate := 0.05 + saikoro3/100.0            // 0.051..0.10
	if success {
		agene := int64(float64(prices[idx])*rate*1.11111) + 250
		if agene > 5000 {
			agene = 5000
		}
		prices[idx] += agene
		return fmt.Sprintf("%s新商品成功 +%d", Symbols[idx], agene)
	}
	// 新商品失敗は減算のみ(景気わると異なり250円リセットは無い=レガシー忠実)。
	agene := int64(float64(prices[idx]) * rate)
	prices[idx] -= agene
	return fmt.Sprintf("%s新商品失敗 -%d", Symbols[idx], agene)
}
