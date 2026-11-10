package ledger

import (
	"encoding/base64"
	"reflect"
	"testing"
)

func TestNativeEvidenceStoreCommittedReplayKeepsSourceAfterLiveEditsAndRefund(t *testing.T) {
	s := testStore(t)
	raw := nativeHeader + nativeExpense
	p := alipayPreview(t, s, []byte(raw), "synthetic-wallet")
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	id := p.Rows[0].Record.ID
	corrected, e := s.Correct(id, CorrectionInput{Version: 1, Type: "expense", AmountMinor: "1999", Reason: "合成已提交金额更正"})
	if e != nil {
		t.Fatal(e)
	}
	annotated, e := s.Annotate(id, AnnotationInput{Version: corrected.Version, Category: "合成调整分类", Note: "合成已提交备注修订"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateRefund(id, RefundInput{Version: annotated.Version, Date: "2026-09-16", Amount: "1.00", Currency: "CNY", Account: "合成退款资金账户", Note: "合成关联退款"}, "synthetic-committed-refund"); e != nil {
		t.Fatal(e)
	}
	before := nativeEvidenceTables(t, s)
	again, e := s.PreviewAlipay("repeat.csv", base64.StdEncoding.EncodeToString([]byte(raw)), "synthetic-wallet")
	if e != nil || !again.AlreadyCommitted || len(again.Rows) != 1 || !reflect.DeepEqual(again.Rows, p.Rows) || !reflect.DeepEqual(again.SourceDocument, p.SourceDocument) {
		t.Fatal("live edits/refund replaced frozen committed source", again, e)
	}
	if after := nativeEvidenceTables(t, s); !reflect.DeepEqual(before, after) {
		t.Fatal("readback of committed source changed live ledger or evidence")
	}
}
