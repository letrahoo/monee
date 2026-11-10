package ledger

import "testing"

// This is an opportunistic concurrency smoke test plus settled visibility checks.
// The start signal does not force SQL-level overlap; it is not a deterministic
// proof of isolation across Dashboard's queries.
func TestExpensePairEffectiveIndependentReaderConcurrentSmoke(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	var path string
	if err := root.db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	other, err := Open(path) // Before confirmation: retain the normal startup fence.
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	reader, err := other.Scoped(s.actorID, s.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		d   Dashboard
		err error
	}
	check := func(r result, active bool, version int64) {
		t.Helper()
		count, amount := 2, "2468"
		if active {
			count, amount = 1, "1234"
		}
		if r.err != nil || r.d.Version != version || r.d.TotalCount != count || r.d.FilteredCount != count || len(r.d.Transactions) != count || r.d.ExpenseMinor != amount || r.d.GrossExpenseMinor != amount || len(r.d.Categories) != 1 || r.d.Categories[0].AmountMinor != amount {
			t.Fatal("mixed snapshot", r.d, r.err)
		}
		if active && r.d.Transactions[0].ID != in.PrimaryTransactionID {
			t.Fatal(r.d)
		}
	}
	// A read racing with confirmation must see entirely before or entirely after.
	start := make(chan struct{})
	done := make(chan result, 1)
	go func() { <-start; d, e := reader.Dashboard("2026-09", "", 1); done <- result{d, e} }()
	close(start)
	receipt, err := s.confirmExpensePair(in, "effective-independent-confirm")
	first := <-done
	if err != nil {
		t.Fatal(err)
	}
	if first.d.Version == in.LedgerVersion {
		check(first, false, in.LedgerVersion)
	} else {
		check(first, true, receipt.LedgerVersion)
	}
	d, err := reader.Dashboard("2026-09", "", 1)
	check(result{d, err}, true, receipt.LedgerVersion)
	start = make(chan struct{})
	done = make(chan result, 1)
	go func() { <-start; d, e := reader.Dashboard("2026-09", "", 1); done <- result{d, e} }()
	close(start)
	withdrawn, err := s.withdrawExpensePair(expensePairWithdrawInput{LedgerVersion: receipt.LedgerVersion, DecisionID: receipt.ID, DecisionVersion: receipt.Version, Reason: "synthetic concurrent withdrawal"}, "effective-independent-withdraw")
	last := <-done
	if err != nil {
		t.Fatal(err)
	}
	if last.d.Version == receipt.LedgerVersion {
		check(last, true, receipt.LedgerVersion)
	} else {
		check(last, false, withdrawn.LedgerVersion)
	}
	d, err = reader.Dashboard("2026-09", "", 1)
	check(result{d, err}, false, withdrawn.LedgerVersion)
}
