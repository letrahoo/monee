package ledger

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// These tests call private metadata scaffolding, never an HTTP/UI decision path.
// Normal Open must still refuse populated foundation tables. Accounting remains
// two ordinary expenses until a future complete effective-projection delivery.
func expensePairLifecycleFixture(t *testing.T) (*Store, *Store, expensePairConfirmInput) {
	t.Helper()
	return lifecycleFixtureVariant(t, nativeExpense, "synthetic-lifecycle-wallet")
}

func lifecycleAdmit(t *testing.T, root *Store, systemRole string) string {
	t.Helper()
	id := newID()
	foundationExec(t, root, "INSERT INTO app_users(id,display_name,created_at) VALUES(?,?,?)", id, "合成生命周期身份", now())
	foundationExec(t, root, "INSERT INTO user_access(user_id,enabled,role,version) VALUES(?,1,?,1)", id, systemRole)
	return id
}

func lifecycleMember(t *testing.T, root *Store, role string) *Store {
	t.Helper()
	id := lifecycleAdmit(t, root, "member")
	foundationExec(t, root, "INSERT INTO ledger_members(ledger_id,user_id,role,version,created_at) VALUES(?,?,?,1,?)", root.ledgerID, id, role, now())
	s, err := root.Scoped(id, root.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func lifecycleFixtureVariant(t *testing.T, secondRow, secondWallet string) (*Store, *Store, expensePairConfirmInput) {
	t.Helper()
	root := testStore(t)
	s := lifecycleMember(t, root, "owner")
	ids := make([]string, 2)
	imports := make([]string, 2)
	for i, row := range []string{nativeExpense, secondRow} {
		row = strings.ReplaceAll(row, "pay-1", []string{"lifecycle-pay-a", "lifecycle-pay-b"}[i])
		row = strings.ReplaceAll(row, "order-1", []string{"lifecycle-order-a", "lifecycle-order-b"}[i])
		wallet := "synthetic-lifecycle-wallet"
		if i == 1 {
			wallet = secondWallet
		}
		p := alipayPreview(t, s, []byte(nativeHeader+row), wallet)
		if len(p.Errors) != 0 || len(p.Rows) != 1 || p.SourceDocument == nil {
			t.Fatal(p)
		}
		committed, err := s.Commit(p.ID, p.LedgerVersion, true)
		if err != nil || committed.Added != 1 || committed.Skipped != 0 {
			t.Fatal(committed, err)
		}
		detail, err := s.ImportDetail(p.ID)
		if err != nil || len(detail.Evidence) != 1 {
			t.Fatal(detail, err)
		}
		ids[i], imports[i] = detail.Evidence[0].TransactionID, p.ID
		var added int
		if err = root.db.QueryRow("SELECT COUNT(*) FROM source_records WHERE import_id=? AND transaction_id=? AND disposition='added'", p.ID, ids[i]).Scan(&added); err != nil || added != 1 {
			t.Fatal(added, err)
		}
	}
	if ids[0] == ids[1] || imports[0] == imports[1] {
		t.Fatal("fixture must have two distinct committed native observations")
	}
	in := expensePairConfirmInput{PrimaryTransactionID: ids[0], SecondaryTransactionID: ids[1], Reason: "  合成同账户确认\n保留明确主记录  "}
	lifecycleRefresh(t, root, &in)
	return root, s, in
}

func lifecycleLedgerVersion(t *testing.T, root *Store, want int64) {
	t.Helper()
	var got int64
	if err := root.db.QueryRow("SELECT version FROM ledgers WHERE id=?", root.ledgerID).Scan(&got); err != nil || got != want {
		t.Fatal("persisted ledger version", got, want, err)
	}
}

func lifecycleRefresh(t *testing.T, root *Store, in *expensePairConfirmInput) {
	t.Helper()
	if err := root.db.QueryRow("SELECT version FROM ledgers WHERE id=?", root.ledgerID).Scan(&in.LedgerVersion); err != nil {
		t.Fatal(err)
	}
	if err := root.db.QueryRow("SELECT version FROM transactions WHERE ledger_id=? AND id=?", root.ledgerID, in.PrimaryTransactionID).Scan(&in.PrimaryVersion); err != nil {
		t.Fatal(err)
	}
	if err := root.db.QueryRow("SELECT version FROM transactions WHERE ledger_id=? AND id=?", root.ledgerID, in.SecondaryTransactionID).Scan(&in.SecondaryVersion); err != nil {
		t.Fatal(err)
	}
}

func lifecycleRejected(t *testing.T, root *Store, code string, call func() (expensePairReceipt, error)) {
	t.Helper()
	before := accountingFactTables(t, root)
	out, err := call()
	var p *Problem
	if err == nil || out != (expensePairReceipt{}) || !errors.As(err, &p) {
		t.Fatal("expected zero receipt and static problem", out, err)
	}
	if code != "" && p.Code != code {
		t.Fatal("unexpected rejection code", p.Code, code)
	}
	if strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("private input was echoed", err)
	}
	if !reflect.DeepEqual(before, accountingFactTables(t, root)) {
		t.Fatal("rejected lifecycle command changed application rows")
	}
}

func lifecycleOnlyMetadataChanged(t *testing.T, before, after map[string][][]any) {
	t.Helper()
	allowed := map[string]bool{"ledgers": true, "expense_pair_decisions": true, "expense_pair_members": true, "expense_pair_revisions": true, "change_log": true, "idempotency": true}
	if len(before) != len(after) {
		t.Fatal("private lifecycle changed schema/table set")
	}
	for table, rows := range before {
		if !allowed[table] && !reflect.DeepEqual(rows, after[table]) {
			t.Fatal("private lifecycle modified non-metadata table", table)
		}
	}
}

func lifecycleAssertObject(t *testing.T, text string) {
	t.Helper()
	var value map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &value); err != nil || len(value) == 0 {
		t.Fatal("persisted snapshot must be a populated JSON object", err)
	}
}

