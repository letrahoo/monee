package httpapi

import (
	"encoding/json"
	"testing"
)

func TestExpensePairHTTPAuthAndDisabledCapability(t *testing.T) {
	h, token := apiForTest(t)
	routes := []struct{ method, path string }{
		{"POST", "/api/v1/expense-pairs/preview"}, {"POST", "/api/v1/expense-pairs"},
		{"GET", "/api/v1/transactions/synthetic/expense-pairs"}, {"POST", "/api/v1/expense-pairs/synthetic/withdraw"},
	}
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json", "Idempotency-Key": "pair-http-disabled-synthetic"}
	for _, route := range routes {
		w := request(h, route.method, route.path, `{}`, nil)
		if w.Code != 401 {
			t.Fatal(route, w.Code, w.Body.String())
		}
		w = request(h, route.method, route.path, `{}`, headers)
		var p map[string]string
		if w.Code != 503 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p["code"] != "expense_pair_not_enabled" {
			t.Fatal(route, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/expense-pairs/preview", "/api/v1/expense-pairs", "/api/v1/expense-pairs/synthetic/withdraw"} {
		w := request(h, "POST", path, `{"unexpected":"field"}`, headers)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
