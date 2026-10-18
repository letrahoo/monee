package ledger

import (
	"strings"
	"testing"
)

func TestReviewItemMetadataCorruptReferenceAndEvents(t *testing.T) {
	p, s := metadataFixture()
	p.SourceDocument.ContentHash = "private-token-value"
	p.SourceDocument.Parser = "private-parser-value"
	p.SourceDocument.ParserVersion = -1
	s.envelopeVersion = -2
	p.SourceDocument.Records[0].Ordinal = -3
	s.previewJSON = encode(p)
	out, e := evaluateReviewItemMetadata(p.ID, 2, s)
	if e != nil || out.SourceRef.DocumentContentHash != "" || out.SourceRef.Parser != "" || out.SourceRef.ParserVersion != 0 || out.SourceRef.EnvelopeParserVersion != 0 || out.SourceRef.Ordinal != 0 || strings.Contains(encode(out), "private-") {
		t.Fatal(out, e)
	}
	for _, payload := range []string{`{broken private}`, `null`, `{"state":"undone","changes":{"t":{"version":0,"deletedAt":"2026-01-01T00:00:00Z"}}}`, `{"state":"undone","changes":{"t":{"version":1,"deletedAt":"bad"}}}`, `{"state":"undone","state":"undone","changes":{}}`} {
		p, s = metadataFixture()
		s.committed = true
		s.eventOperation = "import_undo"
		s.eventVersion = 1
		s.eventJSON = payload
		out, e = evaluateReviewItemMetadata(p.ID, 2, s)
		if e != nil || out.BatchState != "unknown" || !metadataHas(out, "batch_event_invalid") || out.Classification != "unknown" {
			t.Fatal(out, e)
		}
	}
	for _, payload := range []string{`{"id":"synthetic-import","ID":"synthetic-import"}`, `{"id":"synthetic-import","sourceDocument":{"parser":"a","parser":"b"}}`} {
		_, s = metadataFixture()
		s.previewJSON = payload
		out, e = evaluateReviewItemMetadata("synthetic-import", 2, s)
		if e != nil || !metadataHas(out, "snapshot_invalid") {
			t.Fatal(out, e)
		}
	}
}

func TestReviewItemMetadataMoreIncompleteEvidence(t *testing.T) {
	for _, mutation := range []func(*Preview){
		func(p *Preview) { p.SourceDocument.Records = nil },
		func(p *Preview) {
			r := &p.SourceDocument.Records[0]
			r.Identifiers = append(r.Identifiers, r.Identifiers[1])
		},
		func(p *Preview) { r := &p.SourceDocument.Records[0]; r.Identifiers = r.Identifiers[:1] },
		func(p *Preview) {
			r := &p.SourceDocument.Records[0]
			r.Fields.Note = strings.Repeat("x", 1001)
			r.Raw["商品说明"] = r.Fields.Note
		},
		func(p *Preview) {
			r := &p.SourceDocument.Records[0]
			r.Fields.Merchant = strings.Repeat("x", 201)
			r.Raw["交易对方"] = r.Fields.Merchant
			p.Pending[0].Merchant = r.Fields.Merchant
		},
	} {
		p, s := metadataFixture()
		mutation(&p)
		s.previewJSON = encode(p)
		out, e := evaluateReviewItemMetadata(p.ID, 2, s)
		if e != nil || out.Classification != "unknown" || len(out.Blockers) == 0 {
			t.Fatal(out, e)
		}
	}
}

func TestReviewItemMetadataNativeSpecialReasonOverwrite(t *testing.T) {
	for _, kind := range []string{"退款", "转账", "信用卡还款"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			row := strings.Replace(nativeExpense, "餐饮美食", kind, 1)
			row = strings.Replace(row, "12.34", "1.001", 1)
			p := alipayPreview(t, s, []byte(nativeHeader+row), "synthetic-wallet")
			if len(p.Pending) != 1 || strings.Contains(p.Pending[0].Reason, kind) {
				t.Fatal("fixture should have overwritten special reason", p)
			}
			before := metadataDBSnapshot(t, s)
			out, e := s.ReviewItemMetadata(p.ID, 2)
			if e != nil || out.Classification != "special" || !metadataHas(out, "special_source") || !metadataHas(out, "amount_invalid") || metadataDBSnapshot(t, s) != before {
				t.Fatal(out, e)
			}
		})
	}
	s := testStore(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeRefund), "synthetic-wallet")
	if _, e := s.ReviewItemMetadata(p.ID, 1_000_001); e == nil {
		t.Fatal("excess line accepted")
	}
}
