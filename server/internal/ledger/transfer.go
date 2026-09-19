package ledger

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

func (s *Store) CreateTransfer(in TransferInput, key string) (Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Transaction
	n, err := normalizeTransfer(in)
	if err != nil {
		return out, err
	}
	if len(key) < 16 || len(key) > 128 || strings.ContainsAny(key, "\x00\r\n") {
		return out, problem("invalid", "缺少有效的幂等请求标识")
	}
	requestHash := hash(encode([]any{"transfer-v1", n}))
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	// Permission precedes replay: downgrading/removing a member revokes retries.
	if err = s.authorize(tx, true); err != nil {
		return out, err
	}
	var previousHash, previousJSON string
	err = tx.QueryRow("SELECT request_hash,response_json FROM idempotency WHERE ledger_id=? AND key=?", s.ledgerID, key).Scan(&previousHash, &previousJSON)
	if err == nil {
		if previousHash != requestHash {
			return out, problem("conflict", "该请求标识已用于另一笔数据")
		}
		err = json.Unmarshal([]byte(previousJSON), &out)
		return out, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	out = Transaction{ID: newID(), Version: 1, Type: "transfer", Date: n.Date, Currency: n.Currency,
		AmountMinor: strconv.FormatInt(n.AmountMinor, 10), Account: n.FromAccount, ToAccount: n.ToAccount,
		Merchant: "本人账户转账", Category: "本人转账", Source: "手动转账", Note: n.Note}
	if err = s.insert(tx, out); err != nil {
		return Transaction{}, err
	}
	if _, err = tx.Exec("UPDATE ledgers SET version=version+1 WHERE id=?", s.ledgerID); err != nil {
		return Transaction{}, err
	}
	if _, err = tx.Exec("INSERT INTO idempotency VALUES(?,?,?,?,?)", s.ledgerID, key, requestHash, encode(out), now()); err != nil {
		return Transaction{}, err
	}
	if err = tx.Commit(); err != nil {
		return Transaction{}, err
	}
	return out, nil
}

// TransferInput records an explicitly confirmed movement between two of the
// ledger owner's funding accounts. It is not an instruction to move real money.
// Native source matching is separate: an unknown side must remain pending.
type TransferInput struct {
	Date        string `json:"date"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	FromAccount string `json:"fromAccount"`
	ToAccount   string `json:"toAccount"`
	Note        string `json:"note"`
}

type normalizedTransfer struct {
	Date, Currency, FromAccount, ToAccount, Note string
	AmountMinor                                  int64
}

type transferLeg struct {
	Account     string
	AmountMinor int64
}

// Both legs use funding accounts. No income or expense category posting is
// created, and no guessed account is introduced to make an incomplete pair fit.
func (t normalizedTransfer) legs() [2]transferLeg {
	return [2]transferLeg{{t.FromAccount, -t.AmountMinor}, {t.ToAccount, t.AmountMinor}}
}

func normalizeTransfer(in TransferInput) (normalizedTransfer, error) {
	var out normalizedTransfer
	from, to := strings.TrimSpace(in.FromAccount), strings.TrimSpace(in.ToAccount)
	if from == "" || to == "" || from == "待核实资金账户" || to == "待核实资金账户" {
		return out, problem("invalid", "请明确转出和转入账户；缺失的一侧须保留待核实，不能猜测入账")
	}
	if from == to {
		return out, problem("invalid", "转出和转入账户不能相同")
	}
	// Reuse ordinary field/date/currency/amount limits without exposing transfer
	// as an ordinary income or expense. This temporary value is never persisted.
	n, err := normalize(Input{Date: in.Date, Type: "income", Amount: in.Amount,
		Currency: in.Currency, Merchant: to, Account: from, Category: "本人转账",
		Source: "手动转账", Note: in.Note})
	if err != nil {
		return out, err
	}
	minor, err := strconv.ParseInt(n.AmountMinor, 10, 64)
	if err != nil {
		return out, err
	}
	return normalizedTransfer{Date: n.Date, Currency: n.Currency, FromAccount: n.Account,
		ToAccount: n.Merchant, Note: n.Note, AmountMinor: minor}, nil
}
