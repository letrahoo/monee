package ledger

import (
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestExpensePairLifecycleRejectsNativeDocumentHashDrift(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	var importID string
	if e := root.db.QueryRow("SELECT import_id FROM source_records WHERE transaction_id=? AND disposition='added'", in.PrimaryTransactionID).Scan(&importID); e != nil {
		t.Fatal(e)
	}
	p, _ := nativeEvidenceSaved(t, root, importID)
	p.SourceDocument.ContentHash = strings.Repeat("a", 64)
	foundationExec(t, root, "UPDATE imports SET preview_json=? WHERE id=?", encode(p), importID)
	lifecycleRejected(t, root, "expense_pair_unavailable", func() (expensePairReceipt, error) { return s.confirmExpensePair(in, "pair-native-document-hash-drift") })
}

func TestExpensePairLifecycleRejectsUnknownPaymentPlaceholder(t *testing.T) {
	for _, payment := range []string{"", "/", "-", "待核实资金账户"} {
		t.Run(payment, func(t *testing.T) {
			root := testStore(t)
			s := lifecycleMember(t, root, "owner")
			ids := []string{}
			for i, trace := range []string{"unknown-payment-a", "unknown-payment-b"} {
				row := strings.Replace(nativeExpense, "银行卡A", payment, 1)
				row = strings.ReplaceAll(row, "pay-1", trace)
				row = strings.ReplaceAll(row, "order-1", trace+"-order")
				p := alipayPreview(t, s, []byte(nativeHeader+row), "synthetic-unknown-payment")
				result, e := s.Commit(p.ID, p.LedgerVersion, true)
				if e != nil || result.Added != 1 {
					t.Fatal(i, result, e)
				}
				d, e := s.ImportDetail(p.ID)
				if e != nil || len(d.Evidence) != 1 {
					t.Fatal(d, e)
				}
				ids = append(ids, d.Evidence[0].TransactionID)
			}
			in := expensePairConfirmInput{PrimaryTransactionID: ids[0], SecondaryTransactionID: ids[1], Reason: "合成未知资金字段"}
			lifecycleRefresh(t, root, &in)
			lifecycleRejected(t, root, "unsupported", func() (expensePairReceipt, error) {
				return s.confirmExpensePair(in, "pair-unknown-payment-placeholder")
			})
		})
	}
}

func TestExpensePairLifecycleAliAndWeChatOriginals(t *testing.T) {
	root := testStore(t)
	s := lifecycleMember(t, root, "owner")
	a := alipayPreview(t, s, []byte(nativeHeader+nativeExpense), "synthetic-alipay-source")
	if r, e := s.Commit(a.ID, a.LedgerVersion, true); e != nil || r.Added != 1 {
		t.Fatal(r, e)
	}
	text := "微信支付账单明细\n交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注\n2026-09-15 10:20:30,商户消费,合成咖啡店,早餐,支出,¥12.34,银行卡A,支付成功,wx-private-lifecycle,wx-private-order,合成备注\n"
	w, e := s.PreviewWeChat("synthetic-wechat.csv", base64.StdEncoding.EncodeToString([]byte(text)), "synthetic-wechat-source")
	if e != nil || len(w.Errors) != 0 || len(w.Rows) != 1 {
		t.Fatal(w, e)
	}
	if r, e := s.Commit(w.ID, w.LedgerVersion, true); e != nil || r.Added != 1 {
		t.Fatal(r, e)
	}
	ad, e := s.ImportDetail(a.ID)
	if e != nil || len(ad.Evidence) != 1 {
		t.Fatal(ad, e)
	}
	wd, e := s.ImportDetail(w.ID)
	if e != nil || len(wd.Evidence) != 1 {
		t.Fatal(wd, e)
	}
	in := expensePairConfirmInput{PrimaryTransactionID: wd.Evidence[0].TransactionID, SecondaryTransactionID: ad.Evidence[0].TransactionID, Reason: "合成两个native格式明确保留微信记录"}
	lifecycleRefresh(t, root, &in)
	before := lifecycleFinancialRows(t, root)
	r, e := s.confirmExpensePair(in, "pair-alipay-wechat-native-positive")
	if e != nil || r.PrimaryID != in.PrimaryTransactionID {
		t.Fatal(r, e)
	}
	if _, e = s.withdrawExpensePair(expensePairWithdrawInput{LedgerVersion: r.LedgerVersion, DecisionID: r.ID, DecisionVersion: r.Version, Reason: "合成跨格式撤回"}, "pair-alipay-wechat-native-withdraw"); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, lifecycleFinancialRows(t, root)) {
		t.Fatal("Mixed native metadata lifecycle changed original financial/source rows")
	}
}

