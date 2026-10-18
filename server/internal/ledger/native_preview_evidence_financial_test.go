package ledger

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func TestNativeEvidenceStoreDuplicateDoesNotResurrectOrReallocateRefund(t *testing.T) {
	for _, variant := range []string{"soft-deleted", "refund-linked"} {
		t.Run(variant, func(t *testing.T) {
			s := testStore(t)
			raw := nativeHeader + nativeExpense
			p := alipayPreview(t, s, []byte(raw), "synthetic-wallet")
			other := alipayPreview(t, s, []byte(strings.ReplaceAll(raw, "\n", "\r\n")), "synthetic-wallet")
			if _, e := s.Commit(other.ID, other.LedgerVersion, false); e != nil {
				t.Fatal(e)
			}
			id := other.Rows[0].Record.ID
			if variant == "soft-deleted" {
				u, e := s.PreviewImportUndo(other.ID, false)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = s.UndoImport(other.ID, u.LedgerVersion); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e := s.CreateRefund(id, RefundInput{Version: 1, Date: "2026-09-16", Amount: "1.00", Currency: "CNY", Account: "合成退款账户", Note: "合成关联退款"}, "synthetic-source-refund"); e != nil {
					t.Fatal(e)
				}
			}
			before := dashboard(t, s)
			again, e := s.PreviewAlipay("repeat.csv", base64.StdEncoding.EncodeToString([]byte(raw)), "synthetic-wallet")
			if e != nil || again.DuplicateCount != 1 || len(again.Rows) != 1 || again.Rows[0].Record.ID != id || again.Rows[0].Record.AmountMinor != "-1234" || again.Rows[0].Record.RefundOf != "" || !reflect.DeepEqual(again.SourceDocument, p.SourceDocument) {
				t.Fatal("dynamic state replaced original candidate", again, e)
			}
			result, e := s.Commit(again.ID, again.LedgerVersion, false)
			if e != nil || result.Added != 0 || result.Skipped != 1 {
				t.Fatal(result, e)
			}
			after := dashboard(t, s)
			if before.TotalCount != after.TotalCount || before.GrossExpenseMinor != after.GrossExpenseMinor || before.RefundMinor != after.RefundMinor || before.ExpenseMinor != after.ExpenseMinor || before.IncomeMinor != after.IncomeMinor {
				t.Fatal("duplicate revived/reallocated financial data", before, after)
			}
			if variant == "soft-deleted" && after.TotalCount != 0 {
				t.Fatal("soft-deleted original resurrected")
			}
		})
	}
}
