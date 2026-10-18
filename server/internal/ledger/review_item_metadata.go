package ledger

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

// ReviewItemMetadata describes retained source evidence, never write eligibility.
// In particular ordinary_candidate is not permission to confirm a transaction.
type ReviewItemMetadata struct {
	ContractVersion int                 `json:"contractVersion"`
	ReadOnly        bool                `json:"readOnly"`
	Actions         []string            `json:"actions"`
	ImportID        string              `json:"importId"`
	Line            int                 `json:"line"`
	BatchState      string              `json:"batchState"`
	Classification  string              `json:"classification"`
	SourceRef       ReviewItemSourceRef `json:"sourceRef"`
	Facts           ReviewItemFacts     `json:"facts"`
	Blockers        []ReviewItemBlocker `json:"blockers"`
}
type ReviewItemSourceRef struct {
	Ordinal               int    `json:"ordinal"`
	DocumentContentHash   string `json:"documentContentHash"`
	Parser                string `json:"parser"`
	ParserVersion         int    `json:"parserVersion"`
	EnvelopeParserVersion int    `json:"envelopeParserVersion"`
}
type ReviewItemFacts struct {
	Direction           string `json:"direction"`
	BusinessType        string `json:"businessType"`
	Status              string `json:"status"`
	Currency            string `json:"currency"`
	SourceAccount       string `json:"sourceAccount"`
	PaymentTracePresent bool   `json:"paymentTracePresent"`
}
type ReviewItemBlocker struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

// reviewMetadataSnapshot holds only stored evidence. Evaluation does not parse
// original files, allocate IDs, normalize transactions, consult a clock or write.
type reviewMetadataSnapshot struct {
	previewJSON     string
	envelopeVersion int
	envelopeHash    string
	committed       bool
	eventJSON       string
	eventOperation  string
	eventVersion    int
	linked          bool
}

