package ledger

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// These are supplied current observations, not a verified snapshot or evidence
// that two transactions describe the same economic event. No source label enters
// this hypothetical gross-expense calculation.
type associationExpensePairInput struct {
	Accounting associationExpenseAccountingInput
	Date       string
	Merchant   string
}

// The before/after values concern this supplied pair's gross expense only. This
// is not a whole-ledger total, net/cash/account delta, decision, or write plan.
// Versions merely preserve the caller's bindings; they do not prove freshness.
type associationExpensePairProjection struct {
	LedgerID             string
	LedgerVersion        int64
	Primary              associationExpenseAccountingFacts
	Secondary            associationExpenseAccountingFacts
	Date                 string
	Merchant             string
	PairGrossBeforeMinor int64
	PairGrossAfterMinor  int64
	PairGrossDeltaMinor  int64
}

type associationExpensePairProjectionError struct{ Code string }

func (e *associationExpensePairProjectionError) Error() string {
	switch e.Code {
	case "pair_ledger_version_invalid":
		return "假设支出对投影的账本版本无效"
	case "pair_transaction_invalid":
		return "假设支出对必须包含两笔不同的交易"
	case "pair_fund_account_mismatch":
		return "假设支出对的实际记账资金账户不同"
	case "pair_posting_overlap":
		return "假设支出对的分录标识存在重叠"
	case "pair_amount_mismatch":
		return "假设支出对的金额不一致"
	case "pair_date_invalid":
		return "假设支出对的日期无效或不一致"
	case "pair_merchant_invalid":
		return "假设支出对的商户无效或不一致"
	case "pair_primary_invalid":
		return "必须明确指定假设支出对中的一笔交易"
	default:
		return "假设支出对投影的输入不一致"
	}
}

// A future Store command would still need authorization, a fresh consistent
// snapshot, source/evidence and membership checks, refund/correction/undo guards,
// explicit financial consent and a complete decision/revocation lifecycle. This
// pure function provides none of those and never applies its hypothetical values.
func planAssociationExpensePairProjection(scope string, ledgerVersion int64, a, b associationExpensePairInput, primaryTransactionID string) (associationExpensePairProjection, error) {
	fail := func(code string) (associationExpensePairProjection, error) {
		return associationExpensePairProjection{}, &associationExpensePairProjectionError{Code: code}
	}
	left, err := inspectAssociationExpenseAccountingFacts(scope, a.Accounting)
	if err != nil {
		return associationExpensePairProjection{}, err
	}
	right, err := inspectAssociationExpenseAccountingFacts(scope, b.Accounting)
	if err != nil {
		return associationExpensePairProjection{}, err
	}
	if ledgerVersion < 0 {
		return fail("pair_ledger_version_invalid")
	}
	if left.TransactionID == right.TransactionID {
		return fail("pair_transaction_invalid")
	}
	if left.FundAccountID != right.FundAccountID {
		return fail("pair_fund_account_mismatch")
	}
	for _, leftID := range []string{left.FundPostingID, left.ExpensePostingID} {
		if leftID == right.FundPostingID || leftID == right.ExpensePostingID {
			return fail("pair_posting_overlap")
		}
	}
	if left.AmountMinor != right.AmountMinor {
		return fail("pair_amount_mismatch")
	}
	if !associationPairDate(a.Date) || !associationPairDate(b.Date) || a.Date != b.Date {
		return fail("pair_date_invalid")
	}
	if !associationPairMerchant(a.Merchant) || !associationPairMerchant(b.Merchant) || a.Merchant != b.Merchant {
		return fail("pair_merchant_invalid")
	}
	primary, secondary := left, right
	switch primaryTransactionID {
	case left.TransactionID:
	case right.TransactionID:
		primary, secondary = right, left
	default:
		return fail("pair_primary_invalid")
	}
	// Both inspectors already established -1e12 <= AmountMinor <= -1. Only
	// bounded values are negated/multiplied; arbitrary posting values are not.
	magnitude := -left.AmountMinor
	before := magnitude * 2
	return associationExpensePairProjection{LedgerID: scope, LedgerVersion: ledgerVersion, Primary: primary, Secondary: secondary, Date: a.Date, Merchant: a.Merchant, PairGrossBeforeMinor: before, PairGrossAfterMinor: magnitude, PairGrossDeltaMinor: magnitude - before}, nil
}

func associationPairDate(value string) bool {
	date, err := time.Parse("2006-01-02", value)
	return err == nil && date.Year() >= 1900 && date.Year() <= 2200 && date.Format("2006-01-02") == value
}
func associationPairMerchant(value string) bool {
	return value != "" && utf8.ValidString(value) && strings.TrimSpace(value) == value && len([]rune(value)) <= 200 && !strings.ContainsFunc(value, unicode.IsControl)
}
