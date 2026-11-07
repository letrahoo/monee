package ledger

import (
	"errors"
	"reflect"
	"testing"
)

func pairAPIPreviewInput(in expensePairConfirmInput) ExpensePairPreviewInput {
	return ExpensePairPreviewInput{LedgerVersion: in.LedgerVersion, PrimaryTransactionID: in.PrimaryTransactionID, SecondaryTransactionID: in.SecondaryTransactionID, PrimaryVersion: in.PrimaryVersion, SecondaryVersion: in.SecondaryVersion}
}
func pairAPIProblem(t *testing.T, err error, code string) {
	t.Helper()
	var p *Problem
	if !errors.As(err, &p) || p.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestExpensePairAPICapabilityAndLifecycle(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	before := nativeEvidenceTables(t, root)
	_, err := s.PreviewExpensePair(pairAPIPreviewInput(in))
	pairAPIProblem(t, err, "expense_pair_not_enabled")
	_, err = s.ConfirmExpensePair(in, "api-confirm-key-synthetic")
	pairAPIProblem(t, err, "expense_pair_not_enabled")
	_, err = s.WithdrawExpensePair(ExpensePairWithdrawInput{}, "api-withdraw-key-synthetic")
	pairAPIProblem(t, err, "expense_pair_not_enabled")
	_, err = s.ExpensePairs(in.PrimaryTransactionID)
	pairAPIProblem(t, err, "expense_pair_not_enabled")
	if !reflect.DeepEqual(before, nativeEvidenceTables(t, root)) {
		t.Fatal("disabled endpoints wrote")
	}
	// Isolated open connection only: no normal Open/schema6 activation exists.
	foundationExec(t, root, "PRAGMA user_version=6")
	before = nativeEvidenceTables(t, root)
	preview, err := s.PreviewExpensePair(pairAPIPreviewInput(in))
	if err != nil {
		t.Fatal(err)
	}
	if preview.PrimaryID != in.PrimaryTransactionID || preview.SecondaryID != in.SecondaryTransactionID || preview.PairGrossBeforeMinor != "2468" || preview.PairGrossAfterMinor != "1234" || preview.PairGrossDeltaMinor != "-1234" {
		t.Fatal(preview)
	}
	if !reflect.DeepEqual(before, nativeEvidenceTables(t, root)) {
		t.Fatal("preview wrote")
	}
	r, err := s.ConfirmExpensePair(in, "api-confirm-key-synthetic")
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.ConfirmExpensePair(in, "api-confirm-key-synthetic")
	if err != nil || same != r {
		t.Fatal(same, err)
	}
	for _, id := range []string{in.PrimaryTransactionID, in.SecondaryTransactionID} {
		detail, err := s.ExpensePairs(id)
		if err != nil || len(detail.Decisions) != 1 || detail.Decisions[0] != r || detail.LedgerVersion != r.LedgerVersion {
			t.Fatal(detail, err)
		}
	}
	if d := dashboard(t, s); d.TotalCount != 1 || d.ExpenseMinor != "1234" {
		t.Fatal(d)
	}
	_, err = s.PreviewExpensePair(pairAPIPreviewInput(in))
	pairAPIProblem(t, err, "conflict")
	withdrawn, err := s.WithdrawExpensePair(ExpensePairWithdrawInput{LedgerVersion: r.LedgerVersion, DecisionID: r.ID, DecisionVersion: r.Version, Reason: "合成API撤回"}, "api-withdraw-key-synthetic")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := s.ExpensePairs(in.PrimaryTransactionID)
	if err != nil || len(detail.Decisions) != 1 || detail.Decisions[0] != withdrawn || withdrawn.Status != "retracted" {
		t.Fatal(detail, err)
	}
	if d := dashboard(t, s); d.TotalCount != 2 || d.ExpenseMinor != "2468" {
		t.Fatal(d)
	}
}

func TestExpensePairAPIAuthorizationAndCorruption(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	viewer := lifecycleMember(t, root, "viewer")
	lifecycleRefresh(t, root, &in)
	foundationExec(t, root, "PRAGMA user_version=6")
	_, err := viewer.PreviewExpensePair(pairAPIPreviewInput(in))
	pairAPIProblem(t, err, "ledger_readonly")
	_, err = viewer.ConfirmExpensePair(in, "api-denied-confirm-key")
	pairAPIProblem(t, err, "ledger_readonly")
	r, err := s.ConfirmExpensePair(in, "api-owner-confirm-key")
	if err != nil {
		t.Fatal(err)
	}
	d, err := viewer.ExpensePairs(in.PrimaryTransactionID)
	if err != nil || len(d.Decisions) != 1 {
		t.Fatal(d, err)
	}
	_, err = viewer.WithdrawExpensePair(ExpensePairWithdrawInput{LedgerVersion: r.LedgerVersion, DecisionID: r.ID, DecisionVersion: 1, Reason: "合成"}, "api-denied-withdraw-key")
	pairAPIProblem(t, err, "ledger_readonly")
	foundationExec(t, root, "UPDATE expense_pair_members SET active=0 WHERE decision_id=?", r.ID)
	d, err = s.ExpensePairs(in.PrimaryTransactionID)
	pairAPIProblem(t, err, "expense_pair_unavailable")
	if !reflect.DeepEqual(d, ExpensePairDetail{}) {
		t.Fatal("partial damaged response", d)
	}
	foundationExec(t, root, "DELETE FROM ledger_members WHERE user_id=?", s.actorID)
	_, err = s.ExpensePairs(in.PrimaryTransactionID)
	pairAPIProblem(t, err, "ledger_denied")
}
