package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"github.com/letrahoo/monee/server/internal/ledger"
	"os"
	"testing"
)

func TestWeChatAndAnnotationEndpoints(t *testing.T) {
	h, token := apiForTest(t)
	raw, e := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(map[string]string{"filename": "wx.xlsx", "account": "wallet", "contentBase64": base64.StdEncoding.EncodeToString(raw)})
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}
	if w := request(h, "POST", "/api/v1/imports/wechat/preview", string(b), nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request(h, "POST", "/api/v1/imports/wechat/preview", string(b), headers)
	var p ledger.Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.NewCount != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	b, _ = json.Marshal(map[string]any{"ledgerVersion": p.LedgerVersion})
	w = request(h, "POST", "/api/v1/imports/"+p.ID+"/commit", string(b), headers)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	id := p.Rows[0].Record.ID
	path := "/api/v1/transactions/" + id
	w = request(h, "GET", path+"/sources", "", headers)
	var sources []ledger.TransactionSource
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &sources) != nil || len(sources) != 1 || sources[0].Line != 2 {
		t.Fatal(w.Body.String())
	}
	w = request(h, "PATCH", path+"/annotation", `{"version":1,"category":"早餐","note":"合成核实"}`, headers)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "PATCH", path+"/annotation", `{"version":1,"category":"其他","note":""}`, headers)
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	w = request(h, "GET", path+"/annotations", "", headers)
	var revisions []ledger.AnnotationRevision
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &revisions) != nil || len(revisions) != 1 || revisions[0].After.Category != "早餐" {
		t.Fatal(w.Body.String())
	}
}
