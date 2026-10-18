package ledger

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

func nativeEvidenceSaved(t *testing.T, s *Store, id string) (Preview, string) {
	t.Helper()
	var p Preview
	var raw, body string
	if e := s.db.QueryRow("SELECT raw_csv,preview_json FROM imports WHERE id=? AND ledger_id=?", id, s.ledgerID).Scan(&raw, &body); e != nil {
		t.Fatal(e)
	}
	if e := json.Unmarshal([]byte(body), &p); e != nil {
		t.Fatal(e)
	}
	return p, raw
}
func nativeEvidenceTables(t *testing.T, s *Store) map[string][][]any {
	t.Helper()
	names, e := s.db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if e != nil {
		t.Fatal(e)
	}
	var tables []string
	for names.Next() {
		var name string
		if e = names.Scan(&name); e != nil {
			t.Fatal(e)
		}
		tables = append(tables, name)
	}
	if e = names.Err(); e != nil {
		t.Fatal(e)
	}
	names.Close()
	out := map[string][][]any{}
	for _, name := range tables {
		rows, e := s.db.Query(`SELECT * FROM "` + strings.ReplaceAll(name, `"`, `""`) + `"`)
		if e != nil {
			t.Fatal(e)
		}
		cols, e := rows.Columns()
		if e != nil {
			t.Fatal(e)
		}
		var data [][]any
		for rows.Next() {
			v := make([]any, len(cols))
			p := make([]any, len(cols))
			for i := range v {
				p[i] = &v[i]
			}
			if e = rows.Scan(p...); e != nil {
				t.Fatal(e)
			}
			data = append(data, v)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		rows.Close()
		out[name] = data
	}
	return out
}
func nativeEvidenceConflict(t *testing.T, s *Store, run func() error) {
	t.Helper()
	before := nativeEvidenceTables(t, s)
	err := run()
	var p *Problem
	if !errors.As(err, &p) || p.Code != "conflict" {
		t.Fatalf("expected conservative conflict, got %v", err)
	}
	if strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("bad evidence echoed")
	}
	if after := nativeEvidenceTables(t, s); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected refresh mutated application tables")
	}
}

func TestNativeEvidenceStoreRepeatRefreshesVersionAndCommits(t *testing.T) {
	s := testStore(t)
	csv := nativeHeader + nativeExpense + nativeRefund
	p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
	first, raw := nativeEvidenceSaved(t, s, p.ID)
	first.Pending[0].Reason = "首次合成原因应保留"
	if _, e := s.db.Exec("UPDATE imports SET preview_json=? WHERE id=?", encode(first), p.ID); e != nil {
		t.Fatal(e)
	}
	var initial []any = make([]any, 5)
	ptr := make([]any, 5)
	for i := range initial {
		ptr[i] = &initial[i]
	}
	if e := s.db.QueryRow("SELECT filename,content_hash,raw_csv,parser_version,created_at FROM imports WHERE id=?", p.ID).Scan(ptr...); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Create(Input{Date: "2026-09-20", Type: "income", Amount: "1.00", Currency: "CNY", Merchant: "合成独立商户", Source: "合成手动来源"}, "independent-version"); e != nil {
		t.Fatal(e)
	}
	refreshed, e := s.PreviewAlipay("renamed-synthetic.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
	if e != nil {
		t.Fatal(e)
	}
	if refreshed.ID != p.ID || refreshed.Filename != first.Filename || refreshed.LedgerVersion <= p.LedgerVersion || len(refreshed.Pending) != 1 || refreshed.Pending[0].Reason != first.Pending[0].Reason || !reflect.DeepEqual(refreshed.SourceDocument, first.SourceDocument) {
		t.Fatal("immutable evidence or live version incorrect", refreshed)
	}
	var current []any = make([]any, 5)
	for i := range current {
		ptr[i] = &current[i]
	}
	if e = s.db.QueryRow("SELECT filename,content_hash,raw_csv,parser_version,created_at FROM imports WHERE id=?", p.ID).Scan(ptr...); e != nil || !reflect.DeepEqual(initial, current) {
		t.Fatal("original import envelope metadata changed", e)
	}
	_, savedRaw := nativeEvidenceSaved(t, s, p.ID)
	if savedRaw != raw {
		t.Fatal("source bytes changed")
	}
	r, e := s.Commit(refreshed.ID, refreshed.LedgerVersion, false)
	if e != nil || r.Added != 1 || r.Pending != 1 {
		t.Fatal("fresh version no longer committable", r, e)
	}
}

func TestNativeEvidenceStoreRejectsExtractionDrift(t *testing.T) {
	cases := map[string]func(*ingestion.Document){
		"amount": func(d *ingestion.Document) {
			d.Records[0].Fields.Amount = "13.00"
			d.Records[0].Raw["金额"] = "13.00"
		},
		"date": func(d *ingestion.Document) {
			d.Records[0].Fields.Date = "2026-09-16 10:20:30"
			d.Records[0].Raw["交易时间"] = d.Records[0].Fields.Date
		},
		"unknown raw evidence": func(d *ingestion.Document) { d.Records[0].Raw["新增合成列"] = "synthetic-secret" },
		"scope":                func(d *ingestion.Document) { d.Records[0].Identifiers[0].Scope = `["alipay","other-wallet"]` },
		"source account":       func(d *ingestion.Document) { d.Records[0].Fields.Account = "different" },
		"ordinal":              func(d *ingestion.Document) { d.Records[0].Ordinal = 9 },
		"line":                 func(d *ingestion.Document) { d.Records[0].Line = 9 },
		"document version":     func(d *ingestion.Document) { d.ParserVersion++ },
		"hash":                 func(d *ingestion.Document) { d.ContentHash = hash("different original source") },
		"issues": func(d *ingestion.Document) {
			d.Issues = append(d.Issues, ingestion.Issue{Line: 2, Code: "synthetic", Message: "synthetic-secret"})
		},
		"pending to ordinary": func(d *ingestion.Document) {
			r := &d.Records[1]
			r.Fields.Category = "餐饮美食"
			r.Fields.Type = "支出"
			r.Raw["交易分类"] = "餐饮美食"
			r.Raw["收/支"] = "支出"
			r.Raw["交易状态"] = "交易成功"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense+nativeRefund), "synthetic-wallet")
			old, raw := nativeEvidenceSaved(t, s, p.ID)
			d := old.SourceDocument
			mutate(d)
			nativeEvidenceConflict(t, s, func() error { _, e := s.preview("new-name.csv", raw, d); return e })
		})
	}
}

