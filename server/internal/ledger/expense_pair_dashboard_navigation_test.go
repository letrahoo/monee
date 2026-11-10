package ledger

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExpensePairDashboardNavigationTracksVisiblePrimary(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	viewer := lifecycleMember(t, root, "viewer")
	lifecycleRefresh(t, root, &in)
	receipt, err := s.confirmExpensePair(in, "navigation-confirm-synthetic")
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []*Store{s, viewer} {
		d := dashboard(t, client)
		if len(d.Transactions) != 1 || d.Transactions[0].ID != receipt.PrimaryID || len(d.ExpensePairs) != 1 || d.ExpensePairs[0] != receipt {
			t.Fatal(d)
		}
		// Navigation belongs only to the visible result page, not the whole ledger.
		empty, err := client.Dashboard(d.Month, "no-synthetic-match", 1)
		if err != nil || len(empty.Transactions) != 0 || len(empty.ExpensePairs) != 0 {
			t.Fatal(empty, err)
		}
		empty, err = client.Dashboard(d.Month, "", 2)
		if err != nil || len(empty.Transactions) != 0 || len(empty.ExpensePairs) != 0 {
			t.Fatal(empty, err)
		}
		bytes, err := json.Marshal(d.Transactions[0])
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(bytes), "expensePair") {
			t.Fatal("derived navigation leaked into persisted transaction contract")
		}
	}
	effectiveWithdraw(t, s, receipt)
	restored := dashboard(t, s)
	if len(restored.Transactions) != 2 || len(restored.ExpensePairs) != 0 {
		t.Fatal(restored)
	}
}

func TestExpensePairDashboardNavigationFailsClosedOnDamagedDecision(t *testing.T) {
	root, s, _, r := effectiveConfirmed(t)
	foundationExec(t, root, "UPDATE expense_pair_members SET active=0 WHERE decision_id=?", r.ID)
	d, err := s.Dashboard("2026-09", "", 1)
	pairAPIProblem(t, err, "expense_pair_unavailable")
	if len(d.Transactions) != 0 || len(d.ExpensePairs) != 0 {
		t.Fatal("partial navigation escaped", d)
	}
}
