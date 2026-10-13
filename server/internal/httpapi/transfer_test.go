package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

const transferBody = `{"date":"2026-09-19","amount":"100","currency":"CNY","fromAccount":"合成银行","toAccount":"合成钱包","note":"合成转账"}`

func TestTransferHTTPValidationRetryAndTotals(t *testing.T) {
	h, token := apiForTest(t)
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json", "Idempotency-Key": "transfer-http-request"}
	if w := request(h, "POST", "/api/v1/transfers", transferBody, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	var first ledger.Transaction
	for i := 0; i < 2; i++ {
		w := request(h, "POST", "/api/v1/transfers", transferBody, headers)
		var got ledger.Transaction
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatal("retry changed response")
		}
	}
	if first.Type != "transfer" || first.AmountMinor != "10000" || first.Account != "合成银行" || first.ToAccount != "合成钱包" {
		t.Fatal(first)
	}
	w := request(h, "GET", "/api/v1/dashboard?month=2026-09&q=合成钱包", "", headers)
	var d ledger.Dashboard
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil || d.IncomeMinor != "0" || d.ExpenseMinor != "0" || d.GrossExpenseMinor != "0" || d.RefundMinor != "0" || d.TotalCount != 1 || d.FilteredCount != 1 || len(d.Categories) != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(h, "POST", "/api/v1/transfers", strings.Replace(transferBody, `"100"`, `"101"`, 1), headers); w.Code != 409 {
		t.Fatal("key conflict", w.Code, w.Body.String())
	}
	headers["Idempotency-Key"] = "transfer-http-invalid"
	for _, tc := range []struct {
		body   string
		status int
	}{
		{strings.Replace(transferBody, `"100"`, `100`, 1), 400},
		{strings.Replace(transferBody, `"100"`, `"0"`, 1), 422},
		{strings.Replace(transferBody, `"100"`, `"1.001"`, 1), 422},
		{strings.Replace(transferBody, `"100"`, `"10000000000.01"`, 1), 422},
		{strings.Replace(transferBody, `"合成钱包"`, `"合成银行"`, 1), 422},
		{strings.Replace(transferBody, `"合成钱包"`, `""`, 1), 422},
		{strings.Replace(transferBody, `"合成钱包"`, `"待核实资金账户"`, 1), 422},
		{strings.Replace(transferBody, `"CNY"`, `"USD"`, 1), 422},
		{strings.Replace(transferBody, `"note":`, `"refundOf":`, 1), 400},
	} {
		if w = request(h, "POST", "/api/v1/transfers", tc.body, headers); w.Code != tc.status {
			t.Fatal(w.Code, tc.status, w.Body.String())
		}
	}
	headers["Origin"] = "https://evil.example"
	if w = request(h, "POST", "/api/v1/transfers", transferBody, headers); w.Code != 403 {
		t.Fatal("foreign origin", w.Code)
	}
}

func TestTransferCookieRequiresCSRF(t *testing.T) {
	h, token := apiForTest(t)
	digest := sha256.Sum256([]byte("http://" + host))
	headers := map[string]string{"Cookie": fmt.Sprintf("monee_session_%s=%s", base64.RawURLEncoding.EncodeToString(digest[:])[:12], token), "Content-Type": "application/json", "Idempotency-Key": "transfer-csrf-request"}
	for _, csrf := range []string{"", "invalid"} {
		headers["X-Monee-CSRF"] = csrf
		if w := request(h, "POST", "/api/v1/transfers", transferBody, headers); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	w := request(h, "GET", "/api/v1/auth/me", "", headers)
	var state struct {
		CSRF string `json:"csrfToken"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil || state.CSRF == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	headers["X-Monee-CSRF"] = state.CSRF
	if w = request(h, "POST", "/api/v1/transfers", transferBody, headers); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestTransferHTTPMembershipIsolationAndRevokedRetry(t *testing.T) {
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
	first, err := data.CreateLedger(ids[0], "转账合成甲")
	if err != nil {
		t.Fatal(err)
	}
	second, err := data.CreateLedger(ids[0], "转账合成乙")
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
	invites, err := data.Invitations(ids[1])
	if err != nil || len(invites) != 1 {
		t.Fatal(err)
	}
	if err = data.RespondInvitation(ids[1], invites[0].ID, true); err != nil {
		t.Fatal(err)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	call := func(user int, ledgerID string, want int) ledger.Transaction {
		t.Helper()
		w := request(h, "POST", "/api/v1/transfers", transferBody, map[string]string{"Authorization": "Bearer " + tokens[user], "X-Monee-Ledger": ledgerID, "Content-Type": "application/json", "Idempotency-Key": "transfer-tenant-request"})
		if w.Code != want {
			t.Fatal(w.Code, want, w.Body.String())
		}
		var result ledger.Transaction
		if want == 201 {
			if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	call(1, first.ID, 403)
	call(1, second.ID, 403)
	if err = owner.ChangeMember(ids[1], "editor", 1); err != nil {
		t.Fatal(err)
	}
	x := call(1, first.ID, 201)
	if y := call(1, first.ID, 201); y != x {
		t.Fatal("replay differs")
	}
	if y := call(0, second.ID, 201); y.ID == x.ID {
		t.Fatal("idempotency leaked across ledgers")
	}
	if err = owner.ChangeMember(ids[1], "viewer", 2); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, 403)
	if err = owner.ChangeMember(ids[1], "remove", 3); err != nil {
		t.Fatal(err)
	}
	call(1, first.ID, 403)
	if y := call(0, first.ID, 201); y.ID != x.ID {
		t.Fatal("owner retry lost original")
	}
}
