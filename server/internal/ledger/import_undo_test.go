package ledger

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func committedUndoFixture(t *testing.T, s *Store, csv string) Preview {
	t.Helper()
	p := preview(t, s, csv)
	if _, e := s.Commit(p.ID, p.LedgerVersion, true); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestImportUndoRestoreAndIdempotency(t *testing.T) {
	s := testStore(t)
	p := committedUndoFixture(t, s, sampleCSV)
	original, e := s.ImportDetail(p.ID)
	if e != nil {
		t.Fatal(e)
	}
	evidence, _ := json.Marshal(original.Evidence)
	originalChanges := count(t, s, "change_log")
	u, e := s.PreviewImportUndo(p.ID, false)
	if e != nil || u.ChangeCount != 3 || u.IncomeMinor != "2500000" || u.ExpenseMinor != "12450" || count(t, s, "change_log") != originalChanges {
		t.Fatal(u, e)
	}
	if dashboard(t, s).TotalCount != 3 {
		t.Fatal("preview wrote ledger")
	}
	if _, e = s.UndoImport(p.ID, u.LedgerVersion-1); e == nil {
		t.Fatal("stale preview accepted")
	}
	undone, e := s.UndoImport(p.ID, u.LedgerVersion)
	if e != nil || undone.State != "undone" || dashboard(t, s).TotalCount != 0 || dashboard(t, s).ExpenseMinor != "0" {
		t.Fatal(undone, e)
	}
	if count(t, s, "transactions") != 3 || count(t, s, "postings") != 6 {
		t.Fatal("physical records removed")
	}
	again, e := s.UndoImport(p.ID, u.LedgerVersion)
	if e != nil || !again.AlreadyApplied || again.LedgerVersion != undone.LedgerVersion {
		t.Fatal(again, e)
	}
	same := preview(t, s, sampleCSV)
	if !same.AlreadyCommitted {
		t.Fatal("undone file became new import")
	}
	if _, e = s.Commit(same.ID, same.LedgerVersion, true); e != nil || dashboard(t, s).TotalCount != 0 {
		t.Fatal("reimport resurrected transactions", e)
	}
	// Different bytes but identical source IDs must also remain a duplicate.
	alternate := preview(t, s, strings.Replace(sampleCSV, "28.50", "28.5", 1))
	if alternate.DuplicateCount != 3 || alternate.NewCount != 0 {
		t.Fatal(alternate)
	}
	if _, e = s.Commit(alternate.ID, alternate.LedgerVersion, true); e != nil || dashboard(t, s).TotalCount != 0 {
		t.Fatal("different-file reimport resurrected transactions", e)
	}
	restore, e := s.PreviewImportUndo(p.ID, true)
	if e != nil || restore.ChangeCount != 3 || restore.BlockedCount != 0 {
		t.Fatal(restore, e)
	}
	restored, e := s.RestoreImport(p.ID, restore.LedgerVersion)
	if e != nil || restored.State != "committed" || dashboard(t, s).ExpenseMinor != "12450" || dashboard(t, s).IncomeMinor != "2500000" {
		t.Fatal(restored, e)
	}
	again, e = s.RestoreImport(p.ID, restore.LedgerVersion)
	if e != nil || !again.AlreadyApplied {
		t.Fatal(again, e)
	}
	after, e := s.ImportDetail(p.ID)
	afterEvidence, _ := json.Marshal(after.Evidence)
	if e != nil || string(evidence) != string(afterEvidence) {
		t.Fatal("source evidence mutated", e)
	}
	var balance int
	if e = s.db.QueryRow("SELECT sum(amount_minor) FROM postings").Scan(&balance); e != nil || balance != 0 {
		t.Fatal(balance, e)
	}
}
func TestImportUndoProtectsEditsSharedAndManual(t *testing.T) {
	s := testStore(t)
	p := committedUndoFixture(t, s, sampleCSV)
	if _, e := s.Annotate(p.Rows[0].Record.ID, AnnotationInput{Version: 1, Category: "已核实"}); e != nil {
		t.Fatal(e)
	}
	header := strings.Split(sampleCSV, "\n")[0] + "\n"
	duplicate := committedUndoFixture(t, s, header+strings.Split(sampleCSV, "\n")[2]+"\n")
	manual, e := s.Create(testInput(), "synthetic-manual-undo-01")
	if e != nil {
		t.Fatal(e)
	}
	p2, e := s.PreviewImportUndo(duplicate.ID, false)
	if e != nil || p2.ChangeCount != 0 || p2.PreservedCount != 1 {
		t.Fatal(p2, e)
	}
	u, e := s.PreviewImportUndo(p.ID, false)
	if e != nil || u.ChangeCount != 1 || u.PreservedCount != 2 || u.IncomeMinor != "2500000" {
		t.Fatal(u, e)
	}
	if _, e = s.UndoImport(p.ID, u.LedgerVersion); e != nil {
		t.Fatal(e)
	}
	d := dashboard(t, s)
	if d.TotalCount != 3 || d.IncomeMinor != "0" || d.ExpenseMinor != "13684" {
		t.Fatal(d)
	}
	var found bool
	for _, r := range d.Transactions {
		if r.ID == manual.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("manual record touched")
	}
	r, e := s.PreviewImportUndo(p.ID, true)
	if e != nil || r.ChangeCount != 1 || r.BlockedCount != 0 {
		t.Fatal(r, e)
	}
	if _, e = s.RestoreImport(p.ID, r.LedgerVersion); e != nil {
		t.Fatal(e)
	}
	// Re-undo recognizes only unchanged transactions from its own restoration.
	u, e = s.PreviewImportUndo(p.ID, false)
	if e != nil || u.ChangeCount != 1 || u.PreservedCount != 2 {
		t.Fatal(u, e)
	}
}
func TestImportUndoProtectsCorrection(t *testing.T) {
	s := testStore(t)
	p := committedUndoFixture(t, s, sampleCSV)
	if _, e := s.Correct(p.Rows[0].Record.ID, CorrectionInput{Version: 1, Type: "income", AmountMinor: "42", Reason: "合成更正"}); e != nil {
		t.Fatal(e)
	}
	u, e := s.PreviewImportUndo(p.ID, false)
	if e != nil || u.ChangeCount != 2 || u.PreservedCount != 1 {
		t.Fatal(u, e)
	}
	if _, e = s.UndoImport(p.ID, u.LedgerVersion); e != nil {
		t.Fatal(e)
	}
	if d := dashboard(t, s); d.TotalCount != 1 || d.IncomeMinor != "42" {
		t.Fatal(d)
	}
}
func TestImportRestoreConflictIsAtomic(t *testing.T) {
	s := testStore(t)
	p := committedUndoFixture(t, s, sampleCSV)
	u, e := s.PreviewImportUndo(p.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UndoImport(p.ID, u.LedgerVersion); e != nil {
		t.Fatal(e)
	}
	// Simulate a future independently synced mutation to one tombstone.
	if _, e = s.db.Exec("UPDATE transactions SET version=version+1 WHERE id=?", p.Rows[1].Record.ID); e != nil {
		t.Fatal(e)
	}
	r, e := s.PreviewImportUndo(p.ID, true)
	if e != nil || r.ChangeCount != 2 || r.PreservedCount != 1 || r.BlockedCount != 1 {
		t.Fatal(r, e)
	}
	before := count(t, s, "change_log")
	if _, e = s.RestoreImport(p.ID, r.LedgerVersion); e == nil {
		t.Fatal("changed tombstone restored")
	}
	if dashboard(t, s).TotalCount != 0 || count(t, s, "change_log") != before {
		t.Fatal("partial restore")
	}
}
func TestImportUndoConcurrentRequests(t *testing.T) {
	s := testStore(t)
	p := committedUndoFixture(t, s, sampleCSV)
	u, e := s.PreviewImportUndo(p.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan ImportUndoPreview, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := s.UndoImport(p.ID, u.LedgerVersion); results <- r; failures <- e }()
	}
	wg.Wait()
	close(results)
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	applied := 0
	for r := range results {
		if !r.AlreadyApplied {
			applied++
		}
	}
	if applied != 1 || dashboard(t, s).Version != u.LedgerVersion+1 {
		t.Fatal("concurrent duplicate mutation")
	}
}
func TestImportUndoRejectsUncommittedOrForeign(t *testing.T) {
	s := testStore(t)
	p := preview(t, s, sampleCSV)
	if _, e := s.PreviewImportUndo(p.ID, false); e == nil {
		t.Fatal("uncommitted preview accepted")
	}
	if _, e := s.UndoImport("unknown", 0); e == nil {
		t.Fatal("unknown import accepted")
	}
}
