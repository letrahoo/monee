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

func pairUnitMember(suffix string, amount int64) associationExpensePairInput {
	ledgerID := "synthetic-ledger"
	transactionID := "synthetic-transaction-" + suffix
	return associationExpensePairInput{Date: "2026-09-15", Merchant: "合成商户", Accounting: associationExpenseAccountingInput{
		LedgerID: ledgerID, TransactionID: transactionID, Version: 2, Kind: "expense", Currency: "CNY", AmountStorageClass: "integer", AmountRaw: amount, AmountMinor: strconv.FormatInt(amount, 10),
		Postings: []associationExpensePostingInput{
			{ID: "synthetic-fund-posting-" + suffix, TransactionLedgerID: ledgerID, TransactionID: transactionID, AccountID: "synthetic-shared-fund", AccountLedgerID: ledgerID, AccountKind: "clearing", Currency: "CNY", AmountStorageClass: "integer", AmountRaw: amount},
			{ID: "synthetic-expense-posting-" + suffix, TransactionLedgerID: ledgerID, TransactionID: transactionID, AccountID: "synthetic-expense-account-" + suffix, AccountLedgerID: ledgerID, AccountKind: "expense", Currency: "CNY", AmountStorageClass: "integer", AmountRaw: -amount},
		},
	}}
}

func pairUnitReject(t *testing.T, a, b associationExpensePairInput, primary string) {
	t.Helper()
	beforeA, beforeB := fmt.Sprintf("%#v", a), fmt.Sprintf("%#v", b)
	out, err := planAssociationExpensePairProjection("synthetic-ledger", 7, a, b, primary)
	if err == nil || out != (associationExpensePairProjection{}) {
		t.Fatal("expected zero projection on error", out, err)
	}
	var pairError *associationExpensePairProjectionError
	var factError *associationExpenseAccountingError
	if (!errors.As(err, &pairError) || pairError.Code == "") && (!errors.As(err, &factError) || factError.Code == "") {
		t.Fatal("unexpected non-static error", err)
	}
	if strings.Contains(err.Error(), "synthetic-") || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("error echoed facts", err)
	}
	if fmt.Sprintf("%#v", a) != beforeA || fmt.Sprintf("%#v", b) != beforeB {
		t.Fatal("mutated supplied observations")
	}
}

func TestAssociationExpensePairUnitHypotheticalGrossAndExplicitPrimary(t *testing.T) {
	a, b := pairUnitMember("a", -1234), pairUnitMember("b", -1234)
	b.Accounting.Version = 9
	originalA, originalB := fmt.Sprintf("%#v", a), fmt.Sprintf("%#v", b)
	for _, primary := range []string{a.Accounting.TransactionID, b.Accounting.TransactionID} {
		out, err := planAssociationExpensePairProjection("synthetic-ledger", 7, a, b, primary)
		if err != nil || out.LedgerID != "synthetic-ledger" || out.LedgerVersion != 7 || out.Primary.TransactionID != primary || out.Secondary.TransactionID == primary || out.Date != a.Date || out.Merchant != a.Merchant || out.PairGrossBeforeMinor != 2468 || out.PairGrossAfterMinor != 1234 || out.PairGrossDeltaMinor != -1234 {
			t.Fatal(out, err)
		}
		swapped, err := planAssociationExpensePairProjection("synthetic-ledger", 7, b, a, primary)
		if err != nil || swapped != out {
			t.Fatal("input order changed explicit representative", swapped, err)
		}
	}
	if fmt.Sprintf("%#v", a) != originalA || fmt.Sprintf("%#v", b) != originalB {
		t.Fatal("input changed")
	}
	// Supplied ledger/member versions are preserved, not interpreted as proof of a
	// single fresh snapshot. No history or business eligibility enters this helper.
	out, err := planAssociationExpensePairProjection("synthetic-ledger", 0, a, b, b.Accounting.TransactionID)
	if err != nil || out.LedgerVersion != 0 || out.Primary.Version != 9 {
		t.Fatal(out, err)
	}
	out, err = planAssociationExpensePairProjection("synthetic-ledger", math.MaxInt64, a, b, a.Accounting.TransactionID)
	if err != nil || out.LedgerVersion != math.MaxInt64 {
		t.Fatal(out, err)
	}
}

