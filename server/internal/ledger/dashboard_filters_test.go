package ledger

import (
	"fmt"
	"github.com/letrahoo/monee/server/internal/auth"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func filteredDashboard(t *testing.T, s *Store, query string, page int, f DashboardFilters) Dashboard {
	t.Helper()
	d, err := s.DashboardWithFilters("2026-09", query, page, f)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func dashboardFilterAccount(t *testing.T, d Dashboard, name string) string {
	t.Helper()
	for _, a := range d.FilterOptions.Accounts {
		if a.Name == name {
			return a.ID
		}
	}
	t.Fatalf("missing account %q: %+v", name, d.FilterOptions)
	return ""
}
func assertDashboardSummary(t *testing.T, got, want Dashboard) {
	t.Helper()
	if got.LedgerID != want.LedgerID || got.Version != want.Version || got.TotalCount != want.TotalCount || got.IncomeMinor != want.IncomeMinor || got.ExpenseMinor != want.ExpenseMinor || got.GrossExpenseMinor != want.GrossExpenseMinor || got.RefundMinor != want.RefundMinor || got.SourceCount != want.SourceCount || !reflect.DeepEqual(got.Categories, want.Categories) || !reflect.DeepEqual(got.FilterOptions, want.FilterOptions) || !reflect.DeepEqual(got.Months, want.Months) {
		t.Fatalf("filter changed month summary: got=%+v want=%+v", got, want)
	}
}
func TestDashboardFiltersCombinedClearAndTypes(t *testing.T) {
	s := testStore(t)
	var original Transaction
	for i, in := range []Input{
		{Date: "2026-09-14", Type: "expense", Amount: "100", Currency: "CNY", Merchant: "Match Cafe", Category: "餐饮", Account: "钱包 A", Source: "手动"},
		{Date: "2026-09-14", Type: "expense", Amount: "20", Currency: "CNY", Merchant: "Match Book", Category: "学习", Account: "钱包 A", Source: "手动"},
		{Date: "2026-09-14", Type: "expense", Amount: "10", Currency: "CNY", Merchant: "Match Cafe", Category: "餐饮", Account: "钱包 B", Source: "手动"},
		{Date: "2026-09-14", Type: "income", Amount: "200", Currency: "CNY", Merchant: "Match Salary", Category: "工资", Account: "钱包 A", Source: "银行"},
		{Date: "2026-10-14", Type: "expense", Amount: "1", Currency: "CNY", Merchant: "Other Month", Category: "十月分类", Account: "十月账户", Source: "手动"},
	} {
		r, err := s.Create(in, fmt.Sprintf("filters-create-%04d", i))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			original = r
		}
	}
	r, err := s.CreateRefund(original.ID, RefundInput{Version: 1, Date: "2026-09-19", Amount: "30", Currency: "CNY", Account: "钱包 A", Note: "Match Refund"}, "filters-refund-001")
	if err != nil {
		t.Fatal(err)
	}
	baseline := dashboard(t, s)
	if baseline.TotalCount != 5 || baseline.IncomeMinor != "20000" || baseline.GrossExpenseMinor != "13000" || baseline.RefundMinor != "3000" || baseline.ExpenseMinor != "10000" {
		t.Fatal(baseline)
	}
	a := dashboardFilterAccount(t, baseline, "钱包 A")
	if len(baseline.FilterOptions.Accounts) != 2 || !reflect.DeepEqual(baseline.FilterOptions.Categories, []string{"学习", "工资", "餐饮"}) {
		t.Fatal(baseline.FilterOptions)
	}
	for _, tc := range []struct {
		f     DashboardFilters
		q     string
		count int
		id    string
	}{
		{DashboardFilters{}, "", 5, ""}, {DashboardFilters{Type: "income"}, "", 1, ""}, {DashboardFilters{Type: "expense"}, "", 3, ""},
		{DashboardFilters{Type: "refund"}, "", 1, r.ID}, {DashboardFilters{Type: "transfer"}, "", 0, ""},
		{DashboardFilters{Type: "expense", AccountID: a, Category: "餐饮"}, " mAtCh ", 1, original.ID},
		{DashboardFilters{Type: "expense", AccountID: a, Category: "餐饮"}, "Book", 0, ""},
		{DashboardFilters{Category: "餐"}, "", 0, ""}, {DashboardFilters{Category: "餐饮 "}, "", 0, ""}, {DashboardFilters{AccountID: newID()}, "", 0, ""},
	} {
		d := filteredDashboard(t, s, tc.q, 1, tc.f)
		assertDashboardSummary(t, d, baseline)
		if d.FilteredCount != tc.count || len(d.Transactions) != tc.count || tc.id != "" && d.Transactions[0].ID != tc.id {
			t.Fatalf("%+v %q: %+v", tc.f, tc.q, d)
		}
	}
	cleared, err := s.Dashboard("2026-09", "", 1)
	if err != nil || !reflect.DeepEqual(cleared, baseline) {
		t.Fatal("legacy clear differs", err)
	}
}
func TestDashboardFiltersStableIdentityRenameAndPostings(t *testing.T) {
	s := testStore(t)
	in := testInput()
	in.Account = "旧名"
	r, err := s.Create(in, "filters-rename-001")
	if err != nil {
		t.Fatal(err)
	}
	id := dashboardFilterAccount(t, dashboard(t, s), "旧名")
	if _, err = s.db.Exec("UPDATE accounts SET name='新名',version=version+1 WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	d := filteredDashboard(t, s, "", 1, DashboardFilters{AccountID: id})
	if d.FilteredCount != 1 || d.Transactions[0].ID != r.ID || d.Transactions[0].Account != "旧名" || dashboardFilterAccount(t, d, "新名") != id {
		t.Fatal(d)
	}
	other, err := s.Create(in, "filters-rename-002")
	if err != nil {
		t.Fatal(err)
	}
	oldID := dashboardFilterAccount(t, dashboard(t, s), "旧名")
	if oldID == id {
		t.Fatal("new account reused renamed identity")
	}
	for _, tc := range []struct{ id, want string }{{id, r.ID}, {oldID, other.ID}, {strings.ToUpper(id), r.ID}} {
		d = filteredDashboard(t, s, "", 1, DashboardFilters{AccountID: tc.id})
		if d.FilteredCount != 1 || d.Transactions[0].ID != tc.want {
			t.Fatal(d)
		}
	}
	// Any clearing posting may match, but detail rows must not be multiplied.
	if _, err = s.db.Exec("INSERT INTO postings VALUES(?,?,?,?,?)", newID(), r.ID, oldID, int64(0), "CNY"); err != nil {
		t.Fatal(err)
	}
	d = filteredDashboard(t, s, "", 1, DashboardFilters{AccountID: oldID})
	if d.FilteredCount != 2 || len(d.Transactions) != 2 {
		t.Fatal("second posting or dedup", d)
	}
}
func TestDashboardFiltersStablePagination(t *testing.T) {
	s := testStore(t)
	expected := []string{}
	for i := 0; i < PageSize*2+3; i++ {
		r, err := s.Create(testInput(), fmt.Sprintf("filters-paging-%04d", i))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.db.Exec("UPDATE transactions SET created_at='2026-09-14T00:00:00Z' WHERE id=?", r.ID); err != nil {
			t.Fatal(err)
		}
		expected = append(expected, r.ID)
	}
	sort.Strings(expected)
	baseline := dashboard(t, s)
	id := baseline.FilterOptions.Accounts[0].ID
	got := []string{}
	for page := 1; page <= 4; page++ {
		d := filteredDashboard(t, s, "中文", page, DashboardFilters{Type: "expense", AccountID: id, Category: "餐饮"})
		assertDashboardSummary(t, d, baseline)
		if d.FilteredCount != len(expected) {
			t.Fatal(d)
		}
		for _, r := range d.Transactions {
			got = append(got, r.ID)
		}
		again := filteredDashboard(t, s, "中文", page, DashboardFilters{Type: "expense", AccountID: id, Category: "餐饮"})
		if !reflect.DeepEqual(d, again) {
			t.Fatal("unstable retry")
		}
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatal("pagination dropped, duplicated or reordered rows")
	}
}
func TestDashboardFiltersInvalid(t *testing.T) {
	s := testStore(t)
	for _, tc := range []struct {
		month, q string
		page     int
		f        DashboardFilters
	}{
		{"2026-13", "", 1, DashboardFilters{}}, {"2026-09", "", 0, DashboardFilters{}}, {"2026-09", "", 100001, DashboardFilters{}},
		{"2026-09", strings.Repeat("搜", 101), 1, DashboardFilters{}}, {"2026-09", "x\x00", 1, DashboardFilters{}},
		{"2026-09", "", 1, DashboardFilters{Type: "all"}}, {"2026-09", "", 1, DashboardFilters{Type: "Income"}},
		{"2026-09", "", 1, DashboardFilters{AccountID: "wallet"}}, {"2026-09", "", 1, DashboardFilters{AccountID: newID() + "\n"}},
		{"2026-09", "", 1, DashboardFilters{AccountID: strings.Repeat("a", 1000)}},
		{"2026-09", "", 1, DashboardFilters{Category: strings.Repeat("类", 201)}}, {"2026-09", "", 1, DashboardFilters{Category: "x\n"}},
		{"2026-09", "", 1, DashboardFilters{Category: "x\x00"}}, {"2026-09", "", 1, DashboardFilters{Category: string([]byte{0xff})}},
	} {
		_, err := s.DashboardWithFilters(tc.month, tc.q, tc.page, tc.f)
		p, ok := err.(*Problem)
		if !ok || p.Code != "invalid" {
			t.Fatalf("accepted %+v: %v", tc, err)
		}
	}
	if _, err := s.DashboardWithFilters("2026-09", "", 1, DashboardFilters{Category: strings.Repeat("类", 200)}); err != nil {
		t.Fatal("valid maximum", err)
	}
}
func TestDashboardFiltersSoftDeletedAndPendingExcluded(t *testing.T) {
	s := testStore(t)
	in := testInput()
	in.Account = "已删账户"
	in.Category = "已删分类"
	r, err := s.Create(in, "filters-deleted-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE transactions SET deleted_at=? WHERE id=?", now(), r.ID); err != nil {
		t.Fatal(err)
	}
	alipayPreview(t, s, []byte(nativeHeader+nativeRefund), "待确认账户")
	preview(t, s, sampleCSV)
	d := dashboard(t, s)
	if d.TotalCount != 0 || d.FilteredCount != 0 || len(d.FilterOptions.Accounts) != 0 || len(d.FilterOptions.Categories) != 0 || d.FilterOptions.Accounts == nil || d.FilterOptions.Categories == nil {
		t.Fatal(d)
	}
}
func TestDashboardFiltersLedgerIsolationAndReauthorization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	access, err := auth.Open(path, []auth.Selector{{Provider: "github", Kind: "subject", Value: "43001"}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	identity := auth.Identity{Provider: "github", Subject: "43001", Username: "synthetic"}
	if err = access.RememberIdentity(identity); err != nil {
		t.Fatal(err)
	}
	token, err := access.CreateSession(identity)
	if err != nil {
		t.Fatal(err)
	}
	user, err := access.Session(token)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateLedger(user.ID, "合成甲")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateLedger(user.ID, "合成乙")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Scoped(user.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Scoped(user.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := testInput()
	in.Account = "同名"
	if _, err = a.Create(in, "filters-isolate-001"); err != nil {
		t.Fatal(err)
	}
	in.Category = "仅乙分类"
	if _, err = b.Create(in, "filters-isolate-001"); err != nil {
		t.Fatal(err)
	}
	firstID := dashboardFilterAccount(t, dashboard(t, a), "同名")
	secondID := dashboardFilterAccount(t, dashboard(t, b), "同名")
	if firstID == secondID {
		t.Fatal("accounts not ledger-scoped")
	}
	d := filteredDashboard(t, a, "", 1, DashboardFilters{AccountID: secondID})
	if d.FilteredCount != 0 || d.TotalCount != 1 || len(d.FilterOptions.Accounts) != 1 || d.FilterOptions.Accounts[0].ID != firstID || !reflect.DeepEqual(d.FilterOptions.Categories, []string{"餐饮"}) {
		t.Fatal("cross-ledger leak", d)
	}
	var transactionID string
	if err = a.db.QueryRow("SELECT id FROM transactions WHERE ledger_id=?", first.ID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec("INSERT INTO postings VALUES(?,?,?,?,?)", newID(), transactionID, secondID, 0, "CNY"); err != nil {
		t.Fatal(err)
	}
	if d = filteredDashboard(t, a, "", 1, DashboardFilters{AccountID: secondID}); d.FilteredCount != 0 || len(d.FilterOptions.Accounts) != 1 {
		t.Fatal(d)
	}
	if _, err = s.db.Exec("UPDATE ledger_members SET role='viewer' WHERE ledger_id=? AND user_id=?", first.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	filteredDashboard(t, a, "", 1, DashboardFilters{Type: "expense"})
	if _, err = s.db.Exec("DELETE FROM ledger_members WHERE ledger_id=? AND user_id=?", first.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.DashboardWithFilters("2026-09", "", 1, DashboardFilters{}); err == nil {
		t.Fatal("revoked scoped store still reads")
	}
	if _, err = s.db.Exec("UPDATE user_access SET enabled=0 WHERE user_id=?", user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.DashboardWithFilters("2026-09", "", 1, DashboardFilters{}); err == nil {
		t.Fatal("revoked admission still reads")
	}
}

// This isolated fixture simulates a future transfer row without changing schema3
// or enabling transfer creation in production. Positive transfer money is not income.
func TestDashboardFiltersFutureTransferNotIncome(t *testing.T) {
	s := testStore(t)
	in := testInput()
	in.Account = "转出账户"
	r, err := s.Create(in, "filters-future-transfer-001")
	if err != nil {
		t.Fatal(err)
	}
	// CHECK bypass is test-only and scoped to this disposable database.
	if _, err = s.db.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE transactions SET kind='transfer',amount_minor=1234 WHERE id=?", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}
	d := filteredDashboard(t, s, "", 1, DashboardFilters{Type: "transfer"})
	if d.FilteredCount != 1 || d.TotalCount != 1 || d.IncomeMinor != "0" || d.ExpenseMinor != "0" || d.GrossExpenseMinor != "0" || d.RefundMinor != "0" || len(d.Categories) != 0 {
		t.Fatal(d)
	}
}

func TestDashboardFiltersRejectOverflowEvenWhenHidden(t *testing.T) {
	for _, kind := range []string{"income", "expense"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			for i := 0; i < 2; i++ {
				in := testInput()
				in.Type = kind
				r, err := s.Create(in, fmt.Sprintf("filters-overflow-%04d", i))
				if err != nil {
					t.Fatal(err)
				}
				amount := int64(math.MaxInt64)
				if kind == "expense" {
					amount = -amount
				}
				if _, err = s.db.Exec("UPDATE transactions SET amount_minor=? WHERE id=?", amount, r.ID); err != nil {
					t.Fatal(err)
				}
			}
			_, err := s.DashboardWithFilters("2026-09", "no match", 1, DashboardFilters{Type: "transfer"})
			p, ok := err.(*Problem)
			if !ok || p.Code != "overflow" {
				t.Fatal("overflow hidden by filter", err)
			}
		})
	}
}
