package ledger

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

// This guard freezes source evidence, not the mutable ledger projection. True
// semantic drift needs an explicit future migration, never silent re-preview.
func nativePreviewEvidenceProblem() error {
	return problem("conflict", "原生来源快照已变化或证据不完整，无法自动刷新此批预览")
}

// nativeEvidenceDecode fails closed on unknown struct fields as well as duplicate
// semantic struct keys. Map keys (including native raw columns and transaction
// IDs in events) remain case-sensitive. DisallowUnknownFields alone accepts
// both Scope and ſcope as aliases for a struct field.
func nativeEvidenceDecode(raw string, target any) bool {
	d := json.NewDecoder(strings.NewReader(raw))
	var walk func(int, reflect.Type) bool
	walk = func(depth int, typ reflect.Type) bool {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if depth > 64 {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			if typ.Kind() != reflect.Struct && typ.Kind() != reflect.Map {
				return false
			}
			keys := map[string]bool{}
			for d.More() {
				token, err = d.Token()
				if err != nil {
					return false
				}
				key, ok := token.(string)
				if !ok {
					return false
				}
				var child reflect.Type
				if typ.Kind() == reflect.Struct {
					key = nativeEvidenceFold(key)
					for i := 0; i < typ.NumField(); i++ {
						field := typ.Field(i)
						if field.PkgPath != "" {
							continue
						}
						name := strings.Split(field.Tag.Get("json"), ",")[0]
						if name == "-" {
							continue
						}
						if name == "" {
							name = field.Name
						}
						if nativeEvidenceFold(name) == key {
							child = field.Type
							break
						}
					}
					if child == nil {
						return false
					}
				} else {
					child = typ.Elem()
				}
				if keys[key] {
					return false
				}
				keys[key] = true
				if !walk(depth+1, child) {
					return false
				}
			}
		case '[':
			if typ.Kind() != reflect.Slice {
				return false
			}
			for d.More() {
				if !walk(depth+1, typ.Elem()) {
					return false
				}
			}
		default:
			return false
		}
		_, err = d.Token()
		return err == nil
	}
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") || !walk(0, reflect.TypeOf(target).Elem()) {
		return false
	}
	if _, err := d.Token(); err != io.EOF {
		return false
	}
	d = json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(target) == nil && nativeEvidenceShape(json.RawMessage(raw), reflect.TypeOf(target).Elem())
}

// Missing/null zero-valued fields are not evidence that a parser actually saved
// an empty value. Require the current schema's non-optional keys at every level.
func nativeEvidenceShape(raw json.RawMessage, typ reflect.Type) bool {
	if strings.TrimSpace(string(raw)) == "null" {
		return false
	}
	if typ.Kind() == reflect.Pointer {
		return nativeEvidenceShape(raw, typ.Elem())
	}
	switch typ.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return false
		}
		folded := map[string]json.RawMessage{}
		for key, value := range fields {
			folded[nativeEvidenceFold(key)] = value
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			value, exists := folded[nativeEvidenceFold(name)]
			optional := false
			for _, part := range tag[1:] {
				if part == "omitempty" {
					optional = true
				}
			}
			if !exists {
				if optional {
					continue
				}
				return false
			}
			if !nativeEvidenceShape(value, field.Type) {
				return false
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return false
		}
		for _, value := range values {
			if !nativeEvidenceShape(value, typ.Elem()) {
				return false
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return false
		}
		for _, value := range values {
			if !nativeEvidenceShape(value, typ.Elem()) {
				return false
			}
		}
	}
	return true
}
func nativeEvidenceFold(value string) string {
	return strings.Map(func(r rune) rune {
		smallest := r
		for n := unicode.SimpleFold(r); n != r; n = unicode.SimpleFold(n) {
			if n < smallest {
				smallest = n
			}
		}
		return smallest
	}, value)
}

