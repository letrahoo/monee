package ledger

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/letrahoo/monee/server/internal/redaction"
)

type RedactionExclusion struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

type RedactionPreview struct {
	ImportID      string                `json:"importId"`
	LocalOnly     bool                  `json:"localOnly"`
	IncludedCount int                   `json:"includedCount"`
	Excluded      []RedactionExclusion  `json:"excluded"`
	Payload       json.RawMessage       `json:"payload"`
	Treatments    []redaction.Treatment `json:"treatments"`
}

// ImportRedactionPreview only reads an authorized source snapshot. It grants no
// provider consent, changes no booked data, and has no network capability.
func (s *Store) ImportRedactionPreview(id string, projector *redaction.Projector) (RedactionPreview, error) {
	out := RedactionPreview{ImportID: id, LocalOnly: true, Excluded: []RedactionExclusion{}, Treatments: []redaction.Treatment{}}
	detail, err := s.ImportDetail(id)
	if err != nil {
		return out, err
	}
	if projector == nil {
		return out, problem("redaction_unavailable", "脱敏预览暂不可用")
	}
	p := detail.Preview
	if p.SourceDocument == nil || (p.Format != "alipay" && p.Format != "wechat") {
		return out, problem("invalid", "此批次没有可用于脱敏预览的原生来源记录")
	}
	if len(p.SourceDocument.Issues) > 0 || len(p.SourceDocument.Records) > MaxRows {
		return out, problem("invalid", "请先处理此批次的文件解析问题")
	}
	directions := map[int]string{}
	for _, row := range p.Rows {
		if row.Record.Type == "expense" || row.Record.Type == "income" {
			directions[row.Line] = row.Record.Type
		}
	}
	inputs := []redaction.Input{}
	seen := map[int]bool{}
	for _, row := range p.SourceDocument.Records {
		if row.Line < 1 || seen[row.Line] {
			return out, problem("invalid", "来源记录位置无效")
		}
		seen[row.Line] = true
		exclude := func(reason string) {
			out.Excluded = append(out.Excluded, RedactionExclusion{Line: row.Line, Reason: reason})
		}
		f := row.Fields
		date, err := time.Parse("2006-01-02 15:04:05", f.Date)
		if err != nil || date.Year() < 1900 || date.Year() > 2200 || date.Format("2006-01-02 15:04:05") != f.Date {
			exclude("交易日期无效")
			continue
		}
		minor, err := parseAmount(strings.TrimSpace(f.Amount))
		if err != nil {
			exclude("金额须为有效正数且最多两位小数")
			continue
		}
		if f.Currency != "CNY" {
			exclude("币种须明确为 CNY")
			continue
		}
		if (p.Format == "alipay" && f.Source != "支付宝") || (p.Format == "wechat" && f.Source != "微信") {
			exclude("来源标识不一致")
			continue
		}
		direction := directions[row.Line]
		if direction == "" {
			direction = "unknown"
		}
		input := redaction.Input{RecordID: id + ":" + strconv.Itoa(row.Line), Date: date.Format("2006-01-02"), AmountMinor: strconv.FormatInt(minor, 10), Currency: f.Currency, Direction: direction, Source: p.Format, Account: p.SourceAccount, Merchant: f.Merchant, Note: f.Note, Identifiers: row.Identifiers}
		// A malformed identifier or oversized private field excludes that occurrence;
		// errors use fixed text and never echo raw source values.
		if _, err := projector.Project(s.ledgerID, []redaction.Input{input}); err != nil {
			exclude("来源字段未通过脱敏校验")
			continue
		}
		inputs = append(inputs, input)
	}
	out.IncludedCount = len(inputs)
	if len(inputs) == 0 {
		out.Payload = json.RawMessage(`{"version":"` + redaction.Version + `","records":[]}`)
		return out, nil
	}
	preview, err := projector.Project(s.ledgerID, inputs)
	if err != nil {
		return out, problem("invalid", "来源字段未通过脱敏校验")
	}
	out.Payload, out.Treatments = preview.Bytes(), preview.Treatments()
	return out, nil
}
