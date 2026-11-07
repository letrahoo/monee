package ledger

import (
	"fmt"
	"testing"
)

func TestExistingCategoryOptionsAlwaysRemainExactlySelectable(t *testing.T) {
	s := testStore(t)
	for i, category := range []string{"餐\t饮", "餐\u0085饮", "餐饮"} {
		in := testInput()
		in.Date, in.Category, in.Merchant = "2026-09-14", category, fmt.Sprintf("合成类别 %d", i)
		if _, err := s.Create(in, fmt.Sprintf("legacy-category-%04d", i)); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.Dashboard("2026-09", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, category := range all.FilterOptions.Categories {
		got, err := s.DashboardWithFilters("2026-09", "", 1, DashboardFilters{Category: category})
		if err != nil || got.FilteredCount != 1 || len(got.Transactions) != 1 || got.Transactions[0].Category != category {
			t.Fatal("server option is not selectable", category, got, err)
		}
	}
}
