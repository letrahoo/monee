package ledger

import "testing"

func TestImportHistoryShowsUndoAndRestoreWithoutReimport(t *testing.T) {
	s := testStore(t)
	raw := []byte(nativeHeader + nativeExpense)
	p := alipayPreview(t, s, raw, "wallet")
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	before, e := s.PreviewImportUndo(p.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UndoImport(p.ID, before.LedgerVersion); e != nil {
		t.Fatal(e)
	}
	h, e := s.ImportHistory(1)
	if e != nil || len(h.Imports) != 1 || !h.Imports[0].Undone {
		t.Fatal(h, e)
	}
	d, e := s.ImportDetail(p.ID)
	if e != nil || !d.Undone || !d.Preview.Undone || !d.Preview.AlreadyCommitted {
		t.Fatal(d, e)
	}
	again := alipayPreview(t, s, raw, "wallet")
	if !again.Undone || !again.AlreadyCommitted {
		t.Fatal("repeat must describe withdrawn batch", again)
	}
	restore, e := s.PreviewImportUndo(p.ID, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RestoreImport(p.ID, restore.LedgerVersion); e != nil {
		t.Fatal(e)
	}
	h, e = s.ImportHistory(1)
	if e != nil || h.Imports[0].Undone {
		t.Fatal(h, e)
	}
	again = alipayPreview(t, s, raw, "wallet")
	if again.Undone || !again.AlreadyCommitted {
		t.Fatal("restored state not reflected", again)
	}
}