// nativeEvidenceRefresh compares complete original extraction and allocation.
// IDs, row statuses/messages, counters and ledger version are refreshable; source
// fields, payment scopes and pending membership are not. Inputs are never edited.
func nativeEvidenceRefresh(raw, id, filename string, envelopeVersion int, fresh Preview) (Preview, error) {
	var old Preview
	if !nativeEvidenceDecode(raw, &old) || old.ID != id || old.Filename != filename || envelopeVersion != 2 || old.AlreadyCommitted || old.Undone || old.LedgerVersion < 0 || !nativeEvidenceValid(old) || !nativeEvidenceValid(fresh) {
		return Preview{}, nativePreviewEvidenceProblem()
	}
	if old.Format != fresh.Format || old.SourceAccount != fresh.SourceAccount || !reflect.DeepEqual(old.SourceDocument, fresh.SourceDocument) {
		return Preview{}, nativePreviewEvidenceProblem()
	}
	if len(old.Pending) != len(fresh.Pending) || len(old.Rows) != len(fresh.Rows) {
		return Preview{}, nativePreviewEvidenceProblem()
	}
	for i, previous := range old.Pending {
		next := fresh.Pending[i]
		previous.Reason = ""
		next.Reason = ""
		if previous != next {
			return Preview{}, nativePreviewEvidenceProblem()
		}
	}
	for i, previous := range old.Rows {
		next := fresh.Rows[i]
		if previous.Line != next.Line {
			return Preview{}, nativePreviewEvidenceProblem()
		}
		a, b := previous.Record, next.Record
		a.ID = ""
		b.ID = ""
		a.Version = 0
		b.Version = 0
		if a != b {
			return Preview{}, nativePreviewEvidenceProblem()
		}
	}
	fresh.ID = old.ID
	fresh.Filename = old.Filename
	fresh.SourceDocument = old.SourceDocument
	fresh.Pending = old.Pending
	return fresh, nil
}

func nativeEvidenceValid(p Preview) bool {
	d := p.SourceDocument
	if d == nil || d.ParserVersion != 1 || (p.Format != "alipay" && p.Format != "wechat") || (p.Format == "alipay" && d.Parser != "alipay-csv") || (p.Format == "wechat" && d.Parser != "wechat-csv" && d.Parser != "wechat-xlsx") {
		return false
	}
	hashBytes, err := hex.DecodeString(d.ContentHash)
	if err != nil || len(hashBytes) != 32 || d.ContentHash != strings.ToLower(d.ContentHash) || d.Records == nil || d.Issues == nil || p.Rows == nil || p.Errors == nil {
		return false
	}
	if len(d.Records) == 0 {
		if len(d.Issues) == 0 || p.SourceAccount != "" {
			return false
		}
	} else if !validName(p.SourceAccount) || p.SourceAccount != strings.TrimSpace(p.SourceAccount) {
		return false
	}
	lines := map[int]ingestion.Record{}
	ordinals := map[int]bool{}
	for _, record := range d.Records {
		if record.Line < 1 || record.Ordinal < 1 || ordinals[record.Ordinal] || record.Raw == nil || record.Identifiers == nil {
			return false
		}
		if _, exists := lines[record.Line]; exists {
			return false
		}
		if record.Fields.Account != p.SourceAccount || !nativeEvidenceRaw(p.Format, d.Parser, record) {
			return false
		}
		lines[record.Line] = record
		ordinals[record.Ordinal] = true
	}
	allocated := map[int]bool{}
	for _, r := range p.Pending {
		source, ok := lines[r.Line]
		if !ok || allocated[r.Line] || r.Date != source.Fields.Date || r.Amount != source.Fields.Amount || r.Merchant != source.Fields.Merchant || r.Reason == "" {
			return false
		}
		allocated[r.Line] = true
	}
	expectedErrors := []string{}
	for _, issue := range d.Issues {
		if issue.Line < 0 || issue.Code == "" || issue.Message == "" {
			return false
		}
		expectedErrors = append(expectedErrors, fmt.Sprintf("第 %d 行：%s", issue.Line, issue.Message))
	}
	for _, r := range p.Rows {
		if _, ok := lines[r.Line]; !ok || allocated[r.Line] {
			return false
		}
		allocated[r.Line] = true
		switch r.Status {
		case "new":
			if r.Message != "" {
				return false
			}
		case "duplicate":
			if r.Message != "相同来源流水已入账或在文件内重复，将跳过" {
				return false
			}
		case "similar":
			if r.Message != "日期、金额、商户相同，可能与另一来源重复" {
				return false
			}
		case "conflict":
			if r.Message != "相同来源流水号的数据不一致" {
				return false
			}
			expectedErrors = append(expectedErrors, fmt.Sprintf("第 %d 行：%s", r.Line, r.Message))
		default:
			return false
		}
	}
	if len(allocated) != len(lines) || len(expectedErrors) != len(p.Errors) {
		return false
	}
	errors := map[string]int{}
	for _, message := range expectedErrors {
		errors[message]++
	}
	for _, message := range p.Errors {
		if errors[message] == 0 {
			return false
		}
		errors[message]--
	}
	return true
}

