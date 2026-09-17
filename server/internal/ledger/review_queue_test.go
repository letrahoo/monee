package ledger

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestReviewQueueRetainsQuestionsAcrossBatchStates(t *testing.T) {
	s := testStore(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense+nativeRefund), "synthetic-wallet")
	q, e := s.ReviewQueue(1, "")
	if e != nil || q.TotalCount != 1 || len(q.Items) != 1 {
		t.Fatal(q, e)
	}
	item := q.Items[0]
	if item.BatchState != "preview" || item.SourceAccount != "synthetic-wallet" || item.ImportID != p.ID || item.Line != 3 || item.ID != p.ID+":3" || item.Amount != "20.00" || item.Date != "2026-09-13 10:00:00" || item.Merchant != "合成退款商户" || item.Reason != p.Pending[0].Reason || item.Raw["交易订单号"] != "pay-3" || item.Raw["交易状态"] != "退款成功" {
		t.Fatal("source evidence mismatch", item)
	}
	if _, e = s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	q, e = s.ReviewQueue(1, "")
	if e != nil || q.TotalCount != 1 || q.Items[0].BatchState != "committed" {
		t.Fatal(q, e)
	}
	if dashboard(t, s).TotalCount != 1 {
		t.Fatal("ordinary source row not booked")
	}
	u, e := s.PreviewImportUndo(p.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UndoImport(p.ID, u.LedgerVersion); e != nil {
		t.Fatal(e)
	}
	q, e = s.ReviewQueue(1, "")
	if e != nil || q.TotalCount != 1 || q.Items[0].BatchState != "undone" || q.Items[0].ID != item.ID || q.Items[0].Raw["交易订单号"] != "pay-3" {
		t.Fatal(q, e)
	}
	r, e := s.PreviewImportUndo(p.ID, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RestoreImport(p.ID, r.LedgerVersion); e != nil {
		t.Fatal(e)
	}
	q, e = s.ReviewQueue(1, "")
	if e != nil || q.TotalCount != 1 || q.Items[0].BatchState != "committed" {
		t.Fatal(q, e)
	}
	before := map[string]int{}
	for _, table := range []string{"transactions", "imports", "source_records", "change_log"} {
		before[table] = count(t, s, table)
	}
	version := dashboard(t, s).Version
	var beforeJSON string
	if e = s.db.QueryRow("SELECT preview_json FROM imports WHERE id=?", p.ID).Scan(&beforeJSON); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReviewQueue(1, "alipay"); e != nil {
		t.Fatal(e)
	}
	for table, n := range before {
		if count(t, s, table) != n {
			t.Fatal("read mutated", table)
		}
	}
	var afterJSON string
	if e = s.db.QueryRow("SELECT preview_json FROM imports WHERE id=?", p.ID).Scan(&afterJSON); e != nil {
		t.Fatal(e)
	}
	if beforeJSON != afterJSON || dashboard(t, s).Version != version {
		t.Fatal("read changed source or ledger version")
	}
}
func TestReviewQueueAllPendingAndFailedBatches(t *testing.T) {
	s := testStore(t)
	only := alipayPreview(t, s, []byte(nativeHeader+nativeRefund), "wallet")
	if _, e := s.Commit(only.ID, only.LedgerVersion, false); e == nil {
		t.Fatal("all pending committed")
	}
	q, e := s.ReviewQueue(1, "alipay")
	if e != nil || q.TotalCount != 1 || q.Items[0].BatchState != "preview" {
		t.Fatal(q, e)
	}
	base := alipayPreview(t, s, []byte(nativeHeader+nativeExpense), "wallet")
	if _, e = s.Commit(base.ID, base.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	failed := alipayPreview(t, s, []byte(nativeHeader+strings.Replace(nativeExpense, "12.34", "12.35", 1)+nativeRefund), "wallet")
	if failed.ID == "" || len(failed.Errors) == 0 || len(failed.Pending) != 1 {
		t.Fatal("failed fixture invalid", failed)
	}
	if _, e = s.PreviewWeChat("broken.xlsx", base64.StdEncoding.EncodeToString([]byte("broken")), "wallet"); e != nil {
		t.Fatal(e)
	}
	q, e = s.ReviewQueue(1, "")
	if e != nil || q.TotalCount != 2 {
		t.Fatal(q, e)
	}
	states := map[string]string{}
	for _, i := range q.Items {
		states[i.ImportID] = i.BatchState
	}
	if states[failed.ID] != "failed" || states[only.ID] != "preview" {
		t.Fatal(states)
	}
	if len(q.Items) != 2 || q.Items[0].ID == q.Items[1].ID {
		t.Fatal("different-file questions deduplicated")
	}
}
func TestReviewQueueFormatsPaginationAndUnroundedEvidence(t *testing.T) {
	s := testStore(t)
	lines := nativeHeader
	for i := 0; i < 51; i++ {
		lines += strings.ReplaceAll(nativeRefund, "pay-3", fmt.Sprintf("refund-%03d", i))
	}
	p := alipayPreview(t, s, []byte(lines), "wallet")
	if len(p.Errors) > 0 || len(p.Pending) != 51 {
		t.Fatal(p)
	}
	raw, e := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	wx, e := s.PreviewWeChat("synthetic.xlsx", base64.StdEncoding.EncodeToString(raw), "wallet")
	if e != nil {
		t.Fatal(e)
	}
	first, e := s.ReviewQueue(1, "alipay")
	if e != nil || first.PageSize != 50 || first.TotalCount != 51 || len(first.Items) != 50 || first.Items[0].Line != 2 || first.Items[49].Line != 51 {
		t.Fatal(first, e)
	}
	second, e := s.ReviewQueue(2, "alipay")
	if e != nil || second.TotalCount != 51 || len(second.Items) != 1 || second.Items[0].Line != 52 {
		t.Fatal(second, e)
	}
	empty, e := s.ReviewQueue(3, "alipay")
	if e != nil || len(empty.Items) != 0 || empty.TotalCount != 51 {
		t.Fatal(empty, e)
	}
	all, e := s.ReviewQueue(1, "")
	if e != nil || all.TotalCount != 54 {
		t.Fatal(all, e)
	}
	wechat, e := s.ReviewQueue(1, "wechat")
	if e != nil || wechat.TotalCount != 3 || len(wechat.Items) != 3 {
		t.Fatal(wechat, e)
	}
	var precise bool
	for _, item := range wechat.Items {
		if item.SourceAccount != "wallet" || item.ImportID != wx.ID || item.Format != "wechat" || item.Line == 2 {
			t.Fatal("ordinary source row leaked", item)
		}
		if item.Amount == "1.001" && item.Raw["金额(元)"] == "1.001" {
			precise = true
		}
	}
	if !precise {
		t.Fatal("invalid source amount rounded or discarded", wechat)
	}
	for _, page := range []int{0, -1, 1000001} {
		if _, e = s.ReviewQueue(page, ""); e == nil {
			t.Fatal("invalid page accepted", page)
		}
	}
	for _, format := range []string{"standard", "bank", "ALIPAY", "' OR 1=1 --"} {
		if _, e = s.ReviewQueue(1, format); e == nil {
			t.Fatal("invalid format accepted", format)
		}
	}
	encoded, e := json.Marshal(empty)
	if e != nil || strings.Contains(string(encoded), `"items":null`) {
		t.Fatal("empty result is not array", string(encoded), e)
	}
}
func TestReviewQueueExcludesLinkedSourceRows(t *testing.T) {
	s := testStore(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense+nativeRefund), "wallet")
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	// Model a future reviewed row linked to a transaction without deleting its
	// immutable pending evidence. A resolved source occurrence must leave queue.
	_, e := s.db.Exec("INSERT INTO source_records VALUES(?,?,?,?,?,?)", newID(), p.ID, p.Pending[0].Line, p.Rows[0].Record.ID, encode(p.Rows[0].Record), "duplicate")
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.ReviewQueue(1, "")
	if e != nil || q.TotalCount != 0 || len(q.Items) != 0 {
		t.Fatal(q, e)
	}
}

func TestReviewQueueDistinguishesSourceAccounts(t *testing.T) {
	s := testStore(t)
	first := alipayPreview(t, s, []byte(nativeHeader+nativeRefund), "wallet-a")
	second := alipayPreview(t, s, []byte(nativeHeader+nativeRefund), "wallet-b")
	q, e := s.ReviewQueue(1, "alipay")
	if e != nil || q.TotalCount != 2 || first.ID == second.ID {
		t.Fatal(q, e)
	}
	accounts := map[string]string{}
	for _, item := range q.Items {
		accounts[item.ImportID] = item.SourceAccount
	}
	if accounts[first.ID] != "wallet-a" || accounts[second.ID] != "wallet-b" {
		t.Fatal("wallet accounts collapsed", accounts)
	}
	// Earlier snapshots lacking the optional alias stay inspectable.
	if _, e = s.db.Exec("UPDATE imports SET preview_json=json_remove(preview_json,'$.sourceAccount') WHERE id=?", first.ID); e != nil {
		t.Fatal(e)
	}
	q, e = s.ReviewQueue(1, "alipay")
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range q.Items {
		if item.ImportID == first.ID && item.SourceAccount != "" {
			t.Fatal("missing legacy account not empty", item)
		}
	}
}
