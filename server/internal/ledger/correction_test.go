package ledger

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCorrectionPreviewPostingsAuditAndSourceIdentity(t *testing.T) {
	s := testStore(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense), "wallet")
	if _, err := s.Commit(p.ID, p.LedgerVersion, false); err != nil {
		t.Fatal(err)
	}
	original := p.Rows[0].Record
	initial := dashboard(t, s)
	initialLog := count(t, s, "change_log")
	var originalFingerprint, sourceJSON string
	if err := s.db.QueryRow("SELECT fingerprint FROM transactions WHERE id=?", original.ID).Scan(&originalFingerprint); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT normalized_json FROM source_records WHERE transaction_id=?", original.ID).Scan(&sourceJSON); err != nil {
		t.Fatal(err)
	}
	in := CorrectionInput{Version: 1, Type: "income", AmountMinor: "2000", Reason: "合成类型更正"}
	preview, err := s.PreviewCorrection(original.ID, in)
	if err != nil || preview.Before != original || preview.After.Version != 2 || preview.IncomeDeltaMinor != "2000" || preview.ExpenseDeltaMinor != "-1234" || preview.NetDeltaMinor != "3234" {
		t.Fatal(preview, err)
	}
	if dashboard(t, s).Version != initial.Version || count(t, s, "change_log") != initialLog {
		t.Fatal("preview mutated ledger")
	}
	corrected, err := s.Correct(original.ID, in)
	if err != nil || corrected != preview.After {
		t.Fatal(corrected, err)
	}
	d := dashboard(t, s)
	if d.IncomeMinor != "2000" || d.ExpenseMinor != "0" || d.TotalCount != 1 || d.Version != initial.Version+1 {
		t.Fatal(d)
	}
	var n int
	var sum, fund, category int64
	if err = s.db.QueryRow("SELECT count(*),sum(amount_minor) FROM postings WHERE transaction_id=?", original.ID).Scan(&n, &sum); err != nil || n != 2 || sum != 0 {
		t.Fatal(n, sum, err)
	}
	if err = s.db.QueryRow("SELECT p.amount_minor FROM postings p JOIN accounts a ON a.id=p.account_id WHERE p.transaction_id=? AND a.kind='clearing'", original.ID).Scan(&fund); err != nil || fund != 2000 {
		t.Fatal(fund, err)
	}
	if err = s.db.QueryRow("SELECT p.amount_minor FROM postings p JOIN accounts a ON a.id=p.account_id WHERE p.transaction_id=? AND a.kind='income'", original.ID).Scan(&category); err != nil || category != -2000 {
		t.Fatal(category, err)
	}
	var actualFingerprint, actualSource string
	s.db.QueryRow("SELECT fingerprint FROM transactions WHERE id=?", original.ID).Scan(&actualFingerprint)
	s.db.QueryRow("SELECT normalized_json FROM source_records WHERE transaction_id=?", original.ID).Scan(&actualSource)
	if actualFingerprint != originalFingerprint || actualSource != sourceJSON {
		t.Fatal("source evidence changed")
	}
	history, err := s.CorrectionHistory(original.ID)
	if err != nil || len(history) != 1 || history[0].Before != original || history[0].After != corrected || history[0].Reason != in.Reason || history[0].CreatedAt == "" {
		t.Fatal(history, err)
	}
	var payload string
	var baseVersion, entityVersion int
	if err = s.db.QueryRow("SELECT payload_json,base_version,entity_version FROM change_log WHERE entity_id=? AND operation='correct'", original.ID).Scan(&payload, &baseVersion, &entityVersion); err != nil || baseVersion != 1 || entityVersion != 2 {
		t.Fatal(err)
	}
	var audit CorrectionRevision
	if err = json.Unmarshal([]byte(payload), &audit); err != nil || audit.After != corrected {
		t.Fatal(audit, err)
	}
	again := alipayPreview(t, s, []byte("元信息\n"+nativeHeader+nativeExpense), "wallet")
	if len(again.Errors) != 0 || again.DuplicateCount != 1 {
		t.Fatal("correction broke source dedup", again)
	}
	if _, err = s.Commit(again.ID, again.LedgerVersion, false); err != nil {
		t.Fatal(err)
	}
	if dashboard(t, s).Transactions[0] != corrected {
		t.Fatal("reimport overwrote correction")
	}
	changedSource := alipayPreview(t, s, []byte(strings.Replace(nativeHeader+nativeExpense, "12.34", "13.34", 1)), "wallet")
	if len(changedSource.Errors) == 0 {
		t.Fatal("changed source evidence silently accepted")
	}
	if _, err = s.Correct(original.ID, in); err == nil {
		t.Fatal("stale confirmation accepted")
	}
	// A later annotation and source repeat still retain the financial correction.
	annotated, err := s.Annotate(original.ID, AnnotationInput{Version: 2, Category: "测试收入", Note: "已确认"})
	if err != nil || annotated.AmountMinor != "2000" {
		t.Fatal(annotated, err)
	}
	next := CorrectionInput{Version: 3, Type: "expense", AmountMinor: "1", Reason: "最小金额更正"}
	preview, err = s.PreviewCorrection(original.ID, next)
	if err != nil || preview.IncomeDeltaMinor != "-2000" || preview.ExpenseDeltaMinor != "1" || preview.NetDeltaMinor != "-2001" {
		t.Fatal(preview, err)
	}
	if _, err = s.Correct(original.ID, next); err != nil {
		t.Fatal(err)
	}
	if d = dashboard(t, s); d.IncomeMinor != "0" || d.ExpenseMinor != "1" || d.Categories[0].Name != "测试收入" {
		t.Fatal(d)
	}
}

