package ledger

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImportHistoryPreservesDuplicateEvidence(t *testing.T) {
	s := testStore(t)
	p := preview(t, s, sampleCSV)
	d, err := s.ImportDetail(p.ID)
	if err != nil || d.Result != nil || d.CommittedAt != nil || len(d.Evidence) != 0 {
		t.Fatal("uncommitted evidence", d, err)
	}
	if _, err = s.Commit(p.ID, p.LedgerVersion, false); err != nil {
		t.Fatal(err)
	}
	// Byte-different exports retain separate document occurrences, while the
	// existing source identity links each occurrence to the same transaction.
	q := preview(t, s, strings.ReplaceAll(sampleCSV, "\n", "\r\n"))
	if _, err = s.Commit(q.ID, q.LedgerVersion, false); err != nil {
		t.Fatal(err)
	}
	d, err = s.ImportDetail(q.ID)
	if err != nil || d.Result == nil || d.Result.Skipped != 3 || len(d.Evidence) != 3 || !d.Preview.AlreadyCommitted {
		t.Fatal(d, err)
	}
	for i, e := range d.Evidence {
		if e.Disposition != "duplicate" || e.Line != i+2 || e.TransactionID != e.Record.ID {
			t.Fatal(e)
		}
	}
	h, err := s.ImportHistory(1)
	if err != nil || h.TotalCount != 2 || len(h.Imports) != 2 {
		t.Fatal(h, err)
	}
	h, err = s.ImportHistory(2)
	if err != nil || h.TotalCount != 2 || len(h.Imports) != 0 {
		t.Fatal(h, err)
	}
	if _, err = s.ImportHistory(0); err == nil {
		t.Fatal("invalid page accepted")
	}
	if _, err = s.ImportDetail("missing"); err == nil {
		t.Fatal("missing import accepted")
	}
	encoded, _ := json.Marshal(d)
	if strings.Contains(string(encoded), "raw_csv") || strings.Contains(string(encoded), "date,type,amount") {
		t.Fatal("raw file leaked")
	}
	if _, err = s.Commit(q.ID, q.LedgerVersion, false); err != nil {
		t.Fatal(err)
	}
	again, err := s.ImportDetail(q.ID)
	if err != nil || len(again.Evidence) != 3 || count(t, s, "transactions") != 3 {
		t.Fatal("retry duplicated evidence", err)
	}
}

func TestCSVExtractionCannotBypassFinancialValidation(t *testing.T) {
	s := testStore(t)
	for _, csv := range []string{strings.Replace(sampleCSV, "expense", "refund", 1), strings.Replace(sampleCSV, "28.50", "28.501", 1)} {
		p, err := s.Preview("unsupported.csv", csv)
		if err != nil || len(p.Errors) == 0 || p.ID != "" {
			t.Fatal("invalid evidence became committable", p, err)
		}
	}
	if count(t, s, "imports") != 0 || count(t, s, "transactions") != 0 {
		t.Fatal("invalid input persisted")
	}
}
