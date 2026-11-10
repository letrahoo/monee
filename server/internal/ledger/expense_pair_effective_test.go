package ledger

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Effective projection and writer guards are still behind the Open fence; these
// tests drive them through the private lifecycle on an isolated synthetic store.

func effectiveConfirmed(t *testing.T) (*Store, *Store, expensePairConfirmInput, expensePairReceipt) {
	t.Helper()
	root, s, in := expensePairLifecycleFixture(t)
	r, err := s.confirmExpensePair(in, "effective-confirm-key")
	if err != nil {
		t.Fatal(err)
	}
	return root, s, in, r
}

func effectiveWithdraw(t *testing.T, s *Store, r expensePairReceipt) {
	t.Helper()
	current, err := s.version()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.withdrawExpensePair(expensePairWithdrawInput{LedgerVersion: current, DecisionID: r.ID, DecisionVersion: r.Version, Reason: "合成撤回后恢复原操作"}, "effective-withdraw-key"); err != nil {
		t.Fatal(err)
	}
}

func effectiveLocked(t *testing.T, root *Store, call func() error) {
	t.Helper()
	before := nativeEvidenceTables(t, root)
	err := call()
	var p *Problem
	if err == nil || !errors.As(err, &p) || p.Code != "expense_pair_locked" {
		t.Fatal("expected expense_pair_locked", err)
	}
	if !reflect.DeepEqual(before, nativeEvidenceTables(t, root)) {
		t.Fatal("blocked writer changed rows")
	}
}