func lifecycleFinancialRows(t *testing.T, s *Store) map[string][][]any {
	t.Helper()
	all := nativeEvidenceTables(t, s)
	for _, name := range []string{"expense_pair_decisions", "expense_pair_members", "expense_pair_revisions", "ledgers", "change_log", "idempotency"} {
		delete(all, name)
	}
	return all
}
func lifecycleLateFailure(t *testing.T, withdraw bool) {
	t.Helper()
	root, s, in := expensePairLifecycleFixture(t)
	var w expensePairWithdrawInput
	if withdraw {
		created, e := s.confirmExpensePair(in, "pair-before-failed-withdraw")
		if e != nil {
			t.Fatal(e)
		}
		w = expensePairWithdrawInput{LedgerVersion: created.LedgerVersion, DecisionID: created.ID, DecisionVersion: created.Version, Reason: "合成撤回晚失败"}
	}
	foundationExec(t, root, "CREATE TRIGGER lifecycle_test_abort_idempotency BEFORE INSERT ON idempotency BEGIN SELECT RAISE(ABORT,'synthetic-final-write-failure'); END")
	defer func() {
		if _, e := root.db.Exec("DROP TRIGGER IF EXISTS lifecycle_test_abort_idempotency"); e != nil {
			t.Error(e)
		}
	}()
	before := nativeEvidenceTables(t, root)
	schema := foundationSchemaSignature(t, root.db)
	var out expensePairReceipt
	var e error
	if withdraw {
		out, e = s.withdrawExpensePair(w, "pair-failed-withdraw-idempotency")
	} else {
		out, e = s.confirmExpensePair(in, "pair-failed-confirm-idempotency")
	}
	if e == nil || out != (expensePairReceipt{}) || strings.Contains(e.Error(), "synthetic-final-write-failure") {
		t.Fatal("Late SQL error must be static and return no usable receipt", out, e)
	}
	if !reflect.DeepEqual(before, nativeEvidenceTables(t, root)) || !reflect.DeepEqual(schema, foundationSchemaSignature(t, root.db)) {
		t.Fatal("Late failure committed ledger/audit/member/revision/key partial state")
	}
	foundationExec(t, root, "DROP TRIGGER lifecycle_test_abort_idempotency")
	if withdraw {
		out, e = s.withdrawExpensePair(w, "pair-failed-withdraw-idempotency")
	} else {
		out, e = s.confirmExpensePair(in, "pair-failed-confirm-idempotency")
	}
	if e != nil || out.ID == "" {
		t.Fatal("Rolled-back key must be retryable without hidden partial data", out, e)
	}
}
func TestExpensePairLifecycleConfirmLateFailureRollsBack(t *testing.T) {
	lifecycleLateFailure(t, false)
}
func TestExpensePairLifecycleWithdrawLateFailureRollsBack(t *testing.T) {
	lifecycleLateFailure(t, true)
}

