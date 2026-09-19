package ledger

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/letrahoo/monee/server/internal/storage"
)

func refundExpense(t *testing.T, s *Store) Transaction {
	t.Helper()
	in := testInput()
	in.Amount = "100"
	r, err := s.Create(in, "refund-original-001")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func refundInput() RefundInput {
	return RefundInput{Version: 1, Date: "2026-09-19", Amount: "30", Currency: "CNY", Account: "合成退款账户", Note: "合成部分退款"}
}

func TestRefundTotalsRetryAndCrossMonth(t *testing.T) {
	s := testStore(t)
	original := refundExpense(t, s)
	in := refundInput()
	r, err := s.CreateRefund(original.ID, in, "refund-request-001")
	if err != nil {
		t.Fatal(err)
	}
	if r.Type != "refund" || r.RefundOf != original.ID || r.AmountMinor != "3000" || r.Category != original.Category {
		t.Fatal(r)
	}
	again, err := s.CreateRefund(original.ID, in, "refund-request-001")
	if err != nil || again != r {
		t.Fatal("retry", err, again)
	}
	d := dashboard(t, s)
	if d.GrossExpenseMinor != "10000" || d.RefundMinor != "3000" || d.ExpenseMinor != "7000" || d.IncomeMinor != "0" || d.TotalCount != 2 {
		t.Fatal(d)
	}
	if len(d.Categories) != 1 || d.Categories[0].AmountMinor != "7000" || d.Categories[0].RefundMinor != "3000" {
		t.Fatal(d.Categories)
	}
	linked, err := s.Refunds(r.ID)
	if err != nil || linked.Original.ID != original.ID || linked.Original.Version != 2 || linked.RemainingMinor != "7000" || len(linked.Refunds) != 1 || linked.Refunds[0] != r {
		t.Fatal(err, linked)
	}
	in.Version = 2
	in.Date = "2026-10-02"
	in.Amount = "70"
	if _, err = s.CreateRefund(original.ID, in, "refund-request-002"); err != nil {
		t.Fatal(err)
	}
	d, err = s.Dashboard("2026-10", "", 1)
	if err != nil || d.GrossExpenseMinor != "0" || d.RefundMinor != "7000" || d.ExpenseMinor != "-7000" || d.IncomeMinor != "0" || len(d.Categories) != 1 || d.Categories[0].AmountMinor != "-7000" {
		t.Fatal(err, d)
	}
	if september := dashboard(t, s); september.ExpenseMinor != "7000" {
		t.Fatal("cross-month rewrote past", september)
	}
	linked, err = s.Refunds(original.ID)
	if err != nil || linked.RemainingMinor != "0" || linked.RefundedMinor != "10000" || len(linked.Refunds) != 2 {
		t.Fatal(err, linked)
	}
	in.Version = 3
	in.Amount = "0.01"
	if _, err = s.CreateRefund(original.ID, in, "refund-request-003"); err == nil {
		t.Fatal("over-refunded")
	}
	var invalid int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM (SELECT transaction_id FROM postings GROUP BY transaction_id HAVING SUM(amount_minor)<>0 OR COUNT(*)<>2)").Scan(&invalid); err != nil || invalid != 0 {
		t.Fatal("postings", err, invalid)
	}
	if count(t, s, "transactions") != 3 || count(t, s, "postings") != 6 {
		t.Fatal("retry/rejection wrote rows")
	}
}

