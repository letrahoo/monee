package ledger

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func accountingUnitInput() associationExpenseAccountingInput {
	return associationExpenseAccountingInput{
		LedgerID: "synthetic-ledger", TransactionID: "synthetic-transaction", Kind: "expense", Currency: "CNY", AmountMinor: "-1234", AmountStorageClass: "integer", Version: 1, AmountRaw: int64(-1234),
		Postings: []associationExpensePostingInput{
			{ID: "synthetic-fund-posting", TransactionLedgerID: "synthetic-ledger", TransactionID: "synthetic-transaction", AccountID: "synthetic-fund-account", AccountLedgerID: "synthetic-ledger", AccountKind: "clearing", Currency: "CNY", AmountStorageClass: "integer", AmountRaw: int64(-1234)},
			{ID: "synthetic-expense-posting", TransactionLedgerID: "synthetic-ledger", TransactionID: "synthetic-transaction", AccountID: "synthetic-expense-account", AccountLedgerID: "synthetic-ledger", AccountKind: "expense", Currency: "CNY", AmountStorageClass: "integer", AmountRaw: int64(1234)},
		},
	}
}

func accountingUnitReject(t *testing.T, in associationExpenseAccountingInput) {
	t.Helper()
	before := fmt.Sprintf("%#v", in)
	out, err := inspectAssociationExpenseAccountingFacts("synthetic-ledger", in)
	var factError *associationExpenseAccountingError
	if !errors.As(err, &factError) || factError.Code == "" {
		t.Fatalf("expected typed static fact error, got %v", err)
	}
	if out != (associationExpenseAccountingFacts{}) {
		t.Fatal("error returned partial facts", out)
	}
	if strings.Contains(err.Error(), "synthetic-") || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("input echoed", err)
	}
	if fmt.Sprintf("%#v", in) != before {
		t.Fatal("inspector mutated input")
	}
}

func TestAssociationAccountingUnitValidFactsAndOrder(t *testing.T) {
	in := accountingUnitInput()
	before := fmt.Sprintf("%#v", in)
	want := associationExpenseAccountingFacts{LedgerID: in.LedgerID, TransactionID: in.TransactionID, Version: 1, AmountMinor: -1234, FundPostingID: in.Postings[0].ID, FundAccountID: in.Postings[0].AccountID, ExpensePostingID: in.Postings[1].ID, ExpenseAccountID: in.Postings[1].AccountID}
	got, err := inspectAssociationExpenseAccountingFacts(in.LedgerID, in)
	if err != nil || got != want {
		t.Fatal(got, err)
	}
	if fmt.Sprintf("%#v", in) != before {
		t.Fatal("changed input")
	}
	in.Postings[0], in.Postings[1] = in.Postings[1], in.Postings[0]
	got, err = inspectAssociationExpenseAccountingFacts(in.LedgerID, in)
	if err != nil || got != want {
		t.Fatal("order changed accounting facts", got, err)
	}
	// A current corrected/annotated expense may have any positive version. No
	// correction/refund history or association-policy assertion is supplied here.
	in.Version = 17
	got, err = inspectAssociationExpenseAccountingFacts(in.LedgerID, in)
	if err != nil || got.Version != 17 {
		t.Fatal("positive current version rejected", got, err)
	}
}

