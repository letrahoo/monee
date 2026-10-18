package ledger

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

func metadataHas(out ReviewItemMetadata, code string) bool {
	for _, b := range out.Blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}
func metadataFixture() (Preview, reviewMetadataSnapshot) {
	d := ingestion.ParseAlipayCSV([]byte(nativeHeader+nativeExpense), "synthetic-wallet")
	f := d.Records[0].Fields
	p := Preview{ID: "synthetic-import", Format: "alipay", SourceAccount: "synthetic-wallet", SourceDocument: &d, Pending: []PendingRow{{Line: 2, Date: f.Date, Merchant: f.Merchant, Amount: f.Amount, Reason: "not authoritative"}}, Rows: []PreviewRow{}, Errors: []string{}}
	return p, reviewMetadataSnapshot{previewJSON: encode(p), envelopeVersion: 2, envelopeHash: hash("separate envelope")}
}
func TestReviewItemMetadataPureEvidence(t *testing.T) {
	cases := []struct {
		name, classification, block string
		mutate                      func(*Preview, *reviewMetadataSnapshot)
	}{
		{"ordinary", "ordinary_candidate", "", func(*Preview, *reviewMetadataSnapshot) {}},
		{"reason ignored", "ordinary_candidate", "", func(p *Preview, _ *reviewMetadataSnapshot) { p.Pending[0].Reason = "退款转账任意显示原因" }},
		{"missing doc", "unknown", "document_missing", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument = nil }},
		{"standard", "unknown", "format_unsupported", func(p *Preview, _ *reviewMetadataSnapshot) { p.Format = ""; p.SourceDocument = nil; p.Pending = nil }},
		{"missing raw", "unknown", "raw_fields_conflict", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument.Records[0].Raw = nil }},
		{"missing account", "unknown", "source_account_invalid", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceAccount = "" }},
		{"account conflict", "unknown", "source_account_invalid", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument.Records[0].Fields.Account = "other" }},
		{"hash missing", "unknown", "hash_missing_or_invalid", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument.ContentHash = "" }},
		{"hash bad", "unknown", "hash_missing_or_invalid", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument.ContentHash = "oops" }},
		{"envelope hash", "unknown", "hash_missing_or_invalid", func(_ *Preview, s *reviewMetadataSnapshot) { s.envelopeHash = "" }},
		{"doc version", "unknown", "parser_unsupported", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument.ParserVersion = 2 }},
		{"envelope version", "unknown", "parser_unsupported", func(_ *Preview, s *reviewMetadataSnapshot) { s.envelopeVersion = 1 }},
		{"wrong parser", "unknown", "parser_unsupported", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument.Parser = "wechat-csv" }},
		{"wrong format", "unknown", "parser_unsupported", func(p *Preview, _ *reviewMetadataSnapshot) { p.Format = "wechat" }},
		{"duplicate record", "unknown", "record_ambiguous", func(p *Preview, _ *reviewMetadataSnapshot) {
			p.SourceDocument.Records = append(p.SourceDocument.Records, p.SourceDocument.Records[0])
		}},
		{"duplicate pending", "unknown", "pending_ambiguous", func(p *Preview, _ *reviewMetadataSnapshot) { p.Pending = append(p.Pending, p.Pending[0]) }},
		{"duplicate ordinal", "unknown", "ordinal_ambiguous", func(p *Preview, _ *reviewMetadataSnapshot) {
			r := p.SourceDocument.Records[0]
			r.Line = 3
			p.SourceDocument.Records = append(p.SourceDocument.Records, r)
		}},
		{"zero ordinal", "unknown", "ordinal_ambiguous", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument.Records[0].Ordinal = 0 }},
		{"pending conflict", "unknown", "pending_conflict", func(p *Preview, _ *reviewMetadataSnapshot) { p.Pending[0].Amount = "99" }},
		{"rows conflict", "unknown", "pending_conflict", func(p *Preview, _ *reviewMetadataSnapshot) { p.Rows = []PreviewRow{{Line: 2}} }},
		{"raw conflict", "unknown", "raw_fields_conflict", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument.Records[0].Raw["收/支"] = "收入" }},
		{"alias conflict", "unknown", "raw_fields_conflict", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument.Records[0].Raw["金额(元)"] = "12.34" }},
		{"scope conflict", "unknown", "identifier_conflict", func(p *Preview, _ *reviewMetadataSnapshot) {
			p.SourceDocument.Records[0].Identifiers[0].Scope = `["alipay","other"]`
		}},
		{"trace conflict", "unknown", "identifier_conflict", func(p *Preview, _ *reviewMetadataSnapshot) {
			p.SourceDocument.Records[0].Identifiers[0].Value = "different"
		}},
		{"merchant only", "unknown", "payment_trace_missing_or_invalid", func(p *Preview, _ *reviewMetadataSnapshot) {
			p.SourceDocument.Records[0].Identifiers = p.SourceDocument.Records[0].Identifiers[1:]
		}},
		{"trace duplicate", "unknown", "payment_trace_missing_or_invalid", func(p *Preview, _ *reviewMetadataSnapshot) {
			r := &p.SourceDocument.Records[0]
			r.Identifiers = append(r.Identifiers, r.Identifiers[0])
		}},
		{"currency", "unknown", "currency_unsupported", func(p *Preview, _ *reviewMetadataSnapshot) { p.SourceDocument.Records[0].Fields.Currency = "" }},
		{"unrecognized type", "unknown", "ordinary_source_unproven", func(p *Preview, _ *reviewMetadataSnapshot) {
			r := &p.SourceDocument.Records[0]
			r.Fields.Category = "其他"
			r.Raw["交易分类"] = "其他"
		}},
		{"normalized expense", "unknown", "ordinary_source_unproven", func(p *Preview, _ *reviewMetadataSnapshot) {
			r := &p.SourceDocument.Records[0]
			r.Fields.Type = "expense"
			r.Raw["收/支"] = "expense"
		}},
		{"failed", "unknown", "batch_failed", func(p *Preview, _ *reviewMetadataSnapshot) { p.Errors = []string{"private arbitrary error"} }},
		{"linked", "unknown", "linked_source", func(_ *Preview, s *reviewMetadataSnapshot) { s.linked = true }},
		{"missing committed timestamp", "unknown", "batch_event_invalid", func(_ *Preview, s *reviewMetadataSnapshot) {
			s.eventOperation = "import_restore"
			s.eventVersion = 1
			s.eventJSON = `{"state":"committed","changes":{}}`
		}},
		{"bad event", "unknown", "batch_event_invalid", func(_ *Preview, s *reviewMetadataSnapshot) {
			s.committed = true
			s.eventOperation = "import_undo"
			s.eventVersion = 1
			s.eventJSON = `{"state":"undone"}`
		}},
		{"wrong operation state", "unknown", "batch_event_invalid", func(_ *Preview, s *reviewMetadataSnapshot) {
			s.committed = true
			s.eventOperation = "import_undo"
			s.eventVersion = 1
			s.eventJSON = `{"state":"committed","changes":{}}`
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, s := metadataFixture()
			tc.mutate(&p, &s)
			s.previewJSON = encode(p)
			before := encode(p)
			out, e := evaluateReviewItemMetadata(p.ID, 2, s)
			if e != nil || out.Classification != tc.classification || (tc.block != "" && !metadataHas(out, tc.block)) {
				t.Fatalf("%+v %v", out, e)
			}
			if encode(p) != before {
				t.Fatal("modified input")
			}
			if out.ContractVersion != 1 || !out.ReadOnly || out.Actions == nil || len(out.Actions) != 0 || out.Blockers == nil {
				t.Fatal(out)
			}
			for _, b := range out.Blockers {
				if strings.Contains(b.Code, "private") || strings.Contains(b.Path, "private") {
					t.Fatal("leaked error")
				}
			}
		})
	}
}

