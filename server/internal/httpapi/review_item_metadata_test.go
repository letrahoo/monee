package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestReviewItemMetadataEndpointAndStrictParameters(t *testing.T) {
	h, token := apiForTest(t)
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}
	body, err := json.Marshal(map[string]string{"filename": "synthetic-metadata.csv", "account": "合成钱包", "contentBase64": base64.StdEncoding.EncodeToString([]byte(reviewQueueCSV))})
	if err != nil {
		t.Fatal(err)
	}
	w := request(h, "POST", "/api/v1/imports/alipay/preview", string(body), headers)
	var preview ledger.Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || len(preview.Pending) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	endpoint := fmt.Sprintf("/api/v1/imports/%s/review-items/%d/metadata", preview.ID, preview.Pending[0].Line)
	if w = request(h, "GET", endpoint, "", nil); w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("anonymous source disclosure/cache policy", w.Code)
	}
	invalidAnonymous := fmt.Sprintf("/api/v1/imports/%s/review-items/0/metadata?unused=1", preview.ID)
	if w = request(h, "GET", invalidAnonymous, "", nil); w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("parameter error exposed before authentication", w.Code)
	}
	before := request(h, "GET", "/api/v1/dashboard?month=2026-09", "", headers).Body.String()
	queue := request(h, "GET", "/api/v1/review-queue", "", headers).Body.String()
	importBefore := request(h, "GET", "/api/v1/imports/"+preview.ID, "", headers).Body.String()
	w = request(h, "GET", endpoint, "", headers)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	var out ledger.ReviewItemMetadata
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ContractVersion != 1 || !out.ReadOnly || out.Actions == nil || len(out.Actions) != 0 || out.ImportID != preview.ID || out.Line != preview.Pending[0].Line || out.Classification != "special" || len(out.Blockers) == 0 {
		t.Fatalf("unsafe metadata contract: %+v", out)
	}
	for _, secret := range []string{"synthetic-review-01", "synthetic-order-01", "synthetic-metadata.csv", "raw_csv", "contentBase64", "\"raw\":", "\"sourceDocument\":"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("unrequested source envelope/identifier exposed", secret)
		}
	}
	original := w.Body.String()
	if w = request(h, "GET", endpoint, "", headers); w.Code != 200 || w.Body.String() != original {
		t.Fatal("repeated GET unstable", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"?unused=1", "?line=2", "?page=1", "?x=1&x=2"} {
		if w = request(h, "GET", endpoint+suffix, "", headers); w.Code != 422 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(suffix, w.Code)
		}
	}
	for _, line := range []string{"0", "-1", "01", "+1", "1000001", "9999999999999999999999999", "not-a-line"} {
		path := fmt.Sprintf("/api/v1/imports/%s/review-items/%s/metadata", preview.ID, line)
		if w = request(h, "GET", path, "", headers); w.Code != 422 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(line, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{fmt.Sprintf("/api/v1/imports/%s/review-items/999999/metadata", preview.ID), "/api/v1/imports/missing/review-items/2/metadata"} {
		if w = request(h, "GET", path, "", headers); w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(path, w.Code)
		}
	}
	if w = request(h, "POST", endpoint, "{}", headers); w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
		t.Fatal("mutation route unexpectedly present", w.Code)
	}
	if got := request(h, "GET", "/api/v1/dashboard?month=2026-09", "", headers).Body.String(); got != before {
		t.Fatal("metadata changed financial dashboard")
	}
	if got := request(h, "GET", "/api/v1/review-queue", "", headers).Body.String(); got != queue {
		t.Fatal("metadata changed review queue")
	}
	if got := request(h, "GET", "/api/v1/imports/"+preview.ID, "", headers).Body.String(); got != importBefore {
		t.Fatal("metadata changed stored preview/import evidence")
	}
}

func TestReviewItemMetadataPerRequestRolesAdmissionAndTenantIsolation(t *testing.T) {
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
	identities := []auth.Identity{{Provider: "github", Subject: "1001", Username: "owner"}, {Provider: "github", Subject: "2002", Username: "viewer"}, {Provider: "github", Subject: "3003", Username: "editor"}, {Provider: "github", Subject: "4004", Username: "other"}}
	var tokens, ids []string
	var editorEntry auth.Entry
	for i, identity := range identities {
		if err = access.RememberIdentity(identity); err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			entry, e := access.Add(identities[0], auth.Selector{Provider: "github", Kind: "subject", Value: identity.Subject}, "")
			if e != nil {
				t.Fatal(e)
			}
			if i == 2 {
				editorEntry = entry
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
	first, err := data.CreateLedger(ids[0], "合成来源甲")
	if err != nil {
		t.Fatal(err)
	}
	second, err := data.CreateLedger(ids[3], "合成来源乙")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := data.Scoped(ids[0], first.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, role := range map[int]string{1: "viewer", 2: "editor"} {
		if err = owner.Invite(ids[i], role); err != nil {
			t.Fatal(err)
		}
		invitations, e := data.Invitations(ids[i])
		if e != nil || len(invitations) != 1 {
			t.Fatal(e, invitations)
		}
		if err = data.RespondInvitation(ids[i], invitations[0].ID, true); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := owner.PreviewAlipay("synthetic.csv", base64.StdEncoding.EncodeToString([]byte(reviewQueueCSV)), "wallet")
	if err != nil || len(preview.Pending) != 1 {
		t.Fatal(err, preview)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	endpoint := fmt.Sprintf("/api/v1/imports/%s/review-items/%d/metadata", preview.ID, preview.Pending[0].Line)
	call := func(who int, ledgerID string, want int) {
		t.Helper()
		w := request(h, "GET", endpoint, "", map[string]string{"Authorization": "Bearer " + tokens[who], "X-Monee-Ledger": ledgerID})
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("who%d got%d want%d: %s", who, w.Code, want, w.Body.String())
		}
	}
	call(0, first.ID, 200)
	call(1, first.ID, 200)
	call(2, first.ID, 200)
	call(3, second.ID, 404) // Existing import in another authorized ledger is not disclosed.
	call(3, first.ID, 403)
	call(0, second.ID, 403) // A system admin is not automatically a member.
	if err = owner.ChangeMember(ids[1], "remove", 1); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, 403)
	invalid := fmt.Sprintf("/api/v1/imports/%s/review-items/0/metadata?unused=1", preview.ID)
	w := request(h, "GET", invalid, "", map[string]string{"Authorization": "Bearer " + tokens[1], "X-Monee-Ledger": first.ID})
	if w.Code != 403 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("revocation must precede parameter disclosure", w.Code)
	}
	if _, err = access.SetEnabled(identities[0], editorEntry.ID, false, editorEntry.Version); err != nil {
		t.Fatal(err)
	}
	call(2, first.ID, 403)
	if d, e := owner.Dashboard("2026-09", "", 1); e != nil || d.TotalCount != 0 {
		t.Fatal("metadata wrote financial data", d, e)
	}
}