func effectiveVersion(t *testing.T, root *Store, id string) int64 {
	t.Helper()
	var v int64
	if err := root.db.QueryRow("SELECT version FROM transactions WHERE id=?", id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestExpensePairEffectiveDashboardCountsOnceAndRestoresAfterWithdraw(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	raw := dashboard(t, s)
	if raw.TotalCount != 2 || raw.ExpenseMinor != "2468" {
		t.Fatal("fixture must start with two gross expenses", raw)
	}
	viewer := lifecycleMember(t, root, "viewer")
	lifecycleRefresh(t, root, &in)
	original := lifecycleFinancialRows(t, root)
	r, err := s.confirmExpensePair(in, "effective-dashboard-confirm")
	if err != nil {
		t.Fatal(err)
	}
	once := dashboard(t, s)
	if once.TotalCount != 1 || once.FilteredCount != 1 || once.ExpenseMinor != "1234" || once.GrossExpenseMinor != "1234" || once.RefundMinor != "0" || len(once.Transactions) != 1 || once.Transactions[0].ID != in.PrimaryTransactionID {
		t.Fatal("active pair must count once with the explicit primary", once)
	}
	if len(once.Categories) != 1 || once.Categories[0].AmountMinor != "1234" || once.Version != r.LedgerVersion {
		t.Fatal("category totals and version must use the same effective snapshot", once)
	}
	// Searching for the shared merchant must not reveal the suppressed duplicate.
	searched, err := s.Dashboard("2026-09", "合成咖啡店", 1)
	if err != nil || searched.FilteredCount != 1 || len(searched.Transactions) != 1 {
		t.Fatal(searched, err)
	}
	// Viewers read the same effective projection.
	if v := dashboard(t, viewer); v.ExpenseMinor != "1234" || v.TotalCount != 1 {
		t.Fatal("viewer projection differs", v)
	}
	effectiveWithdraw(t, s, r)
	restored := dashboard(t, s)
	if restored.TotalCount != 2 || restored.ExpenseMinor != "2468" || !reflect.DeepEqual(raw.Transactions, restored.Transactions) {
		t.Fatal("withdrawn pair must restore both originals", restored)
	}
	if !reflect.DeepEqual(original, lifecycleFinancialRows(t, root)) {
		t.Fatal("effective projection must not rewrite original financial rows")
	}
}

func TestExpensePairEffectiveDashboardFailsClosedOnDamagedMetadata(t *testing.T) {
	for name, damage := range map[string]string{
		"member facts":   "UPDATE expense_pair_members SET facts_json='{}' WHERE role='secondary'",
		"orphan member":  "DELETE FROM expense_pair_decisions",
		"root version":   "UPDATE expense_pair_decisions SET version=7",
		"revision chain": "DELETE FROM expense_pair_revisions",
	} {
		t.Run(name, func(t *testing.T) {
			root, s, _, _ := effectiveConfirmed(t)
			foundationExec(t, root, "PRAGMA foreign_keys=OFF")
			foundationExec(t, root, damage)
			out, err := s.Dashboard("2026-09", "", 1)
			var p *Problem
			if err == nil || !errors.As(err, &p) || p.Code != "expense_pair_unavailable" || out.ExpenseMinor == "2468" && out.TotalCount == 2 && len(out.Transactions) == 2 {
				t.Fatal("damaged metadata must not fall back to raw totals", out, err)
			}
		})
	}
}

func TestExpensePairEffectiveMissingSchemaFailsClosed(t *testing.T) {
	root, s, in, _ := effectiveConfirmed(t)
	for _, table := range []string{"expense_pair_members", "expense_pair_revisions", "expense_pair_decisions"} {
		foundationExec(t, root, "ALTER TABLE "+table+" RENAME TO missing_"+table)
	}
	_, err := s.Dashboard("2026-09", "", 1)
	var p *Problem
	if !errors.As(err, &p) || p.Code != "expense_pair_unavailable" {
		t.Fatal("missing schema5 tables must not double count", err)
	}
	_, err = s.Annotate(in.PrimaryTransactionID, AnnotationInput{Version: in.PrimaryVersion, Note: "must not write"})
	if !errors.As(err, &p) || p.Code != "expense_pair_unavailable" {
		t.Fatal("missing schema5 tables must not unlock writes", err)
	}
}

func TestExpensePairEffectiveDriftedMemberFailsClosed(t *testing.T) {
	root, s, in, _ := effectiveConfirmed(t)
	// An external writer bypassing guards changes the secondary's facts.
	foundationExec(t, root, "UPDATE transactions SET note='外部修改' ,version=version+1 WHERE id=?", in.SecondaryTransactionID)
	if _, err := s.Dashboard("2026-09", "", 1); err == nil || !strings.Contains(err.Error(), "关联决策") {
		t.Fatal("drifted active member must fail closed", err)
	}
}

func TestExpensePairEffectiveGuardsOldWriters(t *testing.T) {
	root, s, in, r := effectiveConfirmed(t)
	for _, id := range []string{in.PrimaryTransactionID, in.SecondaryTransactionID} {
		id := id
		version := effectiveVersion(t, root, id)
		effectiveLocked(t, root, func() error {
			_, e := s.Annotate(id, AnnotationInput{Version: version, Category: "餐饮美食", Note: "合成备注修改"})
			return e
		})
		effectiveLocked(t, root, func() error {
			_, e := s.PreviewCorrection(id, CorrectionInput{Version: version, Type: "expense", AmountMinor: "1000", Reason: "合成更正"})
			return e
		})
		effectiveLocked(t, root, func() error {
			_, e := s.Correct(id, CorrectionInput{Version: version, Type: "expense", AmountMinor: "1000", Reason: "合成更正"})
			return e
		})
		effectiveLocked(t, root, func() error {
			_, e := s.CreateRefund(id, RefundInput{Version: version, Date: "2026-09-16", Amount: "1.00", Currency: "CNY", Account: "合成退款账户"}, "effective-refund-"+id[:8]+"-key")
			return e
		})
	}
	// Ordinary new writers stay available and are never auto-grouped.
	if _, err := s.Create(Input{Date: "2026-09-15", Type: "expense", Amount: "12.34", Currency: "CNY", Merchant: "合成咖啡店", Category: "餐饮美食", Source: "手动", Account: "银行卡A"}, "effective-new-manual-key"); err != nil {
		t.Fatal(err)
	}
	if d := dashboard(t, s); d.TotalCount != 2 || d.ExpenseMinor != "2468" {
		t.Fatal("new independent expense must count once more", d)
	}
	effectiveWithdraw(t, s, r)
	version := effectiveVersion(t, root, in.SecondaryTransactionID)
	if _, err := s.Annotate(in.SecondaryTransactionID, AnnotationInput{Version: version, Category: "餐饮美食", Note: "撤回后可修改"}); err != nil {
		t.Fatal("withdrawn member must be editable again", err)
	}
	version = effectiveVersion(t, root, in.PrimaryTransactionID)
	if _, err := s.CreateRefund(in.PrimaryTransactionID, RefundInput{Version: version, Date: "2026-09-16", Amount: "1.00", Currency: "CNY", Account: "合成退款账户"}, "effective-refund-after-withdraw"); err != nil {
		t.Fatal("withdrawn member must accept refunds again", err)
	}
}

func TestExpensePairEffectiveBlocksWholeImportUndo(t *testing.T) {
	root, s, in, r := effectiveConfirmed(t)
	var importID string
	if err := root.db.QueryRow("SELECT import_id FROM source_records WHERE transaction_id=? AND disposition='added'", in.SecondaryTransactionID).Scan(&importID); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewImportUndo(importID, false)
	if err != nil || preview.BlockedCount != 1 || preview.ChangeCount != 0 || preview.Rows[0].Action != "preserve" || !strings.Contains(preview.Rows[0].Reason, "撤回关联") {
		t.Fatal("undo preview must report the locked member", preview, err)
	}
	effectiveLocked(t, root, func() error {
		_, e := s.UndoImport(importID, preview.LedgerVersion)
		return e
	})
	effectiveWithdraw(t, s, r)
	preview, err = s.PreviewImportUndo(importID, false)
	if err != nil || preview.BlockedCount != 0 || preview.ChangeCount != 1 {
		t.Fatal(preview, err)
	}
	if _, err = s.UndoImport(importID, preview.LedgerVersion); err != nil {
		t.Fatal("withdrawn member import must be undoable", err)
	}
	if d := dashboard(t, s); d.TotalCount != 1 || d.ExpenseMinor != "1234" {
		t.Fatal(d)
	}
}

func TestExpensePairEffectiveBlocksDuplicateSourceLinkCommit(t *testing.T) {
	root, s, _, r := effectiveConfirmed(t)
	row := strings.ReplaceAll(strings.ReplaceAll(nativeExpense, "pay-1", "lifecycle-pay-a"), "order-1", "lifecycle-order-a")
	// A different file (one extra closed, never-booked line) re-observes the primary.
	closed := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(nativeExpense, "pay-1", "effective-closed"), "order-1", "effective-closed-order"), "交易成功", "交易关闭")
	p := alipayPreview(t, s, []byte(nativeHeader+row+closed), "synthetic-lifecycle-wallet")
	if len(p.Rows) != 1 || p.Rows[0].Status != "duplicate" {
		t.Fatal("re-import must be recognised as a duplicate of the primary", p.Rows)
	}
	effectiveLocked(t, root, func() error {
		_, e := s.Commit(p.ID, p.LedgerVersion, true)
		return e
	})
	effectiveWithdraw(t, s, r)
	p = alipayPreview(t, s, []byte(nativeHeader+row+closed), "synthetic-lifecycle-wallet")
	if res, err := s.Commit(p.ID, p.LedgerVersion, true); err != nil || res.Skipped != 1 || res.Added != 0 {
		t.Fatal("after withdrawal the duplicate link commits normally", res, err)
	}
}