func TestReviewItemMetadataInvalidSnapshotAndMoney(t *testing.T) {
	for _, raw := range []string{"{SECRET", "null", "[]", `{"id":"synthetic-import","id":"synthetic-import"}`, `{"id":"synthetic-import","pending":"bad"}`, `{} {}`} {
		_, s := metadataFixture()
		s.previewJSON = raw
		out, e := evaluateReviewItemMetadata("synthetic-import", 2, s)
		if e != nil || out.Classification != "unknown" || !metadataHas(out, "snapshot_invalid") {
			t.Fatal(out, e)
		}
	}
	for _, amount := range []string{"0", "-1.00", "1.001", "10000000000.01", "1e3", ""} {
		p, s := metadataFixture()
		p.SourceDocument.Records[0].Fields.Amount = amount
		p.SourceDocument.Records[0].Raw["金额"] = amount
		p.Pending[0].Amount = amount
		s.previewJSON = encode(p)
		out, e := evaluateReviewItemMetadata(p.ID, 2, s)
		if e != nil || !metadataHas(out, "amount_invalid") || out.Classification != "unknown" {
			t.Fatal(amount, out, e)
		}
	}
	for _, date := range []string{"2026-02-29 12:00:00", "1899-01-01 12:00:00", "bad"} {
		p, s := metadataFixture()
		p.SourceDocument.Records[0].Fields.Date = date
		p.SourceDocument.Records[0].Raw["交易时间"] = date
		p.Pending[0].Date = date
		s.previewJSON = encode(p)
		out, e := evaluateReviewItemMetadata(p.ID, 2, s)
		if e != nil || !metadataHas(out, "date_invalid") {
			t.Fatal(out, e)
		}
	}
}