func (s *Store) ReviewItemMetadata(importID string, line int) (ReviewItemMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ReviewItemMetadata{}, err
	}
	defer tx.Rollback()
	if err = s.authorize(tx, false); err != nil {
		return ReviewItemMetadata{}, err
	}
	if line < 1 || line > 1_000_000 {
		return ReviewItemMetadata{}, problem("invalid", "来源行号无效")
	}
	var snap reviewMetadataSnapshot
	var committed sql.NullString
	err = tx.QueryRow(`SELECT preview_json,parser_version,content_hash,committed_at FROM imports WHERE ledger_id=? AND id=?`, s.ledgerID, importID).Scan(&snap.previewJSON, &snap.envelopeVersion, &snap.envelopeHash, &committed)
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewItemMetadata{}, problem("not_found", "找不到此账本的导入记录")
	}
	if err != nil {
		return ReviewItemMetadata{}, err
	}
	snap.committed = committed.Valid && committed.String != ""
	err = tx.QueryRow(`SELECT payload_json,operation,payload_version FROM change_log WHERE ledger_id=? AND entity_type='import' AND entity_id=? AND operation IN ('import_undo','import_restore') ORDER BY sequence DESC LIMIT 1`, s.ledgerID, importID).Scan(&snap.eventJSON, &snap.eventOperation, &snap.eventVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ReviewItemMetadata{}, err
	}
	var links int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM source_records sr JOIN imports i ON i.id=sr.import_id WHERE i.ledger_id=? AND i.id=? AND sr.line=?`, s.ledgerID, importID, line).Scan(&links); err != nil {
		return ReviewItemMetadata{}, err
	}
	snap.linked = links > 0
	return evaluateReviewItemMetadata(importID, line, snap)
}

func (out *ReviewItemMetadata) block(code, path string) {
	for _, b := range out.Blockers {
		if b.Code == code && b.Path == path {
			return
		}
	}
	out.Blockers = append(out.Blockers, ReviewItemBlocker{code, path})
}

func evaluateReviewItemMetadata(importID string, line int, snap reviewMetadataSnapshot) (ReviewItemMetadata, error) {
	out := ReviewItemMetadata{ContractVersion: 1, ReadOnly: true, Actions: []string{}, ImportID: importID, Line: line, BatchState: "unknown", Classification: "unknown", Blockers: []ReviewItemBlocker{}}
	if snap.envelopeVersion >= 0 {
		out.SourceRef.EnvelopeParserVersion = snap.envelopeVersion
	}
	var p Preview
	if !reviewMetadataJSON(snap.previewJSON, &p) || p.ID != importID {
		out.block("snapshot_invalid", "snapshot")
		return out, nil
	}
	out.BatchState = "preview"
	if snap.committed {
		out.BatchState = "committed"
	}
	if len(p.Errors) > 0 {
		out.BatchState = "failed"
		out.block("batch_failed", "batchState")
	}
	if snap.eventOperation != "" {
		var event importUndoState
		valid := reviewMetadataJSON(snap.eventJSON, &event) && event.Changes != nil && snap.eventVersion == 1 && snap.committed && ((snap.eventOperation == "import_undo" && event.State == "undone") || (snap.eventOperation == "import_restore" && event.State == "committed"))
		for id, change := range event.Changes {
			if id == "" || change.Version < 1 {
				valid = false
			}
			if event.State == "undone" {
				if _, err := time.Parse(time.RFC3339Nano, change.DeletedAt); err != nil {
					valid = false
				}
			} else if change.DeletedAt != "" {
				valid = false
			}
		}
		if !valid {
			out.BatchState = "unknown"
			out.block("batch_event_invalid", "batchState")
		} else if event.State == "undone" {
			out.BatchState = "undone"
			out.block("batch_undone", "batchState")
		}
	}
	if snap.linked {
		out.block("linked_source", "sourceRecord")
	}
	if p.Format != "alipay" && p.Format != "wechat" {
		out.block("format_unsupported", "format")
		return out, nil
	}
	pendingCount := 0
	var pending PendingRow
	for _, r := range p.Pending {
		if r.Line == line {
			pending = r
			pendingCount++
		}
	}
	if pendingCount == 0 {
		return out, problem("not_found", "找不到此来源复核行")
	}
	if pendingCount != 1 {
		out.block("pending_ambiguous", "pending")
		return out, nil
	}
	for _, r := range p.Rows {
		if r.Line == line {
			out.block("pending_conflict", "pending")
		}
	}
	d := p.SourceDocument
	if d == nil {
		out.block("document_missing", "sourceDocument")
		return out, nil
	}
	if reviewMetadataHash(d.ContentHash) {
		out.SourceRef.DocumentContentHash = d.ContentHash
	}
	if d.Parser == "alipay-csv" || d.Parser == "wechat-csv" || d.Parser == "wechat-xlsx" {
		out.SourceRef.Parser = d.Parser
	}
	if d.ParserVersion >= 0 {
		out.SourceRef.ParserVersion = d.ParserVersion
	}
	if snap.envelopeVersion != 2 || d.ParserVersion != 1 || (p.Format == "alipay" && d.Parser != "alipay-csv") || (p.Format == "wechat" && d.Parser != "wechat-csv" && d.Parser != "wechat-xlsx") {
		out.block("parser_unsupported", "sourceRef")
	}
	if !reviewMetadataHash(d.ContentHash) || !reviewMetadataHash(snap.envelopeHash) {
		out.block("hash_missing_or_invalid", "sourceRef.documentContentHash")
	}
	if len(d.Issues) > 0 {
		out.block("document_issues", "sourceDocument.issues")
	}
	records := 0
	var r ingestion.Record
	ordinals := map[int]bool{}
	lines := map[int]bool{}
	for _, record := range d.Records {
		if record.Ordinal < 1 || ordinals[record.Ordinal] {
			out.block("ordinal_ambiguous", "sourceDocument.records")
		}
		if record.Line < 1 || lines[record.Line] {
			out.block("record_ambiguous", "sourceDocument.records")
		}
		ordinals[record.Ordinal] = true
		lines[record.Line] = true
		if record.Line == line {
			r = record
			records++
		}
	}
	if records != 1 {
		out.block("record_ambiguous", "sourceDocument.records")
		return out, nil
	}
	if r.Ordinal > 0 {
		out.SourceRef.Ordinal = r.Ordinal
	}
	f := r.Fields
	statusKey := "交易状态"
	if p.Format == "wechat" {
		statusKey = "当前状态"
	}
	status := strings.TrimSpace(r.Raw[statusKey])
	out.Facts = ReviewItemFacts{Direction: f.Type, BusinessType: f.Category, Status: status, Currency: f.Currency, SourceAccount: p.SourceAccount}
	special := reviewMetadataSpecial(r)
	if special {
		out.block("special_source", "sourceDocument.records.raw")
	}
	for _, other := range d.Records {
		if other.Line != line && f.Merchant != "" && (other.Fields.Merchant == f.Merchant || strings.TrimSpace(other.Raw["交易对方"]) == f.Merchant) && reviewMetadataRefund(other) {
			out.block("same_merchant_refund", "sourceDocument.records")
			break
		}
	}
	if !validName(p.SourceAccount) || p.SourceAccount != strings.TrimSpace(p.SourceAccount) || f.Account != p.SourceAccount {
		out.block("source_account_invalid", "facts.sourceAccount")
	}
	if pending.Date != f.Date || pending.Merchant != f.Merchant || pending.Amount != f.Amount {
		out.block("pending_conflict", "pending")
	}
	if !reviewMetadataRaw(p.Format, r) {
		out.block("raw_fields_conflict", "sourceDocument.records.raw")
	}
	if f.Currency != "CNY" {
		out.block("currency_unsupported", "facts.currency")
	}
	if date, err := time.Parse("2006-01-02 15:04:05", f.Date); err != nil || date.Year() < 1900 || date.Year() > 2200 {
		out.block("date_invalid", "sourceDocument.records.fields.date")
	}
	if _, err := parseAmount(f.Amount); err != nil {
		out.block("amount_invalid", "sourceDocument.records.fields.amount")
	}
	if f.Merchant == "" {
		out.block("merchant_missing", "sourceDocument.records.fields.merchant")
	}
	paymentKey := "收/付款方式"
	if p.Format == "wechat" {
		paymentKey = "支付方式"
	}
	for _, value := range []string{f.Merchant, f.Category, f.Source, f.ExternalID, strings.TrimSpace(r.Raw[paymentKey])} {
		if len([]rune(value)) > 200 || strings.ContainsAny(value, "\x00\r\n") {
			out.block("fields_invalid", "sourceDocument.records.fields")
		}
	}
	if len([]rune(f.Note)) > 1000 || strings.ContainsRune(f.Note, 0) {
		out.block("fields_invalid", "sourceDocument.records.fields")
	}
	traceKind := p.Format + "_payment_trace"
	traceCount := 0
	merchantCount := 0
	traceValid := false
	for _, id := range r.Identifiers {
		expectedScope := []string{p.Format, p.SourceAccount}
		expectedValue := f.ExternalID
		if id.Kind == "merchant_order" {
			merchantCount++
			expectedScope = append(expectedScope, f.Merchant)
			key := "商家订单号"
			if p.Format == "wechat" {
				key = "商户单号"
			}
			expectedValue = strings.TrimSpace(r.Raw[key])
		} else if id.Kind != traceKind {
			out.block("identifier_conflict", "sourceDocument.records.identifiers")
			continue
		}
		var scope []string
		valid := json.Unmarshal([]byte(id.Scope), &scope) == nil && len(scope) == len(expectedScope) && id.Value != "" && id.Value != "/" && id.Value != "-" && id.Value == expectedValue
		for i := range scope {
			if i >= len(expectedScope) || scope[i] != expectedScope[i] {
				valid = false
			}
		}
		if !valid {
			out.block("identifier_conflict", "sourceDocument.records.identifiers")
		}
		if id.Kind == traceKind {
			traceCount++
			traceValid = valid
		}
	}
	merchantKey := "商家订单号"
	if p.Format == "wechat" {
		merchantKey = "商户单号"
	}
	merchantValue := strings.TrimSpace(r.Raw[merchantKey])
	expectedMerchants := 0
	if merchantValue != "" && merchantValue != "/" && merchantValue != "-" {
		expectedMerchants = 1
	}
	if merchantCount != expectedMerchants {
		out.block("identifier_conflict", "sourceDocument.records.identifiers")
	}
	out.Facts.PaymentTracePresent = traceCount == 1 && traceValid
	if !out.Facts.PaymentTracePresent {
		out.block("payment_trace_missing_or_invalid", "facts.paymentTracePresent")
	}
	known := false
	if p.Format == "wechat" {
		known = f.Type == "支出" && f.Category == "商户消费" && status == "支付成功"
	} else {
		if status == "交易成功" {
			if f.Type == "收入" {
				known = f.Category == "工资" || f.Category == "工资收入"
			}
			if f.Type == "支出" {
				for _, category := range []string{"餐饮美食", "日用百货", "交通出行", "服饰装扮", "数码电器", "家居家装", "运动户外", "美容美发", "母婴亲子", "宠物", "文化休闲", "生活服务", "教育培训", "医疗健康", "酒店旅游", "住房物业", "商业服务", "公益捐赠"} {
					if f.Category == category {
						known = true
					}
				}
			}
		}
	}
	if !known && !special {
		out.block("ordinary_source_unproven", "sourceDocument.records.raw")
	}
	if special {
		out.Classification = "special"
	} else if len(out.Blockers) == 0 {
		out.Classification = "ordinary_candidate"
	}
	return out, nil
}

func reviewMetadataHash(value string) bool {
	b, e := hex.DecodeString(value)
	return e == nil && len(b) == 32 && value == strings.ToLower(value)
}

func reviewMetadataRefund(r ingestion.Record) bool {
	return strings.Contains(r.Raw["交易状态"]+r.Raw["当前状态"], "退") || strings.Contains(r.Raw["交易类型"]+r.Raw["交易分类"]+r.Raw["交易来源"]+r.Fields.Category, "退款")
}
func reviewMetadataSpecial(r ingestion.Record) bool {
	if reviewMetadataRefund(r) {
		return true
	}
	business := r.Fields.Type + " " + r.Fields.Category + " " + r.Fields.Note + " " + r.Fields.Merchant
	for _, key := range []string{"交易类型", "交易分类", "交易来源", "收/支", "商品说明", "商品名称", "商品", "交易对方", "交易状态", "当前状态"} {
		business += " " + r.Raw[key]
	}
	business = strings.ToLower(business)
	for _, word := range []string{"refund", "transfer", "repayment", "退款", "转账", "还款", "充值", "提现", "理财", "基金", "借款", "借呗", "余额宝", "红包", "代付", "分期", "外币", "美元", "港币", "转入", "转出", "信用借还", "借还", "收款", "金融", "利息", "手续费"} {
		if strings.Contains(business, word) {
			return true
		}
	}
	return false
}

// Compare the stored normalized extraction to native columns without reparsing
// the document. Known aliases must occur exactly once, even when empty.
func reviewMetadataRaw(format string, r ingestion.Record) bool {
	f := r.Fields
	get := func(keys ...string) (string, bool) {
		value := ""
		n := 0
		for _, k := range keys {
			if v, ok := r.Raw[k]; ok {
				value = strings.TrimSpace(v)
				n++
			}
		}
		return value, n == 1
	}
	check := func(value string, keys ...string) bool { actual, ok := get(keys...); return ok && actual == value }
	if !check(f.Date, "交易时间") || !check(f.Type, "收/支") || !check(f.Merchant, "交易对方") {
		return false
	}
	if format == "alipay" {
		_, payment := get("收/付款方式")
		_, status := get("交易状态")
		_, merchant := get("商家订单号")
		return f.Source == "支付宝" && payment && status && merchant && check(f.Amount, "金额", "金额(元)") && check(f.Category, "交易分类", "交易来源") && check(f.Note, "商品说明", "商品名称") && check(f.ExternalID, "交易订单号")
	}
	amount, ok := get("金额(元)")
	amount = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(amount, "¥"), "￥"))
	_, payment := get("支付方式")
	_, status := get("当前状态")
	_, merchant := get("商户单号")
	return f.Source == "微信" && payment && status && merchant && ok && f.Amount == amount && check(f.Category, "交易类型") && check(f.Note, "商品") && check(f.ExternalID, "交易单号")
}

// Reject duplicate JSON object keys, trailing data, null roots and wrong types.
// No decoder error or source JSON is exposed in the public blocker contract.
// encoding/json matches struct fields with Unicode simple-fold, not merely
// lowercase. Canonicalize the entire equivalence cycle so aliases such as long-s,
// Kelvin sign and final sigma cannot silently overwrite retained source facts.
func reviewMetadataFoldKey(key string) string {
	return strings.Map(func(r rune) rune {
		minimum := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < minimum {
				minimum = next
			}
		}
		return minimum
	}, key)
}

func reviewMetadataJSON(raw string, target any) bool {
	decoder := json.NewDecoder(strings.NewReader(raw))
	var walk func(int) bool
	walk = func(depth int) bool {
		if depth > 64 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for decoder.More() {
				token, err = decoder.Token()
				if err != nil {
					return false
				}
				key, ok := token.(string)
				key = reviewMetadataFoldKey(key)
				if !ok || keys[key] {
					return false
				}
				keys[key] = true
				if !walk(depth + 1) {
					return false
				}
			}
		case '[':
			for decoder.More() {
				if !walk(depth + 1) {
					return false
				}
			}
		default:
			return false
		}
		_, err = decoder.Token()
		return err == nil
	}
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") || !walk(0) {
		return false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return false
	}
	return json.Unmarshal([]byte(raw), target) == nil
}
