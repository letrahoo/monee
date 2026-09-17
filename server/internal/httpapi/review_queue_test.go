package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

const reviewQueueCSV = "交易时间,交易分类,交易对方,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号\n2026-09-17 09:00:00,退款,合成商户,退货,收入,12.345,余额,退款成功,synthetic-review-01,synthetic-order-01\n"

func TestReviewQueueEndpoint(t *testing.T) {
	h, token := apiForTest(t)
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}
	body, _ := json.Marshal(map[string]string{"filename": "synthetic.csv", "account": "wallet", "contentBase64": base64.StdEncoding.EncodeToString([]byte(reviewQueueCSV))})
	w := request(h, "POST", "/api/v1/imports/alipay/preview", string(body), headers)
	var p ledger.Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || len(p.Pending) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/v1/review-queue", "", nil)
	if w.Code != 401 {
		t.Fatal("unauthenticated source disclosure", w.Code)
	}
	w = request(h, "GET", "/api/v1/review-queue?format=alipay&page=1", "", headers)
	var q ledger.ReviewQueue
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &q) != nil || q.TotalCount != 1 || len(q.Items) != 1 || q.Items[0].ImportID != p.ID || q.Items[0].SourceAccount != "wallet" || q.Items[0].Amount != "12.345" || q.Items[0].BatchState != "preview" || q.Items[0].Raw["交易订单号"] != "synthetic-review-01" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, privateField := range []string{"raw_csv", "contentBase64", "SourceDocument", "content_hash"} {
		if strings.Contains(w.Body.String(), privateField) {
			t.Fatal("unrequested full file envelope exposed", privateField)
		}
	}
	for _, query := range []string{"?page=0", "?page=-1", "?page=1000001", "?page=garbage", "?format=standard"} {
		if w = request(h, "GET", "/api/v1/review-queue"+query, "", headers); w.Code != 422 {
			t.Fatal(query, w.Code, w.Body.String())
		}
	}
	if w = request(h, "GET", "/api/v1/review-queue?format=wechat", "", headers); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &q) != nil || q.TotalCount != 0 || len(q.Items) != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(h, "POST", "/api/v1/review-queue", "{}", headers); w.Code != 404 && w.Code != 405 {
		t.Fatal("write endpoint unexpectedly available", w.Code)
	}
}

func TestReviewQueueViewerAndTenantAuthorization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.db")
	data, e := ledger.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer data.Close()
	access, e := auth.Open(path, []auth.Selector{{Provider: "github", Kind: "subject", Value: "1001"}})
	if e != nil {
		t.Fatal(e)
	}
	defer access.Close()
	identities := []auth.Identity{{Provider: "github", Subject: "1001", Username: "owner"}, {Provider: "github", Subject: "2002", Username: "viewer"}, {Provider: "github", Subject: "3003", Username: "other"}}
	tokens := []string{}
	ids := []string{}
	for index, identity := range identities {
		if e = access.RememberIdentity(identity); e != nil {
			t.Fatal(e)
		}
		if index > 0 {
			if _, e = access.Add(identities[0], auth.Selector{Provider: "github", Kind: "subject", Value: identity.Subject}, ""); e != nil {
				t.Fatal(e)
			}
		}
		token, e := access.CreateSession(identity)
		if e != nil {
			t.Fatal(e)
		}
		tokens = append(tokens, token)
		u, e := access.Session(token)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, u.ID)
	}
	first, e := data.CreateLedger(ids[0], "合成甲")
	if e != nil {
		t.Fatal(e)
	}
	second, e := data.CreateLedger(ids[2], "合成乙")
	if e != nil {
		t.Fatal(e)
	}
	owner, e := data.Scoped(ids[0], first.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = owner.Invite(ids[1], "viewer"); e != nil {
		t.Fatal(e)
	}
	pending, e := data.Invitations(ids[1])
	if e != nil || len(pending) != 1 {
		t.Fatal(e, pending)
	}
	if e = data.RespondInvitation(ids[1], pending[0].ID, true); e != nil {
		t.Fatal(e)
	}
	p, e := owner.PreviewAlipay("synthetic.csv", base64.StdEncoding.EncodeToString([]byte(reviewQueueCSV)), "wallet")
	if e != nil || len(p.Pending) != 1 {
		t.Fatal(p, e)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	call := func(who int, ledgerID string, want int) ledger.ReviewQueue {
		t.Helper()
		w := request(h, "GET", "/api/v1/review-queue", "", map[string]string{"Authorization": "Bearer " + tokens[who], "X-Monee-Ledger": ledgerID})
		if w.Code != want {
			t.Fatal(w.Code, want, w.Body.String())
		}
		var out ledger.ReviewQueue
		if want == 200 {
			if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
				t.Fatal(e)
			}
		}
		return out
	}
	q := call(1, first.ID, 200)
	if q.TotalCount != 1 || q.Items[0].ImportID != p.ID {
		t.Fatal("viewer source missing", q)
	}
	q = call(2, second.ID, 200)
	if q.TotalCount != 0 {
		t.Fatal("cross-ledger source leak", q)
	}
	call(0, second.ID, 403) // System administrator is not implicitly a ledger member.
	call(2, first.ID, 403)
	if e = owner.ChangeMember(ids[1], "remove", 1); e != nil {
		t.Fatal(e)
	}
	call(1, first.ID, 403)
	if d, e := owner.Dashboard("2026-09", "", 1); e != nil || d.TotalCount != 0 {
		t.Fatal("read-only queue wrote financial data", d, e)
	}
}
