package ingestion

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

var columns = []string{"date", "type", "amount", "currency", "merchant", "category", "source", "account", "external_id", "note"}

// StandardColumns returns a copy so exporters share the interchange contract
// without being able to mutate parser configuration.
func StandardColumns() []string { return append([]string(nil), columns...) }

// ParseStandardCSV accepts only the explicit Monee interchange format, not
// native bank exports. Keep unsupported business types as evidence for the
// ledger to reject or route to review; never coerce them into ordinary income.
func ParseStandardCSV(text string) Document {
	d := Document{ContentHash: digest([]byte(text)), Parser: "monee-standard-csv", ParserVersion: 1, Records: []Record{}, Issues: []Issue{}}
	issue := func(line int, code, message string) { d.Issues = append(d.Issues, Issue{line, code, message}) }
	if len(text) > MaxBytes || !utf8.ValidString(text) || strings.ContainsRune(text, '\uFFFD') {
		issue(0, "encoding_or_size", "请使用不超过 2 MiB 的 UTF-8 CSV 文件")
		return d
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, "\uFEFF")))
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		issue(0, "header", "文件为空或 CSV 表头无效")
		return d
	}
	positions := map[string]int{}
	for index, value := range head {
		value = strings.TrimSpace(value)
		known := false
		for _, h := range columns {
			if h == value {
				known = true
				break
			}
		}
		if !known {
			issue(1, "header", "不支持的表头："+value+"。请使用标准 CSV 模板")
			return d
		}
		if _, exists := positions[value]; exists {
			issue(1, "header", "重复的表头："+value)
			return d
		}
		positions[value] = index
	}
	for _, h := range []string{"date", "type", "amount", "currency", "merchant", "source"} {
		if _, ok := positions[h]; !ok {
			issue(1, "header", "缺少必要表头："+h)
			return d
		}
	}
	for count := 0; ; count++ {
		fields, e := r.Read()
		if e == io.EOF {
			break
		}
		if count >= MaxRows {
			issue(0, "row_limit", "单次最多导入 1000 行，请拆分文件")
			return d
		}
		if e != nil {
			line := 0
			if ce, ok := e.(*csv.ParseError); ok {
				line = ce.Line
			}
			issue(line, "csv", "CSV 格式错误："+e.Error())
			return d
		}
		line, _ := r.FieldPos(0)
		if len(fields) != len(head) {
			issue(line, "column_count", fmt.Sprintf("第 %d 行：列数与表头不一致", line))
			if len(d.Issues) >= 20 {
				return d
			}
			continue
		}
		get := func(name string) string {
			if index, ok := positions[name]; ok {
				return fields[index]
			}
			return ""
		}
		f := Fields{Date: get("date"), Type: get("type"), Amount: get("amount"), Currency: get("currency"), Merchant: get("merchant"), Category: get("category"), Source: get("source"), Account: get("account"), ExternalID: get("external_id"), Note: get("note")}
		ids := []Identifier{}
		if value := strings.TrimSpace(f.ExternalID); value != "" {
			account := strings.TrimSpace(f.Account)
			if account == "" {
				account = "待核实资金账户"
			}
			// Scope uses the standard file's exact source/account pair. Future
			// native adapters must supply issuer-specific kinds and namespaces.
			scope, _ := json.Marshal([]string{strings.TrimSpace(f.Source), account})
			ids = append(ids, Identifier{Kind: "source_transaction", Scope: string(scope), Value: value})
		}
		d.Records = append(d.Records, Record{Ordinal: count + 1, Line: line, Fields: f, Identifiers: ids})
	}
	if len(d.Records) == 0 && len(d.Issues) == 0 {
		issue(0, "empty", "CSV 中没有账单记录")
	}
	return d
}
