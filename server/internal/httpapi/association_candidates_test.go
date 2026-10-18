package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestAssociationCandidatesHTTPReadOnlyScopeAndContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-association.db")
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
	identities := []auth.Identity{{Provider: "github", Subject: "1001", Username: "synthetic-owner"}, {Provider: "github", Subject: "2002", Username: "synthetic-viewer"}, {Provider: "github", Subject: "3003", Username: "synthetic-editor"}, {Provider: "github", Subject: "4004", Username: "synthetic-other"}}
	var tokens, ids []string
	var editorEntry auth.Entry
	for i, identity := range identities {
		if e = access.RememberIdentity(identity); e != nil {
			t.Fatal(e)
		}
		if i > 0 {
			entry, err := access.Add(identities[0], auth.Selector{Provider: "github", Kind: "subject", Value: identity.Subject}, "")
			if err != nil {
				t.Fatal(err)
			}
			if i == 2 {
				editorEntry = entry
			}
		}
		token, err := access.CreateSession(identity)
		if err != nil {
			t.Fatal(err)
		}
		u, err := access.Session(token)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
		ids = append(ids, u.ID)
	}
	first, e := data.CreateLedger(ids[0], "合成相似甲")
	if e != nil {
		t.Fatal(e)
	}
	second, e := data.CreateLedger(ids[3], "合成相似乙")
	if e != nil {
		t.Fatal(e)
	}
	owner, e := data.Scoped(ids[0], first.ID)
	if e != nil {
		t.Fatal(e)
	}
	for i, role := range map[int]string{1: "viewer", 2: "editor"} {
		if e = owner.Invite(ids[i], role); e != nil {
			t.Fatal(e)
		}
		invites, err := data.Invitations(ids[i])
		if err != nil || len(invites) != 1 {
			t.Fatal(err, invites)
		}
		if e = data.RespondInvitation(ids[i], invites[0].ID, true); e != nil {
			t.Fatal(e)
		}
	}
	viewerScope, e := data.Scoped(ids[1], first.ID)
	if e != nil {
		t.Fatal(e)
	}
	editorScope, e := data.Scoped(ids[2], first.ID)
	if e != nil {
		t.Fatal(e)
	}
	input := ledger.Input{Date: "2026-09-14", Type: "expense", Amount: "12.34", Currency: "CNY", Merchant: "合成相似商户", Source: "支付宝", Account: "synthetic-account-hidden", ExternalID: "synthetic-trace-hidden-base", Note: "synthetic-note-hidden"}
	base, e := owner.Create(input, "association-http-base")
	if e != nil {
		t.Fatal(e)
	}
	input.Source = "银行卡"
	input.ExternalID = "synthetic-trace-hidden-other"
	other, e := owner.Create(input, "association-http-other")
	if e != nil {
		t.Fatal(e)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	endpoint := "/api/v1/transactions/" + base.ID + "/association-candidates"
	headers := func(i int, l string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + tokens[i], "X-Monee-Ledger": l}
	}
	call := func(i int, l, suffix string, want int) ledger.AssociationCandidates {
		t.Helper()
		w := request(h, "GET", endpoint+suffix, "", headers(i, l))
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s actor%d got%d want%d: %s", suffix, i, w.Code, want, w.Body.String())
		}
		var p ledger.AssociationCandidates
		if want == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			for _, hidden := range []string{"synthetic-trace-hidden", "synthetic-account-hidden", "synthetic-note-hidden", `"externalId"`, `"account"`, `"note"`, `"raw"`, `"confidence"`, `"allowedConfirm"`} {
				if strings.Contains(w.Body.String(), hidden) {
					t.Fatal("DTO exposes source/private fields or unsupported permission", hidden)
				}
			}
		}
		return p
	}
	before, e := owner.Dashboard("2026-09", "", 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range []int{0, 1, 2} {
		p := call(i, first.ID, "", 200)
		if p.TotalCount != 1 || len(p.Items) != 1 || p.Items[0].Transaction.ID != other.ID || p.Base.ID != base.ID || p.SourceRolesVerified || !p.ReadOnly || p.Basis != "current_transaction_similarity" || p.Actions == nil || len(p.Actions) != 0 {
			t.Fatal("bad shared read contract", p)
		}
	}
	for _, suffix := range []string{"?page=0", "?page=-1", "?page=01", "?page=", "?page=1&page=1", "?page=1000001", "?page=9999999999999999999999", "?page=+1", "?page=%2B1", "?page=%zz", "?page=1;page=2", "?unknown=1"} {
		call(0, first.ID, suffix, 422)
		w := request(h, "GET", endpoint+suffix, "", nil)
		if w.Code != 401 {
			t.Fatal("query was parsed before normal authentication", suffix, w.Code)
		}
	}
	call(0, second.ID, "?page=0", 403)
	call(3, first.ID, "?page=0", 403)
	call(3, second.ID, "", 404)
	if w := request(h, "GET", "/api/v1/transactions/nonexistent/association-candidates", "", headers(0, first.ID)); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(h, "POST", endpoint, "{}", headers(0, first.ID)); w.Code != 404 && w.Code != 405 {
		t.Fatal("unexpected association write route", w.Code)
	}
	if e = owner.ChangeMember(ids[1], "remove", 1); e != nil {
		t.Fatal(e)
	}
	call(1, first.ID, "?page=0", 403)
	if _, e = viewerScope.TransactionAssociationCandidates(base.ID, 1); e == nil {
		t.Fatal("stale viewer store bypassed membership revocation")
	}
	if _, e = access.SetEnabled(identities[0], editorEntry.ID, false, editorEntry.Version); e != nil {
		t.Fatal(e)
	}
	call(2, first.ID, "?page=0", 403)
	if _, e = editorScope.TransactionAssociationCandidates(base.ID, 1); e == nil {
		t.Fatal("stale editor store bypassed disabled admission")
	}
	after, e := owner.Dashboard("2026-09", "", 1)
	if e != nil || !reflect.DeepEqual(before, after) || after.TotalCount != 2 || after.ExpenseMinor != "2468" {
		t.Fatal("query reduced apparent duplicate spending or mutated finance", before, after, e)
	}
	input.Type = "income"
	input.Source = "合成收入"
	input.ExternalID = "synthetic-income"
	income, e := owner.Create(input, "association-http-income")
	if e != nil {
		t.Fatal(e)
	}
	w := request(h, "GET", fmt.Sprintf("/api/v1/transactions/%s/association-candidates", income.ID), "", headers(0, first.ID))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatal("unsupported base collapsed into ordinary empty", w.Code, w.Body.String())
	}
}
