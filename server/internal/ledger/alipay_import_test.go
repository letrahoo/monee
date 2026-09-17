package ledger

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const nativeHeader = "交易时间,交易分类,交易对方,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号\n"
const nativeExpense = "2026-09-15 10:20:30,餐饮美食,合成咖啡店,早餐,支出,12.34,银行卡A,交易成功,pay-1,order-1\n"
const nativeSalary = "2026-09-14 10:00:00,工资收入,合成公司,工资,收入,100.00,余额,交易成功,pay-2,order-2\n"
const nativeRefund = "2026-09-13 10:00:00,退款,合成退款商户,退货,收入,20.00,余额,退款成功,pay-3,order-3\n"

func alipayPreview(t *testing.T, s *Store, raw []byte, account string) Preview {
	t.Helper()
	p, e := s.PreviewAlipay("alipay.csv", base64.StdEncoding.EncodeToString(raw), account)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestNativeImportBooksOnlyValidatedRowsAndRetainsEvidence(t *testing.T) {
	s := testStore(t)
	raw := []byte(nativeHeader + nativeExpense + nativeSalary + nativeRefund)
	p := alipayPreview(t, s, raw, "wallet-a")
	if len(p.Errors) > 0 || p.NewCount != 2 || len(p.Pending) != 1 || p.ID == "" {
		t.Fatalf("unexpected preview %+v", p)
	}
	if count(t, s, "transactions") != 0 {
		t.Fatal("preview wrote financial records")
	}
	result, e := s.Commit(p.ID, p.LedgerVersion, false)
	if e != nil || result.Added != 2 || result.Pending != 1 {
		t.Fatal(result, e)
	}
	d := dashboard(t, s)
	if d.ExpenseMinor != "1234" || d.IncomeMinor != "10000" {
		t.Fatal("incorrect totals", d)
	}
	detail, e := s.ImportDetail(p.ID)
	if e != nil || detail.ParserVersion != 2 || len(detail.Preview.Pending) != 1 || len(detail.Preview.SourceDocument.Records) != 3 || len(detail.Evidence) != 2 {
		t.Fatal("evidence incomplete", e)
	}
	if detail.Preview.SourceDocument.Records[2].Raw["交易状态"] != "退款成功" {
		t.Fatal("refund evidence lost")
	}
	var envelope string
	if e = s.db.QueryRow("SELECT raw_csv FROM imports WHERE id=?", p.ID).Scan(&envelope); e != nil {
		t.Fatal(e)
	}
	var original struct{ Content string }
	if e = json.Unmarshal([]byte(envelope), &original); e != nil {
		t.Fatal(e)
	}
	saved, _ := base64.StdEncoding.DecodeString(original.Content)
	if string(saved) != string(raw) {
		t.Fatal("original bytes changed")
	}
	replay := alipayPreview(t, s, raw, "wallet-a")
	if !replay.AlreadyCommitted || replay.ID != p.ID {
		t.Fatal("file retry not idempotent")
	}
	repeated, e := s.Commit(p.ID, p.LedgerVersion, false)
	if e != nil || repeated != result {
		t.Fatal("commit retry not idempotent")
	}
	// Encoding/line endings change the source document, never the payment identity.
	gb, e := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(strings.ReplaceAll(string(raw), "\n", "\r\n")))
	if e != nil {
		t.Fatal(e)
	}
	duplicate := alipayPreview(t, s, gb, "wallet-a")
	if duplicate.DuplicateCount != 2 || duplicate.NewCount != 0 || len(duplicate.Pending) != 1 {
		t.Fatal("encoding changed payment identity")
	}
	result, e = s.Commit(duplicate.ID, duplicate.LedgerVersion, false)
	if e != nil || result.Skipped != 2 || count(t, s, "transactions") != 2 {
		t.Fatal("duplicate booked", e)
	}
	changed := alipayPreview(t, s, []byte(strings.Replace(string(raw), "银行卡A", "银行卡B", 1)), "wallet-a")
	if len(changed.Errors) == 0 {
		t.Fatal("changed funding evidence silently became a new transaction")
	}
	other := alipayPreview(t, s, raw, "wallet-b")
	if other.NewCount != 2 || other.DuplicateCount != 0 || other.SimilarCount != 2 {
		t.Fatal("separate wallet collapsed", other)
	}
}
func TestNativeReviewAndMalformedInputs(t *testing.T) {
	for name, row := range map[string]string{
		"precision":        strings.Replace(nativeExpense, "12.34", "12.345", 1),
		"closed":           strings.Replace(nativeExpense, "交易成功", "交易关闭", 1),
		"transfer":         strings.Replace(nativeExpense, "餐饮美食", "转账", 1),
		"missing trace":    strings.Replace(nativeExpense, "pay-1", "", 1),
		"unknown category": strings.Replace(nativeExpense, "餐饮美食", "其他", 1),
		"unknown income":   strings.Replace(nativeSalary, "工资收入", "其他", 1),
		"invalid date":     strings.Replace(nativeExpense, "2026-09-15", "2026-02-30", 1),
	} {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			p := alipayPreview(t, s, []byte(nativeHeader+row), "a")
			if len(p.Pending) != 1 || len(p.Rows) != 0 || p.ID == "" {
				t.Fatal(p)
			}
			if _, e := s.Commit(p.ID, p.LedgerVersion, false); e == nil {
				t.Fatal("review-only batch committed")
			}
			if count(t, s, "transactions") != 0 {
				t.Fatal("review entered ledger")
			}
		})
	}
	s := testStore(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense+strings.Replace(nativeRefund, "合成退款商户", "合成咖啡店", 1)), "a")
	if len(p.Pending) != 2 || len(p.Rows) != 0 {
		t.Fatal("refund-linked original booked")
	}
	p = alipayPreview(t, s, []byte(nativeHeader+nativeExpense+"bad,row\n"), "a")
	if len(p.Errors) == 0 || p.ID == "" {
		t.Fatal("partial parsing presented as complete")
	}
	if _, e := s.PreviewAlipay("a.csv", "invalid!", "a"); e == nil {
		t.Fatal("invalid base64")
	}
	if _, e := s.PreviewAlipay("a.csv", "", " "); e == nil {
		t.Fatal("missing account")
	}
}
