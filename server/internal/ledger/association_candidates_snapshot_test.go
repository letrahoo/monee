package ledger

import (
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestAssociationCandidatesStoreNativeAndLegacyStyleStayUnverified(t *testing.T) {
	s := testStore(t)
	in := testInput()
	in.Date = "2026-09-15"
	in.Merchant = "合成咖啡店"
	in.Source = "合成银行展示"
	in.ExternalID = "manual-unverified"
	base, e := s.Create(in, "association-mixed-base")
	if e != nil {
		t.Fatal(e)
	}
	p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense), "synthetic-wallet")
	if r, e := s.Commit(p.ID, p.LedgerVersion, true); e != nil || r.Added != 1 {
		t.Fatal(r, e)
	}
	in.Source = "合成旧迁移展示"
	in.ExternalID = "ai-financial:synthetic-legacy"
	legacy, e := s.Create(in, "association-mixed-legacy")
	if e != nil {
		t.Fatal(e)
	}
	before := associationTestSnapshot(t, s)
	out, e := s.TransactionAssociationCandidates(base.ID, 1)
	if e != nil || out.TotalCount != 2 || out.SourceRolesVerified || len(out.Actions) != 0 {
		t.Fatal("trace namespace or import link promoted provenance", out, e)
	}
	wantIDs := map[string]bool{p.Rows[0].Record.ID: true, legacy.ID: true}
	if len(out.Items) != len(wantIDs) {
		t.Fatal("native/legacy hint page length differs from count", out)
	}
	for _, item := range out.Items {
		if !wantIDs[item.Transaction.ID] {
			t.Fatal("native/legacy hint replaced with an unrelated/self transaction", item)
		}
		delete(wantIDs, item.Transaction.ID)
	}
	if len(wantIDs) != 0 {
		t.Fatal("native or legacy-style booked projection was hidden")
	}
	if !reflect.DeepEqual(before, associationTestSnapshot(t, s)) {
		t.Fatal("current similarity lookup modified import evidence")
	}
	if d := dashboard(t, s); d.TotalCount != 3 || d.ExpenseMinor != "3702" {
		t.Fatal("hint query silently deduped transactions", d)
	}
}

func TestAssociationCandidatesStoreOneSnapshotDuringCorrection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-snapshot.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	other, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	base := associationTestCreate(t, s, "合成来源A", "snapshot-base")
	candidate := associationTestCreate(t, s, "合成来源B", "snapshot-other")
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	var writeErr error
	go func() {
		defer wg.Done()
		<-start
		_, writeErr = other.Correct(candidate.ID, CorrectionInput{Version: 1, Type: "expense", AmountMinor: "1235", Reason: "合成并发更正"})
	}()
	close(start)
	defer wg.Wait() // Join the writer before either Store closes, including Fatal paths.
	for i := 0; i < 20; i++ {
		p, e := s.TransactionAssociationCandidates(base.ID, 1)
		if e != nil {
			t.Fatal(e)
		}
		switch p.LedgerVersion {
		case 2:
			if p.TotalCount != 1 || len(p.Items) != 1 || p.Items[0].Transaction.AmountMinor != "-1234" || p.Items[0].Transaction.Version != 1 {
				t.Fatal("mixed pre-correction read snapshot", p)
			}
		case 3:
			if p.TotalCount != 0 || len(p.Items) != 0 {
				t.Fatal("mixed post-correction read snapshot", p)
			}
		default:
			t.Fatal("unexpected financial version", p.LedgerVersion)
		}
	}
	wg.Wait()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	before := associationTestSnapshot(t, s)
	p, e := s.TransactionAssociationCandidates(base.ID, 1)
	if e != nil || p.LedgerVersion != 3 || p.TotalCount != 0 || len(p.Items) != 0 || !reflect.DeepEqual(before, associationTestSnapshot(t, s)) {
		t.Fatal("post-correction read changed tables", p, e)
	}
}
