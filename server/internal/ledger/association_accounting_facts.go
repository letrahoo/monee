package ledger

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// These inputs contain only supplied structural facts, not source labels or
// association policy. A future caller must acquire them from an authorized,
// consistent database snapshot; this pure inspector does not establish that.
type associationExpenseAccountingInput struct {
	LedgerID           string
	TransactionID      string
	Kind               string
	Currency           string
	AmountMinor        string
	AmountStorageClass string
	Version            int64
	AmountRaw          any
	Postings           []associationExpensePostingInput
}

type associationExpensePostingInput struct {
	ID string
	// postings has no ledger_id column. This is the supplied owning transaction's
	// ledger (for example, from JOIN transactions), not a new persisted attribute.
	TransactionLedgerID string
	TransactionID       string
	// AccountID is the supplied actual referenced account identity, never its name.
	// AccountLedgerID and AccountKind must come from that same account reference.
	AccountID          string
	AccountLedgerID    string
	AccountKind        string
	Currency           string
	AmountStorageClass string
	AmountRaw          any
}

// Coherent facts are not association eligibility, confirmed economic equivalence,
// a verified bank/account owner, or authority to alter any financial projection.
type associationExpenseAccountingFacts struct {
	LedgerID         string
	TransactionID    string
	Version          int64
	AmountMinor      int64
	FundPostingID    string
	FundAccountID    string
	ExpensePostingID string
	ExpenseAccountID string
}

type associationExpenseAccountingError struct{ Code string }

func (e *associationExpenseAccountingError) Error() string {
	switch e.Code {
	case "accounting_scope_invalid":
		return "支出会计事实的账本范围无效"
	case "transaction_invalid":
		return "支出交易结构不完整或不一致"
	case "transaction_amount_invalid":
		return "支出交易金额不是支持范围内的规范整数"
	case "posting_count_invalid":
		return "支出会计事实必须包含两条分录"
	case "posting_invalid":
		return "支出分录标识或交易归属不一致"
	case "account_invalid":
		return "支出分录的账户引用或类型不一致"
	case "posting_amount_invalid":
		return "支出分录的币种或整数金额不一致"
	default:
		return "支出会计事实不一致"
	}
}

func inspectAssociationExpenseAccountingFacts(expectedLedgerID string, in associationExpenseAccountingInput) (associationExpenseAccountingFacts, error) {
	fail := func(code string) (associationExpenseAccountingFacts, error) {
		return associationExpenseAccountingFacts{}, &associationExpenseAccountingError{Code: code}
	}
	if !associationAccountingID(expectedLedgerID) || !associationAccountingID(in.LedgerID) || in.LedgerID != expectedLedgerID {
		return fail("accounting_scope_invalid")
	}
	if !associationAccountingID(in.TransactionID) || in.Version < 1 || in.Kind != "expense" || in.Currency != "CNY" {
		return fail("transaction_invalid")
	}
	amount, ok := in.AmountRaw.(int64)
	if in.AmountStorageClass != "integer" || !ok || amount < -1_000_000_000_000 || amount >= 0 || in.AmountMinor != strconv.FormatInt(amount, 10) {
		return fail("transaction_amount_invalid")
	}
	if len(in.Postings) != 2 {
		return fail("posting_count_invalid")
	}
	a, b := in.Postings[0], in.Postings[1]
	if a.ID == b.ID {
		return fail("posting_invalid")
	}
	if a.AccountID == b.AccountID {
		return fail("account_invalid")
	}
	var fund, expense associationExpensePostingInput
	var fundCount, expenseCount int
	for _, posting := range in.Postings {
		if !associationAccountingID(posting.ID) || !associationAccountingID(posting.TransactionID) || posting.TransactionID != in.TransactionID || posting.TransactionLedgerID != expectedLedgerID {
			return fail("posting_invalid")
		}
		if !associationAccountingID(posting.AccountID) || posting.AccountLedgerID != expectedLedgerID {
			return fail("account_invalid")
		}
		minor, integer := posting.AmountRaw.(int64)
		if posting.Currency != in.Currency || posting.AmountStorageClass != "integer" || !integer {
			return fail("posting_amount_invalid")
		}
		switch posting.AccountKind {
		case "clearing":
			if minor != amount {
				return fail("posting_amount_invalid")
			}
			fund = posting
			fundCount++
		case "expense":
			// Negation is safe only after the bounded negative transaction was checked.
			// No sum or negation of arbitrary posting amounts can overflow here.
			if minor != -amount {
				return fail("posting_amount_invalid")
			}
			expense = posting
			expenseCount++
		default:
			return fail("account_invalid")
		}
	}
	if fundCount != 1 || expenseCount != 1 {
		return fail("account_invalid")
	}
	return associationExpenseAccountingFacts{LedgerID: expectedLedgerID, TransactionID: in.TransactionID, Version: in.Version, AmountMinor: amount, FundPostingID: fund.ID, FundAccountID: fund.AccountID, ExpensePostingID: expense.ID, ExpenseAccountID: expense.AccountID}, nil
}

// IDs remain opaque and are never trimmed, normalized, or interpreted as real
// bank/payment identity. Canonical nonempty text prevents ambiguous references.
func associationAccountingID(value string) bool {
	return value != "" && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}
