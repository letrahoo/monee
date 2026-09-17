package ingestion

import (
	"bytes"
	"encoding/csv"
	"io"
	"strings"
	"unicode/utf8"
)

// ParseWeChatCSV accepts the explicit WeChat column contract in UTF-8. There is
// no original CSV sample in the reference repository; do not infer additional
// dialects or encodings. Metadata before the header is not financial evidence.
func ParseWeChatCSV(raw []byte, account string) Document {
	d := Document{ContentHash: digest(raw), Parser: "wechat-csv", ParserVersion: 1, Encoding: "utf-8", Records: []Record{}, Issues: []Issue{}}
	fail := func(line int, code, message string) Document {
		d.Issues = append(d.Issues, Issue{line, code, message})
		return d
	}
	if len(raw) > MaxBytes {
		return fail(0, "size", "文件不能超过 2 MiB")
	}
	if strings.TrimSpace(account) == "" {
		return fail(0, "account", "必须指定微信来源账户名称")
	}
	if !utf8.Valid(raw) || bytes.Contains(raw, []byte("\uFFFD")) || bytes.ContainsRune(raw, 0) {
		return fail(0, "encoding", "微信 CSV 必须使用无损 UTF-8 编码")
	}
	text := bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	// Permit exporter metadata as standalone lines. Financial-looking rows
	// before a header are rejected rather than silently discarded.
	offset, headerLine := 0, 0
	for line := 1; line <= 100 && offset < len(text); line++ {
		end := bytes.IndexByte(text[offset:], '\n')
		if end < 0 {
			end = len(text) - offset
		}
		candidate := strings.TrimSpace(string(text[offset : offset+end]))
		probe := csv.NewReader(strings.NewReader(candidate))
		probe.TrimLeadingSpace = true
		fields, err := probe.Read()
		if err == nil && len(fields) > 0 && strings.TrimSpace(fields[0]) == "交易时间" {
			headerLine = line
			break
		}
		if strings.Contains(candidate, ",") || strings.Contains(candidate, "\"") {
			return fail(line, "header", "表头前存在无法识别的数据，请使用原始微信账单")
		}
		offset += end + 1
	}
	if headerLine == 0 {
		return fail(0, "header", "前 100 行中没有微信交易时间表头")
	}
	r := csv.NewReader(bytes.NewReader(text[offset:]))
	r.FieldsPerRecord = -1
	// Leading whitespace in values is preserved; only normalized fields trim it.
	head, err := r.Read()
	if err != nil || len(head) > 32 {
		return fail(headerLine, "header", "微信 CSV 表头无效或列数超过限制")
	}
	positions := map[string]int{}
	for i, value := range head {
		name := strings.TrimSpace(value)
		head[i] = name
		if name == "" {
			return fail(headerLine, "header", "存在空列名")
		}
		if _, exists := positions[name]; exists {
			return fail(headerLine, "header", "表头列名重复")
		}
		positions[name] = i
	}
	for _, name := range weChatColumns {
		if _, exists := positions[name]; !exists {
			return fail(headerLine, "header", "缺少微信账单列："+name)
		}
	}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			line := headerLine
			if parseError, ok := err.(*csv.ParseError); ok {
				line += parseError.Line - 1
			}
			return fail(line, "csv", "CSV 引号或结构错误")
		}
		line, _ := r.FieldPos(0)
		line += headerLine - 1
		if len(row) != len(head) {
			return fail(line, "column_count", "列数与表头不一致，未将该行静默忽略")
		}
		if len(d.Records) >= MaxRows {
			return fail(line, "row_limit", "单次最多解析 1000 条，请拆分文件")
		}
		fields := make(map[string]string, len(head))
		for name, i := range positions {
			fields[name] = row[i]
		}
		d.Records = append(d.Records, weChatRecord(fields, account, len(d.Records)+1, line))
	}
	if len(d.Records) == 0 {
		return fail(headerLine, "empty", "没有微信账单记录")
	}
	return d
}
