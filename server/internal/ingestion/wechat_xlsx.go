package ingestion

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The supported source is the single-sheet WeChat export observed in ai-ledger.
// Preserve XML decimal strings and IDs; never round money through float64.
func ParseWeChatXLSX(raw []byte, account string) Document {
	d := Document{ContentHash: digest(raw), Parser: "wechat-xlsx", ParserVersion: 1, Encoding: "xlsx", Records: []Record{}, Issues: []Issue{}}
	fail := func(line int, msg string) Document { d.Issues = append(d.Issues, Issue{line, "xlsx", msg}); return d }
	if len(raw) > MaxBytes {
		return fail(0, "文件不能超过 2 MiB")
	}
	if strings.TrimSpace(account) == "" {
		return fail(0, "必须指定微信来源账户名称")
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return fail(0, "无法读取 XLSX，请选择未加密的微信账单")
	}
	if len(z.File) > 128 {
		return fail(0, "XLSX 文件结构超出限制")
	}
	files := map[string][]byte{}
	var expanded uint64
	sheets := 0
	for _, f := range z.File {
		if f.UncompressedSize64 > 20<<20 {
			return fail(0, "XLSX 条目超过 20 MiB")
		}
		expanded += f.UncompressedSize64
		if expanded > 20<<20 {
			return fail(0, "XLSX 解压内容超过 20 MiB")
		}
		if _, ok := files[f.Name]; ok {
			return fail(0, "XLSX 存在重复条目")
		}
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheets++
		}
		rc, e := f.Open()
		if e != nil {
			return fail(0, "XLSX 条目损坏")
		}
		b, e := io.ReadAll(io.LimitReader(rc, (20<<20)+1))
		rc.Close()
		if e != nil || len(b) > 20<<20 {
			return fail(0, "XLSX 条目无法安全读取")
		}
		files[f.Name] = b
	}
	if sheets != 1 || len(files["xl/worksheets/sheet1.xml"]) == 0 {
		return fail(0, "目前仅支持单工作表的微信原始导出，请勿合并或改写工作表")
	}
	type rich struct {
		Text string `xml:"t"`
		Runs []struct {
			Text string `xml:"t"`
		} `xml:"r"`
	}
	richText := func(r rich) string {
		s := r.Text
		for _, v := range r.Runs {
			s += v.Text
		}
		return s
	}
	var shared struct {
		Items []rich `xml:"si"`
	}
	if b := files["xl/sharedStrings.xml"]; len(b) > 0 {
		if xml.Unmarshal(b, &shared) != nil || len(shared.Items) > 20000 {
			return fail(0, "共享文本表无效或过大")
		}
	}
	type cell struct {
		Ref     string  `xml:"r,attr"`
		Type    string  `xml:"t,attr"`
		Value   string  `xml:"v"`
		Inline  rich    `xml:"is"`
		Formula *string `xml:"f"`
	}
	type sheetRow struct {
		Index int    `xml:"r,attr"`
		Cells []cell `xml:"c"`
	}
	dec := xml.NewDecoder(bytes.NewReader(files["xl/worksheets/sheet1.xml"]))
	headers := map[int]string{}
	headerFound := false
	lastRow := 0
	rowCount := 0
	required := []string{"交易时间", "交易类型", "交易对方", "商品", "收/支", "金额(元)", "支付方式", "当前状态", "交易单号", "商户单号"}
	for {
		token, e := dec.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fail(0, "工作表 XML 损坏")
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "row" {
			continue
		}
		var row sheetRow
		if dec.DecodeElement(&row, &start) != nil {
			return fail(0, "工作表行无效")
		}
		rowCount++
		if rowCount > MaxRows+100 || row.Index <= lastRow || row.Index > 100000 || len(row.Cells) > 32 {
			return fail(row.Index, "工作表行序或大小超出限制")
		}
		lastRow = row.Index
		values := map[int]string{}
		types := map[int]string{}
		for _, c := range row.Cells {
			if c.Formula != nil {
				return fail(row.Index, "账单含公式，请使用未经修改的原始导出")
			}
			col := 0
			at := 0
			for at < len(c.Ref) && c.Ref[at] >= 'A' && c.Ref[at] <= 'Z' {
				col = col*26 + int(c.Ref[at]-'A'+1)
				at++
			}
			n, e := strconv.Atoi(c.Ref[at:])
			if e != nil || n != row.Index || col < 1 || col > 32 {
				return fail(row.Index, "单元格位置无效")
			}
			if _, ok := values[col]; ok {
				return fail(row.Index, "单元格重复")
			}
			v := c.Value
			switch c.Type {
			case "s":
				i, e := strconv.Atoi(v)
				if e != nil || i < 0 || i >= len(shared.Items) {
					return fail(row.Index, "共享文本索引无效")
				}
				v = richText(shared.Items[i])
			case "inlineStr":
				v = richText(c.Inline)
			case "str", "n", "":
			default:
				return fail(row.Index, "存在不支持的单元格类型")
			}
			values[col] = v
			types[col] = c.Type
		}
		if !headerFound {
			found := false
			for _, v := range values {
				if strings.TrimSpace(v) == "交易时间" {
					found = true
				}
			}
			if !found {
				if rowCount >= 100 {
					return fail(row.Index, "前 100 行中没有微信账单表头")
				}
				continue
			}
			seen := map[string]bool{}
			for col, v := range values {
				v = strings.TrimSpace(v)
				if v == "" {
					continue
				}
				if seen[v] {
					return fail(row.Index, "表头列名重复")
				}
				seen[v] = true
				headers[col] = v
			}
			for _, v := range required {
				if !seen[v] {
					return fail(row.Index, "缺少微信账单列："+v)
				}
			}
			headerFound = true
			continue
		}
		nonempty := false
		for _, v := range values {
			if strings.TrimSpace(v) != "" {
				nonempty = true
			}
		}
		if !nonempty {
			continue
		}
		if len(d.Records) >= MaxRows {
			return fail(row.Index, "单次最多解析 1000 条")
		}
		fields := map[string]string{}
		for col, v := range values {
			name, ok := headers[col]
			if !ok && strings.TrimSpace(v) != "" {
				return fail(row.Index, "数据出现在无列名字段")
			}
			if ok {
				fields[name] = v
			}
			if (name == "交易单号" || name == "商户单号") && v != "" && types[col] != "s" && types[col] != "inlineStr" && types[col] != "str" {
				return fail(row.Index, "订单号必须为文本，避免电子表格精度损失")
			}
		}
		get := func(k string) string { return strings.TrimSpace(fields[k]) }
		amount := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(get("金额(元)"), "¥"), "￥"))
		f := Fields{Date: get("交易时间"), Type: get("收/支"), Amount: amount, Currency: "CNY", Merchant: get("交易对方"), Category: get("交易类型"), Source: "微信", Account: strings.TrimSpace(account), ExternalID: get("交易单号"), Note: get("商品")}
		scope, _ := json.Marshal([]string{"wechat", f.Account})
		ids := []Identifier{}
		if v := f.ExternalID; v != "" && v != "/" && v != "-" {
			ids = append(ids, Identifier{"wechat_payment_trace", string(scope), v})
		}
		if v := get("商户单号"); v != "" && v != "/" && v != "-" {
			scope, _ := json.Marshal([]string{"wechat", f.Account, f.Merchant})
			ids = append(ids, Identifier{"merchant_order", string(scope), v})
		}
		d.Records = append(d.Records, Record{Ordinal: len(d.Records) + 1, Line: row.Index, Fields: f, Identifiers: ids, Raw: fields})
	}
	if !headerFound || len(d.Records) == 0 {
		return fail(0, fmt.Sprintf("未找到微信账单记录（已读取 %d 行）", rowCount))
	}
	return d
}
