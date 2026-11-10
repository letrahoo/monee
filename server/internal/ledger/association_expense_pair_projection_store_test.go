package ledger

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

// Only a test adapter. Both inputs are deliberately obtained in one read-only
// transaction here; the pure projection function itself cannot prove this fact.
func expensePairTempInputs(t *testing.T, s *Store, aID, bID string) (associationExpensePairInput, associationExpensePairInput, int64) {
	t.Helper()
	tx, e := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	var version int64
	if e = tx.QueryRow("SELECT version FROM ledgers WHERE id=?", s.ledgerID).Scan(&version); e != nil {
		t.Fatal(e)
	}
	read := func(id string) associationExpensePairInput {
		var p associationExpensePairInput
		p.Accounting = accountingFactsFromTempTx(t, tx, id)
		if e = tx.QueryRow("SELECT occurred_on,merchant FROM transactions WHERE id=?", id).Scan(&p.Date, &p.Merchant); e != nil {
			t.Fatal(e)
		}
		return p
	}
	return read(aID), read(bID), version
}
func createPairTempExpense(t *testing.T, s *Store, account, category, key string) Transaction {
	t.Helper()
	in := testInput()
	in.Account = account
	in.Category = category
	in.ExternalID = key
	r, e := s.Create(in, "pair-projection-"+key)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestAssociationExpensePairStoreGrossIsHypotheticalAndReadOnly(t *testing.T) {
	s := testStore(t)
	aTx := createPairTempExpense(t, s, "合成共同账户", "合成分类甲", "first")
	bTx := createPairTempExpense(t, s, "合成共同账户", "合成分类乙", "second")
	a, b, version := expensePairTempInputs(t, s, aTx.ID, bTx.ID)
	before := accountingFactTables(t, s)
	d := dashboard(t, s)
	p, e := planAssociationExpensePairProjection(s.ledgerID, version, a, b, bTx.ID)
	if e != nil {
		t.Fatal(e)
	}
	expectedA, errA := inspectAssociationExpenseAccountingFacts(s.ledgerID, a.Accounting)
	expectedB, errB := inspectAssociationExpenseAccountingFacts(s.ledgerID, b.Accounting)
	if errA != nil || errB != nil {
		t.Fatal(errA, errB)
	}
	if p.Primary != expectedB || p.Secondary != expectedA || p.Date != a.Date || p.Merchant != a.Merchant {
		t.Fatal("Explicit primary must retain that member's complete facts and classification references", p)
	}
	if p.Primary.TransactionID != bTx.ID || p.Secondary.TransactionID != aTx.ID || p.Primary.Version != bTx.Version || p.Secondary.Version != aTx.Version || p.LedgerVersion != version || p.PairGrossBeforeMinor != 2468 || p.PairGrossAfterMinor != 1234 || p.PairGrossDeltaMinor != -1234 || p.Primary.FundAccountID != p.Secondary.FundAccountID || p.Primary.ExpenseAccountID == p.Secondary.ExpenseAccountID {
		t.Fatal(p)
	}
	swapped, e := planAssociationExpensePairProjection(s.ledgerID, version, b, a, bTx.ID)
	if e != nil || swapped != p {
		t.Fatal("Explicit primary must determine the same projection, not input order", swapped, e)
	}
	if !reflect.DeepEqual(before, accountingFactTables(t, s)) || !reflect.DeepEqual(d, dashboard(t, s)) || d.ExpenseMinor != "2468" || d.TotalCount != 2 {
		t.Fatal("Hypothetical arithmetic changed or was confused with the actual ledger")
	}
	other := createPairTempExpense(t, s, "合成其他账户", "合成分类乙", "different-fund")
	otherInput, freshA, v := expensePairTempInputs(t, s, other.ID, aTx.ID)
	before = accountingFactTables(t, s)
	out, e := planAssociationExpensePairProjection(s.ledgerID, v, freshA, otherInput, aTx.ID)
	if e == nil || out != (associationExpensePairProjection{}) {
		t.Fatal("Different actual fund-account IDs must be rejected", out, e)
	}
	if !reflect.DeepEqual(before, accountingFactTables(t, s)) {
		t.Fatal("Rejected different-account inputs changed any table")
	}
}
func TestAssociationExpensePairStoreDoesNotClaimEligibilityOrFreshness(t *testing.T) {
	t.Run("refund child and deleted member", func(t *testing.T) {
		s := testStore(t)
		aTx := createPairTempExpense(t, s, "合成共同账户", "合成分类甲", "refund-base")
		bTx := createPairTempExpense(t, s, "合成共同账户", "合成分类乙", "deleted-member")
		if _, e := s.CreateRefund(aTx.ID, RefundInput{Version: 1, Date: "2026-09-15", Amount: "1.00", Currency: "CNY", Account: "合成共同账户"}, "pair-projection-refund"); e != nil {
			t.Fatal(e)
		}
		if _, e := s.db.Exec("UPDATE transactions SET deleted_at=? WHERE id=?", now(), bTx.ID); e != nil {
			t.Fatal(e)
		}
		a, b, v := expensePairTempInputs(t, s, aTx.ID, bTx.ID)
		before := accountingFactTables(t, s)
		d := dashboard(t, s)
		p, e := planAssociationExpensePairProjection(s.ledgerID, v, a, b, aTx.ID)
		if e != nil || p.PairGrossBeforeMinor != 2468 || p.PairGrossAfterMinor != 1234 || p.Primary.Version <= 1 {
			t.Fatal("Supplied coherent inputs are arithmetic, not active/refund eligibility", p, e)
		}
		if !reflect.DeepEqual(before, accountingFactTables(t, s)) || !reflect.DeepEqual(d, dashboard(t, s)) {
			t.Fatal("Projection inspected or changed current eligibility as if this were a financial command")
		}
	})
	t.Run("old coherent inputs after correction", func(t *testing.T) {
		s := testStore(t)
		aTx := createPairTempExpense(t, s, "合成共同账户", "合成分类甲", "old-a")
		bTx := createPairTempExpense(t, s, "合成共同账户", "合成分类乙", "old-b")
		oldA, oldB, oldVersion := expensePairTempInputs(t, s, aTx.ID, bTx.ID)
		if _, e := s.Correct(aTx.ID, CorrectionInput{Version: aTx.Version, Type: "expense", AmountMinor: "1250", Reason: "合成当前金额更正"}); e != nil {
			t.Fatal(e)
		}
		currentA, currentB, newVersion := expensePairTempInputs(t, s, aTx.ID, bTx.ID)
		before := accountingFactTables(t, s)
		p, e := planAssociationExpensePairProjection(s.ledgerID, oldVersion, oldA, oldB, aTx.ID)
		if e != nil || p.PairGrossAfterMinor != 1234 || p.LedgerVersion != oldVersion || newVersion <= oldVersion {
			t.Fatal("Coherent supplied snapshot does not refresh itself", p, e)
		}
		out, e := planAssociationExpensePairProjection(s.ledgerID, newVersion, currentA, currentB, aTx.ID)
		if e == nil || out != (associationExpensePairProjection{}) {
			t.Fatal("Current different amounts must not be silently reconciled", out, e)
		}
		if !reflect.DeepEqual(before, accountingFactTables(t, s)) {
			t.Fatal("Old/current hypothetical computation mutated database")
		}
	})
}
