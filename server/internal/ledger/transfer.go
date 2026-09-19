package ledger

import (
	"strconv"
	"strings"
)

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
