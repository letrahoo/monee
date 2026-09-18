package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestRefundCookieRequiresCSRF(t *testing.T) {
	h, token := apiForTest(t)
	w := request(h, "POST", "/api/v1/transactions", `{"date":"2026-09-14","type":"expense","amount":"100","currency":"CNY","merchant":"合成","source":"合成"}`, map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json", "Idempotency-Key": "refund-csrf-original"})
	var original ledger.Transaction
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &original) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	digest := sha256.Sum256([]byte("http://" + host))
	headers := map[string]string{"Cookie": fmt.Sprintf("monee_session_%s=%s", base64.RawURLEncoding.EncodeToString(digest[:])[:12], token), "Content-Type": "application/json", "Idempotency-Key": "refund-csrf-request"}
	base := "/api/v1/transactions/" + original.ID + "/refunds"
	body := `{"version":1,"date":"2026-09-19","amount":"30","currency":"CNY"}`
	if w = request(h, "POST", base, body, headers); w.Code != 403 {
		t.Fatal("missing csrf", w.Code)
	}
	headers["X-Monee-CSRF"] = "invalid"
	if w = request(h, "POST", base, body, headers); w.Code != 403 {
		t.Fatal("invalid csrf", w.Code)
	}
	w = request(h, "GET", "/api/v1/auth/me", "", headers)
	var state struct {
		CSRF string `json:"csrfToken"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil || state.CSRF == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	headers["X-Monee-CSRF"] = state.CSRF
	if w = request(h, "POST", base, body, headers); w.Code != 201 {
		t.Fatal("valid csrf", w.Code, w.Body.String())
	}
}

func TestRefundHTTPValidationRetryAndTotals(t *testing.T) {
	h, token := apiForTest(t)
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json", "Idempotency-Key": "refund-http-original"}
	w := request(h, "POST", "/api/v1/transactions", `{"date":"2026-09-14","type":"expense","amount":"100","currency":"CNY","merchant":"合成消费","source":"合成"}`, headers)
	var original ledger.Transaction
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &original) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	base := "/api/v1/transactions/" + original.ID + "/refunds"
	body := `{"version":1,"date":"2026-10-01","amount":"30","currency":"CNY","account":"合成退款账户"}`
	for _, method := range []string{"GET", "POST"} {
		if w = request(h, method, base, body, nil); w.Code != 401 {
			t.Fatal("anonymous", w.Code)
		}
	}
	headers["Idempotency-Key"] = "refund-http-request-01"
	for i := 0; i < 2; i++ {
		w = request(h, "POST", base, body, headers)
		if w.Code != 201 {
			t.Fatal("write/retry", w.Code, w.Body.String())
		}
	}
	w = request(h, "GET", base, "", headers)
	var summary ledger.RefundSummary
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &summary) != nil || summary.RemainingMinor != "7000" || len(summary.Refunds) != 1 || summary.Original.Version != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/v1/dashboard?month=2026-10", "", headers)
	var d ledger.Dashboard
	if json.Unmarshal(w.Body.Bytes(), &d) != nil || d.ExpenseMinor != "-3000" || d.RefundMinor != "3000" || d.IncomeMinor != "0" || d.Transactions[0].RefundOf != original.ID {
		t.Fatal(w.Body.String())
	}
	headers["Idempotency-Key"] = "refund-http-request-02"
	for _, tc := range []struct {
		body   string
		status int
	}{
		{body, 409},
		{`{"version":2,"date":"2026-10-01","amount":1,"currency":"CNY"}`, 400},
		{`{"version":2,"date":"2026-10-01","amount":"1.001","currency":"CNY"}`, 422},
		{`{"version":2,"date":"2026-10-01","amount":"71","currency":"CNY"}`, 422},
		{`{"version":2,"date":"2026-10-01","amount":"1","currency":"CNY","refundOf":"other"}`, 400},
	} {
		w = request(h, "POST", base, tc.body, headers)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	headers["Origin"] = "https://evil.example"
	if w = request(h, "POST", base, body, headers); w.Code != 403 {
		t.Fatal("foreign origin", w.Code)
	}
}

func TestRefundHTTPMembershipIsolationAndRevokedRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
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
	identities := []auth.Identity{{Provider: "github", Subject: "1001", Username: "owner"}, {Provider: "github", Subject: "2002", Username: "reader"}}
	var tokens, ids []string
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
		u, e := access.Session(token)
		if e != nil || u == nil {
			t.Fatal(e)
		}
		ids = append(ids, u.ID)
	}
	first, err := data.CreateLedger(ids[0], "合成甲")
	if err != nil {
		t.Fatal(err)
	}
	second, err := data.CreateLedger(ids[0], "合成乙")
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
	original, err := owner.Create(ledger.Input{Date: "2026-09-14", Type: "expense", Amount: "100", Currency: "CNY", Merchant: "合成", Source: "合成"}, "refund-tenant-original")
	if err != nil {
		t.Fatal(err)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	base := "/api/v1/transactions/" + original.ID + "/refunds"
	body := `{"version":1,"date":"2026-09-19","amount":"30","currency":"CNY"}`
	call := func(user int, ledgerID, method string, want int) {
		t.Helper()
		w := request(h, method, base, body, map[string]string{"Authorization": "Bearer " + tokens[user], "X-Monee-Ledger": ledgerID, "Content-Type": "application/json", "Idempotency-Key": "refund-tenant-request"})
		if w.Code != want {
			t.Fatalf("%s got %d want %d: %s", method, w.Code, want, w.Body.String())
		}
	}
	call(1, first.ID, "GET", 200)
	call(1, first.ID, "POST", 403)
	call(0, second.ID, "GET", 404)
	call(0, second.ID, "POST", 404)
	call(1, second.ID, "GET", 403)
	call(1, second.ID, "POST", 403)
	if err = owner.ChangeMember(ids[1], "editor", 1); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, "POST", 201)
	call(1, first.ID, "POST", 201)
	if err = owner.ChangeMember(ids[1], "viewer", 2); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, "POST", 403)
	if err = owner.ChangeMember(ids[1], "remove", 3); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, "GET", 403)
	call(1, first.ID, "POST", 403)
	call(0, first.ID, "GET", 200)
}
