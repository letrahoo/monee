package ledger

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestExpensePairEffectiveCorruptionBlocksReadersAndWriters(t *testing.T) {
	for _, damage := range []string{"marker", "member", "one-table", "two-tables", "three-tables"} {
		t.Run(damage, func(t *testing.T) {
			root, s, in, r := effectiveConfirmed(t)
			var importID string
			if err := root.db.QueryRow("SELECT import_id FROM source_records WHERE transaction_id=?", in.PrimaryTransactionID).Scan(&importID); err != nil {
				t.Fatal(err)
			}
			row := strings.ReplaceAll(strings.ReplaceAll(nativeExpense, "pay-1", "lifecycle-pay-a"), "order-1", "lifecycle-order-a")
			closed := strings.ReplaceAll(nativeExpense, "交易成功", "交易关闭")
			preview := alipayPreview(t, s, []byte(nativeHeader+row+closed), "synthetic-lifecycle-wallet")
			switch damage {
			case "marker":
				foundationExec(t, root, "UPDATE expense_pair_members SET active=0 WHERE transaction_id=?", in.PrimaryTransactionID)
			case "member":
				foundationExec(t, root, "DELETE FROM expense_pair_members WHERE transaction_id=?", in.PrimaryTransactionID)
			default:
				n := map[string]int{"one-table": 1, "two-tables": 2, "three-tables": 3}[damage]
				for _, table := range []string{"expense_pair_members", "expense_pair_revisions", "expense_pair_decisions"}[:n] {
					foundationExec(t, root, "ALTER TABLE "+table+" RENAME TO missing_"+table)
				}
			}
			calls := map[string]func() error{
				"dashboard": func() error { _, e := s.Dashboard("2026-09", "", 1); return e },
				"annotation": func() error {
					_, e := s.Annotate(in.PrimaryTransactionID, AnnotationInput{Version: in.PrimaryVersion, Note: "blocked"})
					return e
				},
				"preview": func() error {
					_, e := s.PreviewCorrection(in.PrimaryTransactionID, CorrectionInput{Version: in.PrimaryVersion, Type: "expense", AmountMinor: "1000", Reason: "synthetic"})
					return e
				},
				"correction": func() error {
					_, e := s.Correct(in.PrimaryTransactionID, CorrectionInput{Version: in.PrimaryVersion, Type: "expense", AmountMinor: "1000", Reason: "synthetic"})
					return e
				},
				"refund": func() error {
					_, e := s.CreateRefund(in.PrimaryTransactionID, RefundInput{Version: in.PrimaryVersion, Date: "2026-09-16", Amount: "1.00", Currency: "CNY", Account: "合成退款账户"}, "corrupt-refund-no-write")
					return e
				},
				"commit":  func() error { _, e := s.Commit(preview.ID, preview.LedgerVersion, true); return e },
				"undo":    func() error { _, e := s.UndoImport(importID, r.LedgerVersion); return e },
				"restore": func() error { _, e := s.RestoreImport(importID, r.LedgerVersion); return e },
			}
			for name, call := range calls {
				t.Run(name, func(t *testing.T) {
					before := nativeEvidenceTables(t, root)
					err := call()
					var p *Problem
					if !errors.As(err, &p) || p.Code != "expense_pair_unavailable" {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(before, nativeEvidenceTables(t, root)) {
						t.Fatal("damaged metadata allowed writes")
					}
				})
			}
		})
	}
}
