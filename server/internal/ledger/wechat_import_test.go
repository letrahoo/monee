package ledger

import (
	"encoding/base64"
	"os"
	"testing"
)

func TestWeChatNativePreviewCommitHistory(t *testing.T) {
	raw, e := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	s := testStore(t)
	content := base64.StdEncoding.EncodeToString(raw)
	p, e := s.PreviewWeChat("wechat.xlsx", content, "wallet-a")
	if e != nil || len(p.Errors) > 0 || p.NewCount != 1 || len(p.Pending) != 3 || p.Format != "wechat" {
		t.Fatal(p, e)
	}
	if count(t, s, "transactions") != 0 {
		t.Fatal("preview wrote money")
	}
	result, e := s.Commit(p.ID, p.LedgerVersion, false)
	if e != nil || result.Added != 1 || result.Pending != 3 {
		t.Fatal(result, e)
	}
	d := dashboard(t, s)
	if d.ExpenseMinor != "1234" || d.IncomeMinor != "0" {
		t.Fatal(d)
	}
	repeat, e := s.PreviewWeChat("renamed.xlsx", content, "wallet-a")
	if e != nil || !repeat.AlreadyCommitted || repeat.ID != p.ID {
		t.Fatal(repeat, e)
	}
	detail, e := s.ImportDetail(p.ID)
	if e != nil || len(detail.Evidence) != 1 || len(detail.Preview.SourceDocument.Records) != 4 {
		t.Fatal(detail, e)
	}
	other, e := s.PreviewWeChat("wechat.xlsx", content, "wallet-b")
	if e != nil || other.DuplicateCount != 0 || other.SimilarCount != 1 {
		t.Fatal(other, e)
	}
}

func TestFailedNativeImportIsRetainedButNeverBookable(t *testing.T) {
	s := testStore(t)
	p, e := s.PreviewWeChat("broken.xlsx", base64.StdEncoding.EncodeToString([]byte("broken")), "wallet")
	if e != nil || p.ID == "" || len(p.Errors) == 0 {
		t.Fatal(p, e)
	}
	if _, e = s.Commit(p.ID, p.LedgerVersion, false); e == nil {
		t.Fatal("failed batch committed")
	}
	h, e := s.ImportHistory(1)
	if e != nil || h.TotalCount != 1 || h.Imports[0].Errors == 0 {
		t.Fatal(h, e)
	}
	detail, e := s.ImportDetail(p.ID)
	if e != nil || len(detail.Preview.Errors) == 0 {
		t.Fatal(detail, e)
	}
	if count(t, s, "transactions") != 0 {
		t.Fatal("failed batch wrote money")
	}
	again, e := s.PreviewWeChat("renamed.xlsx", base64.StdEncoding.EncodeToString([]byte("broken")), "wallet")
	if e != nil || again.ID != p.ID {
		t.Fatal(again, e)
	}
}
