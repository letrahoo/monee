package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestImportUndoEndpoints(t *testing.T) {
	h, token := apiForTest(t)
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}
	csv := "date,type,amount,currency,merchant,source,external_id\n2026-09-17,expense,12.34,CNY,合成商户,合成,undo-http-01\n"
	b, _ := json.Marshal(map[string]string{"filename": "synthetic.csv", "csv": csv})
	w := request(h, "POST", "/api/v1/imports/preview", string(b), headers)
	var p ledger.Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.ID == "" {
		t.Fatal(w.Body.String())
	}
	path := "/api/v1/imports/" + p.ID
	b, _ = json.Marshal(map[string]any{"ledgerVersion": p.LedgerVersion})
	if w = request(h, "POST", path+"/commit", string(b), headers); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, action := range []string{"undo", "restore"} {
		for _, method := range []string{"GET", "POST"} {
			suffix := "/" + action
			if method == "GET" {
				suffix += "-preview"
			}
			if w = request(h, method, path+suffix, `{"ledgerVersion":1}`, nil); w.Code != 401 {
				t.Fatal("unauthenticated", w.Code)
			}
		}
	}
	w = request(h, "GET", path+"/undo-preview", "", headers)
	var u ledger.ImportUndoPreview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &u) != nil || u.ChangeCount != 1 || u.ExpenseMinor != "1234" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(h, "POST", path+"/undo", `{"ledgerVersion":0}`, headers); w.Code != 409 {
		t.Fatal("stale", w.Code)
	}
	if w = request(h, "POST", path+"/undo", `{"ledgerVersion":1,"transactionIds":["other"]}`, headers); w.Code != 400 {
		t.Fatal("client scope accepted", w.Code)
	}
	b, _ = json.Marshal(map[string]any{"ledgerVersion": u.LedgerVersion})
	w = request(h, "POST", path+"/undo", string(b), headers)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &u) != nil || u.State != "undone" || u.AlreadyApplied {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "POST", path+"/undo", string(b), headers)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &u) != nil || !u.AlreadyApplied {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/v1/dashboard?month=2026-09", "", headers)
	var d ledger.Dashboard
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil || d.TotalCount != 0 {
		t.Fatal(w.Body.String())
	}
	w = request(h, "GET", "/api/v1/transactions/"+p.Rows[0].Record.ID+"/sources", "", headers)
	var sources []ledger.TransactionSource
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &sources) != nil || len(sources) != 1 {
		t.Fatal("source evidence lost", w.Body.String())
	}
	w = request(h, "GET", path+"/restore-preview", "", headers)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &u) != nil || u.ChangeCount != 1 {
		t.Fatal(w.Body.String())
	}
	b, _ = json.Marshal(map[string]any{"ledgerVersion": u.LedgerVersion})
	w = request(h, "POST", path+"/restore", string(b), headers)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &u) != nil || u.State != "committed" {
		t.Fatal(w.Body.String())
	}
	w = request(h, "GET", "/api/v1/dashboard?month=2026-09", "", headers)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil || d.TotalCount != 1 || d.ExpenseMinor != "1234" {
		t.Fatal(w.Body.String())
	}
}

func TestImportUndoAuthorizationAndTenantIsolation(t *testing.T) {
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
	p, e := owner.Preview("synthetic.csv", "date,type,amount,currency,merchant,source,external_id\n2026-09-17,expense,1.00,CNY,合成,合成,undo-auth-1\n")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = owner.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	call := func(who int, ledgerID, method, suffix string, want int) {
		t.Helper()
		w := request(h, method, "/api/v1/imports/"+p.ID+suffix, `{"ledgerVersion":1}`, map[string]string{"Authorization": "Bearer " + tokens[who], "Content-Type": "application/json", "X-Monee-Ledger": ledgerID})
		if w.Code != want {
			t.Fatalf("%s %s: %d wanted %d: %s", method, suffix, w.Code, want, w.Body.String())
		}
	}
	for _, action := range []string{"undo", "restore"} {
		call(1, first.ID, http.MethodGet, "/"+action+"-preview", 200)
		call(1, first.ID, http.MethodPost, "/"+action, 403)
		call(2, second.ID, http.MethodGet, "/"+action+"-preview", 404)
		call(2, second.ID, http.MethodPost, "/"+action, 404)
		call(0, second.ID, http.MethodGet, "/"+action+"-preview", 403)
	}
	if e = owner.ChangeMember(ids[1], "remove", 1); e != nil {
		t.Fatal(e)
	}
	call(1, first.ID, http.MethodGet, "/undo-preview", 403)
	call(1, first.ID, http.MethodPost, "/undo", 403)
	if d, e := owner.Dashboard("2026-09", "", 1); e != nil || d.TotalCount != 1 {
		t.Fatal("unauthorized action changed ledger", d, e)
	}
}