func lifecycleStored(t *testing.T, s *Store, in expensePairConfirmInput, receipt expensePairReceipt, reason string) {
	t.Helper()
	var status, creator string
	var version, contract int64
	if err := s.db.QueryRow("SELECT status,version,contract_version,created_by FROM expense_pair_decisions WHERE ledger_id=? AND id=?", s.ledgerID, receipt.ID).Scan(&status, &version, &contract, &creator); err != nil {
		t.Fatal(err)
	}
	if status != receipt.Status || version != receipt.Version || contract != 1 || creator != s.actorID {
		t.Fatal(status, version, contract, creator, receipt)
	}
	rows, err := s.db.Query("SELECT transaction_id,role,transaction_version,active,facts_json,evidence_json FROM expense_pair_members WHERE ledger_id=? AND decision_id=? ORDER BY role", s.ledgerID, receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	members := 0
	for rows.Next() {
		var id, role, facts, evidence string
		var memberVersion, active int64
		if err = rows.Scan(&id, &role, &memberVersion, &active, &facts, &evidence); err != nil {
			t.Fatal(err)
		}
		wantID, wantVersion := in.PrimaryTransactionID, in.PrimaryVersion
		if role == "secondary" {
			wantID, wantVersion = in.SecondaryTransactionID, in.SecondaryVersion
		} else if role != "primary" {
			t.Fatal(role)
		}
		wantActive := int64(1)
		if receipt.Status == "retracted" {
			wantActive = 0
		}
		if id != wantID || memberVersion != wantVersion || active != wantActive {
			t.Fatal(id, role, memberVersion, active)
		}
		lifecycleAssertObject(t, facts)
		lifecycleAssertObject(t, evidence)
		members++
	}
	if err = rows.Err(); err != nil || members != 2 {
		t.Fatal(members, err)
	}
	rows.Close()
	var operations, audits int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM expense_pair_revisions WHERE ledger_id=? AND decision_id=?", s.ledgerID, receipt.ID).Scan(&operations); err != nil || int64(operations) != receipt.Version {
		t.Fatal(operations, err)
	}
	var operation, actor, storedReason, snapshot string
	var revisionContract int64
	if err = s.db.QueryRow("SELECT operation,actor_id,reason,snapshot_json,contract_version FROM expense_pair_revisions WHERE ledger_id=? AND decision_id=? AND version=?", s.ledgerID, receipt.ID, receipt.Version).Scan(&operation, &actor, &storedReason, &snapshot, &revisionContract); err != nil {
		t.Fatal(err)
	}
	wantOperation := "confirm"
	if receipt.Status == "retracted" {
		wantOperation = "withdraw"
	}
	if operation != wantOperation || actor != s.actorID || storedReason != strings.TrimSpace(reason) || revisionContract != 1 {
		t.Fatal(operation, actor, storedReason, revisionContract)
	}
	lifecycleAssertObject(t, snapshot)
	if err = s.db.QueryRow("SELECT COUNT(*) FROM change_log WHERE ledger_id=? AND entity_id=? AND actor_id=?", s.ledgerID, receipt.ID, s.actorID).Scan(&audits); err != nil || int64(audits) != receipt.Version {
		t.Fatal("missing lifecycle audit", audits, err)
	}
	var entity, auditOperation, payload string
	var baseVersion, entityVersion, payloadVersion int64
	if err = s.db.QueryRow("SELECT entity_type,operation,base_version,entity_version,payload_version,payload_json FROM change_log WHERE ledger_id=? AND entity_id=? AND entity_version=?", s.ledgerID, receipt.ID, receipt.Version).Scan(&entity, &auditOperation, &baseVersion, &entityVersion, &payloadVersion, &payload); err != nil {
		t.Fatal(err)
	}
	if entity != "expense_pair_decision" || auditOperation != "expense_pair_"+wantOperation || baseVersion != receipt.Version-1 || entityVersion != receipt.Version || payloadVersion != 1 || payload != snapshot {
		t.Fatal("revision/audit binding differs", entity, auditOperation, baseVersion, entityVersion, payloadVersion)
	}
}