// Native raw columns and identifier scopes are evidence too. Missing payment
// traces remain legitimate pending evidence, but cannot be invented or repaired.
func nativeEvidenceRaw(format, parser string, r ingestion.Record) bool {
	f := r.Fields
	get := func(keys ...string) (string, bool) {
		value := ""
		n := 0
		for _, key := range keys {
			if v, ok := r.Raw[key]; ok {
				value = strings.TrimSpace(v)
				n++
			}
		}
		// XLSX stores only actual cells. Its existing parser treats an omitted
		// cell as an empty value; read that semantics without filling Raw keys.
		return value, n == 1 || (parser == "wechat-xlsx" && n == 0 && len(keys) == 1)
	}
	check := func(value string, keys ...string) bool { actual, ok := get(keys...); return ok && actual == value }
	if !check(f.Date, "交易时间") || !check(f.Type, "收/支") || !check(f.Merchant, "交易对方") || f.Currency != "CNY" {
		return false
	}
	merchantKey := "商家订单号"
	if format == "alipay" {
		_, payment := get("收/付款方式")
		_, status := get("交易状态")
		_, merchant := get(merchantKey)
		if f.Source != "支付宝" || !payment || !status || !merchant || !check(f.Amount, "金额", "金额(元)") || !check(f.Category, "交易分类", "交易来源") || !check(f.Note, "商品说明", "商品名称") || !check(f.ExternalID, "交易订单号") {
			return false
		}
	} else {
		merchantKey = "商户单号"
		amount, ok := get("金额(元)")
		amount = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(amount, "¥"), "￥"))
		_, payment := get("支付方式")
		_, status := get("当前状态")
		_, merchant := get(merchantKey)
		if f.Source != "微信" || !payment || !status || !merchant || !ok || f.Amount != amount || !check(f.Category, "交易类型") || !check(f.Note, "商品") || !check(f.ExternalID, "交易单号") {
			return false
		}
	}
	expected := map[string]ingestion.Identifier{}
	present := func(v string) bool { return v != "" && v != "/" && v != "-" }
	if present(f.ExternalID) {
		expected[format+"_payment_trace"] = ingestion.Identifier{Kind: format + "_payment_trace", Scope: encode([]string{format, f.Account}), Value: f.ExternalID}
	}
	merchant := strings.TrimSpace(r.Raw[merchantKey])
	if present(merchant) {
		expected["merchant_order"] = ingestion.Identifier{Kind: "merchant_order", Scope: encode([]string{format, f.Account, f.Merchant}), Value: merchant}
	}
	if len(expected) != len(r.Identifiers) {
		return false
	}
	for _, id := range r.Identifiers {
		want, ok := expected[id.Kind]
		if !ok || want.Value != id.Value {
			return false
		}
		var a, b []string
		if json.Unmarshal([]byte(id.Scope), &a) != nil || json.Unmarshal([]byte(want.Scope), &b) != nil || !reflect.DeepEqual(a, b) {
			return false
		}
		delete(expected, id.Kind)
	}
	return len(expected) == 0
}

// The commit-time source link is a frozen financial projection; live transaction
// corrections/refunds/annotations must never be copied into source evidence.
func nativeEvidenceCommittedRows(tx *sql.Tx, ledgerID string, p Preview) error {
	for _, row := range p.Rows {
		var raw string
		err := tx.QueryRow(`SELECT sr.normalized_json FROM source_records sr JOIN imports i ON i.id=sr.import_id WHERE i.ledger_id=? AND i.id=? AND sr.line=?`, ledgerID, p.ID, row.Line).Scan(&raw)
		if err == sql.ErrNoRows {
			return nativePreviewEvidenceProblem()
		}
		if err != nil {
			return err
		}
		var frozen Transaction
		if !nativeEvidenceDecode(raw, &frozen) || frozen != row.Record {
			return nativePreviewEvidenceProblem()
		}
	}
	return nil
}

func nativeEvidenceTimestamp(value string) bool {
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

// No event means committed only after committed_at was observed in this same
// transaction. Damaged/mismatched events never acquire the default state.
func nativeEvidenceUndoState(tx *sql.Tx, ledgerID, id string, committed bool) (string, error) {
	var raw, operation string
	var version int
	err := tx.QueryRow(`SELECT payload_json,operation,payload_version FROM change_log WHERE ledger_id=? AND entity_id=? AND entity_type='import' AND operation IN ('import_undo','import_restore') ORDER BY sequence DESC LIMIT 1`, ledgerID, id).Scan(&raw, &operation, &version)
	if err == sql.ErrNoRows {
		if committed {
			return "committed", nil
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var state importUndoState
	if !committed || version != 1 || !nativeEvidenceDecode(raw, &state) || state.Changes == nil || (operation == "import_undo" && state.State != "undone") || (operation == "import_restore" && state.State != "committed") {
		return "", nativePreviewEvidenceProblem()
	}
	for id, change := range state.Changes {
		if id == "" || change.Version < 1 {
			return "", nativePreviewEvidenceProblem()
		}
		if state.State == "undone" {
			if _, err := time.Parse(time.RFC3339Nano, change.DeletedAt); err != nil {
				return "", nativePreviewEvidenceProblem()
			}
		} else if change.DeletedAt != "" {
			return "", nativePreviewEvidenceProblem()
		}
	}
	return state.State, nil
}