func TestExpensePairLifecycleEditorCanWithdrawAfterCreatorRevoked(t *testing.T) {
	root, owner, in := expensePairLifecycleFixture(t)
	confirmed, e := owner.confirmExpensePair(in, "pair-owner-confirm-before-revoke")
	if e != nil {
		t.Fatal(e)
	}
	editor := lifecycleMember(t, root, "editor")
	foundationExec(t, root, "UPDATE user_access SET enabled=0 WHERE user_id=?", owner.actorID)
	before := lifecycleFinancialRows(t, root)
	w := expensePairWithdrawInput{LedgerVersion: confirmed.LedgerVersion, DecisionID: confirmed.ID, DecisionVersion: confirmed.Version, Reason: "合成编辑者纠正误关联"}
	out, e := editor.withdrawExpensePair(w, "pair-editor-withdraw-revoked-creator")
	if e != nil || out.Status != "retracted" || out.Version != 2 {
		t.Fatal(out, e)
	}
	var creator, withdrawActor string
	if e = root.db.QueryRow("SELECT created_by FROM expense_pair_decisions WHERE ledger_id=? AND id=?", root.ledgerID, out.ID).Scan(&creator); e != nil || creator != owner.actorID {
		t.Fatal("Withdrawal rewrote historical creator", creator, e)
	}
	if e = root.db.QueryRow("SELECT actor_id FROM expense_pair_revisions WHERE ledger_id=? AND decision_id=? AND version=2", root.ledgerID, out.ID).Scan(&withdrawActor); e != nil || withdrawActor != editor.actorID {
		t.Fatal(withdrawActor, e)
	}
	if !reflect.DeepEqual(before, lifecycleFinancialRows(t, root)) {
		t.Fatal("Creator revocation changed original finance during withdrawal")
	}
	foundationExec(t, root, "UPDATE ledger_members SET role='viewer' WHERE ledger_id=? AND user_id=?", root.ledgerID, editor.actorID)
	lifecycleRejected(t, root, "ledger_readonly", func() (expensePairReceipt, error) {
		return editor.withdrawExpensePair(w, "pair-editor-withdraw-revoked-creator")
	})
}
func TestExpensePairLifecycleIndependentConnectionsHaveOneWinner(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	var path string
	if e := root.db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); e != nil {
		t.Fatal(e)
	}
	other, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	second, e := other.Scoped(s.actorID, s.ledgerID)
	if e != nil {
		t.Fatal(e)
	}
	before := lifecycleFinancialRows(t, root)
	beforeKey := count(t, root, "idempotency")
	type result struct {
		receipt expensePairReceipt
		err     error
	}
	start := make(chan struct{})
	done := make(chan result, 2)
	for i, store := range []*Store{s, second} {
		go func(i int, store *Store) {
			<-start
			key := []string{"pair-independent-connection-a", "pair-independent-connection-b"}[i]
			r, e := store.confirmExpensePair(in, key)
			done <- result{r, e}
		}(i, store)
	}
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		r := <-done
		if r.err == nil {
			successes++
			if r.receipt.Status != "active" || r.receipt.Version != 1 {
				t.Fatal(r.receipt)
			}
		} else {
			var p *Problem
			if r.receipt != (expensePairReceipt{}) || !errors.As(r.err, &p) || (p.Code != "conflict" && p.Code != "expense_pair_unavailable") {
				t.Fatal("Losing concurrent request returned invalid or leaked result", r.receipt, r.err)
			}
		}
	}
	v, e := root.version()
	if e != nil || successes != 1 || v != in.LedgerVersion+1 || count(t, root, "expense_pair_decisions") != 1 || count(t, root, "expense_pair_members") != 2 || count(t, root, "expense_pair_revisions") != 1 || count(t, root, "idempotency") != beforeKey+1 {
		t.Fatal("Concurrent confirmations were not one atomic winner", successes, v, e)
	}
	if !reflect.DeepEqual(before, lifecycleFinancialRows(t, root)) {
		t.Fatal("Private metadata transaction changed original financial/source rows")
	}
}
func TestExpensePairLifecycleValidPrivateDataStillRequiresCompleteConsumer(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	if _, e := s.confirmExpensePair(in, "pair-valid-data-startup-fence"); e != nil {
		t.Fatal(e)
	}
	var path string
	if e := root.db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); e != nil {
		t.Fatal(e)
	}
	root.Close()
	opened, e := Open(path)
	if opened != nil {
		opened.Close()
	}
	var p *Problem
	if e == nil || opened != nil || !errors.As(e, &p) || p.Code != "association_foundation_unavailable" {
		t.Fatal("Private lifecycle must not remove intermediate startup capability fence", opened, e)
	}
}
