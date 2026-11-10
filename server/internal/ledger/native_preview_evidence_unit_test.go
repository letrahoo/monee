package ledger

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

func nativePureFixture(t *testing.T) Preview {
	t.Helper()
	d := ingestion.ParseAlipayCSV([]byte(nativeHeader+nativeExpense+nativeRefund), "pure-wallet")
	tx, e := normalize(Input{Date: "2026-09-15", Type: "expense", Amount: "12.34", Currency: "CNY", Merchant: "合成咖啡店", Category: "餐饮美食", Source: "支付宝", Account: "银行卡A", ExternalID: "alipay-native-v1:synthetic"})
	if e != nil {
		t.Fatal(e)
	}
	tx.ID = "pure-candidate"
	f := d.Records[1].Fields
	return Preview{ID: "pure-import", Filename: "first.csv", Format: "alipay", SourceAccount: "pure-wallet", SourceDocument: &d, Rows: []PreviewRow{{Line: 2, Record: tx, Status: "new"}}, Pending: []PendingRow{{Line: 3, Date: f.Date, Merchant: f.Merchant, Amount: f.Amount, Reason: "original reason"}}, Errors: []string{}, NewCount: 1}
}
func nativePureClone(t *testing.T, p Preview) Preview {
	t.Helper()
	var out Preview
	if e := json.Unmarshal([]byte(encode(p)), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestNativePreviewEvidencePureRefresh(t *testing.T) {
	old := nativePureFixture(t)
	fresh := nativePureClone(t, old)
	fresh.Rows[0].Record.ID = "different-generated-ID"
	fresh.Rows[0].Record.Version = 3
	fresh.Rows[0].Status = "duplicate"
	fresh.Rows[0].Message = "相同来源流水已入账或在文件内重复，将跳过"
	fresh.NewCount = 0
	fresh.DuplicateCount = 1
	fresh.LedgerVersion = 42
	fresh.Filename = "renamed.csv"
	fresh.Pending[0].Reason = "new display reason"
	oldJSON, freshJSON := encode(old), encode(fresh)
	out, e := nativeEvidenceRefresh(oldJSON, old.ID, old.Filename, 2, fresh)
	if e != nil || out.LedgerVersion != 42 || out.Filename != old.Filename || out.Pending[0].Reason != old.Pending[0].Reason || out.Rows[0].Record.ID != fresh.Rows[0].Record.ID || out.DuplicateCount != 1 || !reflect.DeepEqual(out.SourceDocument, old.SourceDocument) {
		t.Fatal(out, e)
	}
	if encode(old) != oldJSON || encode(fresh) != freshJSON {
		t.Fatal("helper changed input")
	}
}
func TestNativePreviewEvidencePureDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Preview)
	}{
		{"parser version", func(p *Preview) { p.SourceDocument.ParserVersion++ }},
		{"parser name", func(p *Preview) { p.SourceDocument.Parser = "future-parser" }},
		{"document hash", func(p *Preview) { p.SourceDocument.ContentHash = hash("different bytes") }},
		{"raw source", func(p *Preview) { p.SourceDocument.Records[0].Raw["交易状态"] = "退款成功" }},
		{"raw unknown column", func(p *Preview) { p.SourceDocument.Records[0].Raw["new evidence"] = "new" }},
		{"account", func(p *Preview) { p.SourceAccount = "other-wallet" }},
		{"date", func(p *Preview) { p.SourceDocument.Records[0].Fields.Date = "2026-09-16 10:20:30" }},
		{"amount", func(p *Preview) { p.SourceDocument.Records[0].Fields.Amount = "12.35" }},
		{"scope", func(p *Preview) { p.SourceDocument.Records[0].Identifiers[0].Scope = `["alipay","other"]` }},
		{"line", func(p *Preview) { p.SourceDocument.Records[0].Line = 7 }},
		{"ordinal", func(p *Preview) { p.SourceDocument.Records[0].Ordinal = 9 }},
		{"duplicate ordinal", func(p *Preview) { p.SourceDocument.Records[1].Ordinal = 1 }},
		{"duplicate line", func(p *Preview) { p.SourceDocument.Records[1].Line = 2 }},
		{"missing document", func(p *Preview) { p.SourceDocument = nil }},
		{"missing raw", func(p *Preview) { p.SourceDocument.Records[0].Raw = nil }},
		{"missing identifiers", func(p *Preview) { p.SourceDocument.Records[0].Identifiers = nil }},
		{"fields type", func(p *Preview) { p.SourceDocument.Records[0].Fields.Type = "expense" }},
		{"row amount", func(p *Preview) { p.Rows[0].Record.AmountMinor = "-999" }},
		{"row source", func(p *Preview) { p.Rows[0].Record.Source = "other" }},
		{"row payment identity", func(p *Preview) { p.Rows[0].Record.ExternalID = "other" }},
		{"row refund association", func(p *Preview) { p.Rows[0].Record.RefundOf = "tx" }},
		{"pending amount", func(p *Preview) { p.Pending[0].Amount = "999" }},
		{"pending allocation", func(p *Preview) { p.Pending = nil }},
		{"row allocation", func(p *Preview) { p.Rows = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			old := nativePureFixture(t)
			fresh := nativePureClone(t, old)
			tc.mutate(&fresh)
			a, b := encode(old), encode(fresh)
			_, e := nativeEvidenceRefresh(a, old.ID, old.Filename, 2, fresh)
			if p, ok := e.(*Problem); !ok || p.Code != "conflict" {
				t.Fatalf("expected conflict got %v", e)
			}
			if encode(old) != a || encode(fresh) != b {
				t.Fatal("mutated")
			}
		})
	}
}
func TestNativePreviewEvidenceStrictJSON(t *testing.T) {
	p := nativePureFixture(t)
	raw := encode(p)
	invalid := []string{"null", "[]", "{synthetic-secret", raw + " {}", strings.Replace(raw, `"id":"pure-import"`, `"id":"pure-import","ID":"pure-import"`, 1), strings.Replace(raw, `"sourceDocument":`, `"SourceDocument":null,"ſourceDocument":`, 1), strings.Replace(raw, `"raw":{`, `"futureEvidence":true,"raw":{`, 1), strings.Replace(raw, `"ordinal":1`, `"ordinal":1,"ordinal":1`, 1), strings.Replace(raw, `"scope":`, `"ſcope":"bad","scope":`, 1)}
	for _, bad := range invalid {
		var decoded Preview
		if nativeEvidenceDecode(bad, &decoded) {
			t.Fatalf("accepted malformed snapshot %s", bad)
		}
		_, e := nativeEvidenceRefresh(bad, p.ID, p.Filename, 2, p)
		if e == nil || strings.Contains(e.Error(), "synthetic-secret") {
			t.Fatal(e)
		}
	}
	for _, bad := range []string{strings.Replace(raw, `"note":"早餐"`, `"note":null`, 1), strings.Replace(raw, `"type":"支出",`, ``, 1), strings.Replace(raw, `"encoding":"utf-8"`, `"unknownEvidence":true`, 1)} {
		var decoded Preview
		if nativeEvidenceDecode(bad, &decoded) {
			t.Fatal("accepted null, missing or unknown evidence field")
		}
	}
	if nativeEvidenceFold("Scope") != nativeEvidenceFold("ſcope") || nativeEvidenceFold("parser") != nativeEvidenceFold("parſer") {
		t.Fatal("incomplete SimpleFold")
	}
}
func TestNativePreviewEvidenceCaseSensitiveRawKeys(t *testing.T) {
	p := nativePureFixture(t)
	p.SourceDocument.Records[0].Raw["extra"] = "lower"
	p.SourceDocument.Records[0].Raw["EXTRA"] = "upper"
	p.SourceDocument.Records[0].Raw["ſcope"] = "long-s"
	p.SourceDocument.Records[0].Raw["Scope"] = "capital-s"
	raw := encode(p)
	var decoded Preview
	if !nativeEvidenceDecode(raw, &decoded) || !reflect.DeepEqual(p.SourceDocument, decoded.SourceDocument) {
		t.Fatal("distinct map columns rejected or lost")
	}
	fresh := nativePureClone(t, p)
	out, e := nativeEvidenceRefresh(raw, p.ID, p.Filename, 2, fresh)
	if e != nil || !reflect.DeepEqual(out.SourceDocument, p.SourceDocument) {
		t.Fatal(out, e)
	}
	bad := strings.Replace(raw, `"extra":"lower"`, `"extra":"lower","extra":"duplicate"`, 1)
	if nativeEvidenceDecode(bad, &decoded) {
		t.Fatal("duplicate exact map key accepted")
	}
}