func TestExpensePairLifecycleStoreConfirmWithdrawReplayAndHistory(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	original := accountingFactTables(t, root)
	before := dashboard(t, s)
	confirmed, err := s.confirmExpensePair(in, "lifecycle-confirm-original")
	if err != nil || confirmed.ID == "" || confirmed.Version != 1 || confirmed.Status != "active" || confirmed.LedgerVersion != in.LedgerVersion+1 || confirmed.PrimaryID != in.PrimaryTransactionID || confirmed.SecondaryID != in.SecondaryTransactionID {
		t.Fatal(confirmed, err)
	}
	lifecycleStored(t, s, in, confirmed, in.Reason)
	lifecycleLedgerVersion(t, root, confirmed.LedgerVersion)
	lifecycleOnlyMetadataChanged(t, original, accountingFactTables(t, root))
	afterConfirm := accountingFactTables(t, root)
	replay, err := s.confirmExpensePair(in, "lifecycle-confirm-original")
	if err != nil || replay != confirmed || !reflect.DeepEqual(afterConfirm, accountingFactTables(t, root)) {
		t.Fatal("confirmation replay wrote or changed receipt", replay, err)
	}
	changed := in
	changed.Reason = "不同合成确认理由"
	lifecycleRejected(t, root, "conflict", func() (expensePairReceipt, error) { return s.confirmExpensePair(changed, "lifecycle-confirm-original") })
	withdraw := expensePairWithdrawInput{LedgerVersion: confirmed.LedgerVersion, DecisionID: confirmed.ID, DecisionVersion: confirmed.Version, Reason: "合成撤回误关联"}
	withdrawn, err := s.withdrawExpensePair(withdraw, "lifecycle-withdraw-original")
	if err != nil || withdrawn.ID != confirmed.ID || withdrawn.Version != 2 || withdrawn.Status != "retracted" || withdrawn.LedgerVersion != confirmed.LedgerVersion+1 || withdrawn.PrimaryID != confirmed.PrimaryID || withdrawn.SecondaryID != confirmed.SecondaryID {
		t.Fatal(withdrawn, err)
	}
	lifecycleStored(t, s, in, withdrawn, withdraw.Reason)
	lifecycleLedgerVersion(t, root, withdrawn.LedgerVersion)
	afterWithdraw := accountingFactTables(t, root)
	replay, err = s.withdrawExpensePair(withdraw, "lifecycle-withdraw-original")
	if err != nil || replay != withdrawn || !reflect.DeepEqual(afterWithdraw, accountingFactTables(t, root)) {
		t.Fatal("withdraw replay wrote or changed receipt", replay, err)
	}
	changedWithdraw := withdraw
	changedWithdraw.Reason = "不同合成撤回理由"
	lifecycleRejected(t, root, "conflict", func() (expensePairReceipt, error) {
		return s.withdrawExpensePair(changedWithdraw, "lifecycle-withdraw-original")
	})
	lifecycleRejected(t, root, "conflict", func() (expensePairReceipt, error) { return s.confirmExpensePair(in, "lifecycle-confirm-original") })
	lifecycleRefresh(t, root, &in)
	newGroup, err := s.confirmExpensePair(in, "lifecycle-confirm-new-group")
	if err != nil || newGroup.ID == confirmed.ID || newGroup.ID == "" || newGroup.Status != "active" || newGroup.Version != 1 || newGroup.LedgerVersion != withdrawn.LedgerVersion+1 {
		t.Fatal(newGroup, err)
	}
	lifecycleStored(t, s, in, newGroup, in.Reason)
	lifecycleLedgerVersion(t, root, newGroup.LedgerVersion)
	lifecycleStored(t, s, in, withdrawn, withdraw.Reason)
	afterNewGroup := accountingFactTables(t, root)
	replay, err = s.withdrawExpensePair(withdraw, "lifecycle-withdraw-original")
	if err != nil || replay != withdrawn || !reflect.DeepEqual(afterNewGroup, accountingFactTables(t, root)) {
		t.Fatal("old withdrawn receipt must not deactivate a new group", replay, err)
	}
	if count(t, root, "expense_pair_decisions") != 2 || count(t, root, "expense_pair_members") != 4 || count(t, root, "expense_pair_revisions") != 3 {
		t.Fatal("reconfirmation overwrote history")
	}
	lifecycleOnlyMetadataChanged(t, original, accountingFactTables(t, root))
	after := dashboard(t, s)
	if before.TotalCount != 2 || after.TotalCount != 2 || before.ExpenseMinor != "2468" || after.ExpenseMinor != "2468" || after.IncomeMinor != "0" || !reflect.DeepEqual(before.Transactions, after.Transactions) {
		t.Fatal("private metadata must NOT suppress either expense", before, after)
	}
}

