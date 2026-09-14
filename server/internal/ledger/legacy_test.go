package ledger

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func legacyFixture(id string) legacyRecord {
	r := legacyRecord{ID: id, Time: "2026-01-02 12:00:00", Source: "合成来源", Account: "合成账户", Direction: "expense", Amount: json.Number("12.34"), Currency: "CNY", Merchant: "合成商户" + id, Category: "餐饮", Status: "交易成功", Included: true, Disposition: "included_spend", Canonical: true}
	r.Lineage.RawID = "raw-" + id
	r.Lineage.BatchID = "batch-synthetic"
	r.Lineage.FileHash = strings.Repeat("a", 64)
	return r
}
func legacySnapshot(rows ...legacyRecord) []byte {
	b, _ := json.Marshal(map[string]any{"schemaVersion": "1.0", "audit": map[string]any{"totalTransactions": len(rows), "transactions": rows}})
	return b
}
func TestLegacyIsolationAndExactMoney(t *testing.T) {
	good := legacyFixture("good")
	refund := legacyFixture("refund")
	refund.Direction = "refund"
	related := legacyFixture("related")
	related.Merchant = refund.Merchant
	excluded := legacyFixture("excluded")
	excluded.Excluded = true
	income := legacyFixture("income")
	income.Direction = "income"
	partial := legacyFixture("partial")
	partial.Status = "已退款¥1.00"
	precision := legacyFixture("precision")
	precision.Amount = json.Number("12.345")
	missing := legacyFixture("missing")
	missing.Lineage.RawID = ""
	foreign := legacyFixture("foreign")
	foreign.Currency = "USD"
	p, err := PrepareLegacy(legacySnapshot(good, refund, related, excluded, income, partial, precision, missing, foreign))
	if err != nil {
		t.Fatal(err)
	}
	if p.Candidates != 1 || p.Review != 7 || p.Archived != 1 || p.CandidateExpenseMinor != "1234" {
		t.Fatalf("unexpected counts %+v", p)
	}
	rows, issues := parseCSV(p.Files[0].CSV)
	if len(issues) > 0 || len(rows) != 1 || rows[0].Record.AmountMinor != "-1234" || rows[0].Record.ExternalID != "ai-financial:good" {
		t.Fatalf("invalid output %v %v", rows, issues)
	}
}
func TestLegacyImportRetryAndConflict(t *testing.T) {
	r := legacyFixture("stable")
	p, err := PrepareLegacy(legacySnapshot(r))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	preview, err := s.Preview(p.Files[0].Name, p.Files[0].CSV)
	if err != nil || len(preview.Errors) > 0 {
		t.Fatalf("%v %+v", err, preview)
	}
	result, err := s.Commit(preview.ID, preview.LedgerVersion, false)
	if err != nil || result.Added != 1 {
		t.Fatalf("%v %+v", err, result)
	}
	retry, err := s.Preview(p.Files[0].Name, p.Files[0].CSV)
	if err != nil || !retry.AlreadyCommitted {
		t.Fatalf("retry %v %+v", err, retry)
	}
	// A changed file with the same stable identity must still skip the old row.
	preview, err = s.Preview("other.csv", p.Files[0].CSV+"\n")
	if err != nil || preview.DuplicateCount != 1 {
		t.Fatalf("duplicate %v %+v", err, preview)
	}
	r.Amount = json.Number("13.00")
	changed, err := PrepareLegacy(legacySnapshot(r))
	if err != nil {
		t.Fatal(err)
	}
	preview, err = s.Preview("changed.csv", changed.Files[0].CSV)
	if err != nil || len(preview.Errors) == 0 {
		t.Fatalf("expected conflict %v %+v", err, preview)
	}
}
func TestLegacyRejectsMalformedSnapshots(t *testing.T) {
	good := legacyFixture("a")
	for _, raw := range [][]byte{[]byte(`{}`), []byte(`{"schemaVersion":"2.0"}`), legacySnapshot(good, good), []byte(`{"schemaVersion":"1.0","audit":{"totalTransactions":2,"transactions":[]}}`)} {
		if _, err := PrepareLegacy(raw); err == nil {
			t.Fatal("accepted malformed snapshot")
		}
	}
}
func TestLegacySplitsDeterministically(t *testing.T) {
	rows := make([]legacyRecord, 1001)
	for i := range rows {
		rows[i] = legacyFixture(fmt.Sprintf("%04d", i))
	}
	raw := legacySnapshot(rows...)
	a, err := PrepareLegacy(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := PrepareLegacy(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Files) != 2 || a.Files[0].Rows != 1000 || a.Files[1].Rows != 1 || a.Files[0].SHA256 != b.Files[0].SHA256 {
		t.Fatal("batch limits or determinism")
	}
	for _, f := range a.Files {
		if _, issues := parseCSV(f.CSV); len(issues) > 0 {
			t.Fatal(issues)
		}
	}
}

func TestLegacySplitsLargeUTF8Notes(t *testing.T) {
	rows := make([]legacyRecord, 600)
	for i := range rows {
		rows[i] = legacyFixture(fmt.Sprintf("%04d", i))
		rows[i].Notes = strings.Repeat("合", 700)
	}
	p, err := PrepareLegacy(legacySnapshot(rows...))
	if err != nil {
		t.Fatal(err)
	}
	if p.Candidates != 600 || len(p.Files) < 2 {
		t.Fatal("did not split large UTF-8 data")
	}
	for _, file := range p.Files {
		if len(file.CSV) > MaxCSVBytes/2 {
			t.Fatal("exceeded encoded byte budget")
		}
		if _, issues := parseCSV(file.CSV); len(issues) != 0 {
			t.Fatal(issues)
		}
	}
}

func TestLegacyHoldsAllMembersOfSimilarityGroups(t *testing.T) {
	one := legacyFixture("one")
	two := legacyFixture("two")
	two.Merchant = one.Merchant
	p, err := PrepareLegacy(legacySnapshot(one, two))
	if err != nil {
		t.Fatal(err)
	}
	if p.Candidates != 0 || p.Review != 2 || p.CandidateExpenseMinor != "0" || len(p.Files) != 0 {
		t.Fatal("similarity group was auto-approved", p)
	}
}
