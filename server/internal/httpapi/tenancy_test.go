package httpapi

import (
	"encoding/json"
	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
	"path/filepath"
	"testing"
)

func TestLedgerIsolationInvitationsAndRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.db")
	data, err := ledger.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	access, err := auth.Open(path, []auth.Selector{{Provider: "github", Kind: "subject", Value: "1001"}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	identities := []auth.Identity{{Provider: "github", Subject: "1001", Username: "owner"}, {Provider: "github", Subject: "2002", Username: "reader"}, {Provider: "github", Subject: "3003", Username: "other"}}
	tokens := []string{}
	ids := []string{}
	for index, i := range identities {
		if err = access.RememberIdentity(i); err != nil {
			t.Fatal(err)
		}
		if index > 0 {
			if _, err = access.Add(identities[0], auth.Selector{Provider: "github", Kind: "subject", Value: i.Subject}, ""); err != nil {
				t.Fatal(err)
			}
		}
		token, e := access.CreateSession(i)
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
	first, err := data.CreateLedger(ids[0], "甲账本")
	if err != nil {
		t.Fatal(err)
	}
	second, err := data.CreateLedger(ids[2], "乙账本")
	if err != nil {
		t.Fatal(err)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	call := func(index int, ledgerID, method, path, body string, want int) string {
		t.Helper()
		w := request(h, method, path, body, map[string]string{"Authorization": "Bearer " + tokens[index], "X-Monee-Ledger": ledgerID, "Content-Type": "application/json", "Idempotency-Key": "same-key-across-ledgers"})
		if w.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		return w.Body.String()
	}
	input := `{"date":"2026-01-02","type":"expense","amount":"12.34","currency":"CNY","merchant":"合成商户","source":"合成"}`
	call(0, second.ID, "GET", "/api/v1/dashboard", "", 403) // platform superadmin is not a ledger member
	call(0, first.ID, "POST", "/api/v1/transactions", input, 201)
	call(2, second.ID, "POST", "/api/v1/transactions", input, 201) // same idempotency key, different ledger
	call(1, first.ID, "GET", "/api/v1/dashboard", "", 403)
	call(0, first.ID, "POST", "/api/v1/ledgers/"+first.ID+"/invitations", `{"userId":"`+ids[1]+`","role":"viewer"}`, 201)
	call(1, first.ID, "GET", "/api/v1/dashboard", "", 403) // pending invite grants nothing
	pending, err := data.Invitations(ids[1])
	if err != nil || len(pending) != 1 {
		t.Fatal(err, pending)
	}
	call(2, "", "POST", "/api/v1/invitations/"+pending[0].ID+"/respond", `{"accept":true}`, 404)
	call(1, "", "POST", "/api/v1/invitations/"+pending[0].ID+"/respond", `{"accept":true}`, 200)
	call(1, first.ID, "GET", "/api/v1/dashboard?month=2026-01", "", 200)
	call(1, first.ID, "POST", "/api/v1/transactions", input, 403)
	call(1, first.ID, "GET", "/api/v1/imports", "", 200)
	call(1, first.ID, "POST", "/api/v1/imports/preview", `{"filename":"a.csv","csv":"a"}`, 403)
	call(1, first.ID, "POST", "/api/v1/imports/wechat/preview", `{"filename":"a.xlsx","account":"a","contentBase64":""}`, 403)
	call(1, first.ID, "PATCH", "/api/v1/transactions/nonexistent/annotation", `{"version":1,"category":"x","note":""}`, 403)
	call(1, first.ID, "POST", "/api/v1/imports/alipay/preview", `{"filename":"a.csv","account":"a","contentBase64":""}`, 403)
	call(1, first.ID, "GET", "/api/v1/ledgers/"+first.ID+"/members", "", 403)
	owner, err := data.Scoped(ids[0], first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.ChangeMember(ids[1], "editor", 1); err != nil {
		t.Fatal(err)
	}
	csv := "date,type,amount,currency,merchant,source,external_id\n2026-01-03,expense,9.99,CNY,测试,测试,synth-2\n"
	b, _ := json.Marshal(map[string]string{"filename": "test.csv", "csv": csv})
	raw := call(1, first.ID, "POST", "/api/v1/imports/preview", string(b), 200)
	var preview ledger.Preview
	if err = json.Unmarshal([]byte(raw), &preview); err != nil {
		t.Fatal(err)
	}
	commit, _ := json.Marshal(map[string]any{"ledgerVersion": preview.LedgerVersion, "confirmSimilar": false})
	call(2, second.ID, "POST", "/api/v1/imports/"+preview.ID+"/commit", string(commit), 404)
	if err = owner.ChangeMember(ids[1], "remove", 2); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, "POST", "/api/v1/imports/"+preview.ID+"/commit", string(commit), 403)
	call(1, first.ID, "GET", "/api/v1/dashboard", "", 403)
	call(0, first.ID, "POST", "/api/v1/imports/"+preview.ID+"/commit", string(commit), 200)
	call(0, first.ID, "GET", "/api/v1/imports", "", 200)
	call(0, first.ID, "GET", "/api/v1/imports/"+preview.ID, "", 200)
	call(2, second.ID, "GET", "/api/v1/imports/"+preview.ID, "", 404)
	call(1, first.ID, "GET", "/api/v1/imports", "", 403)
	call(1, first.ID, "GET", "/api/v1/imports/"+preview.ID, "", 403)
	call(0, second.ID, "GET", "/api/v1/imports", "", 403)
	call(0, first.ID, "GET", "/api/v1/imports?page=invalid", "", 422)
	call(0, first.ID, "GET", "/api/v1/imports?page=-1", "", 422)

	var d ledger.Dashboard
	raw = call(2, second.ID, "GET", "/api/v1/dashboard?month=2026-01", "", 200)
	if err = json.Unmarshal([]byte(raw), &d); err != nil || d.TotalCount != 1 {
		t.Fatal("cross-ledger leak", err, d)
	}
}
