package ledger

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"
)

func TestExpensePairEffectiveSearchAndPagination(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	for id, note := range map[string]string{in.PrimaryTransactionID: "primary-only-search", in.SecondaryTransactionID: "secondary-only-search"} {
		if _, err := s.Annotate(id, AnnotationInput{Version: 1, Category: "餐饮美食", Note: note}); err != nil {
			t.Fatal(err)
		}
	}
	// Force secondary into raw slot 50, with primary and an older visible row
	// behind it. Effective pagination must fill slot 50 with primary.
	foundationExec(t, root, "UPDATE transactions SET created_at='2026-09-15T01:00:00Z' WHERE id=?", in.PrimaryTransactionID)
	foundationExec(t, root, "UPDATE transactions SET created_at='2026-09-15T02:00:00Z' WHERE id=?", in.SecondaryTransactionID)
	older, err := s.Create(testInput(), "effective-pagination-older")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < PageSize-1; i++ {
		input := testInput()
		input.Date = "2026-09-16"
		if _, err := s.Create(input, fmt.Sprintf("effective-pagination-%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	rawFirst, err := s.Dashboard("2026-09", "", 1)
	if err != nil || len(rawFirst.Transactions) != PageSize || rawFirst.Transactions[PageSize-1].ID != in.SecondaryTransactionID {
		t.Fatal("fixture must place secondary in raw page boundary", rawFirst, err)
	}
	lifecycleRefresh(t, root, &in)
	receipt, err := s.confirmExpensePair(in, "effective-pagination-confirm")
	if err != nil {
		t.Fatal(err)
	}
	before := nativeEvidenceTables(t, root)
	for query, want := range map[string]int{"": PageSize + 1, "primary-only-search": 1, "secondary-only-search": 0} {
		d, err := s.Dashboard("2026-09", query, 1)
		if err != nil || d.FilteredCount != want || d.TotalCount != PageSize+1 || d.GrossExpenseMinor != strconv.Itoa((PageSize+1)*1234) {
			t.Fatal(query, d, err)
		}
		if query == "primary-only-search" && (len(d.Transactions) != 1 || d.Transactions[0].ID != in.PrimaryTransactionID) {
			t.Fatal(d)
		}
		if query == "secondary-only-search" && len(d.Transactions) != 0 {
			t.Fatal(d)
		}
	}
	seen := map[string]bool{}
	for page, want := range map[int]int{1: PageSize, 2: 1, 3: 0} {
		d, err := s.Dashboard("2026-09", "", page)
		if err != nil || len(d.Transactions) != want || d.FilteredCount != PageSize+1 || d.Version != receipt.LedgerVersion {
			t.Fatal(page, d, err)
		}
		if page == 1 && (len(d.ExpensePairs) != 1 || d.ExpensePairs[0] != receipt) {
			t.Fatal("primary navigation missing at page boundary", d)
		}
		if page != 1 && len(d.ExpensePairs) != 0 {
			t.Fatal("navigation leaked to another page", d)
		}
		for _, row := range d.Transactions {
			if seen[row.ID] || row.ID == in.SecondaryTransactionID {
				t.Fatal("duplicate/suppressed row crossed page boundary", row)
			}
			seen[row.ID] = true
		}
		if page == 1 && d.Transactions[PageSize-1].ID != in.PrimaryTransactionID {
			t.Fatal("suppressed row left a page hole", d)
		}
		if page == 2 && d.Transactions[0].ID != older.ID {
			t.Fatal("next page is discontinuous", d)
		}
	}
	if len(seen) != PageSize+1 || !reflect.DeepEqual(before, nativeEvidenceTables(t, root)) {
		t.Fatal("pagination lost rows or mutated evidence")
	}
	effectiveWithdraw(t, s, receipt)
	d, err := s.Dashboard("2026-09", "", 2)
	if err != nil || len(d.Transactions) != 2 || d.TotalCount != PageSize+2 || d.ExpenseMinor != strconv.Itoa((PageSize+2)*1234) {
		t.Fatal(d, err)
	}
	d, err = s.Dashboard("2026-09", "secondary-only-search", 1)
	if err != nil || d.FilteredCount != 1 || len(d.Transactions) != 1 || d.Transactions[0].ID != in.SecondaryTransactionID {
		t.Fatal("withdrawal did not restore secondary search", d, err)
	}
}