func TestNativePreviewEvidenceSparseXLSXRaw(t *testing.T) {
	d := ingestion.ParseWeChatCSV([]byte("交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号\n2026-09-15 10:20:30,商户消费,合成商户,,支出,12.34,,支付成功,,\n"), "pure-wallet")
	if len(d.Records) != 1 {
		t.Fatal(d)
	}
	d.Parser = "wechat-xlsx"
	for _, key := range []string{"商品", "支付方式", "交易单号", "商户单号"} {
		delete(d.Records[0].Raw, key)
	}
	f := d.Records[0].Fields
	p := Preview{ID: "sparse", Filename: "sparse.xlsx", Format: "wechat", SourceAccount: "pure-wallet", SourceDocument: &d, Rows: []PreviewRow{}, Errors: []string{}, Pending: []PendingRow{{Line: 2, Date: f.Date, Amount: f.Amount, Merchant: f.Merchant, Reason: "missing trace"}}}
	before := encode(p)
	out, e := nativeEvidenceRefresh(before, p.ID, p.Filename, 2, nativePureClone(t, p))
	if e != nil || encode(p) != before || !reflect.DeepEqual(out.SourceDocument, p.SourceDocument) {
		t.Fatal(out, e)
	}
	for _, key := range []string{"商品", "支付方式", "交易单号", "商户单号"} {
		if _, ok := out.SourceDocument.Records[0].Raw[key]; ok {
			t.Fatal("filled absent cell", key)
		}
	}
	p.SourceDocument.Parser = "wechat-csv"
	if nativeEvidenceValid(p) {
		t.Fatal("CSV missing required raw keys accepted")
	}
}