func TestAssociationExpensePairUnitPairConstraints(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*associationExpensePairInput, *associationExpensePairInput)
		primary string
	}{
		{"same transaction", func(a, b *associationExpensePairInput) {
			b.Accounting.TransactionID = a.Accounting.TransactionID
			for i := range b.Accounting.Postings {
				b.Accounting.Postings[i].TransactionID = a.Accounting.TransactionID
			}
		}, ""},
		{"different actual fund id", func(a, b *associationExpensePairInput) { b.Accounting.Postings[0].AccountID = "other-fund-account" }, ""},
		{"different amount", func(a, b *associationExpensePairInput) { *b = pairUnitMember("b", -1235) }, ""},
		{"date difference", func(a, b *associationExpensePairInput) { b.Date = "2026-09-16" }, ""},
		{"merchant difference", func(a, b *associationExpensePairInput) { b.Merchant = "合成商户乙" }, ""},
		{"cross ledger", func(a, b *associationExpensePairInput) { b.Accounting.LedgerID = "other-ledger" }, ""},
		{"bad first accounting", func(a, b *associationExpensePairInput) { a.Accounting.AmountRaw = float64(-1234) }, ""},
		{"bad second accounting", func(a, b *associationExpensePairInput) { b.Accounting.Postings[1].AmountRaw = "1234" }, ""},
		{"missing primary", func(a, b *associationExpensePairInput) {}, "missing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, b := pairUnitMember("a", -1234), pairUnitMember("b", -1234)
			tc.change(&a, &b)
			primary := a.Accounting.TransactionID
			if tc.primary != "" {
				primary = ""
			}
			pairUnitReject(t, a, b, primary)
		})
	}
	for _, primary := range []string{"", "unrelated-sensitive-id", " synthetic-transaction-a", "synthetic-transaction-b\n"} {
		pairUnitReject(t, pairUnitMember("a", -1234), pairUnitMember("b", -1234), primary)
	}
	for left := range 2 {
		for right := range 2 {
			a, b := pairUnitMember("a", -1234), pairUnitMember("b", -1234)
			b.Accounting.Postings[right].ID = a.Accounting.Postings[left].ID
			pairUnitReject(t, a, b, a.Accounting.TransactionID)
		}
	}
	a, b := pairUnitMember("a", -1234), pairUnitMember("b", -1234)
	for _, scope := range []string{"", "other-ledger", " synthetic-ledger"} {
		out, e := planAssociationExpensePairProjection(scope, 7, a, b, a.Accounting.TransactionID)
		if e == nil || out != (associationExpensePairProjection{}) {
			t.Fatal(out, e)
		}
	}
	out, e := planAssociationExpensePairProjection("synthetic-ledger", -1, a, b, a.Accounting.TransactionID)
	if e == nil || out != (associationExpensePairProjection{}) {
		t.Fatal(out, e)
	}
}

