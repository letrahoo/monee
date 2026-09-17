package ledger

import "testing"

func TestAnnotationHistoryConflictAndImportIdentity(t *testing.T) {
	s := testStore(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense), "wallet")
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	original := p.Rows[0].Record
	updated, e := s.Annotate(original.ID, AnnotationInput{Version: original.Version, Category: "早餐", Note: "已核实"})
	if e != nil || updated.Version != 2 || updated.AmountMinor != original.AmountMinor {
		t.Fatal(updated, e)
	}
	d := dashboard(t, s)
	if d.ExpenseMinor != "1234" || d.Categories[0].Name != "早餐" {
		t.Fatal(d)
	}
	if _, e = s.Annotate(original.ID, AnnotationInput{Version: 1, Category: "晚餐"}); e == nil {
		t.Fatal("stale update accepted")
	}
	h, e := s.AnnotationHistory(original.ID)
	if e != nil || len(h) != 1 || h[0].Before.Category == h[0].After.Category {
		t.Fatal(h, e)
	}
	var balance int
	if e = s.db.QueryRow("SELECT sum(amount_minor) FROM postings WHERE transaction_id=?", original.ID).Scan(&balance); e != nil || balance != 0 {
		t.Fatal(balance, e)
	}
	again := alipayPreview(t, s, []byte("账单元信息\n"+nativeHeader+nativeExpense), "wallet")
	if len(again.Errors) > 0 || again.DuplicateCount != 1 {
		t.Fatal("annotation broke dedup", again)
	}
	if _, e = s.Commit(again.ID, again.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	if dashboard(t, s).Transactions[0].Category != "早餐" {
		t.Fatal("import overwrote annotation")
	}
}