func TestNativeEvidenceStoreRejectsCorruptOrReinterpretedSnapshot(t *testing.T) {
	cases := map[string]func(Preview) string{
		"invalid JSON":            func(p Preview) string { return `{broken synthetic-secret}` },
		"legacy document missing": func(p Preview) string { p.SourceDocument = nil; return encode(p) },
		"unknown root field": func(p Preview) string {
			return strings.TrimSuffix(encode(p), "}") + `,"syntheticUnknown":"synthetic-secret"}`
		},
		"unicode alias": func(p Preview) string {
			return strings.Replace(encode(p), `"sourceAccount":"synthetic-wallet"`, `"sourceAccount":"other","ſourceAccount":"synthetic-wallet"`, 1)
		},
		"duplicate ordinal": func(p Preview) string {
			p.SourceDocument.Records[1].Ordinal = p.SourceDocument.Records[0].Ordinal
			return encode(p)
		},
		"source field normalized": func(p Preview) string { p.Rows[0].Record.Category = "changed-gate-category"; return encode(p) },
		"pending allocation": func(p Preview) string {
			r := p.Rows[0]
			f := p.SourceDocument.Records[0].Fields
			p.Pending = append(p.Pending, PendingRow{Line: r.Line, Date: f.Date, Merchant: f.Merchant, Amount: f.Amount, Reason: "old rule blocked"})
			p.Rows = nil
			p.NewCount = 0
			return encode(p)
		},
		"unexplained error": func(p Preview) string { p.Errors = []string{"synthetic-secret"}; return encode(p) },
		"fake dynamic error": func(p Preview) string {
			p.Rows[0].Status = "conflict"
			p.Rows[0].Message = "synthetic-secret"
			p.Errors = []string{fmt.Sprintf("第 %d 行：synthetic-secret", p.Rows[0].Line)}
			return encode(p)
		},
		"unknown row status": func(p Preview) string { p.Rows[0].Status = "invented"; return encode(p) },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			csv := nativeHeader + nativeExpense + nativeRefund
			p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
			if _, e := s.db.Exec("UPDATE imports SET preview_json=? WHERE id=?", corrupt(p), p.ID); e != nil {
				t.Fatal(e)
			}
			nativeEvidenceConflict(t, s, func() error {
				_, e := s.PreviewAlipay("repeat.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
				return e
			})
		})
	}
	for _, column := range []string{"raw_csv", "parser_version"} {
		t.Run(column, func(t *testing.T) {
			s := testStore(t)
			csv := nativeHeader + nativeExpense
			p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
			var value any = "synthetic-secret"
			if column == "parser_version" {
				value = 1
			}
			if _, e := s.db.Exec("UPDATE imports SET "+column+"=? WHERE id=?", value, p.ID); e != nil {
				t.Fatal(e)
			}
			nativeEvidenceConflict(t, s, func() error {
				_, e := s.PreviewAlipay("repeat.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
				return e
			})
		})
	}
}