func TestNativePreviewEvidenceExplainedErrorsAndStates(t *testing.T) {
	p := nativePureFixture(t)
	for _, mutate := range []func(*Preview){func(p *Preview) { p.Errors = []string{"unexplained source error"} }, func(p *Preview) { p.Rows[0].Status = "unknown" }, func(p *Preview) {
		p.Rows[0].Status = "conflict"
		p.Rows[0].Message = "arbitrary private"
		p.Errors = []string{"第 2 行：arbitrary private"}
	}, func(p *Preview) { p.AlreadyCommitted = true }, func(p *Preview) { p.Undone = true }} {
		old := nativePureClone(t, p)
		mutate(&old)
		_, e := nativeEvidenceRefresh(encode(old), p.ID, p.Filename, 2, p)
		if e == nil {
			t.Fatal("invalid old state accepted")
		}
	}
	old := nativePureClone(t, p)
	old.Rows[0].Status = "conflict"
	old.Rows[0].Message = "相同来源流水号的数据不一致"
	old.Errors = []string{"第 2 行：相同来源流水号的数据不一致"}
	if _, e := nativeEvidenceRefresh(encode(old), p.ID, p.Filename, 2, p); e != nil {
		t.Fatal("old dynamic conflict froze recovery", e)
	}
	d := ingestion.ParseAlipayCSV([]byte("broken header"), "pure-wallet")
	failed := Preview{ID: "failed", Filename: "bad.csv", Format: "alipay", SourceDocument: &d, Rows: []PreviewRow{}, Errors: []string{"第 0 行：前 100 行中没有支付宝交易时间表头"}}
	if len(d.Records) != 0 || !nativeEvidenceValid(failed) {
		t.Fatal("source header failure rejected", failed)
	}
	fresh := nativePureClone(t, failed)
	fresh.LedgerVersion = 9
	if out, e := nativeEvidenceRefresh(encode(failed), failed.ID, failed.Filename, 2, fresh); e != nil || out.LedgerVersion != 9 || len(out.Errors) != 1 {
		t.Fatal(out, e)
	}
}
