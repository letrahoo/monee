package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestDashboardStructuredFiltersHTTP(t *testing.T) {
	h, token := apiForTest(t)
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}
	for i, in := range []ledger.Input{
		{Date: "2026-09-14", Type: "expense", Amount: "100", Currency: "CNY", Merchant: "Match Cafe", Category: "餐饮", Account: "合成钱包 A", Source: "手动"},
		{Date: "2026-09-14", Type: "expense", Amount: "20", Currency: "CNY", Merchant: "Match Book", Category: "学习", Account: "合成钱包 B", Source: "手动"},
		{Date: "2026-09-14", Type: "income", Amount: "200", Currency: "CNY", Merchant: "Match Salary", Category: "工资", Account: "合成钱包 A", Source: "手动"},
	} {
		body, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		headers["Idempotency-Key"] = fmt.Sprintf("http-filters-create-%04d", i)
		w := request(h, http.MethodPost, "/api/v1/transactions", string(body), headers)
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	get := func(path string, want int) ledger.Dashboard {
		t.Helper()
		w := request(h, http.MethodGet, path, "", headers)
		if w.Code != want {
			t.Fatalf("%s: %d != %d: %s", path, w.Code, want, w.Body.String())
		}
		var d ledger.Dashboard
		if want == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}
	base := get("/api/v1/dashboard?month=2026-09", 200)
	if base.TotalCount != 3 || base.FilteredCount != 3 || len(base.FilterOptions.Accounts) != 2 || len(base.FilterOptions.Categories) != 3 {
		t.Fatal(base)
	}
	id := ""
	for _, a := range base.FilterOptions.Accounts {
		if a.Name == "合成钱包 A" {
			id = a.ID
		}
	}
	if id == "" {
		t.Fatal("missing stable account ID")
	}
	for _, tc := range []struct {
		values url.Values
		want   int
	}{
		{url.Values{"type": {"expense"}, "accountId": {id}, "category": {"餐饮"}, "q": {"mAtCh"}}, 1},
		{url.Values{"type": {"income"}}, 1},
		{url.Values{"type": {"refund"}}, 0},
		{url.Values{"type": {"transfer"}}, 0},
		{url.Values{"accountId": {"00000000-0000-4000-8000-000000000001"}}, 0},
		{url.Values{"category": {"餐"}}, 0},
		{url.Values{"type": {"expense"}, "accountId": {id}, "category": {"餐饮"}, "q": {"Book"}}, 0},
		{url.Values{}, 3},
	} {
		tc.values.Set("month", "2026-09")
		d := get("/api/v1/dashboard?"+tc.values.Encode(), 200)
		if d.FilteredCount != tc.want || len(d.Transactions) != tc.want {
			t.Fatalf("%v: %+v", tc.values, d)
		}
		if d.TotalCount != base.TotalCount || d.IncomeMinor != base.IncomeMinor || d.ExpenseMinor != base.ExpenseMinor || d.GrossExpenseMinor != base.GrossExpenseMinor || d.RefundMinor != base.RefundMinor || !reflect.DeepEqual(d.Categories, base.Categories) || !reflect.DeepEqual(d.FilterOptions, base.FilterOptions) {
			t.Fatal("filter changed totals or options", d)
		}
	}
	for _, values := range []url.Values{
		{"type": {"unknown"}}, {"type": {"Expense"}}, {"accountId": {"display name"}},
		{"category": {strings.Repeat("类", 201)}}, {"category": {"bad\ncategory"}},
		{"q": {"bad\x00query"}}, {"accountId": {id + "\n"}},
		{"type": {"expense", "income"}}, {"unexpected": {"x"}},
	} {
		values.Set("month", "2026-09")
		get("/api/v1/dashboard?"+values.Encode(), 422)
	}
	w := request(h, http.MethodGet, "/api/v1/dashboard?month=2026-09&type=expense&accountId="+id, "", nil)
	if w.Code != 401 {
		t.Fatal("filters bypassed login", w.Code)
	}
	empty := get("/api/v1/dashboard?month=2026-08&type=expense", 200)
	if empty.FilterOptions.Accounts == nil || empty.FilterOptions.Categories == nil || len(empty.FilterOptions.Accounts) != 0 || len(empty.FilterOptions.Categories) != 0 {
		t.Fatal("empty options not arrays", empty)
	}
}
