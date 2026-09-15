package ingestion

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const alipayHeader = "交易时间,交易分类,交易对方,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号,备注,\n"
const alipayRow = "2026-09-01 12:01:02,餐饮,合成商户,午餐,支出,12.340,余额,交易成功,0000123,0000456,测试,\n"

func TestAlipayNativeEvidence(t *testing.T) {
	text := "支付宝交易明细\n账号：合成账户\n" + alipayHeader + strings.Replace(alipayRow, "午餐", "\"午餐\n第二行\"", 1) + strings.Replace(alipayRow, "交易成功", "退款成功", 1)
	utf := []byte("\uFEFF" + text)
	d := ParseAlipayCSV(utf, "account-a")
	if len(d.Issues) != 0 || len(d.Records) != 2 {
		t.Fatalf("unexpected extraction: %+v", d)
	}
	a, b := d.Records[0], d.Records[1]
	if a.Line != 4 || b.Line != 6 || b.Ordinal != 2 {
		t.Fatal("physical source positions lost")
	}
	if a.Fields.Amount != "12.340" || a.Fields.Type != "支出" || a.Fields.Date != "2026-09-01 12:01:02" {
		t.Fatal("native values coerced")
	}
	if a.Raw["商品说明"] != "午餐\n第二行" || b.Raw["交易状态"] != "退款成功" || a.Raw["备注"] != "测试" {
		t.Fatal("raw evidence lost")
	}
	if a.Identifiers[0].Value != "0000123" || a.Identifiers[1].Value != "0000456" || a.Identifiers[0].Kind == a.Identifiers[1].Kind {
		t.Fatal("order namespaces collapsed")
	}
	gb, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	legacy := ParseAlipayCSV(gb, "account-a")
	if len(legacy.Issues) != 0 || legacy.Encoding != "gb18030" || legacy.Records[0].Fields != a.Fields || legacy.ContentHash == d.ContentHash {
		t.Fatal("encoding or original-byte hash regression")
	}
	crlf := ParseAlipayCSV(bytes.ReplaceAll(utf, []byte("\n"), []byte("\r\n")), "account-a")
	if len(crlf.Issues) != 0 || crlf.ContentHash == d.ContentHash || crlf.Records[0].Identifiers[0] != a.Identifiers[0] {
		t.Fatal("newline identity regression")
	}
	other := ParseAlipayCSV(utf, "account-b")
	first, _ := a.Identifiers[0].MatchKey("ledger")
	second, _ := other.Records[0].Identifiers[0].MatchKey("ledger")
	if first == second {
		t.Fatal("source accounts collapsed")
	}
}
func TestAlipayDoesNotSubstituteMerchantOrderOrBookRefunds(t *testing.T) {
	row := strings.Replace(alipayRow, "交易成功", "交易关闭", 1)
	row = strings.Replace(row, "0000123", "", 1)
	d := ParseAlipayCSV([]byte(alipayHeader+row), "a")
	if len(d.Issues) != 0 || d.Records[0].Fields.ExternalID != "" || len(d.Records[0].Identifiers) != 1 || d.Records[0].Identifiers[0].Kind != "merchant_order" {
		t.Fatal("merchant order substituted as payment trace")
	}
	for _, direction := range []string{"收入", "不计收支", "未知"} {
		d = ParseAlipayCSV([]byte(alipayHeader+strings.Replace(alipayRow, "支出", direction, 1)), "a")
		if d.Records[0].Fields.Type != direction {
			t.Fatal("direction invented")
		}
	}
}
func TestAlipayRejectsIncompleteExtraction(t *testing.T) {
	cases := map[string][]byte{
		"missing header":      []byte("not an Alipay export"),
		"duplicate header":    []byte(strings.Replace(alipayHeader, "备注", "交易状态", 1) + alipayRow),
		"missing status":      []byte(strings.Replace(alipayHeader, "交易状态", "别的列", 1) + alipayRow),
		"mixed aliases":       []byte(strings.Replace(alipayHeader, "备注", "金额(元)", 1) + alipayRow),
		"ragged row":          []byte(alipayHeader + "2026-01-01,支出,2\n"),
		"trailing data":       []byte(alipayHeader + strings.TrimSuffix(alipayRow, "\n") + "secret\n"),
		"invalid encoding":    append([]byte(alipayHeader), 0xff),
		"replacement":         []byte(alipayHeader + strings.Replace(alipayRow, "午餐", "\uFFFD", 1)),
		"too large":           bytes.Repeat([]byte("a"), MaxBytes+1),
		"too many":            []byte(alipayHeader + strings.Repeat(alipayRow, MaxRows+1)),
		"header too late":     []byte(strings.Repeat("metadata\n", 100) + alipayHeader + alipayRow),
		"empty":               []byte(alipayHeader),
		"unrecognized footer": []byte(alipayHeader + alipayRow + "不能静默跳过的内容\n"),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			d := ParseAlipayCSV(raw, "account")
			if len(d.Issues) == 0 {
				t.Fatal("accepted incomplete extraction")
			}
		})
	}
	if len(ParseAlipayCSV([]byte(alipayHeader+alipayRow), " ").Issues) == 0 {
		t.Fatal("missing source account accepted")
	}
}
