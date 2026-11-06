package ledger

import (
	"strings"
	"testing"
)

func TestExpensePairEffectiveMixedCommitRollsBackEarlierInsert(t *testing.T) {
	root, s, _, _ := effectiveConfirmed(t)
	fresh := strings.ReplaceAll(strings.ReplaceAll(nativeExpense, "pay-1", "effective-new-row"), "order-1", "effective-new-order")
	duplicate := strings.ReplaceAll(strings.ReplaceAll(nativeExpense, "pay-1", "lifecycle-pay-a"), "order-1", "lifecycle-order-a")
	p := alipayPreview(t, s, []byte(nativeHeader+fresh+duplicate), "synthetic-lifecycle-wallet")
	if len(p.Rows) != 2 || p.Rows[0].Status == "duplicate" || p.Rows[1].Status != "duplicate" {
		t.Fatal(p)
	}
	effectiveLocked(t, root, func() error { _, err := s.Commit(p.ID, p.LedgerVersion, true); return err })
}

func TestExpensePairEffectiveMixedUndoPreservesWholeBatch(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	fresh := strings.ReplaceAll(strings.ReplaceAll(nativeExpense, "pay-1", "effective-batch-new"), "order-1", "effective-batch-order")
	duplicate := strings.ReplaceAll(strings.ReplaceAll(nativeExpense, "pay-1", "lifecycle-pay-a"), "order-1", "lifecycle-order-a")
	p := alipayPreview(t, s, []byte(nativeHeader+fresh+duplicate), "synthetic-lifecycle-wallet")
	if r, err := s.Commit(p.ID, p.LedgerVersion, true); err != nil || r.Added != 1 || r.Skipped != 1 {
		t.Fatal(r, err)
	}
	lifecycleRefresh(t, root, &in)
	receipt, err := s.confirmExpensePair(in, "effective-mixed-batch-confirm")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewImportUndo(p.ID, false)
	if err != nil || preview.BlockedCount != 1 || preview.ChangeCount != 0 || preview.IncomeMinor != "0" || preview.ExpenseMinor != "0" || len(preview.Rows) != 2 {
		t.Fatal(preview, err)
	}
	for _, row := range preview.Rows {
		if row.Action != "preserve" {
			t.Fatal(row)
		}
	}
	effectiveLocked(t, root, func() error { _, err := s.UndoImport(p.ID, preview.LedgerVersion); return err })
	effectiveWithdraw(t, s, receipt)
	preview, err = s.PreviewImportUndo(p.ID, false)
	if err != nil || preview.ChangeCount != 1 || preview.BlockedCount != 0 {
		t.Fatal(preview, err)
	}
	if _, err = s.UndoImport(p.ID, preview.LedgerVersion); err != nil {
		t.Fatal(err)
	}
	preview, err = s.PreviewImportUndo(p.ID, true)
	if err != nil || preview.ChangeCount != 1 {
		t.Fatal(preview, err)
	}
	if _, err = s.RestoreImport(p.ID, preview.LedgerVersion); err != nil {
		t.Fatal(err)
	}
}
