package ingestion

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

const weChatCSVHeader = "交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注\n"
const weChatCSVRow = "2026-09-01 08:00:00,商户消费,合成早餐店,早餐,支出,¥12.34,零钱,支付成功,000012345678901234567890,000001,合成验收\n"

func TestWeChatCSVPreservesRawFieldsAndPhysicalLines(t *testing.T) {
	text := "微信支付账单明细\n\n" + weChatCSVHeader + strings.Replace(weChatCSVRow, "早餐,支出", "\"早餐,套餐\n合成备注\",支出", 1) + strings.Replace(weChatCSVRow, "¥12.34", "1.001", 1)
	for name, raw := range map[string][]byte{
		"utf8": []byte(text), "bom": []byte("\uFEFF" + text), "crlf": []byte(strings.ReplaceAll(text, "\n", "\r\n")),
	} {
		t.Run(name, func(t *testing.T) {
			d := ParseWeChat(raw, " wallet-a ")
			if len(d.Issues) != 0 || len(d.Records) != 2 || d.Parser != "wechat-csv" || d.Encoding != "utf-8" || d.ContentHash != digest(raw) {
				t.Fatal(d)
			}
			r := d.Records[0]
			if r.Line != 4 || d.Records[1].Line != 6 || r.Fields.Note != "早餐,套餐\n合成备注" || r.Fields.Amount != "12.34" || r.Raw["金额(元)"] != "¥12.34" || d.Records[1].Fields.Amount != "1.001" {
				t.Fatal(d.Records)
			}
			if r.Fields.ExternalID != "000012345678901234567890" || r.Identifiers[1].Value != "000001" || r.Fields.Account != "wallet-a" {
				t.Fatal(r)
			}
		})
	}
}

func TestWeChatCSVAndXLSXHaveSameSourceIdentity(t *testing.T) {
	raw, err := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.csv")
	if err != nil {
		t.Fatal(err)
	}
	csv := ParseWeChat(raw, "wallet")
	xlsx := ParseWeChat(wechatFixture(t), "wallet")
	if len(csv.Issues) != 0 || len(xlsx.Issues) != 0 || len(csv.Records) != 4 || len(xlsx.Records) != 4 {
		t.Fatal(csv, xlsx)
	}
	for i, row := range csv.Records {
		other := xlsx.Records[i]
		if row.Line != other.Line+1 || !reflect.DeepEqual(row.Fields, other.Fields) || !reflect.DeepEqual(row.Identifiers, other.Identifiers) || !reflect.DeepEqual(row.Raw, other.Raw) {
			t.Fatal(row, other)
		}
	}
	otherAccount := ParseWeChat(raw, "another-wallet")
	if reflect.DeepEqual(csv.Records[0].Identifiers, otherAccount.Records[0].Identifiers) {
		t.Fatal("account scopes collapsed")
	}
	if csv.ContentHash == xlsx.ContentHash {
		t.Fatal("different container bytes lost")
	}
}

func TestWeChatCSVRejectsAmbiguousOrDamagedFiles(t *testing.T) {
	for name, raw := range map[string][]byte{
		"empty":                nil,
		"header only":          []byte(weChatCSVHeader),
		"missing column":       []byte(strings.Replace(weChatCSVHeader, "当前状态", "其他状态", 1) + weChatCSVRow),
		"duplicate column":     []byte(strings.Replace(weChatCSVHeader, "备注", "交易单号", 1) + weChatCSVRow),
		"blank column":         []byte(strings.Replace(weChatCSVHeader, "备注", "", 1) + weChatCSVRow),
		"extra column":         []byte(weChatCSVHeader + strings.Replace(weChatCSVRow, "合成验收", "合成验收,多列", 1)),
		"missing row column":   []byte(weChatCSVHeader + "2026-09-01,支出,12.34\n"),
		"unclosed quote":       []byte(weChatCSVHeader + "\"2026-09-01,支出,12.34\n"),
		"unexpected footer":    []byte(weChatCSVHeader + weChatCSVRow + "共 1 笔\n"),
		"record before header": []byte(weChatCSVRow + weChatCSVHeader + weChatCSVRow),
		"non UTF8":             append([]byte(weChatCSVHeader+weChatCSVRow), 0xff),
		"replacement":          []byte(weChatCSVHeader + strings.Replace(weChatCSVRow, "早餐", "\uFFFD", 1)),
		"null":                 []byte(weChatCSVHeader + strings.Replace(weChatCSVRow, "早餐", "\x00", 1)),
		"too large":            bytes.Repeat([]byte("a"), MaxBytes+1),
		"too many rows":        []byte(weChatCSVHeader + strings.Repeat(weChatCSVRow, MaxRows+1)),
		"late header":          []byte(strings.Repeat("说明\n", 100) + weChatCSVHeader + weChatCSVRow),
		"broken ZIP":           []byte("PK\x03\x04broken"),
	} {
		t.Run(name, func(t *testing.T) {
			d := ParseWeChat(raw, "wallet")
			if len(d.Issues) == 0 {
				t.Fatal("accepted damaged file", d)
			}
		})
	}
	if len(ParseWeChatCSV([]byte(weChatCSVHeader+weChatCSVRow), " ").Issues) == 0 {
		t.Fatal("accepted missing account")
	}
	d := ParseWeChatCSV([]byte(weChatCSVHeader+strings.Repeat(weChatCSVRow, MaxRows)), "wallet")
	if len(d.Issues) != 0 || len(d.Records) != MaxRows {
		t.Fatal("1000 row boundary", len(d.Records), d.Issues)
	}
}