func TestReviewItemMetadataSpecialCannotBeOverwritten(t *testing.T) {
	for _, kind := range []string{"退款", "转账", "信用卡还款"} {
		t.Run(kind, func(t *testing.T) {
			p, s := metadataFixture()
			r := &p.SourceDocument.Records[0]
			r.Fields.Type = "expense"
			r.Fields.Category = "未分类"
			r.Raw["交易分类"] = kind
			r.Fields.Amount = "1.001"
			p.Pending[0].Reason = "金额无效"
			s.previewJSON = encode(p)
			out, e := evaluateReviewItemMetadata(p.ID, 2, s)
			if e != nil || out.Classification != "special" || !metadataHas(out, "special_source") || !metadataHas(out, "amount_invalid") {
				t.Fatal(out, e)
			}
		})
	}
	p, s := metadataFixture()
	other := ingestion.ParseAlipayCSV([]byte(nativeHeader+strings.Replace(nativeRefund, "合成退款商户", "合成咖啡店", 1)), "synthetic-wallet").Records[0]
	other.Ordinal = 2
	other.Line = 3
	p.SourceDocument.Records = append(p.SourceDocument.Records, other)
	s.previewJSON = encode(p)
	out, e := evaluateReviewItemMetadata(p.ID, 2, s)
	if e != nil || out.Classification != "unknown" || !metadataHas(out, "same_merchant_refund") {
		t.Fatal(out, e)
	}
}