func TestCorrectionValidationNoopAndRollback(t *testing.T) {
	s := testStore(t)
	record, err := s.Create(testInput(), "correction-synthetic-01")
	if err != nil {
		t.Fatal(err)
	}
	good := CorrectionInput{Version: 1, Type: "expense", AmountMinor: "1234", Reason: "无需变动"}
	baseline := dashboard(t, s)
	logCount := count(t, s, "change_log")
	out, err := s.Correct(record.ID, good)
	if err != nil || out != record || dashboard(t, s).Version != baseline.Version || count(t, s, "change_log") != logCount {
		t.Fatal(out, err)
	}
	for _, amount := range []string{"", "0", "-1", "+1", "01", "1.00", "1e3", " 1", "1 ", "1000000000001", "99999999999999999999", "NaN"} {
		bad := good
		bad.AmountMinor = amount
		if _, err = s.PreviewCorrection(record.ID, bad); err == nil {
			t.Fatalf("accepted invalid amount %q", amount)
		}
		if _, err = s.Correct(record.ID, bad); err == nil {
			t.Fatalf("committed invalid amount %q", amount)
		}
	}
	for _, kind := range []string{"refund", "transfer", "repayment", "支出", "income "} {
		bad := good
		bad.Type = kind
		if _, err = s.Correct(record.ID, bad); err == nil {
			t.Fatalf("accepted %q", kind)
		}
	}
	for _, reason := range []string{"", " ", strings.Repeat("字", 501), "a\x00b"} {
		bad := good
		bad.Reason = reason
		if _, err = s.Correct(record.ID, bad); err == nil {
			t.Fatal("accepted invalid reason")
		}
	}
	max := good
	max.AmountMinor = "1000000000000"
	max.Type = "income"
	impact, err := s.PreviewCorrection(record.ID, max)
	if err != nil || impact.NetDeltaMinor != "1000000001234" {
		t.Fatal(impact, err)
	}
	if _, err = s.db.Exec("CREATE TRIGGER reject_correct BEFORE UPDATE ON postings BEGIN SELECT RAISE(ABORT,'synthetic posting failure'); END;"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Correct(record.ID, max); err == nil {
		t.Fatal("posting failure ignored")
	}
	if dashboard(t, s).Version != baseline.Version || dashboard(t, s).Transactions[0] != record || count(t, s, "change_log") != logCount {
		t.Fatal("partial correction persisted")
	}
	if count(t, s, "accounts") != 2 {
		t.Fatal("account creation was not rolled back")
	}
}

func TestCorrectionRejectsInconsistentPostingAndConcurrentConfirmation(t *testing.T) {
	s := testStore(t)
	transaction, err := s.Create(testInput(), "concurrent-correction-fixture")
	if err != nil {
		t.Fatal(err)
	}
	input := CorrectionInput{Version: 1, Type: "income", AmountMinor: "2000", Reason: "并发合成更正"}
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := s.Correct(transaction.ID, input); results <- err }()
	}
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		} else if p, ok := err.(*Problem); !ok || p.Code != "conflict" {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("concurrent confirmations succeeded", successes)
	}
	history, err := s.CorrectionHistory(transaction.ID)
	if err != nil || len(history) != 1 {
		t.Fatal(history, err)
	}
	// Balanced but wrong-value postings must not be silently overwritten.
	if _, err = s.db.Exec("UPDATE postings SET amount_minor=amount_minor*2 WHERE transaction_id=?", transaction.ID); err != nil {
		t.Fatal(err)
	}
	input.Version = 2
	input.AmountMinor = "3000"
	if _, err = s.Correct(transaction.ID, input); err == nil {
		t.Fatal("inconsistent postings accepted")
	}
	if dashboard(t, s).Transactions[0].AmountMinor != "2000" {
		t.Fatal("failed correction changed transaction")
	}
}
