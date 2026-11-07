package httpapi

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

// Exercise real HTTP codecs/authorization over synthetic native imports. The
// isolated open connection is marked6 directly; normal Open still refuses6.
func TestExpensePairHTTPEnabledLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.db")
	root, e := ledger.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	access, e := auth.Open(path, []auth.Selector{{Provider: "github", Kind: "subject", Value: "1001"}})
	if e != nil {
		t.Fatal(e)
	}
	defer access.Close()
	identity := auth.Identity{Provider: "github", Subject: "1001", Username: "synthetic-pair-admin"}
	if e = access.RememberIdentity(identity); e != nil {
		t.Fatal(e)
	}
	token, e := access.CreateSession(identity)
	if e != nil {
		t.Fatal(e)
	}
	user, e := access.Session(token)
	if e != nil || user == nil {
		t.Fatal(e)
	}
	if _, e = root.CreateLedger(user.ID, "合成关联API账本"); e != nil {
		t.Fatal(e)
	}
	h := API{Store: root, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json", "Idempotency-Key": "http-expense-pair-confirm-key"}
	call := func(method, path string, input any, out any, intended int) {
		t.Helper()
		body := ""
		if input != nil {
			b, e := json.Marshal(input)
			if e != nil {
				t.Fatal(e)
			}
			body = string(b)
		}
		w := request(h, method, path, body, headers)
		if w.Code != intended {
			t.Fatal(method, path, w.Code, w.Body.String())
		}
		if out != nil && json.Unmarshal(w.Body.Bytes(), out) != nil {
			t.Fatal(w.Body.String())
		}
	}
	for i := 0; i < 2; i++ {
		raw := fmt.Sprintf("交易时间,交易分类,交易对方,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号\n2026-09-15 10:00:00,餐饮美食,合成咖啡店,早餐,支出,12.34,余额,交易成功,pair-pay-%d,pair-order-%d\n", i, i)
		var p ledger.Preview
		call("POST", "/api/v1/imports/alipay/preview", map[string]string{"filename": "native.csv", "account": "synthetic-wallet", "contentBase64": base64.StdEncoding.EncodeToString([]byte(raw))}, &p, 200)
		var result ledger.CommitResult
		call("POST", "/api/v1/imports/"+p.ID+"/commit", map[string]any{"ledgerVersion": p.LedgerVersion, "confirmSimilar": true}, &result, 200)
		if result.Added != 1 {
			t.Fatal(result)
		}
	}
	var d ledger.Dashboard
	call("GET", "/api/v1/dashboard?month=2026-09", nil, &d, 200)
	if len(d.Transactions) != 2 {
		t.Fatal(d)
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("PRAGMA user_version=6"); e != nil {
		db.Close()
		t.Fatal(e)
	}
	db.Close()
	input := ledger.ExpensePairConfirmInput{LedgerVersion: d.Version, PrimaryTransactionID: d.Transactions[0].ID, SecondaryTransactionID: d.Transactions[1].ID, PrimaryVersion: d.Transactions[0].Version, SecondaryVersion: d.Transactions[1].Version, Reason: "合成同笔消费确认"}
	previewInput := ledger.ExpensePairPreviewInput{LedgerVersion: input.LedgerVersion, PrimaryTransactionID: input.PrimaryTransactionID, SecondaryTransactionID: input.SecondaryTransactionID, PrimaryVersion: input.PrimaryVersion, SecondaryVersion: input.SecondaryVersion}
	var preview ledger.ExpensePairPreview
	call("POST", "/api/v1/expense-pairs/preview", previewInput, &preview, 200)
	if preview.PairGrossDeltaMinor != "-1234" {
		t.Fatal(preview)
	}
	var receipt, replay ledger.ExpensePairReceipt
	call("POST", "/api/v1/expense-pairs", input, &receipt, 201)
	call("POST", "/api/v1/expense-pairs", input, &replay, 201)
	if receipt != replay {
		t.Fatal(receipt, replay)
	}
	call("GET", "/api/v1/dashboard?month=2026-09", nil, &d, 200)
	if d.TotalCount != 1 || d.ExpenseMinor != "1234" {
		t.Fatal(d)
	}
	var detail ledger.ExpensePairDetail
	call("GET", "/api/v1/transactions/"+input.PrimaryTransactionID+"/expense-pairs", nil, &detail, 200)
	if len(detail.Decisions) != 1 || detail.Decisions[0] != receipt {
		t.Fatal(detail)
	}
	headers["Idempotency-Key"] = "http-expense-pair-withdraw-key"
	var withdrawn ledger.ExpensePairReceipt
	call("POST", "/api/v1/expense-pairs/"+receipt.ID+"/withdraw", map[string]any{"ledgerVersion": receipt.LedgerVersion, "decisionVersion": receipt.Version, "reason": "合成撤回"}, &withdrawn, 200)
	if withdrawn.Status != "retracted" {
		t.Fatal(withdrawn)
	}
	call("GET", "/api/v1/dashboard?month=2026-09", nil, &d, 200)
	if d.TotalCount != 2 || d.ExpenseMinor != "2468" {
		t.Fatal(d)
	}
}
