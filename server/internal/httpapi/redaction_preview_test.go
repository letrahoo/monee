package httpapi

import (
	"bytes"
	"encoding/base64"
	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
	"github.com/letrahoo/monee/server/internal/redaction"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactionPreviewAuthorizationAndTenantIsolation(t *testing.T) {
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
	raw := "交易时间,交易分类,交易对方,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号\n2026-09-17 10:00:00,餐饮美食,合成商户,私人备注,支出,12.34,余额,交易成功,payment-secret,order-secret\n"
	p, e := owner.PreviewAlipay("private-file.csv", base64.StdEncoding.EncodeToString([]byte(raw)), "private-account")
	if e != nil {
		t.Fatal(e)
	}
	projector, e := redaction.New(bytes.Repeat([]byte{32}, 32))
	if e != nil {
		t.Fatal(e)
	}
	h := API{Redaction: projector, Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	call := func(who int, ledgerID, method, suffix string, want int) {
		t.Helper()
		w := request(h, method, "/api/v1/imports/"+p.ID+suffix, `{"ledgerVersion":1}`, map[string]string{"Authorization": "Bearer " + tokens[who], "Content-Type": "application/json", "X-Monee-Ledger": ledgerID})
		if w.Code != want {
			t.Fatalf("%s %s: %d wanted %d: %s", method, suffix, w.Code, want, w.Body.String())
		}
	}
	call(0, first.ID, http.MethodGet, "/redaction-preview", 200)
	call(1, first.ID, http.MethodGet, "/redaction-preview", 200)
	call(2, second.ID, http.MethodGet, "/redaction-preview", 404)
	call(0, second.ID, http.MethodGet, "/redaction-preview", 403)
	if e = owner.ChangeMember(ids[1], "remove", 1); e != nil {
		t.Fatal(e)
	}
	call(1, first.ID, http.MethodGet, "/redaction-preview", 403)
	headers := map[string]string{"Authorization": "Bearer " + tokens[0], "X-Monee-Ledger": first.ID}
	endpoint := "/api/v1/imports/" + p.ID + "/redaction-preview"
	w := request(h, "GET", endpoint, "", headers)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"localOnly":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, private := range []string{"private-file.csv", "private-account", "payment-secret", "order-secret", "私人备注", "10:00:00"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("private field echoed", private)
		}
	}
	if w := request(h, "GET", endpoint, "", nil); w.Code != 401 {
		t.Fatal("unauthenticated accepted", w.Code)
	}
	unavailable := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	if w := request(unavailable, "GET", endpoint, "", headers); w.Code != 503 {
		t.Fatal("missing key did not fail closed", w.Code, w.Body.String())
	}
	if w := request(unavailable, "GET", "/api/v1/dashboard", "", headers); w.Code != 200 {
		t.Fatal("key failure disabled ordinary ledger", w.Code)
	}
	if d, e := owner.Dashboard("2026-09", "", 1); e != nil || d.TotalCount != 0 {
		t.Fatal("read changed ledger", d, e)
	}
}
