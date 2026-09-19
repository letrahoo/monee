package ledger

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/storage"
)

func assertTransferPostings(t *testing.T, s *Store, tr Transaction) {
	t.Helper()
	rows, err := s.db.Query(`SELECT a.name,a.kind,p.amount_minor FROM postings p JOIN accounts a ON a.id=p.account_id WHERE p.transaction_id=? ORDER BY p.amount_minor`, tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var legs []transferLeg
	for rows.Next() {
		var p transferLeg
		var kind string
		if err = rows.Scan(&p.Account, &kind, &p.AmountMinor); err != nil {
			t.Fatal(err)
		}
		if kind != "clearing" {
			t.Fatal("transfer posted to a category", kind)
		}
		legs = append(legs, p)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(legs) != 2 || legs[0].Account != tr.Account || legs[1].Account != tr.ToAccount || legs[0].AmountMinor+legs[1].AmountMinor != 0 || legs[0].AmountMinor >= 0 {
		t.Fatal(legs)
	}
}

func TestTransferStoredWithoutIncomeExpenseOrRefund(t *testing.T) {
	s := testStore(t)
	original := refundExpense(t, s)
	if _, err := s.CreateRefund(original.ID, refundInput(), "transfer-original-refund"); err != nil {
		t.Fatal(err)
	}
	before := dashboard(t, s)
	in := syntheticTransfer()
	tr, err := s.CreateTransfer(in, "transfer-create-001")
	if err != nil || tr.Type != "transfer" || tr.AmountMinor != "10001" || tr.Account != in.FromAccount || tr.ToAccount != in.ToAccount {
		t.Fatal(tr, err)
	}
	assertTransferPostings(t, s, tr)
	if replay, e := s.CreateTransfer(in, "transfer-create-001"); e != nil || replay != tr {
		t.Fatal(replay, e)
	}
	after := dashboard(t, s)
	if after.IncomeMinor != before.IncomeMinor || after.ExpenseMinor != before.ExpenseMinor || after.GrossExpenseMinor != before.GrossExpenseMinor || after.RefundMinor != before.RefundMinor || !reflect.DeepEqual(after.Categories, before.Categories) || after.Version != before.Version+1 || after.TotalCount != before.TotalCount+1 {
		t.Fatal(after)
	}
	for _, account := range []string{in.FromAccount, in.ToAccount} {
		d, e := s.Dashboard("2026-09", account, 1)
		if e != nil || d.FilteredCount != 1 || d.Transactions[0] != tr {
			t.Fatal(d, e)
		}
	}
	in.Amount = "101"
	if _, err = s.CreateTransfer(in, "transfer-create-001"); err == nil {
		t.Fatal("accepted changed retry")
	}
	if _, err = s.Create(testInput(), "transfer-create-001"); err == nil {
		t.Fatal("accepted cross-operation key")
	}
	in.Date = "2026-10-01"
	if _, err = s.CreateTransfer(in, "transfer-october-001"); err != nil {
		t.Fatal(err)
	}
	d, err := s.Dashboard("2026-10", "", 1)
	if err != nil || d.TotalCount != 1 || d.IncomeMinor != "0" || d.ExpenseMinor != "0" || d.RefundMinor != "0" || len(d.Categories) != 0 {
		t.Fatal(d, err)
	}
}

func TestTransferEditingPreservesFundingStructure(t *testing.T) {
	s := testStore(t)
	tr, err := s.CreateTransfer(syntheticTransfer(), "transfer-edit-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Correct(tr.ID, CorrectionInput{Version: 1, Type: "income", AmountMinor: "10001", Reason: "synthetic"}); err == nil {
		t.Fatal("converted transfer to income")
	}
	if _, err = s.Annotate(tr.ID, AnnotationInput{Version: 1, Category: "收入", Note: "synthetic"}); err == nil {
		t.Fatal("reclassified transfer")
	}
	if _, err = s.CreateRefund(tr.ID, refundInput(), "transfer-refund-001"); err == nil {
		t.Fatal("refunded transfer")
	}
	after, err := s.Annotate(tr.ID, AnnotationInput{Version: 1, Category: tr.Category, Note: "更正合成备注"})
	if err != nil || after.Note != "更正合成备注" || after.Version != 2 || after.ToAccount != tr.ToAccount {
		t.Fatal(after, err)
	}
	assertTransferPostings(t, s, after)
	if count(t, s, "accounts") != 2 {
		t.Fatal("created category account")
	}
	if _, err = s.Annotate(tr.ID, AnnotationInput{Version: 1, Category: tr.Category, Note: "stale"}); err == nil {
		t.Fatal("accepted stale note")
	}
}

func TestTransferAtomicRollbackAndRetry(t *testing.T) {
	for name, trigger := range map[string]string{
		"second posting": "BEFORE INSERT ON postings WHEN NEW.amount_minor>0",
		"audit":          "BEFORE INSERT ON change_log WHEN NEW.entity_type='transaction'",
		"ledger version": "BEFORE UPDATE OF version ON ledgers",
		"replay":         "BEFORE INSERT ON idempotency",
	} {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			before := dashboard(t, s)
			if _, err := s.db.Exec("CREATE TRIGGER transfer_fault " + trigger + " BEGIN SELECT RAISE(ABORT,'synthetic fault'); END"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateTransfer(syntheticTransfer(), "transfer-atomic-001"); err == nil || !strings.Contains(err.Error(), "synthetic fault") {
				t.Fatal(err)
			}
			for _, table := range []string{"transactions", "postings", "accounts", "change_log", "idempotency"} {
				if count(t, s, table) != 0 {
					t.Fatal("partial write", table)
				}
			}
			if !reflect.DeepEqual(before, dashboard(t, s)) {
				t.Fatal("failed operation changed dashboard")
			}
			if _, err := s.db.Exec("DROP TRIGGER transfer_fault"); err != nil {
				t.Fatal(err)
			}
			tr, err := s.CreateTransfer(syntheticTransfer(), "transfer-atomic-001")
			if err != nil {
				t.Fatal(err)
			}
			if replay, e := s.CreateTransfer(syntheticTransfer(), "transfer-atomic-001"); e != nil || replay != tr {
				t.Fatal(replay, e)
			}
		})
	}
}

func TestTransferConcurrentReplayAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	type result struct {
		tr  Transaction
		err error
	}
	results := make(chan result, 8)
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		store := s
		if i%2 == 1 {
			store = other
		}
		go func(s *Store) {
			<-start
			tr, e := s.CreateTransfer(syntheticTransfer(), "transfer-concurrent-001")
			results <- result{tr, e}
		}(store)
	}
	close(start)
	var first Transaction
	for i := 0; i < 8; i++ {
		r := <-results
		if r.err == nil {
			if first.ID != "" && first != r.tr {
				t.Fatal("distinct replay")
			}
			first = r.tr
		}
	}
	if first.ID == "" || count(t, s, "transactions") != 1 || count(t, s, "postings") != 2 || dashboard(t, s).Version != 1 {
		t.Fatal("not exactly once")
	}
	// SQLite may reject a simultaneous read-to-write promotion. After settling,
	// either connection must replay the single committed result with the same key.
	if tr, e := other.CreateTransfer(syntheticTransfer(), "transfer-concurrent-001"); e != nil || tr != first {
		t.Fatal(tr, e)
	}
	other.Close()
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if tr, e := s.CreateTransfer(syntheticTransfer(), "transfer-concurrent-001"); e != nil || tr != first {
		t.Fatal(tr, e)
	}
	bundle := filepath.Join(t.TempDir(), "backup")
	if err = storage.Snapshot(path, bundle); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored.db")
	if err = storage.Restore(bundle, restored); err != nil {
		t.Fatal(err)
	}
	r, err := Open(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if tr, e := r.CreateTransfer(syntheticTransfer(), "transfer-concurrent-001"); e != nil || tr != first {
		t.Fatal(tr, e)
	}
	assertTransferPostings(t, r, first)
}