func TestAssociationAccountingUnitStructuralFailures(t *testing.T) {
	tests := []struct {
		name   string
		change func(*associationExpenseAccountingInput)
	}{
		{"ledger mismatch", func(in *associationExpenseAccountingInput) { in.LedgerID = "other-ledger" }},
		{"ledger missing", func(in *associationExpenseAccountingInput) { in.LedgerID = "" }},
		{"transaction missing", func(in *associationExpenseAccountingInput) { in.TransactionID = "" }},
		{"transaction control", func(in *associationExpenseAccountingInput) { in.TransactionID = "sensitive\x00tx" }},
		{"version zero", func(in *associationExpenseAccountingInput) { in.Version = 0 }},
		{"version negative", func(in *associationExpenseAccountingInput) { in.Version = -1 }},
		{"unknown kind", func(in *associationExpenseAccountingInput) { in.Kind = "sensitive-unknown-kind" }},
		{"income", func(in *associationExpenseAccountingInput) { in.Kind = "income" }},
		{"refund", func(in *associationExpenseAccountingInput) { in.Kind = "refund" }},
		{"currency", func(in *associationExpenseAccountingInput) { in.Currency = "USD" }},
		{"nil postings", func(in *associationExpenseAccountingInput) { in.Postings = nil }},
		{"empty postings", func(in *associationExpenseAccountingInput) { in.Postings = []associationExpensePostingInput{} }},
		{"one posting", func(in *associationExpenseAccountingInput) { in.Postings = in.Postings[:1] }},
		{"three postings", func(in *associationExpenseAccountingInput) { in.Postings = append(in.Postings, in.Postings[0]) }},
		{"same posting ids", func(in *associationExpenseAccountingInput) { in.Postings[1].ID = in.Postings[0].ID }},
		{"posting id missing", func(in *associationExpenseAccountingInput) { in.Postings[0].ID = "" }},
		{"posting id unicode trim", func(in *associationExpenseAccountingInput) { in.Postings[0].ID = "synthetic-posting\u3000" }},
		{"posting transaction mismatch", func(in *associationExpenseAccountingInput) { in.Postings[0].TransactionID = "other-transaction" }},
		{"posting owning ledger mismatch", func(in *associationExpenseAccountingInput) { in.Postings[0].TransactionLedgerID = "other-ledger" }},
		{"posting owning ledger absent", func(in *associationExpenseAccountingInput) { in.Postings[0].TransactionLedgerID = "" }},
		{"account id missing", func(in *associationExpenseAccountingInput) { in.Postings[0].AccountID = "" }},
		{"same account ids", func(in *associationExpenseAccountingInput) { in.Postings[1].AccountID = in.Postings[0].AccountID }},
		{"account ledger mismatch", func(in *associationExpenseAccountingInput) { in.Postings[0].AccountLedgerID = "other-ledger" }},
		{"unknown account kind", func(in *associationExpenseAccountingInput) { in.Postings[0].AccountKind = "sensitive-unknown-kind" }},
		{"income account kind", func(in *associationExpenseAccountingInput) { in.Postings[1].AccountKind = "income" }},
		{"two clearing accounts", func(in *associationExpenseAccountingInput) {
			in.Postings[1].AccountKind = "clearing"
			in.Postings[1].AmountRaw = int64(-1234)
		}},
		{"two expense accounts", func(in *associationExpenseAccountingInput) {
			in.Postings[0].AccountKind = "expense"
			in.Postings[0].AmountRaw = int64(1234)
		}},
		{"posting currency mismatch", func(in *associationExpenseAccountingInput) { in.Postings[0].Currency = "USD" }},
		{"funding wrong sign", func(in *associationExpenseAccountingInput) { in.Postings[0].AmountRaw = int64(1234) }},
		{"expense wrong sign", func(in *associationExpenseAccountingInput) { in.Postings[1].AmountRaw = int64(-1234) }},
		{"balanced wrong magnitudes", func(in *associationExpenseAccountingInput) {
			in.Postings[0].AmountRaw = int64(-2000)
			in.Postings[1].AmountRaw = int64(2000)
		}},
		{"unbalanced postings", func(in *associationExpenseAccountingInput) { in.Postings[1].AmountRaw = int64(1233) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { in := accountingUnitInput(); tc.change(&in); accountingUnitReject(t, in) })
	}
	for _, scope := range []string{"", "other-ledger", " synthetic-ledger", "synthetic-ledger\n", string([]byte{0xff})} {
		out, e := inspectAssociationExpenseAccountingFacts(scope, accountingUnitInput())
		if e == nil || out != (associationExpenseAccountingFacts{}) {
			t.Fatal("invalid explicit scope accepted", out, e)
		}
	}
}

func TestAssociationAccountingUnitCanonicalAmountsAndOverflow(t *testing.T) {
	for _, minor := range []int64{-1, -1234, -1_000_000_000_000} {
		in := accountingUnitInput()
		in.AmountRaw = minor
		in.AmountMinor = strconv.FormatInt(minor, 10)
		in.Postings[0].AmountRaw = minor
		in.Postings[1].AmountRaw = -minor
		out, e := inspectAssociationExpenseAccountingFacts(in.LedgerID, in)
		if e != nil || out.AmountMinor != minor {
			t.Fatal(out, e)
		}
	}
	for _, minor := range []int64{0, 1, -1_000_000_000_001, math.MinInt64, math.MaxInt64} {
		in := accountingUnitInput()
		in.AmountRaw = minor
		in.AmountMinor = strconv.FormatInt(minor, 10)
		accountingUnitReject(t, in)
	}
	for _, amount := range []string{"", "0", "-0", "+1234", "-01234", "-1234.0", "-1.234e3", " -1234", "-1234 ", "-1235", "-9223372036854775809"} {
		in := accountingUnitInput()
		in.AmountMinor = amount
		accountingUnitReject(t, in)
	}
	for _, huge := range []int64{math.MinInt64, math.MaxInt64} {
		for i := range 2 {
			in := accountingUnitInput()
			in.Postings[i].AmountRaw = huge
			accountingUnitReject(t, in)
		}
	}
}

type accountingUnitNamedInt64 int64

func TestAssociationAccountingUnitStorageTypesAreNotCoerced(t *testing.T) {
	values := []any{nil, float64(-1234), float64(-1234.25), "-1234", []byte("-1234"), int(-1234), int32(-1234), uint64(1234), accountingUnitNamedInt64(-1234), map[string]string{"sensitive": "raw"}}
	for _, raw := range values {
		in := accountingUnitInput()
		in.AmountRaw = raw
		accountingUnitReject(t, in)
		for i := range 2 {
			in = accountingUnitInput()
			in.Postings[i].AmountRaw = raw
			accountingUnitReject(t, in)
		}
	}
	for _, storage := range []string{"", "real", "text", "null", "blob", "INTEGER", "sensitive-unknown-class"} {
		in := accountingUnitInput()
		in.AmountStorageClass = storage
		accountingUnitReject(t, in)
		for i := range 2 {
			in = accountingUnitInput()
			in.Postings[i].AmountStorageClass = storage
			accountingUnitReject(t, in)
		}
	}
}

func TestAssociationAccountingUnitResultIsOnlyNarrowFacts(t *testing.T) {
	typ := reflect.TypeOf(associationExpenseAccountingFacts{})
	expected := map[string]bool{"LedgerID": true, "TransactionID": true, "Version": true, "AmountMinor": true, "FundPostingID": true, "FundAccountID": true, "ExpensePostingID": true, "ExpenseAccountID": true}
	if typ.NumField() != len(expected) {
		t.Fatal("result expanded beyond factual structure")
	}
	for i := range typ.NumField() {
		if !expected[typ.Field(i).Name] {
			t.Fatal("unexpected policy/label/effect field", typ.Field(i).Name)
		}
	}
	input := reflect.TypeOf(associationExpenseAccountingInput{})
	for _, field := range []string{"Date", "RefundOf", "Source", "Account", "Category", "Note", "DeletedAt", "Corrected", "HasRefund", "Actions"} {
		if _, present := input.FieldByName(field); present {
			t.Fatal("policy/display field leaked into fact input", field)
		}
	}
}