// Dump every table of this t.TempDir database so reads are checked against all
// persisted data, not only financial counts or a selected projection.
func metadataDBSnapshot(t *testing.T, s *Store) string {
	t.Helper()
	rows, e := s.db.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if e != nil {
		t.Fatal(e)
	}
	names := []string{}
	for rows.Next() {
		var n string
		if e = rows.Scan(&n); e != nil {
			t.Fatal(e)
		}
		names = append(names, n)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	out := map[string][][]any{}
	for _, name := range names {
		rows, e = s.db.Query(`SELECT * FROM "` + strings.ReplaceAll(name, `"`, `""`) + `" ORDER BY rowid`)
		if e != nil {
			t.Fatal(e)
		}
		cols, e := rows.Columns()
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if e = rows.Scan(ptrs...); e != nil {
				t.Fatal(e)
			}
			out[name] = append(out[name], vals)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		rows.Close()
	}
	return encode(out)
}

func TestReviewItemMetadataStoreStatesAndReadOnly(t *testing.T) {
	s := testStore(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense+nativeRefund), "wallet")
	check := func(state string) {
		t.Helper()
		before := metadataDBSnapshot(t, s)
		queue, e := s.ReviewQueue(1, "")
		if e != nil {
			t.Fatal(e)
		}
		finance := dashboard(t, s)
		one, e := s.ReviewItemMetadata(p.ID, 3)
		if e != nil || one.BatchState != state || one.Classification != "special" {
			t.Fatal(one, e)
		}
		two, e := s.ReviewItemMetadata(p.ID, 3)
		if e != nil || !reflect.DeepEqual(one, two) {
			t.Fatal(two, e)
		}
		afterQueue, e := s.ReviewQueue(1, "")
		if e != nil || !reflect.DeepEqual(queue, afterQueue) || !reflect.DeepEqual(finance, dashboard(t, s)) || metadataDBSnapshot(t, s) != before {
			t.Fatal("read mutated database/queue/finance", e)
		}
		b, _ := json.Marshal(one)
		for _, secret := range []string{"pay-3", "order-3", "银行卡A", "raw_csv", "actorId", "token"} {
			if strings.Contains(string(b), secret) {
				t.Fatal("private evidence leak", string(b))
			}
		}
	}
	check("preview")
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	check("committed")
	u, e := s.PreviewImportUndo(p.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UndoImport(p.ID, u.LedgerVersion); e != nil {
		t.Fatal(e)
	}
	check("undone")
	u, e = s.PreviewImportUndo(p.ID, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RestoreImport(p.ID, u.LedgerVersion); e != nil {
		t.Fatal(e)
	}
	check("committed")
	if _, e = s.db.Exec("INSERT INTO source_records VALUES(?,?,?,?,?,?)", newID(), p.ID, 3, p.Rows[0].Record.ID, encode(p.Rows[0].Record), "duplicate"); e != nil {
		t.Fatal(e)
	}
	out, e := s.ReviewItemMetadata(p.ID, 3)
	if e != nil || !metadataHas(out, "linked_source") || out.BatchState != "committed" {
		t.Fatal(out, e)
	}
	if _, e = s.db.Exec(`UPDATE change_log SET payload_json='{"state":"undone"}' WHERE entity_id=? AND entity_type='import' AND operation='import_restore'`, p.ID); e != nil {
		t.Fatal(e)
	}
	out, e = s.ReviewItemMetadata(p.ID, 3)
	if e != nil || out.BatchState != "unknown" || !metadataHas(out, "batch_event_invalid") {
		t.Fatal(out, e)
	}
	for _, line := range []int{0, -1} {
		_, e = s.ReviewItemMetadata(p.ID, line)
		metadataProblem(t, e, "invalid")
	}
	_, e = s.ReviewItemMetadata(p.ID, 999)
	metadataProblem(t, e, "not_found")
	_, e = s.ReviewItemMetadata("absent", 3)
	metadataProblem(t, e, "not_found")
	if _, e = s.db.Exec(`UPDATE imports SET preview_json='{private broken JSON' WHERE id=?`, p.ID); e != nil {
		t.Fatal(e)
	}
	before := metadataDBSnapshot(t, s)
	out, e = s.ReviewItemMetadata(p.ID, 3)
	if e != nil || !metadataHas(out, "snapshot_invalid") || metadataDBSnapshot(t, s) != before {
		t.Fatal(out, e)
	}
}
func metadataProblem(t *testing.T, e error, code string) {
	t.Helper()
	p, ok := e.(*Problem)
	if !ok || p.Code != code {
		t.Fatalf("want %s got %v", code, e)
	}
}

func TestReviewItemMetadataNativeContainersAndFailed(t *testing.T) {
	s := testStore(t)
	all := alipayPreview(t, s, []byte(nativeHeader+nativeRefund), "all-pending")
	out, e := s.ReviewItemMetadata(all.ID, 2)
	if e != nil || out.BatchState != "preview" || out.Classification != "special" {
		t.Fatal(out, e)
	}
	base := alipayPreview(t, s, []byte(nativeHeader+nativeExpense), "wallet")
	if _, e = s.Commit(base.ID, base.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	failed := alipayPreview(t, s, []byte(nativeHeader+strings.Replace(nativeExpense, "12.34", "12.35", 1)+nativeRefund), "wallet")
	out, e = s.ReviewItemMetadata(failed.ID, 3)
	if e != nil || out.BatchState != "failed" || !metadataHas(out, "batch_failed") {
		t.Fatal(out, e)
	}
	raw, e := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	csv := []byte("交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号\n2026-09-15 10:20:30,转账,合成商户,测试,支出,¥12.34,零钱,支付成功,wx-1,m-1\n")
	for _, data := range [][]byte{raw, csv} {
		p, e := s.PreviewWeChat("synthetic", base64.StdEncoding.EncodeToString(data), "wx-wallet")
		if e != nil || len(p.Pending) == 0 {
			t.Fatal(p, e)
		}
		before := metadataDBSnapshot(t, s)
		for _, r := range p.Pending {
			out, e = s.ReviewItemMetadata(p.ID, r.Line)
			if e != nil || out.SourceRef.ParserVersion != 1 || out.SourceRef.EnvelopeParserVersion != 2 || out.Facts.SourceAccount != "wx-wallet" || out.Classification == "ordinary_candidate" {
				t.Fatal(out, e)
			}
		}
		if metadataDBSnapshot(t, s) != before {
			t.Fatal("wechat read mutated")
		}
	}
	// Pure known WeChat ordinary gate also covers both stored parser containers.
	for _, parser := range []string{"wechat-csv", "wechat-xlsx"} {
		d := ingestion.ParseWeChatCSV([]byte(strings.Replace(string(csv), "转账", "商户消费", 1)), "wx-wallet")
		d.Parser = parser
		f := d.Records[0].Fields
		p := Preview{ID: "wx", Format: "wechat", SourceAccount: f.Account, SourceDocument: &d, Pending: []PendingRow{{Line: 2, Date: f.Date, Merchant: f.Merchant, Amount: f.Amount}}}
		out, e = evaluateReviewItemMetadata("wx", 2, reviewMetadataSnapshot{previewJSON: encode(p), envelopeVersion: 2, envelopeHash: hash("envelope")})
		if e != nil || out.Classification != "ordinary_candidate" {
			t.Fatal(out, e)
		}
	}
}

func TestReviewItemMetadataAuthorization(t *testing.T) {
	s := testStore(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeRefund), "wallet")
	add := func(id, role string, enabled int) {
		t.Helper()
		if _, e := s.db.Exec(`INSERT INTO app_users(id,display_name,created_at) VALUES(?,?,?)`, id, "Synthetic "+id, "test"); e != nil {
			t.Fatal(e)
		}
		if _, e := s.db.Exec(`INSERT INTO user_access(user_id,enabled) VALUES(?,?)`, id, enabled); e != nil {
			t.Fatal(e)
		}
		if role != "" {
			if _, e := s.db.Exec(`INSERT INTO ledger_members(ledger_id,user_id,role,created_at) VALUES(?,?,?,?)`, s.ledgerID, id, role, "test"); e != nil {
				t.Fatal(e)
			}
		}
	}
	for _, role := range []string{"owner", "editor", "viewer"} {
		add(role, role, 1)
		scoped, e := s.Scoped(role, s.ledgerID)
		if e != nil {
			t.Fatal(e)
		}
		before := metadataDBSnapshot(t, s)
		out, e := scoped.ReviewItemMetadata(p.ID, 2)
		if e != nil || out.Facts.SourceAccount != "wallet" || metadataDBSnapshot(t, s) != before {
			t.Fatal(out, e)
		}
		if role == "viewer" {
			if _, e = s.db.Exec(`DELETE FROM ledger_members WHERE user_id='viewer'`); e != nil {
				t.Fatal(e)
			}
			_, e = scoped.ReviewItemMetadata(p.ID, 2)
			metadataProblem(t, e, "ledger_denied")
		}
	}
	add("revoked", "viewer", 1)
	scoped, e := s.Scoped("revoked", s.ledgerID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`UPDATE user_access SET enabled=0 WHERE user_id='revoked'`); e != nil {
		t.Fatal(e)
	}
	_, e = scoped.ReviewItemMetadata(p.ID, 2)
	metadataProblem(t, e, "access_denied")
	add("nonmember", "", 1)
	for _, id := range []string{"nonmember", "not-admitted"} {
		candidate := &Store{db: s.db, mu: s.mu, ledgerID: s.ledgerID, actorID: id}
		_, e = candidate.ReviewItemMetadata(p.ID, 2)
		want := "ledger_denied"
		if id == "not-admitted" {
			want = "access_denied"
		}
		metadataProblem(t, e, want)
	}
	other, e := s.CreateLedger("owner", "Other synthetic ledger")
	if e != nil {
		t.Fatal(e)
	}
	cross, e := s.Scoped("owner", other.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = cross.ReviewItemMetadata(p.ID, 2)
	metadataProblem(t, e, "not_found")
	if _, e = s.Scoped("", s.ledgerID); e == nil {
		t.Fatal("anonymous scope accepted")
	}
}
