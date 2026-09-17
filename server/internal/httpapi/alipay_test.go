package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestAlipayPreviewCommitAndHistoryAPI(t *testing.T) {
	h, token := apiForTest(t)
	raw := "交易时间,交易分类,交易对方,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号\n2026-09-15 10:00:00,餐饮美食,合成咖啡店,早餐,支出,12.34,余额,交易成功,p-1,m-1\n2026-09-14 10:00:00,退款,合成书店,退货,收入,20,余额,退款成功,p-2,m-2\n"
	b, _ := json.Marshal(map[string]string{"filename": "native.csv", "account": "wallet", "contentBase64": base64.StdEncoding.EncodeToString([]byte(raw))})
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}
	if w := request(h, http.MethodPost, "/api/v1/imports/alipay/preview", string(b), nil); w.Code != 401 {
		t.Fatal("unauthenticated import accepted", w.Code)
	}
	w := request(h, http.MethodPost, "/api/v1/imports/alipay/preview", string(b), headers)
	var p ledger.Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.NewCount != 1 || len(p.Pending) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"ledgerVersion": p.LedgerVersion, "confirmSimilar": false})
	w = request(h, http.MethodPost, "/api/v1/imports/"+p.ID+"/commit", string(body), headers)
	var result ledger.CommitResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Added != 1 || result.Pending != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, http.MethodGet, "/api/v1/imports/"+p.ID, "", headers)
	var d ledger.ImportDetail
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil || len(d.Preview.Pending) != 1 || !d.Preview.AlreadyCommitted {
		t.Fatal(w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private evidence cacheable")
	}
	w = request(h, http.MethodGet, "/api/v1/dashboard?month=2026-09", "", headers)
	var totals ledger.Dashboard
	if json.Unmarshal(w.Body.Bytes(), &totals) != nil || totals.ExpenseMinor != "1234" || totals.IncomeMinor != "0" {
		t.Fatal("review affected totals", w.Body.String())
	}
}
