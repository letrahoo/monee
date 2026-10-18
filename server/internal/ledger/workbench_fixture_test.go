package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Exercise the file-picker fixture through the production Go import and reporting
// methods. testStore owns a t.TempDir database; no candidate/runtime data is used.
func TestWorkbenchAnnualFixtureImportFiltersAndRefunds(t *testing.T) {
	s := testStore(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "fixtures", "synthetic", "workbench-annual.csv"))
	if err != nil {
		t.Fatal(err)
	}
	annual := func(year string) AnnualReport {
		t.Helper()
		r, err := s.Annual(year)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	readMonth := func(month string) Dashboard {
		t.Helper()
		d, err := s.Dashboard(month, "", 1)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	checkTotals := func(label, income, gross, refund, net, surplus string, count int, want []string) {
		t.Helper()
		got := []string{income, gross, refund, net, surplus, fmt.Sprint(count)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s totals: got %v want %v", label, got, want)
		}
	}
	checkAnnual := func(r AnnualReport, want ...string) {
		t.Helper()
		checkTotals(fmt.Sprint(r.Year), r.IncomeMinor, r.GrossExpenseMinor, r.RefundMinor, r.ExpenseMinor, r.SurplusMinor, r.TotalCount, want)
	}
	checkMonth := func(m YearMonthSummary, active bool, want ...string) {
		t.Helper()
		checkTotals(m.Month, m.IncomeMinor, m.GrossExpenseMinor, m.RefundMinor, m.ExpenseMinor, m.SurplusMinor, m.TotalCount, want)
		if m.HasRecords != active {
			t.Fatalf("%s hasRecords=%v want %v", m.Month, m.HasRecords, active)
		}
		d := readMonth(m.Month)
		if d.TotalCount != m.TotalCount || d.IncomeMinor != m.IncomeMinor || d.GrossExpenseMinor != m.GrossExpenseMinor || d.RefundMinor != m.RefundMinor || d.ExpenseMinor != m.ExpenseMinor || !reflect.DeepEqual(d.Categories, m.Categories) {
			t.Fatalf("monthly/annual disagreement: %+v / %+v", m, d)
		}
	}
	checkCategories := func(label string, got []CategoryTotal, want ...CategoryTotal) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s categories: got %+v want %+v", label, got, want)
		}
	}
	category := func(name, gross string) CategoryTotal {
		return CategoryTotal{Name: name, AmountMinor: gross, GrossExpenseMinor: gross, RefundMinor: "0"}
	}

	p, err := s.Preview("workbench-annual.csv", string(data))
	if err != nil || len(p.Errors) != 0 || len(p.Rows) != 15 || p.NewCount != 15 || p.DuplicateCount != 0 || p.SimilarCount != 0 {
		t.Fatalf("preview: %+v err=%v", p, err)
	}
	originals := map[string]Transaction{}
	for _, row := range p.Rows {
		if row.Status != "new" || row.Record.ExternalID == "" {
			t.Fatalf("unexpected fixture row: %+v", row)
		}
		if _, exists := originals[row.Record.ExternalID]; exists {
			t.Fatalf("duplicate fixture ID %q", row.Record.ExternalID)
		}
		originals[row.Record.ExternalID] = row.Record
	}
	checkAnnual(annual("2026"), "0", "0", "0", "0", "0", "0")
	checkAnnual(annual("2025"), "0", "0", "0", "0", "0", "0")
	committed, err := s.Commit(p.ID, p.LedgerVersion, false)
	if err != nil || committed.Added != 15 || committed.Skipped != 0 || committed.Pending != 0 {
		t.Fatalf("commit: %+v err=%v", committed, err)
	}
	beforeRetry := annual("2026")
	again, err := s.Preview("workbench-annual.csv", string(data))
	if err != nil || !again.AlreadyCommitted || again.ID != p.ID {
		t.Fatalf("repeat preview: %+v err=%v", again, err)
	}
	retried, err := s.Commit(again.ID, again.LedgerVersion, false)
	if err != nil || retried != committed {
		t.Fatalf("repeat commit must return original result: %+v err=%v", retried, err)
	}
	baseline := annual("2026")
	if !reflect.DeepEqual(beforeRetry, baseline) {
		t.Fatal("repeat import changed annual report or ledger version")
	}
	checkAnnual(baseline, "13000", "10300", "0", "10300", "2700", "13")
	prior := annual("2025")
	checkAnnual(prior, "3000", "1250", "0", "1250", "1750", "2")
	if len(baseline.Months) != 12 || len(prior.Months) != 12 {
		t.Fatal("annual report must contain twelve months")
	}
	checkMonth(prior.Months[11], true, "3000", "1250", "0", "1250", "1750", "2")
	checkMonth(baseline.Months[0], true, "4000", "4000", "0", "4000", "0", "3")
	checkMonth(baseline.Months[8], true, "7000", "4500", "0", "4500", "2500", "6")
	checkMonth(baseline.Months[9], true, "2000", "1800", "0", "1800", "200", "4")
	for _, i := range []int{1, 2, 3, 4, 5, 6, 7, 10, 11} {
		checkMonth(baseline.Months[i], false, "0", "0", "0", "0", "0", "0")
		if len(baseline.Months[i].Categories) != 0 {
			t.Fatal("empty month has categories", baseline.Months[i])
		}
	}
	checkCategories("annual", baseline.Categories, category("学习", "4900"), category("餐饮", "4300"), category("交通", "1100"))
	checkCategories("January", baseline.Months[0].Categories, category("学习", "2500"), category("餐饮", "1500"))
	checkCategories("September tie", baseline.Months[8].Categories, category("学习", "2000"), category("餐饮", "2000"), category("交通", "500"))
	checkCategories("October", baseline.Months[9].Categories, category("餐饮", "800"), category("交通", "600"), category("学习", "400"))

	sep := readMonth("2026-09")
	accountIDs := map[string]string{}
	for _, option := range sep.FilterOptions.Accounts {
		accountIDs[option.Name] = option.ID
	}
	if len(accountIDs) != 2 || accountIDs["合成钱包A"] == "" || accountIDs["合成钱包B"] == "" || accountIDs["合成钱包A"] == accountIDs["合成钱包B"] {
		t.Fatal("fixture account identities", sep.FilterOptions)
	}
	for _, tc := range []struct {
		kind, account, category, query string
		ids                            []string
	}{
		{"expense", "合成钱包A", "餐饮", "咖啡", []string{"008"}},
		{"expense", "合成钱包B", "餐饮", "咖啡", []string{"009"}},
		{"expense", "合成钱包A", "学习", "书店", []string{"010"}},
		{"income", "合成钱包B", "奖金", "九月", []string{"007"}},
		{"expense", "合成钱包A", "餐饮", "书店", nil},
		{"expense", "", "餐饮", "咖啡", []string{"008", "009"}},
		{"refund", "", "", "", nil},
		{"", "", "", "", []string{"006", "007", "008", "009", "010", "011"}},
	} {
		d, err := s.DashboardWithFilters("2026-09", tc.query, 1, DashboardFilters{Type: tc.kind, AccountID: accountIDs[tc.account], Category: tc.category})
		if err != nil {
			t.Fatal(err)
		}
		assertDashboardSummary(t, d, sep)
		if d.FilteredCount != len(tc.ids) || len(d.Transactions) != len(tc.ids) {
			t.Fatalf("filter %+v: %+v", tc, d)
		}
		want := map[string]bool{}
		for _, suffix := range tc.ids {
			want["SYNTH-WB-ANNUAL-"+suffix] = true
		}
		for _, record := range d.Transactions {
			if !want[record.ExternalID] {
				t.Fatalf("filter %+v returned unexpected transaction %+v", tc, record)
			}
			delete(want, record.ExternalID)
		}
		if len(want) != 0 {
			t.Fatal("missing filtered fixture records", want)
		}
	}

	refund := func(suffix, date, amount, account, key string) Transaction {
		t.Helper()
		original := originals["SYNTH-WB-ANNUAL-"+suffix]
		r, err := s.CreateRefund(original.ID, RefundInput{Version: original.Version, Date: date, Amount: amount, Currency: "CNY", Account: account}, key)
		if err != nil {
			t.Fatal(err)
		}
		if r.Type != "refund" || r.RefundOf != original.ID || r.Category != original.Category {
			t.Fatalf("refund lost original association: %+v", r)
		}
		return r
	}
	first := refund("005", "2026-02-05", "5.00", "合成钱包B", "workbench-fixture-refund-001")
	if first.AmountMinor != "500" {
		t.Fatal(first)
	}
	afterFirst := annual("2026")
	checkAnnual(afterFirst, "13000", "10300", "500", "9800", "3200", "14")
	checkMonth(afterFirst.Months[1], true, "0", "0", "500", "-500", "500", "1")
	checkCategories("refund-only February", afterFirst.Months[1].Categories, CategoryTotal{Name: "学习", AmountMinor: "-500", GrossExpenseMinor: "0", RefundMinor: "500"})
	if !reflect.DeepEqual(afterFirst.Months[0], baseline.Months[0]) {
		t.Fatal("February refund changed January period totals")
	}
	second := refund("010", "2026-10-02", "3.00", "合成钱包A", "workbench-fixture-refund-002")
	if second.AmountMinor != "300" {
		t.Fatal(second)
	}
	afterBoth := annual("2026")
	checkAnnual(afterBoth, "13000", "10300", "800", "9500", "3500", "15")
	checkMonth(afterBoth.Months[9], true, "2000", "1800", "300", "1500", "500", "5")
	checkMonth(afterBoth.Months[1], true, "0", "0", "500", "-500", "500", "1")
	if !reflect.DeepEqual(afterBoth.Months[0], baseline.Months[0]) || !reflect.DeepEqual(afterBoth.Months[8], baseline.Months[8]) {
		t.Fatal("cross-month refunds changed original consumption month")
	}
	checkCategories("October after refund", afterBoth.Months[9].Categories, category("餐饮", "800"), category("交通", "600"), CategoryTotal{Name: "学习", AmountMinor: "100", GrossExpenseMinor: "400", RefundMinor: "300"})
	// Learning net 4100 is less than dining net 4300, but its gross 4900
	// remains first: refunds must not silently switch charts to net ordering.
	checkCategories("annual gross order after refunds", afterBoth.Categories, CategoryTotal{Name: "学习", AmountMinor: "4100", GrossExpenseMinor: "4900", RefundMinor: "800"}, category("餐饮", "4300"), category("交通", "1100"))
	checkAnnual(annual("2025"), "3000", "1250", "0", "1250", "1750", "2")
}
