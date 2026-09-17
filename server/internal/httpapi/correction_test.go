package httpapi

import (
	"encoding/json"
	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
	"path/filepath"
	"testing"
)

func TestCorrectionHTTPPreviewCommitHistoryAndValidation(t *testing.T) {
	h, token := apiForTest(t)
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json", "Idempotency-Key": "correction-http-synthetic"}
	w := request(h, "POST", "/api/v1/transactions", `{"date":"2026-09-14","type":"expense","amount":"12.34","currency":"CNY","merchant":"合成更正","source":"合成"}`, headers)
	var original ledger.Transaction
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &original) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	base := "/api/v1/transactions/" + original.ID
	input := `{"version":1,"type":"income","amountMinor":"2000","reason":"合成更正"}`
	for _, route := range []struct{ method, path string }{{"POST", base + "/correction/preview"}, {"PATCH", base + "/correction"}, {"GET", base + "/corrections"}} {
		if w = request(h, route.method, route.path, input, nil); w.Code != 401 {
			t.Fatal("unauthenticated request", w.Code)
		}
	}
	w = request(h, "POST", base+"/correction/preview", input, headers)
	var preview ledger.CorrectionPreview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.NetDeltaMinor != "3234" || preview.Before != original {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/v1/dashboard?month=2026-09", "", headers)
	var d ledger.Dashboard
	if json.Unmarshal(w.Body.Bytes(), &d) != nil || d.ExpenseMinor != "1234" || d.IncomeMinor != "0" {
		t.Fatal("preview mutated data", w.Body.String())
	}
	w = request(h, "PATCH", base+"/correction", input, headers)
	var corrected ledger.Transaction
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &corrected) != nil || corrected != preview.After {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(h, "PATCH", base+"/correction", input, headers); w.Code != 409 {
		t.Fatal("stale input accepted", w.Code)
	}
	w = request(h, "GET", base+"/corrections", "", headers)
	var history []ledger.CorrectionRevision
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &history) != nil || len(history) != 1 || history[0].Before != original || history[0].After != corrected {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, bad := range []struct {
		body string
		code int
	}{
		{`{"version":2,"type":"expense","amountMinor":1,"reason":"x"}`, 400},
		{`{"version":2,"type":"expense","amountMinor":"1.001","reason":"x"}`, 422},
		{`{"version":2,"type":"transfer","amountMinor":"1","reason":"x"}`, 422},
		{`{"version":2,"type":"income","amountMinor":"1","reason":""}`, 422},
		{`{"version":2,"type":"income","amountMinor":"1","reason":"x","incomeDeltaMinor":"999"}`, 400},
	} {
		if w = request(h, "PATCH", base+"/correction", bad.body, headers); w.Code != bad.code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w = request(h, "POST", "/api/v1/transactions/nonexistent/correction/preview", input, headers); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestCorrectionHTTPReadOnlyIsolationAndRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	data, err := ledger.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	identities := []auth.Identity{{Provider: "github", Subject: "1001", Username: "owner"}, {Provider: "github", Subject: "2002", Username: "reader"}}
	access, err := auth.Open(path, []auth.Selector{{Provider: "github", Kind: "subject", Value: "1001"}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	tokens := []string{}
	ids := []string{}
	for i, identity := range identities {
		if err = access.RememberIdentity(identity); err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			if _, err = access.Add(identities[0], auth.Selector{Provider: "github", Kind: "subject", Value: identity.Subject}, ""); err != nil {
				t.Fatal(err)
			}
		}
		token, e := access.CreateSession(identity)
		if e != nil {
			t.Fatal(e)
		}
		tokens = append(tokens, token)
		user, e := access.Session(token)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, user.ID)
	}
	first, err := data.CreateLedger(ids[0], "更正测试甲")
	if err != nil {
		t.Fatal(err)
	}
	second, err := data.CreateLedger(ids[0], "更正测试乙")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := data.Scoped(ids[0], first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.Invite(ids[1], "viewer"); err != nil {
		t.Fatal(err)
	}
	invitations, err := data.Invitations(ids[1])
	if err != nil || len(invitations) != 1 {
		t.Fatal(err)
	}
	if err = data.RespondInvitation(ids[1], invitations[0].ID, true); err != nil {
		t.Fatal(err)
	}
	transaction, err := owner.Create(ledger.Input{Date: "2026-09-14", Type: "expense", Amount: "1", Currency: "CNY", Merchant: "合成", Source: "合成"}, "correction-tenant-test")
	if err != nil {
		t.Fatal(err)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	base := "/api/v1/transactions/" + transaction.ID
	input := `{"version":1,"type":"income","amountMinor":"100","reason":"合成更正"}`
	call := func(user int, ledgerID, method, route string, want int) {
		t.Helper()
		w := request(h, method, route, input, map[string]string{"Authorization": "Bearer " + tokens[user], "X-Monee-Ledger": ledgerID, "Content-Type": "application/json"})
		if w.Code != want {
			t.Fatalf("%s %s: got %d expected %d: %s", method, route, w.Code, want, w.Body.String())
		}
	}
	call(1, first.ID, "POST", base+"/correction/preview", 403)
	call(1, first.ID, "PATCH", base+"/correction", 403)
	call(1, first.ID, "GET", base+"/corrections", 200)
	for _, route := range []struct{ method, path string }{{"POST", base + "/correction/preview"}, {"PATCH", base + "/correction"}, {"GET", base + "/corrections"}} {
		call(0, second.ID, route.method, route.path, 404)
		call(1, second.ID, route.method, route.path, 403)
	}
	if err = owner.ChangeMember(ids[1], "editor", 1); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, "POST", base+"/correction/preview", 200)
	if err = owner.ChangeMember(ids[1], "viewer", 2); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, "PATCH", base+"/correction", 403)
	if err = owner.ChangeMember(ids[1], "remove", 3); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, "GET", base+"/corrections", 403)
	call(0, first.ID, "PATCH", base+"/correction", 200)
}
