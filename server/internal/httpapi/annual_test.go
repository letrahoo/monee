package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestAnnualHTTPAuthorizationIsolationAndViewer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.db")
	data, err := ledger.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	access, err := auth.Open(path, []auth.Selector{{Provider: "github", Kind: "subject", Value: "91001"}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	identities := []auth.Identity{{Provider: "github", Subject: "91001", Username: "synthetic-owner"}, {Provider: "github", Subject: "92002", Username: "synthetic-viewer"}, {Provider: "github", Subject: "93003", Username: "synthetic-other"}}
	tokens, ids := []string{}, []string{}
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
		user, e := access.Session(token)
		if e != nil {
			t.Fatal(e)
		}
		tokens = append(tokens, token)
		ids = append(ids, user.ID)
	}
	first, err := data.CreateLedger(ids[0], "合成甲")
	if err != nil {
		t.Fatal(err)
	}
	second, err := data.CreateLedger(ids[2], "合成乙")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := data.Scoped(ids[0], first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Create(ledger.Input{Date: "2026-01-01", Type: "expense", Amount: "12.34", Currency: "CNY", Merchant: "合成", Source: "合成"}, "annual-http-create-001"); err != nil {
		t.Fatal(err)
	}
	api := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}
	// Test the isolated registration before the parent integrates it into Handler.
	mux := http.NewServeMux()
	api.registerAnnual(mux)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if api.Auth.AuthorizeData(w, r) {
			mux.ServeHTTP(w, r)
		}
	})
	call := func(index int, id, query string, want int) string {
		t.Helper()
		headers := map[string]string{"X-Monee-Ledger": id}
		if index >= 0 {
			headers["Authorization"] = "Bearer " + tokens[index]
		}
		w := request(h, "GET", "/api/v1/annual"+query, "", headers)
		if w.Code != want {
			t.Fatalf("got %d want %d: %s", w.Code, want, w.Body.String())
		}
		if w.Code == 200 && (w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json; charset=utf-8") {
			t.Fatal(w.Header())
		}
		return w.Body.String()
	}
	call(-1, first.ID, "?year=2026", 401)
	call(0, second.ID, "?year=2026", 403)
	call(1, first.ID, "?year=2026", 403)
	if err = owner.Invite(ids[1], "viewer"); err != nil {
		t.Fatal(err)
	}
	invitations, err := data.Invitations(ids[1])
	if err != nil || len(invitations) != 1 {
		t.Fatal(err, invitations)
	}
	call(1, first.ID, "?year=2026", 403)
	if err = data.RespondInvitation(ids[1], invitations[0].ID, true); err != nil {
		t.Fatal(err)
	}
	var out ledger.AnnualReport
	raw := call(1, first.ID, "?year=2026", 200)
	if err = json.Unmarshal([]byte(raw), &out); err != nil || out.LedgerID != first.ID || out.TotalCount != 1 || out.ExpenseMinor != "1234" || out.Version != 1 || len(out.Months) != 12 {
		t.Fatal(err, out)
	}
	raw = call(2, second.ID, "?year=2026", 200)
	if err = json.Unmarshal([]byte(raw), &out); err != nil || out.TotalCount != 0 || out.ExpenseMinor != "0" {
		t.Fatal(err, out)
	}
	call(1, first.ID, "?year=2026%20", 422)
	call(1, first.ID, "", 200)
	viewer, err := data.Scoped(ids[1], first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.ChangeMember(ids[1], "remove", 1); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, "?year=2026", 403)
	// A previously scoped store must recheck membership on every read.
	if _, err = viewer.Annual("2026"); err == nil {
		t.Fatal("revoked scoped store remained authorized")
	}
}
