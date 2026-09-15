package ingestion

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// ParseAlipayCSV extracts evidence only. The source account is an explicit local
// alias; payment method is a separate raw field. Native direction/status are not
// converted into bookable ledger types, even for apparently successful payments.
func ParseAlipayCSV(raw []byte, account string) Document {
	d := Document{ContentHash: digest(raw), Parser: "alipay-csv", ParserVersion: 1, Records: []Record{}, Issues: []Issue{}}
	issue := func(line int, code, message string) { d.Issues = append(d.Issues, Issue{line, code, message}) }
	if len(raw) > MaxBytes {
		issue(0, "size", "文件不能超过 2 MiB")
		return d
	}
	if strings.TrimSpace(account) == "" {
		issue(0, "account", "必须指定来源账户别名，不能使用付款银行卡替代支付宝账户")
		return d
	}
	text := raw
	d.Encoding = "utf-8"
	if !utf8.Valid(raw) {
		var err error
		text, err = simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
		if err != nil {
			issue(0, "encoding", "无法解码 GB18030 文件")
			return d
		}
		d.Encoding = "gb18030"
	}
	if bytes.Contains(text, []byte("\uFFFD")) {
		issue(0, "encoding", "文件含无法解码的字符，请重新导出")
		return d
	}
	text = bytes.TrimPrefix(text, []byte{0xef, 0xbb, 0xbf})
	// Metadata is line-oriented; CSV parsing begins only at the documented header.
	offset, headerLine := 0, 0
	for line := 1; line <= 100 && offset < len(text); line++ {
		end := bytes.IndexByte(text[offset:], '\n')
		if end < 0 {
			end = len(text) - offset
		}
		candidate := strings.TrimSpace(string(text[offset : offset+end]))
		if strings.HasPrefix(candidate, "交易时间,") || strings.HasPrefix(candidate, "\"交易时间\",") {
			headerLine = line
			break
		}
		offset += end + 1
	}
	if headerLine == 0 {
		issue(0, "header", "前 100 行中没有支付宝交易时间表头")
		return d
	}
	r := csv.NewReader(bytes.NewReader(text[offset:]))
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		issue(headerLine, "header", "支付宝表头格式错误")
		return d
	}
	pos := map[string]int{}
	for i, s := range head {
		s = strings.TrimSpace(s)
		head[i] = s
		if s == "" && i == len(head)-1 {
			continue
		} // exporter trailing comma
		if s == "" {
			issue(headerLine, "header", "存在空列名")
			return d
		}
		if _, ok := pos[s]; ok {
			issue(headerLine, "header", "存在重复列名")
			return d
		}
		pos[s] = i
	}
	required := [][]string{{"交易时间"}, {"交易分类", "交易来源"}, {"交易对方"}, {"商品说明", "商品名称"}, {"收/支"}, {"金额", "金额(元)"}, {"收/付款方式"}, {"交易状态"}, {"交易订单号"}, {"商家订单号"}}
	for _, aliases := range required {
		found := 0
		for _, s := range aliases {
			if _, ok := pos[s]; ok {
				found++
			}
		}
		if found != 1 {
			issue(headerLine, "header", fmt.Sprintf("缺少或混用必要列：%s", strings.Join(aliases, " / ")))
			return d
		}
	}
	ordinal := 0
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			line := headerLine
			if ce, ok := e.(*csv.ParseError); ok {
				line += ce.Line - 1
			}
			issue(line, "csv", "CSV 引号或结构错误")
			break
		}
		line, _ := r.FieldPos(0)
		line += headerLine - 1
		if ordinal >= MaxRows {
			issue(line, "row_limit", "单次最多解析 1000 条，请拆分文件")
			break
		}
		ordinal++
		if len(row) != len(head) {
			issue(line, "column_count", "列数与表头不一致，未将该行静默忽略")
			if len(d.Issues) >= 20 {
				break
			}
			continue
		}
		if head[len(head)-1] == "" && strings.TrimSpace(row[len(row)-1]) != "" {
			issue(line, "column_count", "末尾无列名字段包含数据")
			break
		}
		values := map[string]string{}
		for name, i := range pos {
			values[name] = row[i]
		}
		get := func(names ...string) string {
			for _, name := range names {
				if i, ok := pos[name]; ok {
					return strings.TrimSpace(row[i])
				}
			}
			return ""
		}
		f := Fields{Date: get("交易时间"), Type: get("收/支"), Amount: get("金额", "金额(元)"), Currency: "CNY", Merchant: get("交易对方"), Category: get("交易分类", "交易来源"), Source: "支付宝", Account: strings.TrimSpace(account), ExternalID: get("交易订单号"), Note: get("商品说明", "商品名称")}
		ids := []Identifier{}
		scope, _ := json.Marshal([]string{"alipay", f.Account})
		if f.ExternalID != "" && f.ExternalID != "/" && f.ExternalID != "-" {
			ids = append(ids, Identifier{Kind: "alipay_payment_trace", Scope: string(scope), Value: f.ExternalID})
		}
		if v := get("商家订单号"); v != "" && v != "/" && v != "-" {
			merchantScope, _ := json.Marshal([]string{"alipay", f.Account, f.Merchant})
			ids = append(ids, Identifier{Kind: "merchant_order", Scope: string(merchantScope), Value: v})
		}
		d.Records = append(d.Records, Record{Ordinal: ordinal, Line: line, Fields: f, Identifiers: ids, Raw: values})
	}
	if len(d.Records) == 0 && len(d.Issues) == 0 {
		issue(headerLine, "empty", "没有账单记录")
	}
	return d
}
