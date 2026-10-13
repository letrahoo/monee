package ledger

import (
	"errors"
	"strings"
	"testing"
)

func syntheticTransfer() TransferInput {
	return TransferInput{Date: "2026-09-19", Amount: "100.01", Currency: "CNY",
		FromAccount: "合成储蓄账户 A", ToAccount: "合成钱包 B", Note: "合成本人转账"}
}

func TestTransferNormalizationAndBalancedFundingLegs(t *testing.T) {
	in := syntheticTransfer()
	in.Date, in.Currency = " 2026-09-19 ", " CNY "
	in.FromAccount, in.ToAccount, in.Note = " 合成储蓄账户 A ", " 合成钱包 B ", " 合成本人转账 "
	out, err := normalizeTransfer(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Date != "2026-09-19" || out.Currency != "CNY" || out.AmountMinor != 10001 ||
		out.FromAccount != "合成储蓄账户 A" || out.ToAccount != "合成钱包 B" || out.Note != "合成本人转账" {
		t.Fatalf("incorrect normalized transfer: %+v", out)
	}
	legs := out.legs()
	if legs[0].Account != out.FromAccount || legs[1].Account != out.ToAccount ||
		legs[0].AmountMinor != -10001 || legs[1].AmountMinor != 10001 ||
		legs[0].AmountMinor+legs[1].AmountMinor != 0 {
		t.Fatalf("funding legs must balance: %+v", legs)
	}
}

func TestTransferRequiresExplicitDistinctAccountsAndValidFields(t *testing.T) {
	cases := map[string]func(*TransferInput){
		"missing source":        func(i *TransferInput) { i.FromAccount = " " },
		"missing destination":   func(i *TransferInput) { i.ToAccount = "" },
		"unknown source":        func(i *TransferInput) { i.FromAccount = "待核实资金账户" },
		"unknown destination":   func(i *TransferInput) { i.ToAccount = " 待核实资金账户 " },
		"same account":          func(i *TransferInput) { i.ToAccount = " " + i.FromAccount + " " },
		"source too long":       func(i *TransferInput) { i.FromAccount = strings.Repeat("账", 201) },
		"destination too long":  func(i *TransferInput) { i.ToAccount = strings.Repeat("账", 201) },
		"source newline":        func(i *TransferInput) { i.FromAccount = "a\nb" },
		"destination nul":       func(i *TransferInput) { i.ToAccount = "a\x00b" },
		"missing currency":      func(i *TransferInput) { i.Currency = "" },
		"foreign currency":      func(i *TransferInput) { i.Currency = "USD" },
		"zero":                  func(i *TransferInput) { i.Amount = "0" },
		"negative":              func(i *TransferInput) { i.Amount = "-1" },
		"extra precision":       func(i *TransferInput) { i.Amount = "0.001" },
		"scientific notation":   func(i *TransferInput) { i.Amount = "1e2" },
		"grouping separators":   func(i *TransferInput) { i.Amount = "1,000" },
		"beyond amount limit":   func(i *TransferInput) { i.Amount = "10000000000.01" },
		"invalid date":          func(i *TransferInput) { i.Date = "2026-02-29" },
		"date before supported": func(i *TransferInput) { i.Date = "1899-12-31" },
		"date after supported":  func(i *TransferInput) { i.Date = "2201-01-01" },
		"note too long":         func(i *TransferInput) { i.Note = strings.Repeat("注", 1001) },
		"note nul":              func(i *TransferInput) { i.Note = "a\x00b" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := syntheticTransfer()
			mutate(&in)
			out, err := normalizeTransfer(in)
			var p *Problem
			if !errors.As(err, &p) || p.Code != "invalid" || out != (normalizedTransfer{}) {
				t.Fatalf("invalid input returned usable transfer: %+v, %v", out, err)
			}
		})
	}
}

func TestTransferAmountBoundariesStayIntegerAndBalanced(t *testing.T) {
	for amount, want := range map[string]int64{"0.01": 1, "1.1": 110, "0001.01": 101, "10000000000": 1_000_000_000_000} {
		in := syntheticTransfer()
		in.Amount = amount
		out, err := normalizeTransfer(in)
		if err != nil || out.AmountMinor != want {
			t.Fatalf("%s: %+v, %v", amount, out, err)
		}
		legs := out.legs()
		if legs[0].AmountMinor != -want || legs[1].AmountMinor != want || legs[0].AmountMinor+legs[1].AmountMinor != 0 {
			t.Fatalf("%s unbalanced: %+v", amount, legs)
		}
	}
}

func TestTransferStillCannotEnterThroughOrdinaryInput(t *testing.T) {
	_, err := normalize(Input{Date: "2026-09-19", Type: "transfer", Amount: "100", Currency: "CNY", Merchant: "合成转账", Source: "合成来源"})
	if err == nil {
		t.Fatal("ordinary income/expense normalization admitted a transfer")
	}
}

func TestTransferAcceptsSupportedTextAndDateBoundaries(t *testing.T) {
	for _, date := range []string{"1900-01-01", "2200-12-31", "2024-02-29"} {
		in := syntheticTransfer()
		in.Date = date
		in.FromAccount = strings.Repeat("出", 200)
		in.ToAccount = strings.Repeat("入", 200)
		in.Note = strings.Repeat("注", 1000)
		out, err := normalizeTransfer(in)
		if err != nil || out.Date != date || out.FromAccount != in.FromAccount || out.ToAccount != in.ToAccount || out.Note != in.Note {
			t.Fatalf("supported boundary rejected or truncated: %s, %v", date, err)
		}
	}
}