func TestExpensePairLifecycleStoreAdmittedEditorAndBlankRoot(t *testing.T) {
	t.Run("admitted editor", func(t *testing.T) {
		root, _, in := expensePairLifecycleFixture(t)
		editor := lifecycleMember(t, root, "editor")
		out, err := editor.confirmExpensePair(in, "lifecycle-editor-confirm")
		if err != nil || out.Status != "active" {
			t.Fatal(out, err)
		}
		lifecycleStored(t, editor, in, out, in.Reason)
	})
	t.Run("blank internal root is not a business actor", func(t *testing.T) {
		root, _, in := expensePairLifecycleFixture(t)
		lifecycleRejected(t, root, "unauthenticated", func() (expensePairReceipt, error) { return root.confirmExpensePair(in, "lifecycle-root-rejected") })
	})
}

func TestExpensePairLifecycleStoreReauthorizesBeforeCachedSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		revoke     func(*testing.T, *Store, *Store)
	}{
		{"viewer", "ledger_readonly", func(t *testing.T, root, s *Store) {
			foundationExec(t, root, "UPDATE ledger_members SET role='viewer' WHERE ledger_id=? AND user_id=?", s.ledgerID, s.actorID)
		}},
		{"removed", "ledger_denied", func(t *testing.T, root, s *Store) {
			foundationExec(t, root, "DELETE FROM ledger_members WHERE ledger_id=? AND user_id=?", s.ledgerID, s.actorID)
		}},
		{"admission disabled", "access_denied", func(t *testing.T, root, s *Store) {
			foundationExec(t, root, "UPDATE user_access SET enabled=0 WHERE user_id=?", s.actorID)
		}},
		{"identity disabled", "access_denied", func(t *testing.T, root, s *Store) {
			foundationExec(t, root, "UPDATE app_users SET status='disabled' WHERE id=?", s.actorID)
		}},
		{"system admin nonmember", "ledger_denied", func(t *testing.T, root, s *Store) {
			foundationExec(t, root, "UPDATE user_access SET role='superadmin' WHERE user_id=?", s.actorID)
			foundationExec(t, root, "DELETE FROM ledger_members WHERE ledger_id=? AND user_id=?", s.ledgerID, s.actorID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, stage := range []string{"active", "withdrawn"} {
				t.Run(stage, func(t *testing.T) {
					root, s, in := expensePairLifecycleFixture(t)
					out, err := s.confirmExpensePair(in, "lifecycle-cached-authority")
					if err != nil {
						t.Fatal(err)
					}
					withdraw := expensePairWithdrawInput{LedgerVersion: out.LedgerVersion, DecisionID: out.ID, DecisionVersion: 1, Reason: "合成撤回"}
					if stage == "withdrawn" {
						if _, err = s.withdrawExpensePair(withdraw, "lifecycle-cached-withdraw"); err != nil {
							t.Fatal(err)
						}
					}
					tc.revoke(t, root, s)
					for _, key := range []string{"lifecycle-cached-authority", "lifecycle-fresh-authority"} {
						lifecycleRejected(t, root, tc.code, func() (expensePairReceipt, error) { return s.confirmExpensePair(in, key) })
					}
					for _, key := range []string{"lifecycle-cached-withdraw", "lifecycle-no-withdraw-right"} {
						lifecycleRejected(t, root, tc.code, func() (expensePairReceipt, error) { return s.withdrawExpensePair(withdraw, key) })
					}
				})
			}
		})
	}
}