func TestAssociationExpensePairUnitDateAndMerchantBoundaries(t *testing.T) {
	for _, date := range []string{"1900-01-01", "2200-12-31", "2024-02-29"} {
		a, b := pairUnitMember("a", -1), pairUnitMember("b", -1)
		a.Date = date
		b.Date = date
		if _, e := planAssociationExpensePairProjection("synthetic-ledger", 0, a, b, a.Accounting.TransactionID); e != nil {
			t.Fatal(date, e)
		}
	}
	for _, date := range []string{"", "1899-12-31", "2201-01-01", "2026-02-29", "2026-9-15", "2026-09-5", "2026-09-15T00:00:00Z", "2026-09-15 "} {
		a, b := pairUnitMember("a", -1), pairUnitMember("b", -1)
		a.Date = date
		b.Date = date
		pairUnitReject(t, a, b, a.Accounting.TransactionID)
	}
	for _, merchant := range []string{"合成商户", strings.Repeat("中", 200), strings.Repeat("😀", 200)} {
		a, b := pairUnitMember("a", -1), pairUnitMember("b", -1)
		a.Merchant = merchant
		b.Merchant = merchant
		if _, e := planAssociationExpensePairProjection("synthetic-ledger", 0, a, b, a.Accounting.TransactionID); e != nil {
			t.Fatal(e)
		}
	}
	for _, merchant := range []string{"", " ", "\u3000", " sensitive", "sensitive\u00a0", "sensitive\nvalue", "sensitive\u0085value", "sensitive\x00value", strings.Repeat("中", 201), string([]byte{0xff})} {
		a, b := pairUnitMember("a", -1), pairUnitMember("b", -1)
		a.Merchant = merchant
		b.Merchant = merchant
		pairUnitReject(t, a, b, a.Accounting.TransactionID)
	}
	// Equality is exact: no case-folding or Unicode canonicalization is inferred.
	a, b := pairUnitMember("a", -1), pairUnitMember("b", -1)
	a.Merchant = "Merchant"
	b.Merchant = "merchant"
	pairUnitReject(t, a, b, a.Accounting.TransactionID)
}

func TestAssociationExpensePairUnitBoundedArithmeticAndFactDelegation(t *testing.T) {
	for _, amount := range []int64{-1, -1234, -1_000_000_000_000} {
		a, b := pairUnitMember("a", amount), pairUnitMember("b", amount)
		out, e := planAssociationExpensePairProjection("synthetic-ledger", 1, a, b, b.Accounting.TransactionID)
		if e != nil || out.PairGrossBeforeMinor != -amount*2 || out.PairGrossAfterMinor != -amount || out.PairGrossDeltaMinor != amount {
			t.Fatal(out, e)
		}
	}
	// Deliberately do not negate an unbounded input in this fixture: the inspector
	// must reject MinInt64 before the pair planner ever derives magnitudes.
	for _, amount := range []int64{math.MinInt64, math.MaxInt64, 0, 1, -1_000_000_000_001} {
		a, b := pairUnitMember("a", -1), pairUnitMember("b", -1)
		a.Accounting.AmountRaw = amount
		a.Accounting.AmountMinor = strconv.FormatInt(amount, 10)
		pairUnitReject(t, a, b, a.Accounting.TransactionID)
		a, b = pairUnitMember("a", -1), pairUnitMember("b", -1)
		b.Accounting.AmountRaw = amount
		b.Accounting.AmountMinor = strconv.FormatInt(amount, 10)
		pairUnitReject(t, a, b, a.Accounting.TransactionID)
	}
}

func TestAssociationExpensePairUnitNarrowHypothesisShape(t *testing.T) {
	output := reflect.TypeOf(associationExpensePairProjection{})
	fields := map[string]bool{"LedgerID": true, "LedgerVersion": true, "Primary": true, "Secondary": true, "Date": true, "Merchant": true, "PairGrossBeforeMinor": true, "PairGrossAfterMinor": true, "PairGrossDeltaMinor": true}
	if output.NumField() != len(fields) {
		t.Fatal("output expanded beyond pair gross hypothesis")
	}
	for i := range output.NumField() {
		if !fields[output.Field(i).Name] {
			t.Fatal("unexpected financial/policy field", output.Field(i).Name)
		}
	}
	input := reflect.TypeOf(associationExpensePairInput{})
	if input.NumField() != 3 {
		t.Fatal("input widened")
	}
	for _, name := range []string{"Accounting", "Date", "Merchant"} {
		if _, ok := input.FieldByName(name); !ok {
			t.Fatal("missing input binding", name)
		}
	}
	for _, name := range []string{"Actions", "Eligible", "Confirmed", "Token", "Source", "SourceRolesVerified", "RefundOf", "HasRefund", "Corrected", "Active", "PairNetDeltaMinor", "LedgerTotalMinor"} {
		if _, ok := output.FieldByName(name); ok {
			t.Fatal("hypothesis claims business authority", name)
		}
	}
}