func TestNativeEvidenceStoreCommittedAndUndoIgnoreNewParserFailure(t *testing.T) {
	s := testStore(t)
	csv := nativeHeader + nativeExpense + nativeRefund
	p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
	_, raw := nativeEvidenceSaved(t, s, p.ID)
	r, e := s.Commit(p.ID, p.LedgerVersion, false)
	if e != nil {
		t.Fatal(e)
	}
	for phase := 0; phase < 3; phase++ {
		first, _ := nativeEvidenceSaved(t, s, p.ID)
		d := first.SourceDocument
		d.Issues = append(d.Issues, ingestion.Issue{Line: 2, Code: "synthetic-new-parser", Message: "synthetic-secret"})
		before := nativeEvidenceTables(t, s)
		replay, e := s.preview("new-parser.csv", raw, d)
		if e != nil || !replay.AlreadyCommitted || replay.Undone != (phase == 1) || len(replay.Errors) != 0 || len(replay.Pending) != 1 {
			t.Fatal("committed snapshot reinterpreted", phase, replay, e)
		}
		if after := nativeEvidenceTables(t, s); !reflect.DeepEqual(before, after) {
			t.Fatal("committed replay wrote state")
		}
		rr, e := s.Commit(p.ID, p.LedgerVersion, false)
		if e != nil || rr != r {
			t.Fatal("commit retry lost original result", rr, e)
		}
		if phase < 2 {
			u, e := s.PreviewImportUndo(p.ID, phase == 1)
			if e != nil {
				t.Fatal(e)
			}
			if phase == 0 {
				_, e = s.UndoImport(p.ID, u.LedgerVersion)
			} else {
				_, e = s.RestoreImport(p.ID, u.LedgerVersion)
			}
			if e != nil {
				t.Fatal(e)
			}
		}
	}
}

func TestNativeEvidenceStoreDuplicateAfterCorrectionKeepsSourceAmount(t *testing.T) {
	s := testStore(t)
	raw := nativeHeader + nativeExpense
	p := alipayPreview(t, s, []byte(raw), "synthetic-wallet")
	other := alipayPreview(t, s, []byte(strings.ReplaceAll(raw, "\n", "\r\n")), "synthetic-wallet")
	r, e := s.Commit(other.ID, other.LedgerVersion, false)
	if e != nil || r.Added != 1 {
		t.Fatal(r, e)
	}
	detail, e := s.ImportDetail(other.ID)
	if e != nil || len(detail.Evidence) != 1 {
		t.Fatal(detail, e)
	}
	id := detail.Evidence[0].TransactionID
	corrected, e := s.Correct(id, CorrectionInput{Version: 1, Type: "expense", AmountMinor: "1999", Reason: "合成更正"})
	if e != nil {
		t.Fatal(e)
	}
	repeated, e := s.PreviewAlipay("repeat.csv", base64.StdEncoding.EncodeToString([]byte(raw)), "synthetic-wallet")
	if e != nil || repeated.DuplicateCount != 1 || len(repeated.Rows) != 1 || repeated.Rows[0].Record.AmountMinor != "-1234" || repeated.Rows[0].Record.ID != id {
		t.Fatal("current corrected transaction replaced original evidence", repeated, e)
	}
	result, e := s.Commit(p.ID, repeated.LedgerVersion, false)
	if e != nil || result.Skipped != 1 || result.Added != 0 {
		t.Fatal(result, e)
	}
	after, e := scanRecord(s.db.QueryRow("SELECT "+transactionColumns+" FROM transactions WHERE ledger_id=? AND id=?", s.ledgerID, id))
	if e != nil || after.AmountMinor != corrected.AmountMinor || after.Version != corrected.Version {
		t.Fatal("duplicate import changed correction", after, e)
	}
}