func TestExpensePairLifecycleStoreCASAndInputFailuresAreZeroWrite(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	for _, tc := range []struct {
		name   string
		change func(*expensePairConfirmInput)
		key    string
	}{
		{"stale ledger", func(v *expensePairConfirmInput) { v.LedgerVersion-- }, "lifecycle-stale-ledger"},
		{"stale member", func(v *expensePairConfirmInput) { v.SecondaryVersion++ }, "lifecycle-stale-member"},
		{"same member", func(v *expensePairConfirmInput) { v.SecondaryTransactionID = v.PrimaryTransactionID }, "lifecycle-same-member"},
		{"blank reason", func(v *expensePairConfirmInput) { v.Reason = "  " }, "lifecycle-blank-reason"},
		{"NUL reason", func(v *expensePairConfirmInput) { v.Reason = "合成\x00理由" }, "lifecycle-nul-reason"},
		{"long reason", func(v *expensePairConfirmInput) { v.Reason = strings.Repeat("中", 1001) }, "lifecycle-long-reason"},
		{"short key", func(v *expensePairConfirmInput) {}, "short"},
		{"control key", func(v *expensePairConfirmInput) {}, "lifecycle-control-\u0085-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := in
			tc.change(&bad)
			lifecycleRejected(t, root, "", func() (expensePairReceipt, error) { return s.confirmExpensePair(bad, tc.key) })
		})
	}
	out, err := s.confirmExpensePair(in, "lifecycle-cas-confirm")
	if err != nil {
		t.Fatal(err)
	}
	fresh := in
	lifecycleRefresh(t, root, &fresh)
	lifecycleRejected(t, root, "conflict", func() (expensePairReceipt, error) { return s.confirmExpensePair(fresh, "lifecycle-already-active") })
	for _, bad := range []expensePairWithdrawInput{
		{LedgerVersion: out.LedgerVersion - 1, DecisionID: out.ID, DecisionVersion: 1, Reason: "合成过期账本"},
		{LedgerVersion: out.LedgerVersion, DecisionID: out.ID, DecisionVersion: 2, Reason: "合成过期关系"},
	} {
		lifecycleRejected(t, root, "conflict", func() (expensePairReceipt, error) { return s.withdrawExpensePair(bad, "lifecycle-stale-withdraw") })
	}
}

