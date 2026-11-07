package ledger

import "testing"

func TestExpensePairAPIReplayRequiresCurrentCapabilityAndMembership(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	foundationExec(t, root, "PRAGMA user_version=6")
	const confirmKey = "api-replay-confirm-synthetic"
	receipt, err := s.ConfirmExpensePair(in, confirmKey)
	if err != nil {
		t.Fatal(err)
	}
	foundationExec(t, root, "PRAGMA user_version=5")
	_, err = s.ConfirmExpensePair(in, confirmKey)
	pairAPIProblem(t, err, "expense_pair_not_enabled")
	foundationExec(t, root, "PRAGMA user_version=6")
	withdraw := ExpensePairWithdrawInput{LedgerVersion: receipt.LedgerVersion, DecisionID: receipt.ID, DecisionVersion: receipt.Version, Reason: "合成撤回重放"}
	const withdrawKey = "api-replay-withdraw-synthetic"
	if _, err = s.WithdrawExpensePair(withdraw, withdrawKey); err != nil {
		t.Fatal(err)
	}
	foundationExec(t, root, "PRAGMA user_version=5")
	_, err = s.WithdrawExpensePair(withdraw, withdrawKey)
	pairAPIProblem(t, err, "expense_pair_not_enabled")
	foundationExec(t, root, "PRAGMA user_version=6")
	foundationExec(t, root, "DELETE FROM ledger_members WHERE user_id=?", s.actorID)
	_, err = s.ConfirmExpensePair(in, confirmKey)
	pairAPIProblem(t, err, "ledger_denied")
	_, err = s.WithdrawExpensePair(withdraw, withdrawKey)
	pairAPIProblem(t, err, "ledger_denied")
}

func TestExpensePairAPIDetailDoesNotRevealOtherLedgerDecision(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	otherInfo, err := root.CreateLedger(s.actorID, "合成另一账本")
	if err != nil {
		t.Fatal(err)
	}
	other, err := root.Scoped(s.actorID, otherInfo.ID)
	if err != nil {
		t.Fatal(err)
	}
	lifecycleRefresh(t, root, &in)
	foundationExec(t, root, "PRAGMA user_version=6")
	if _, err = s.ConfirmExpensePair(in, "api-cross-ledger-confirm-key"); err != nil {
		t.Fatal(err)
	}
	_, err = other.ExpensePairs(in.PrimaryTransactionID)
	pairAPIProblem(t, err, "not_found")
}
