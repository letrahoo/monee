package ledger

import (
	"errors"
	"reflect"
	"testing"
)

func TestExpensePairEffectiveRefundSummaryRequiresWithdrawal(t *testing.T) {
	root, s, in, receipt := effectiveConfirmed(t)
	viewer := lifecycleMember(t, root, "viewer")
	before := nativeEvidenceTables(t, root)
	for _, reader := range []*Store{s, viewer} {
		for _, id := range []string{in.PrimaryTransactionID, in.SecondaryTransactionID} {
			out, err := reader.Refunds(id)
			var p *Problem
			if !errors.As(err, &p) || p.Code != "expense_pair_locked" || !reflect.DeepEqual(out, RefundSummary{Refunds: []Transaction{}}) {
				t.Fatal("active observations must not advertise independent refund allowances", out, err)
			}
		}
	}
	if !reflect.DeepEqual(before, nativeEvidenceTables(t, root)) {
		t.Fatal("summary read changed evidence")
	}
	effectiveWithdraw(t, s, receipt)
	for _, id := range []string{in.PrimaryTransactionID, in.SecondaryTransactionID} {
		out, err := viewer.Refunds(id)
		if err != nil || out.Original.ID != id || out.RemainingMinor != "1234" || out.RefundedMinor != "0" || len(out.Refunds) != 0 {
			t.Fatal(out, err)
		}
	}
	refund, err := s.CreateRefund(in.PrimaryTransactionID, RefundInput{Version: in.PrimaryVersion, Date: "2026-09-16", Amount: "1.00", Currency: "CNY", Account: "合成退款账户"}, "effective-summary-refund")
	if err != nil {
		t.Fatal(err)
	}
	byOriginal, err := viewer.Refunds(in.PrimaryTransactionID)
	if err != nil {
		t.Fatal(err)
	}
	byRefund, err := viewer.Refunds(refund.ID)
	if err != nil || !reflect.DeepEqual(byOriginal, byRefund) || byOriginal.RefundedMinor != "100" || byOriginal.RemainingMinor != "1134" || len(byOriginal.Refunds) != 1 {
		t.Fatal(byOriginal, byRefund, err)
	}
}

func TestExpensePairEffectiveRefundSummaryDamagedDecisionFailsClosed(t *testing.T) {
	root, s, in, _ := effectiveConfirmed(t)
	foundationExec(t, root, "UPDATE expense_pair_members SET active=0 WHERE transaction_id=?", in.SecondaryTransactionID)
	before := nativeEvidenceTables(t, root)
	for _, id := range []string{in.PrimaryTransactionID, in.SecondaryTransactionID} {
		out, err := s.Refunds(id)
		var p *Problem
		if !errors.As(err, &p) || p.Code != "expense_pair_unavailable" || !reflect.DeepEqual(out, RefundSummary{Refunds: []Transaction{}}) {
			t.Fatal(out, err)
		}
	}
	if !reflect.DeepEqual(before, nativeEvidenceTables(t, root)) {
		t.Fatal("damaged summary read changed evidence")
	}
	// Authorization must win over corruption: do not reveal association state.
	foundationExec(t, root, "DELETE FROM ledger_members WHERE ledger_id=? AND user_id=?", s.ledgerID, s.actorID)
	_, err := s.Refunds(in.PrimaryTransactionID)
	var p *Problem
	if !errors.As(err, &p) || p.Code != "ledger_denied" {
		t.Fatal(err)
	}
}
