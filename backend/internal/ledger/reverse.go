package ledger

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// 取り消しの決まりごと。
//
// 台帳は追記専用で、行を消すことはしない。消すと「世界のお金の合計」を後から
// 検算できなくなり、複式である意味が無くなる。代わりに符号を反転した取引を
// 1本足す(逆仕訳)。結果として残高は取り消し前に戻り、経緯も両方残る。
//
// 取り消すのは取引(ledger_tx)単位。1つの取引は必ず合計0の脚の組で、片方の脚
// だけを戻すとその不変条件が壊れる。
var (
	// ErrTxNotFound means the transaction to reverse does not exist.
	ErrTxNotFound = errors.New("ledger transaction not found")
	// ErrAlreadyReversed means the transaction has already been reversed.
	ErrAlreadyReversed = errors.New("ledger transaction already reversed")
	// ErrWouldGoNegative means reversing would leave someone with less than 0.
	ErrWouldGoNegative = errors.New("reversal would make a balance negative")
)

// NegativeError carries which account would go negative and by how much.
type NegativeError struct {
	Account string
	Result  int64
}

func (e *NegativeError) Error() string {
	return fmt.Sprintf("%s would become %d", e.Account, e.Result)
}
func (e *NegativeError) Is(target error) bool { return target == ErrWouldGoNegative }

// reverseRef is the ref that marks a reversal, and makes reversing twice a
// no-op (ledger_tx.ref は not null のとき一意)。
func reverseRef(txID int64) string { return "reverse:" + strconv.FormatInt(txID, 10) }

// heldAccount reports whether an account belongs to a player (not a system
// faucet/sink). 蛇口は負の残高で当たり前なので、マイナス判定から外す。
func heldAccount(account string) bool {
	return strings.HasPrefix(account, "player:") ||
		strings.HasPrefix(account, "savings:") ||
		strings.HasPrefix(account, "super_savings:")
}

// Reverse undoes a transaction's money by posting its mirror image.
//
// お金だけを戻す。買い物を取り消しても品物は手元に残るし、労働を取り消しても
// 経験値は戻らない。呼び出し側(管理画面)はそれを断ってから使わせること。
func (r *Repo) Reverse(ctx context.Context, txID int64) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM ledger_tx WHERE id = $1)`, txID).Scan(&exists); err != nil {
			return fmt.Errorf("check tx: %w", err)
		}
		if !exists {
			return ErrTxNotFound
		}
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM ledger_tx WHERE ref = $1)`, reverseRef(txID)).Scan(&exists); err != nil {
			return fmt.Errorf("check reversal: %w", err)
		}
		if exists {
			return ErrAlreadyReversed
		}

		rows, err := tx.Query(ctx,
			`SELECT account, delta FROM ledger_entry WHERE tx_id = $1 ORDER BY id`, txID)
		if err != nil {
			return fmt.Errorf("read entries: %w", err)
		}
		var entries []Entry
		for rows.Next() {
			var e Entry
			if err := rows.Scan(&e.Account, &e.Delta); err != nil {
				rows.Close()
				return fmt.Errorf("scan entry: %w", err)
			}
			entries = append(entries, Entry{Account: e.Account, Delta: -e.Delta})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read entries: %w", err)
		}
		if len(entries) == 0 {
			return ErrTxNotFound
		}

		// 使ってしまった後の取り消しで持ち金がマイナスになることがある。黙って
		// 負にすると画面も判定も想定していない状態になるので、止めて知らせる。
		for _, e := range entries {
			if !heldAccount(e.Account) {
				continue
			}
			var bal int64
			if err := tx.QueryRow(ctx,
				`SELECT COALESCE(SUM(delta), 0) FROM ledger_entry WHERE account = $1`,
				e.Account).Scan(&bal); err != nil {
				return fmt.Errorf("read balance: %w", err)
			}
			if bal+e.Delta < 0 {
				return &NegativeError{Account: e.Account, Result: bal + e.Delta}
			}
		}
		// 理由の末尾の ":数字" は集計でまとめられるので、一覧では "reverse" に見える。
		return r.PostTx(ctx, tx, reverseRef(txID), reverseRef(txID), entries)
	})
}