func TestRefundRejectsInvalidInputWithoutMutation(t *testing.T) {
	s := testStore(t)
	original := refundExpense(t, s)
	for name, mutate := range map[string]func(*RefundInput){
		"zero": func(i *RefundInput) { i.Amount = "0" }, "negative": func(i *RefundInput) { i.Amount = "-1" },
		"precision": func(i *RefundInput) { i.Amount = "1.001" }, "overflow": func(i *RefundInput) { i.Amount = "101" },
		"exponent": func(i *RefundInput) { i.Amount = "1e1" }, "currency": func(i *RefundInput) { i.Currency = "USD" },
		"before": func(i *RefundInput) { i.Date = "2026-09-13" }, "invalid date": func(i *RefundInput) { i.Date = "2026-02-30" },
		"missing version": func(i *RefundInput) { i.Version = 0 }, "stale": func(i *RefundInput) { i.Version = 2 },
		"account newline": func(i *RefundInput) { i.Account = "a\nb" }, "note nul": func(i *RefundInput) { i.Note = "a\x00b" },
	} {
		t.Run(name, func(t *testing.T) {
			in := refundInput()
			mutate(&in)
			if _, err := s.CreateRefund(original.ID, in, "refund-invalid-input"); err == nil {
				t.Fatal("accepted", in)
			}
		})
	}
	for _, key := range []string{"", "short", strings.Repeat("a", 129), "refund-request-\n001"} {
		if _, err := s.CreateRefund(original.ID, refundInput(), key); err == nil {
			t.Fatal("accepted key")
		}
	}
	if _, err := s.CreateRefund("missing", refundInput(), "refund-missing-001"); err == nil {
		t.Fatal("missing target")
	}
	income := testInput()
	income.Type = "income"
	i, err := s.Create(income, "refund-income-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateRefund(i.ID, refundInput(), "refund-income-target"); err == nil {
		t.Fatal("income target")
	}
	if _, err = s.Refunds(i.ID); err == nil {
		t.Fatal("income summary")
	}
	if _, err = s.db.Exec("UPDATE transactions SET deleted_at=? WHERE id=?", now(), original.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateRefund(original.ID, refundInput(), "refund-deleted-target"); err == nil {
		t.Fatal("deleted target")
	}
	if count(t, s, "transactions") != 2 || count(t, s, "idempotency") != 2 {
		t.Fatal("invalid request mutated ledger")
	}
}

func TestRefundIdempotencyConflictsAndConcurrency(t *testing.T) {
	s := testStore(t)
	original := refundExpense(t, s)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := refundInput()
			in.Amount = "60"
			_, err := s.CreateRefund(original.ID, in, fmt.Sprintf("refund-concurrent-%d", i))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("concurrent results", success)
	}
	summary, err := s.Refunds(original.ID)
	if err != nil || summary.RefundedMinor != "6000" || summary.RemainingMinor != "4000" {
		t.Fatal(err, summary)
	}
	in := refundInput()
	in.Version = 2
	in.Amount = "40"
	if _, err = s.CreateRefund(original.ID, in, "refund-idempotency-1"); err != nil {
		t.Fatal(err)
	}
	in.Amount = "39"
	if _, err = s.CreateRefund(original.ID, in, "refund-idempotency-1"); err == nil {
		t.Fatal("changed input reused key")
	}
	if _, err = s.Create(testInput(), "refund-idempotency-1"); err == nil {
		t.Fatal("cross-operation key reuse")
	}
	if _, err = s.CreateRefund(original.ID, refundInput(), "refund-original-001"); err == nil {
		t.Fatal("create key reused for refund")
	}
}

func TestRefundDependenciesCorrectionAnnotationAndUndo(t *testing.T) {
	s := testStore(t)
	p := preview(t, s, sampleCSV)
	if _, err := s.Commit(p.ID, p.LedgerVersion, false); err != nil {
		t.Fatal(err)
	}
	original := dashboard(t, s).Transactions[0]
	in := refundInput()
	in.Amount = "10"
	r, err := s.CreateRefund(original.ID, in, "refund-dependency-001")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []CorrectionInput{{Version: 2, Type: "income", AmountMinor: "2850", Reason: "synthetic"}, {Version: 2, Type: "expense", AmountMinor: "999", Reason: "synthetic"}} {
		if _, err = s.PreviewCorrection(original.ID, c); err == nil {
			t.Fatal("unsafe preview")
		}
		if _, err = s.Correct(original.ID, c); err == nil {
			t.Fatal("unsafe correction")
		}
	}
	if _, err = s.Annotate(original.ID, AnnotationInput{Version: 2, Category: "other"}); err == nil {
		t.Fatal("split original category")
	}
	if _, err = s.Annotate(r.ID, AnnotationInput{Version: 1, Category: "other"}); err == nil {
		t.Fatal("split refund category")
	}
	if _, err = s.Annotate(r.ID, AnnotationInput{Version: 1, Category: r.Category, Note: "updated"}); err != nil {
		t.Fatal("refund note", err)
	}
	if _, err = s.Correct(original.ID, CorrectionInput{Version: 2, Type: "expense", AmountMinor: "1000", Reason: "synthetic"}); err != nil {
		t.Fatal("safe correction", err)
	}
	u, err := s.PreviewImportUndo(p.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range u.Rows {
		if row.TransactionID == original.ID {
			found = true
			if row.Action != "preserve" || !strings.Contains(row.Reason, "退款") {
				t.Fatal(row)
			}
		}
	}
	if !found {
		t.Fatal("missing dependency")
	}
	if _, err = s.UndoImport(p.ID, u.LedgerVersion); err != nil {
		t.Fatal(err)
	}
	if summary, err := s.Refunds(original.ID); err != nil || summary.RemainingMinor != "0" {
		t.Fatal("undo broke link", err, summary)
	}
}

func TestRefundPersistenceAndFreshSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	original := refundExpense(t, s)
	in := refundInput()
	r, err := s.CreateRefund(original.ID, in, "refund-persistence-001")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	again, err := s.CreateRefund(original.ID, in, "refund-persistence-001")
	if err != nil || again != r {
		t.Fatal("persistent retry", err)
	}
	if summary, err := s.Refunds(original.ID); err != nil || summary.RemainingMinor != "7000" {
		t.Fatal(err, summary)
	}
	bundle := filepath.Join(t.TempDir(), "backup")
	if err := storage.Snapshot(path, bundle); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored.db")
	if err := storage.Restore(bundle, restored); err != nil {
		t.Fatal(err)
	}
	restoredStore, err := Open(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredStore.Close()
	if summary, err := restoredStore.Refunds(original.ID); err != nil || len(summary.Refunds) != 1 || summary.Refunds[0] != r || summary.RemainingMinor != "7000" {
		t.Fatal("snapshot lost refund", err, summary)
	}
}

func TestRefundSeparateConnectionsCannotOverRefund(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	original := refundExpense(t, first)
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	start := make(chan struct{})
	results := make(chan error, 2)
	for i, s := range []*Store{first, second} {
		go func(i int, s *Store) {
			<-start
			in := refundInput()
			in.Amount = "60"
			_, err := s.CreateRefund(original.ID, in, fmt.Sprintf("refund-connection-%d", i))
			results <- err
		}(i, s)
	}
	close(start)
	success := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("independent stores", success)
	}
	if summary, err := first.Refunds(original.ID); err != nil || summary.RefundedMinor != "6000" {
		t.Fatal("over-refund", err, summary)
	}
}

