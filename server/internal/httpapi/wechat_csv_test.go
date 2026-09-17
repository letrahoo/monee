package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestWeChatCSVRequiresCurrentLedgerWriteMembership(t *testing.T) {
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
	identities := []auth.Identity{{Provider: "github", Subject: "1001", Username: "owner"}, {Provider: "github", Subject: "2002", Username: "viewer"}, {Provider: "github", Subject: "3003", Username: "outsider"}}
	tokens, ids := []string{}, []string{}
	for i, identity := range identities {
		if err := access.RememberIdentity(identity); err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			if _, err := access.Add(identities[0], auth.Selector{Provider: "github", Kind: "subject", Value: identity.Subject}, ""); err != nil {
				t.Fatal(err)
			}
		}
		token, err := access.CreateSession(identity)
		if err != nil {
			t.Fatal(err)
		}
		user, err := access.Session(token)
		if err != nil {
			t.Fatal(err)
		}
		tokens, ids = append(tokens, token), append(ids, user.ID)
	}
	book, err := data.CreateLedger(ids[0], "合成微信CSV")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := data.Scoped(ids[0], book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Invite(ids[1], "viewer"); err != nil {
		t.Fatal(err)
	}
	invitations, err := data.Invitations(ids[1])
	if err != nil || len(invitations) != 1 {
		t.Fatal(invitations, err)
	}
	if err := data.RespondInvitation(ids[1], invitations[0].ID, true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.csv")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"filename": "wechat.csv", "account": "wallet", "contentBase64": base64.StdEncoding.EncodeToString(raw)})
	if err != nil {
		t.Fatal(err)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	call := func(token string, want int) {
		t.Helper()
		headers := map[string]string{"Content-Type": "application/json", "X-Monee-Ledger": book.ID}
		if token != "" {
			headers["Authorization"] = "Bearer " + token
		}
		w := request(h, "POST", "/api/v1/imports/wechat/preview", string(body), headers)
		if w.Code != want {
			t.Fatalf("got %d want %d: %s", w.Code, want, w.Body.String())
		}
	}
	call("", 401)
	call(tokens[1], 403)
	call(tokens[2], 403)
	if history, err := owner.ImportHistory(1); err != nil || history.TotalCount != 0 {
		t.Fatal("denied calls retained private evidence", history, err)
	}
	if err := owner.ChangeMember(ids[1], "editor", 1); err != nil {
		t.Fatal(err)
	}
	call(tokens[1], 200)
	if err := owner.ChangeMember(ids[1], "remove", 2); err != nil {
		t.Fatal(err)
	}
	call(tokens[1], 403)
	if history, err := owner.ImportHistory(1); err != nil || history.TotalCount != 1 {
		t.Fatal(history, err)
	}
	if d, err := owner.Dashboard("", "", 1); err != nil || d.TotalCount != 0 || d.ExpenseMinor != "0" || d.IncomeMinor != "0" {
		t.Fatal("preview or denied call wrote money", d, err)
	}
}
