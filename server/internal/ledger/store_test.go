package ledger

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const sampleCSV = "date,type,amount,currency,merchant,category,source,account,external_id,note\n2026-09-14,expense,28.50,CNY,咖啡,餐饮,支付宝,待核实资金账户,A001,\n2026-09-13,expense,96.00,CNY,书店,学习,微信,待核实资金账户,W001,\n2026-09-10,income,25000.00,CNY,工资,工资,银行,待核实资金账户,B001,\n"

func testStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "monee.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func testInput() Input {
	return Input{Date: "2026-09-14", Type: "expense", Amount: "12.34", Currency: "CNY", Merchant: "中文商户", Category: "餐饮", Source: "手动"}
}
func dashboard(t *testing.T, s *Store) Dashboard {
	t.Helper()
	d, e := s.Dashboard("2026-09", "", 1)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func preview(t *testing.T, s *Store, csv string) Preview {
	t.Helper()
	p, e := s.Preview("sample.csv", csv)
	if e != nil || len(p.Errors) > 0 {
		t.Fatalf("preview: %v %+v", e, p.Errors)
	}
	return p
}
func count(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if e := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

func TestExactMoneyAndValidation(t *testing.T) {
	for input, want := range map[string]int64{"0.01": 1, "12.3": 1230, "10000000000.00": 1_000_000_000_000} {
		n, e := parseAmount(input)
		if e != nil || n != want {
			t.Fatalf("%s: %d %v", input, n, e)
		}
	}
	for _, input := range []string{"0", "-1.00", "1.001", "1e3", "NaN", "1,000", "10000000000.01", ""} {
		if _, e := parseAmount(input); e == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	for _, modify := range []func(*Input){func(i *Input) { i.Currency = "USD" }, func(i *Input) { i.Type = "refund" }, func(i *Input) { i.Date = "2026-02-29" }, func(i *Input) { i.Merchant = "" }} {
		in := testInput()
		modify(&in)
		if _, e := normalize(in); e == nil {
			t.Fatalf("accepted invalid input %+v", in)
		}
	}
}
func TestPersistentBalancedAndIdempotentCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	record, e := s.Create(testInput(), "stable-request-00001")
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Create(testInput(), "stable-request-00001")
	if e != nil || record.ID != again.ID {
		t.Fatal("retry changed identity", e)
	}
	changed := testInput()
	changed.Amount = "99"
	if _, e = s.Create(changed, "stable-request-00001"); e == nil {
		t.Fatal("idempotency key reused for different content")
	}
	oldLedger := s.ledgerID
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	d := dashboard(t, s)
	if d.TotalCount != 1 || d.ExpenseMinor != "1234" || d.Transactions[0].ID != record.ID || s.ledgerID != oldLedger {
		t.Fatalf("lost state: %+v", d)
	}
	var balance int64
	if e = s.db.QueryRow("SELECT SUM(amount_minor) FROM postings WHERE transaction_id=?", record.ID).Scan(&balance); e != nil || balance != 0 {
		t.Fatal("unbalanced", balance, e)
	}
	if count(t, s, "postings") != 2 || count(t, s, "change_log") != 3 {
		t.Fatal("missing atomic postings/account changes")
	}
	var payload string
	s.db.QueryRow("SELECT payload_json FROM change_log WHERE entity_type='transaction'").Scan(&payload)
	var decoded map[string]any
	if e = json.Unmarshal([]byte(payload), &decoded); e != nil || decoded["postings"] == nil {
		t.Fatal("incomplete change payload")
	}
	search, e := s.Dashboard("2026-09", "中文", 1)
	if e != nil || search.FilteredCount != 1 || search.ExpenseMinor != "1234" {
		t.Fatal("search/summary mismatch", e)
	}
}
func TestCSVPreviewCommitAndRetry(t *testing.T) {
	s := testStore(t)
	p := preview(t, s, "\ufeff"+sampleCSV)
	if p.NewCount != 3 || dashboard(t, s).TotalCount != 0 {
		t.Fatal("preview wrote ledger")
	}
	result, e := s.Commit(p.ID, p.LedgerVersion, false)
	if e != nil || result.Added != 3 {
		t.Fatalf("commit %+v %v", result, e)
	}
	retry, e := s.Commit(p.ID, p.LedgerVersion, false)
	if e != nil || retry != result {
		t.Fatal("commit retry not stable", e)
	}
	repeated := preview(t, s, "\ufeff"+sampleCSV)
	if !repeated.AlreadyCommitted {
		t.Fatal("same file not recognized")
	}
	d := dashboard(t, s)
	if d.TotalCount != 3 || d.ExpenseMinor != "12450" || d.IncomeMinor != "2500000" {
		t.Fatalf("wrong totals %+v", d)
	}
	if count(t, s, "source_records") != 3 {
		t.Fatal("source provenance missing")
	}
	// Same external IDs in a differently encoded file are still duplicates.
	overlap := preview(t, s, strings.ReplaceAll(sampleCSV, "\n", "\r\n"))
	if overlap.NewCount != 0 || overlap.DuplicateCount != 3 {
		t.Fatalf("overlap %+v", overlap)
	}
	if _, e = s.Commit(overlap.ID, overlap.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	if dashboard(t, s).TotalCount != 3 || count(t, s, "source_records") != 6 {
		t.Fatal("duplicate was added or provenance discarded")
	}
}
func TestCSVFailureIsNotPartial(t *testing.T) {
	s := testStore(t)
	for _, csv := range []string{strings.Replace(sampleCSV, "28.50", "28.501", 1), strings.Replace(sampleCSV, "expense", "transfer", 1), "date,type,amount,currency,merchant,source\n2026-09-14,expense,1,CNY,broken\n", strings.Repeat("x", MaxCSVBytes+1)} {
		p, e := s.Preview("bad.csv", csv)
		if e != nil || len(p.Errors) == 0 {
			t.Fatalf("invalid CSV accepted %v", e)
		}
	}
	if dashboard(t, s).TotalCount != 0 || count(t, s, "imports") != 0 {
		t.Fatal("invalid preview left partial data")
	}
}
func TestSameIDConflictAndWithinFileDuplicate(t *testing.T) {
	s := testStore(t)
	lines := strings.Split(sampleCSV, "\n")
	csv := lines[0] + "\n" + lines[1] + "\n" + lines[1] + "\n"
	p := preview(t, s, csv)
	if p.NewCount != 1 || p.DuplicateCount != 1 {
		t.Fatalf("wrong duplicate counts %+v", p)
	}
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	conflict, e := s.Preview("changed.csv", strings.Replace(csv, "28.50", "30.00", 1))
	if e != nil || len(conflict.Errors) == 0 {
		t.Fatal("source identity conflict not detected")
	}
	if dashboard(t, s).TotalCount != 1 || count(t, s, "source_records") != 2 {
		t.Fatal("duplicate identity was added")
	}
}
func TestSimilarRequiresConfirmationAndStalePreview(t *testing.T) {
	s := testStore(t)
	p := preview(t, s, sampleCSV)
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	similarCSV := strings.ReplaceAll(strings.ReplaceAll(sampleCSV, "A001", "A002"), "W001", "W002")
	p = preview(t, s, similarCSV)
	if p.SimilarCount != 2 {
		t.Fatalf("expected 2 similarities %+v", p)
	}
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e == nil {
		t.Fatal("similar records silently committed")
	}
	if _, e := s.Create(testInput(), "new-request-0000001"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Commit(p.ID, p.LedgerVersion, true); e == nil {
		t.Fatal("stale preview accepted")
	}
	p = preview(t, s, similarCSV)
	if _, e := s.Commit(p.ID, p.LedgerVersion, true); e != nil {
		t.Fatal(e)
	}
	if dashboard(t, s).TotalCount != 6 {
		t.Fatal("unexpected count")
	}
}
func TestConcurrentRetriesCreateOnlyOnce(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record, e := s.Create(testInput(), "concurrent-request-01")
			ids <- record.ID
			errs <- e
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("multiple identities")
		}
	}
	if dashboard(t, s).TotalCount != 1 {
		t.Fatal("duplicate concurrent request")
	}
}
func TestPostingFailureRollsBackWholeTransaction(t *testing.T) {
	s := testStore(t)
	_, e := s.db.Exec("CREATE TRIGGER reject_post BEFORE INSERT ON postings BEGIN SELECT RAISE(ABORT,'injected failure'); END;")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(testInput(), "rollback-request-0001"); e == nil {
		t.Fatal("injected failure ignored")
	}
	for _, table := range []string{"transactions", "accounts", "postings", "change_log", "idempotency"} {
		if count(t, s, table) != 0 {
			t.Fatalf("partial write to %s", table)
		}
	}
	if dashboard(t, s).Version != 0 {
		t.Fatal("version changed on failed write")
	}
}
func TestPaginationDoesNotChangeMonthSummary(t *testing.T) {
	s := testStore(t)
	for i := range 53 {
		in := testInput()
		in.Merchant = string(rune('一' + i))
		if _, e := s.Create(in, strings.Repeat("k", 16)+in.Merchant); e != nil {
			t.Fatal(e)
		}
	}
	a, e := s.Dashboard("2026-09", "", 1)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Dashboard("2026-09", "", 2)
	if e != nil {
		t.Fatal(e)
	}
	if len(a.Transactions) != 50 || len(b.Transactions) != 3 || a.ExpenseMinor != b.ExpenseMinor || a.TotalCount != 53 {
		t.Fatal("pagination changed totals")
	}
}
func TestFutureSchemaRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	s.db.Exec("PRAGMA user_version=2")
	s.Close()
	if reopened, e := Open(path); e == nil {
		reopened.Close()
		t.Fatal("future schema accepted")
	}
}