func TestRefundLateWriteFailureRollsBackEntireOperation(t *testing.T) {
	// Faults live only in a temporary test database, never a production entry point.
	for name, trigger := range map[string]string{
		"original version": "BEFORE UPDATE OF version ON transactions WHEN OLD.kind='expense'",
		"link audit":       "BEFORE INSERT ON change_log WHEN NEW.operation='refund_linked'",
		"replay response":  "BEFORE INSERT ON idempotency",
	} {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			original := refundExpense(t, s)
			before := dashboard(t, s)
			counts := map[string]int{}
			for _, table := range []string{"transactions", "postings", "accounts", "change_log", "idempotency"} {
				counts[table] = count(t, s, table)
			}
			if _, err := s.db.Exec("CREATE TRIGGER refund_test_failure " + trigger + " BEGIN SELECT RAISE(ABORT,'synthetic late failure'); END"); err != nil {
				t.Fatal(err)
			}
			in, key := refundInput(), "refund-atomic-retry-001"
			if _, err := s.CreateRefund(original.ID, in, key); err == nil || !strings.Contains(err.Error(), "synthetic late failure") {
				t.Fatal("expected injected failure", err)
			}
			for table, want := range counts {
				if got := count(t, s, table); got != want {
					t.Fatalf("%s leaked rows: got %d want %d", table, got, want)
				}
			}
			if after := dashboard(t, s); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed refund changed ledger: before=%+v after=%+v", before, after)
			}
			summary, err := s.Refunds(original.ID)
			if err != nil || summary.Original != original || len(summary.Refunds) != 0 || summary.RemainingMinor != "10000" {
				t.Fatal("failed refund changed original", err, summary)
			}
			if _, err = s.db.Exec("DROP TRIGGER refund_test_failure"); err != nil {
				t.Fatal(err)
			}
			// The original key/version must remain usable after rollback.
			refund, err := s.CreateRefund(original.ID, in, key)
			if err != nil {
				t.Fatal("retry after rollback", err)
			}
			if replay, err := s.CreateRefund(original.ID, in, key); err != nil || replay != refund {
				t.Fatal("retry duplicated successful recovery", err, replay)
			}
			if after := dashboard(t, s); after.Version != before.Version+1 || after.RefundMinor != "3000" || after.TotalCount != before.TotalCount+1 {
				t.Fatal("recovery not exactly once", after)
			}
		})
	}
}

func TestRefundSameKeyConcurrentRetryWritesOnce(t *testing.T) {
	s := testStore(t)
	original := refundExpense(t, s)
	before := dashboard(t, s)
	const attempts = 8
	start := make(chan struct{})
	type result struct {
		refund Transaction
		err    error
	}
	results := make(chan result, attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			<-start
			r, err := s.CreateRefund(original.ID, refundInput(), "refund-same-key-001")
			results <- result{r, err}
		}()
	}
	close(start)
	var first Transaction
	for i := 0; i < attempts; i++ {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if i == 0 {
			first = r.refund
		} else if r.refund != first {
			t.Fatal("same request returned different refunds")
		}
	}
	summary, err := s.Refunds(original.ID)
	if err != nil || summary.Original.Version != original.Version+1 || len(summary.Refunds) != 1 || summary.RefundedMinor != "3000" {
		t.Fatal("duplicate link or original version", err, summary)
	}
	if after := dashboard(t, s); after.Version != before.Version+1 || after.TotalCount != before.TotalCount+1 || after.ExpenseMinor != "7000" {
		t.Fatal("duplicate accounting effect", after)
	}
	if count(t, s, "postings") != 4 || count(t, s, "idempotency") != 2 {
		t.Fatal("duplicate postings or replay record")
	}
}
