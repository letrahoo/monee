package ledger

import (
	"encoding/base64"
	"reflect"
	"testing"
)

// This is an isolated schema4 integration seam, not an association write test.
func TestSameAccountDecisionBaselineTransferRefreshAndExpenseProjection(t *testing.T) {
	s := testStore(t)
	var schema int
	if e := s.db.QueryRow("PRAGMA user_version").Scan(&schema); e != nil || schema != 4 {
		t.Fatal(schema, e)
	}
	raw := []byte(nativeHeader + nativeExpense)
	p := alipayPreview(t, s, raw, "synthetic-wallet")
	if len(p.Errors) > 0 || p.NewCount != 1 || p.SourceDocument == nil {
		t.Fatal(p)
	}
	originalDocument := encode(p.SourceDocument)
	_, originalRaw := nativeEvidenceSaved(t, s, p.ID)
	tr, e := s.CreateTransfer(syntheticTransfer(), "association-baseline-transfer")
	if e != nil {
		t.Fatal(e)
	}
	refreshed, e := s.PreviewAlipay("repeat-synthetic.csv", base64.StdEncoding.EncodeToString(raw), "synthetic-wallet")
	if e != nil || refreshed.ID != p.ID || refreshed.LedgerVersion <= p.LedgerVersion || encode(refreshed.SourceDocument) != originalDocument {
		t.Fatal("Schema4 transfer must invalidate dynamic version without reinterpreting source", refreshed, e)
	}
	_, savedRaw := nativeEvidenceSaved(t, s, p.ID)
	if savedRaw != originalRaw {
		t.Fatal("Native bytes changed after transfer-induced preview refresh")
	}
	receipt, e := s.Commit(refreshed.ID, refreshed.LedgerVersion, false)
	if e != nil || receipt.Added != 1 {
		t.Fatal(receipt, e)
	}
	detail, e := s.ImportDetail(p.ID)
	if e != nil || len(detail.Evidence) != 1 {
		t.Fatal(detail, e)
	}
	expenseID := detail.Evidence[0].TransactionID
	beforeReplay := nativeEvidenceTables(t, s)
	replay, e := s.PreviewAlipay("committed-repeat.csv", base64.StdEncoding.EncodeToString(raw), "synthetic-wallet")
	if e != nil || replay.ID != p.ID || !replay.AlreadyCommitted || encode(replay.SourceDocument) != originalDocument {
		t.Fatal("Committed native snapshot incompatible with schema4 Transaction", replay, e)
	}
	if !reflect.DeepEqual(beforeReplay, nativeEvidenceTables(t, s)) {
		t.Fatal("Committed replay changed any schema4 application table")
	}
	transferFacts, e := inspectAssociationExpenseAccountingFacts(s.ledgerID, accountingFactsFromTempDB(t, s, tr.ID))
	if e == nil || transferFacts != (associationExpenseAccountingFacts{}) {
		t.Fatal("A schema4 transfer must not enter the expense facts domain", transferFacts, e)
	}
	var legs int
	var balance int64
	var fromAmount, toAmount int64
	if e = s.db.QueryRow("SELECT p.amount_minor FROM postings p JOIN accounts a ON a.id=p.account_id WHERE p.transaction_id=? AND a.name=? AND a.ledger_id=? AND a.kind='clearing' AND p.currency='CNY'", tr.ID, syntheticTransfer().FromAccount, s.ledgerID).Scan(&fromAmount); e != nil || fromAmount != -10001 {
		t.Fatal("Transfer debit changed", fromAmount, e)
	}
	if e = s.db.QueryRow("SELECT p.amount_minor FROM postings p JOIN accounts a ON a.id=p.account_id WHERE p.transaction_id=? AND a.name=? AND a.ledger_id=? AND a.kind='clearing' AND p.currency='CNY'", tr.ID, syntheticTransfer().ToAccount, s.ledgerID).Scan(&toAmount); e != nil || toAmount != 10001 {
		t.Fatal("Transfer credit changed", toAmount, e)
	}
	if e = s.db.QueryRow("SELECT COUNT(*),SUM(p.amount_minor) FROM postings p JOIN accounts a ON a.id=p.account_id WHERE p.transaction_id=? AND a.kind='clearing'", tr.ID).Scan(&legs, &balance); e != nil || legs != 2 || balance != 0 {
		t.Fatal("Transfer funding legs changed", legs, balance, e)
	}
	rec := refreshed.Rows[0].Record
	in := testInput()
	in.Date = rec.Date
	in.Merchant = rec.Merchant
	in.Category = rec.Category
	in.Account = rec.Account
	in.ExternalID = "synthetic-second-observation"
	second, e := s.Create(in, "association-baseline-second")
	if e != nil {
		t.Fatal(e)
	}
	a, b, v := expensePairTempInputs(t, s, expenseID, second.ID)
	beforeProjection := nativeEvidenceTables(t, s)
	d := dashboard(t, s)
	projected, e := planAssociationExpensePairProjection(s.ledgerID, v, a, b, second.ID)
	if e != nil || projected.PairGrossBeforeMinor != 2468 || projected.PairGrossAfterMinor != 1234 || projected.Primary.TransactionID != second.ID {
		t.Fatal(projected, e)
	}
	if !reflect.DeepEqual(beforeProjection, nativeEvidenceTables(t, s)) || !reflect.DeepEqual(d, dashboard(t, s)) || d.ExpenseMinor != "2468" || d.IncomeMinor != "0" || d.TotalCount != 3 {
		t.Fatal("Pure projection modified real expenses or counted transfer as income/expense", d)
	}
}