func TestExpensePairLifecycleStoreDifferentNativeTuplesAreZeroWrite(t *testing.T) {
	for _, tc := range []struct{ name, row, wallet string }{
		{"actual different fund", strings.Replace(nativeExpense, "银行卡A", "合成银行卡B", 1), "synthetic-lifecycle-wallet"},
		{"different date", strings.Replace(nativeExpense, "2026-09-15", "2026-09-16", 1), "synthetic-lifecycle-wallet"},
		{"different amount", strings.Replace(nativeExpense, "12.34", "12.35", 1), "synthetic-lifecycle-wallet"},
		{"different merchant", strings.Replace(nativeExpense, "合成咖啡店", "另一合成商户", 1), "synthetic-lifecycle-wallet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, s, in := lifecycleFixtureVariant(t, tc.row, tc.wallet)
			code := ""
			if tc.name == "actual different fund" {
				// The envelope wallet is only a source alias; nativeRows books the
				// CSV payment field. Prove the actual referenced funding IDs differ.
				var funds [2]string
				for i, id := range []string{in.PrimaryTransactionID, in.SecondaryTransactionID} {
					var legs int
					if err := root.db.QueryRow(`SELECT COUNT(*),COALESCE(MIN(p.account_id),'')
 FROM postings p JOIN transactions tr ON tr.id=p.transaction_id JOIN accounts a ON a.id=p.account_id
 WHERE p.transaction_id=? AND tr.ledger_id=? AND a.ledger_id=? AND a.kind='clearing' AND p.amount_minor<0 AND p.currency='CNY'`, id, root.ledgerID, root.ledgerID).Scan(&legs, &funds[i]); err != nil || legs != 1 || funds[i] == "" {
						t.Fatal("counterexample must have one actual funding leg", id, legs, funds[i], err)
					}
				}
				if funds[0] == funds[1] {
					t.Fatal("counterexample still shares the same actual funding account", funds)
				}
				code = "unsupported"
			}
			lifecycleRejected(t, root, code, func() (expensePairReceipt, error) { return s.confirmExpensePair(in, "lifecycle-different-tuple") })
		})
	}
}

func TestExpensePairLifecycleStoreRejectsUntrustedOrChangedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *Store, *Store, *expensePairConfirmInput)
	}{
		{"deleted", func(t *testing.T, root, s *Store, in *expensePairConfirmInput) {
			foundationExec(t, root, "UPDATE transactions SET deleted_at=? WHERE id=?", now(), in.PrimaryTransactionID)
		}},
		{"refund child", func(t *testing.T, root, s *Store, in *expensePairConfirmInput) {
			if _, err := s.CreateRefund(in.PrimaryTransactionID, RefundInput{Version: in.PrimaryVersion, Date: "2026-09-16", Amount: "1.00", Currency: "CNY", Account: "synthetic-lifecycle-wallet"}, "lifecycle-refund-dependency"); err != nil {
				t.Fatal(err)
			}
		}},
		{"correction restored to original amount", func(t *testing.T, root, s *Store, in *expensePairConfirmInput) {
			out, err := s.Correct(in.PrimaryTransactionID, CorrectionInput{Version: in.PrimaryVersion, Type: "expense", AmountMinor: "1250", Reason: "合成更正"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Correct(in.PrimaryTransactionID, CorrectionInput{Version: out.Version, Type: "expense", AmountMinor: "1234", Reason: "合成更正回原值"}); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing native source link", func(t *testing.T, root, s *Store, in *expensePairConfirmInput) {
			foundationExec(t, root, "DELETE FROM source_records WHERE transaction_id=?", in.PrimaryTransactionID)
		}},
		{"duplicate disposition is not added evidence", func(t *testing.T, root, s *Store, in *expensePairConfirmInput) {
			foundationExec(t, root, "UPDATE source_records SET disposition='duplicate' WHERE transaction_id=?", in.PrimaryTransactionID)
		}},
		{"uncommitted source", func(t *testing.T, root, s *Store, in *expensePairConfirmInput) {
			foundationExec(t, root, "UPDATE imports SET committed_at=NULL,result_json=NULL WHERE id=(SELECT import_id FROM source_records WHERE transaction_id=?)", in.PrimaryTransactionID)
		}},
		{"raw bytes drift", func(t *testing.T, root, s *Store, in *expensePairConfirmInput) {
			foundationExec(t, root, "UPDATE imports SET raw_csv=raw_csv||'synthetic-secret' WHERE id=(SELECT import_id FROM source_records WHERE transaction_id=?)", in.PrimaryTransactionID)
		}},
		{"duplicate source JSON keys", func(t *testing.T, root, s *Store, in *expensePairConfirmInput) {
			foundationExec(t, root, "UPDATE imports SET preview_json=? WHERE id=(SELECT import_id FROM source_records WHERE transaction_id=?)", `{"id":"synthetic-secret","id":"duplicate"}`, in.PrimaryTransactionID)
		}},
		{"invalid normalized source JSON", func(t *testing.T, root, s *Store, in *expensePairConfirmInput) {
			foundationExec(t, root, "UPDATE source_records SET normalized_json='null' WHERE transaction_id=?", in.PrimaryTransactionID)
		}},
		{"manual label and external ID are not native evidence", func(t *testing.T, root, s *Store, in *expensePairConfirmInput) {
			input := testInput()
			input.Date = "2026-09-15"
			input.Merchant = "合成咖啡店"
			input.Source = "支付宝"
			input.Account = "synthetic-lifecycle-wallet"
			input.ExternalID = "alipay-native-v1:synthetic-manual-claim"
			out, err := s.Create(input, "lifecycle-manual-impersonation")
			if err != nil {
				t.Fatal(err)
			}
			in.SecondaryTransactionID = out.ID
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, s, in := expensePairLifecycleFixture(t)
			tc.mutate(t, root, s, &in)
			lifecycleRefresh(t, root, &in)
			lifecycleRejected(t, root, "", func() (expensePairReceipt, error) { return s.confirmExpensePair(in, "lifecycle-untrusted-evidence") })
		})
	}
}

func TestExpensePairLifecycleStoreCorruptPersistedSnapshotsFailClosed(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"missing facts fields", "UPDATE expense_pair_members SET facts_json='{}' WHERE decision_id=? AND role='primary'"},
		{"duplicate evidence fields", `UPDATE expense_pair_members SET evidence_json='{"id":"synthetic-secret","id":"duplicate"}' WHERE decision_id=? AND role='secondary'`},
		{"missing revision snapshot fields", "UPDATE expense_pair_revisions SET snapshot_json='{}' WHERE decision_id=?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, s, in := expensePairLifecycleFixture(t)
			out, err := s.confirmExpensePair(in, "lifecycle-persisted-confirm")
			if err != nil {
				t.Fatal(err)
			}
			foundationExec(t, root, tc.query, out.ID)
			lifecycleRejected(t, root, "", func() (expensePairReceipt, error) { return s.confirmExpensePair(in, "lifecycle-persisted-confirm") })
			withdraw := expensePairWithdrawInput{LedgerVersion: out.LedgerVersion, DecisionID: out.ID, DecisionVersion: 1, Reason: "合成损坏状态不能撤回"}
			lifecycleRejected(t, root, "", func() (expensePairReceipt, error) {
				return s.withdrawExpensePair(withdraw, "lifecycle-corrupt-withdraw")
			})
		})
	}
}

func TestExpensePairLifecycleStoreCrossLedgerIDsRemainOpaque(t *testing.T) {
	root, s, in := expensePairLifecycleFixture(t)
	user := lifecycleAdmit(t, root, "member")
	other, err := root.CreateLedger(user, "合成另一生命周期账本")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := root.Scoped(user, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = root.db.QueryRow("SELECT version FROM ledgers WHERE id=?", other.ID).Scan(&in.LedgerVersion); err != nil {
		t.Fatal(err)
	}
	lifecycleRejected(t, root, "", func() (expensePairReceipt, error) {
		out, err := outsider.confirmExpensePair(in, "lifecycle-cross-ledger-confirm")
		if err != nil && (strings.Contains(err.Error(), in.PrimaryTransactionID) || strings.Contains(err.Error(), in.SecondaryTransactionID)) {
			t.Fatal("foreign transaction IDs echoed", err)
		}
		return out, err
	})
	lifecycleRefresh(t, root, &in)
	out, err := s.confirmExpensePair(in, "lifecycle-own-ledger-confirm")
	if err != nil {
		t.Fatal(err)
	}
	withdraw := expensePairWithdrawInput{LedgerVersion: in.LedgerVersion, DecisionID: out.ID, DecisionVersion: 1, Reason: "合成跨账本拒绝"}
	if err = root.db.QueryRow("SELECT version FROM ledgers WHERE id=?", other.ID).Scan(&withdraw.LedgerVersion); err != nil {
		t.Fatal(err)
	}
	lifecycleRejected(t, root, "", func() (expensePairReceipt, error) {
		result, err := outsider.withdrawExpensePair(withdraw, "lifecycle-cross-ledger-withdraw")
		if err != nil && strings.Contains(err.Error(), out.ID) {
			t.Fatal("foreign decision ID echoed", err)
		}
		return result, err
	})
}
