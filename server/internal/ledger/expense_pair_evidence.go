package ledger

import (
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

func expensePairCanonical(raw string, value any) bool {
	return nativeEvidenceDecode(raw, value) && encode(value) == raw
}

func expensePairHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

// Bind the frozen document digest and namespace to its preserved original bytes.
// This validates the stored envelope; it does not reparse or authenticate a bill.
func expensePairEnvelopeValid(raw string, p Preview) bool {
	var envelope struct{ Format, Account, Content string }
	if p.SourceDocument == nil || !expensePairCanonical(raw, &envelope) || envelope.Format != p.SourceDocument.Parser+"-v1" || envelope.Account != p.SourceAccount || !utf8.ValidString(envelope.Account) || strings.TrimSpace(envelope.Account) != envelope.Account || len([]rune(envelope.Account)) < 1 || len([]rune(envelope.Account)) > 80 || len(envelope.Content) > ((ingestion.MaxBytes+2)/3)*4 {
		return false
	}
	original, err := base64.StdEncoding.Strict().DecodeString(envelope.Content)
	return err == nil && len(original) > 0 && len(original) <= ingestion.MaxBytes && base64.StdEncoding.EncodeToString(original) == envelope.Content && hash(string(original)) == p.SourceDocument.ContentHash
}

// Read actual current facts from one authorized caller-owned transaction. The
// owner ledger is joined from transactions: postings has no ledger_id column.
func (s *Store) expensePairReadMember(tx *sql.Tx, id string) (expensePairFactsDocument, error) {
	var result expensePairFactsDocument
	var t Transaction
	var rawAmount any
	var amountType string
	var deleted sql.NullString
	var refunded, corrected bool
	err := tx.QueryRow(`SELECT id,occurred_on,kind,amount_minor,typeof(amount_minor),currency,merchant,category,source,account,external_id,note,version,COALESCE(refund_of,''),COALESCE(to_account,''),deleted_at,
	EXISTS(SELECT 1 FROM transactions r WHERE r.ledger_id=t.ledger_id AND r.refund_of=t.id AND r.kind='refund'),
	EXISTS(SELECT 1 FROM change_log c WHERE c.ledger_id=t.ledger_id AND c.entity_type='transaction' AND c.entity_id=t.id AND c.operation='correct')
	FROM transactions t WHERE t.ledger_id=? AND t.id=?`, s.ledgerID, id).Scan(&t.ID, &t.Date, &t.Type, &rawAmount, &amountType, &t.Currency, &t.Merchant, &t.Category, &t.Source, &t.Account, &t.ExternalID, &t.Note, &t.Version, &t.RefundOf, &t.ToAccount, &deleted, &refunded, &corrected)
	if err == sql.ErrNoRows {
		return result, problem("not_found", "找不到此账本的交易")
	}
	if err != nil {
		return result, expensePairUnavailable()
	}
	if deleted.Valid || refunded || corrected || t.Type != "expense" || t.Currency != "CNY" || t.RefundOf != "" || t.ToAccount != "" {
		return result, expensePairUnsupported()
	}
	if amount, ok := rawAmount.(int64); ok {
		t.AmountMinor = strconv.FormatInt(amount, 10)
	}
	input := associationExpenseAccountingInput{LedgerID: s.ledgerID, TransactionID: t.ID, Version: t.Version, Kind: t.Type, Currency: t.Currency, AmountMinor: t.AmountMinor, AmountStorageClass: amountType, AmountRaw: rawAmount}
	rows, err := tx.Query(`SELECT p.id,t.ledger_id,p.transaction_id,p.account_id,COALESCE(a.ledger_id,''),COALESCE(a.kind,''),p.currency,typeof(p.amount_minor),p.amount_minor
	FROM postings p JOIN transactions t ON t.id=p.transaction_id LEFT JOIN accounts a ON a.id=p.account_id WHERE t.ledger_id=? AND t.id=? ORDER BY p.id`, s.ledgerID, id)
	if err != nil {
		return result, expensePairUnavailable()
	}
	for rows.Next() {
		var p associationExpensePostingInput
		if err = rows.Scan(&p.ID, &p.TransactionLedgerID, &p.TransactionID, &p.AccountID, &p.AccountLedgerID, &p.AccountKind, &p.Currency, &p.AmountStorageClass, &p.AmountRaw); err != nil {
			rows.Close()
			return result, expensePairUnavailable()
		}
		input.Postings = append(input.Postings, p)
	}
	rowErr, closeErr := rows.Err(), rows.Close()
	if rowErr != nil || closeErr != nil {
		return result, expensePairUnavailable()
	}
	facts, err := inspectAssociationExpenseAccountingFacts(s.ledgerID, input)
	if err != nil {
		return result, expensePairUnavailable()
	}
	return expensePairFactsDocument{ContractVersion: 1, Transaction: t, Accounting: expensePairAccountingDocument(facts)}, nil
}

func expensePairInput(f expensePairFactsDocument) associationExpensePairInput {
	a := f.Accounting
	return associationExpensePairInput{Date: f.Transaction.Date, Merchant: f.Transaction.Merchant, Accounting: associationExpenseAccountingInput{
		LedgerID: a.LedgerID, TransactionID: a.TransactionID, Version: a.Version, Kind: f.Transaction.Type, Currency: f.Transaction.Currency, AmountMinor: f.Transaction.AmountMinor, AmountStorageClass: "integer", AmountRaw: a.AmountMinor,
		Postings: []associationExpensePostingInput{
			{ID: a.FundPostingID, TransactionLedgerID: a.LedgerID, TransactionID: a.TransactionID, AccountID: a.FundAccountID, AccountLedgerID: a.LedgerID, AccountKind: "clearing", Currency: f.Transaction.Currency, AmountStorageClass: "integer", AmountRaw: a.AmountMinor},
			{ID: a.ExpensePostingID, TransactionLedgerID: a.LedgerID, TransactionID: a.TransactionID, AccountID: a.ExpenseAccountID, AccountLedgerID: a.LedgerID, AccountKind: "expense", Currency: f.Transaction.Currency, AmountStorageClass: "integer", AmountRaw: -a.AmountMinor},
		},
	}}
}

func expensePairFactsValid(scope string, f expensePairFactsDocument) bool {
	a, t := f.Accounting, f.Transaction
	// Bound before reconstructing the opposite posting; never negate MinInt64.
	if f.ContractVersion != 1 || a.AmountMinor < -1_000_000_000_000 || a.AmountMinor >= 0 || t.ID != a.TransactionID || t.Version != a.Version || t.RefundOf != "" || t.ToAccount != "" || !expensePairTransactionText(t) {
		return false
	}
	facts, err := inspectAssociationExpenseAccountingFacts(scope, expensePairInput(f).Accounting)
	if err != nil || facts != associationExpenseAccountingFacts(a) || !associationPairDate(t.Date) || !associationPairMerchant(t.Merchant) {
		return false
	}
	var roundTrip expensePairFactsDocument
	return expensePairCanonical(encode(f), &roundTrip) && roundTrip == f
}

func expensePairTransactionText(t Transaction) bool {
	for _, value := range []string{t.Category, t.Source, t.Account, t.ExternalID} {
		if value == "" || !utf8.ValidString(value) || strings.TrimSpace(value) != value || len([]rune(value)) > 200 || strings.ContainsAny(value, "\x00\r\n") {
			return false
		}
	}
	return utf8.ValidString(t.Note) && strings.TrimSpace(t.Note) == t.Note && len([]rune(t.Note)) <= 1000 && !strings.ContainsRune(t.Note, 0)
}

// Category/note annotations and their versions may precede confirmation. Source
// identity, date, amount, direction and funding display must remain original.
func expensePairOriginalMatchesCurrent(original, current Transaction) bool {
	original.Version, current.Version = 0, 0
	original.Category, current.Category = "", ""
	original.Note, current.Note = "", ""
	return original == current
}

func (s *Store) expensePairOccurrences(tx *sql.Tx, transactionID string) ([]expensePairSourceOccurrence, string, error) {
	rows, err := tx.Query(`SELECT sr.id,sr.import_id,sr.line,sr.disposition,sr.normalized_json,i.content_hash,i.parser_version,i.committed_at
	FROM source_records sr JOIN imports i ON i.id=sr.import_id WHERE i.ledger_id=? AND sr.transaction_id=? ORDER BY sr.id`, s.ledgerID, transactionID)
	if err != nil {
		return nil, "", expensePairUnavailable()
	}
	out := []expensePairSourceOccurrence{}
	addedID := ""
	for rows.Next() {
		var o expensePairSourceOccurrence
		var normalized string
		var committed sql.NullString
		if err = rows.Scan(&o.SourceRecordID, &o.ImportID, &o.Line, &o.Disposition, &normalized, &o.ImportContentHash, &o.EnvelopeParserVersion, &committed); err != nil {
			rows.Close()
			return nil, "", expensePairUnavailable()
		}
		var original Transaction
		if !associationAccountingID(o.SourceRecordID) || !associationAccountingID(o.ImportID) || o.Line < 1 || !committed.Valid || !nativeEvidenceTimestamp(committed.String) || !expensePairHash(o.ImportContentHash) || (o.EnvelopeParserVersion != 1 && o.EnvelopeParserVersion != 2) || !expensePairCanonical(normalized, &original) || original.ID != transactionID || original.Version < 1 {
			rows.Close()
			return nil, "", expensePairUnavailable()
		}
		if o.Disposition == "added" {
			if addedID != "" {
				rows.Close()
				return nil, "", expensePairUnsupported()
			}
			addedID = o.SourceRecordID
		} else if o.Disposition != "duplicate" {
			rows.Close()
			return nil, "", expensePairUnavailable()
		}
		o.NormalizedHash, o.CommittedAt = hash(normalized), committed.String
		out = append(out, o)
	}
	rowErr, closeErr := rows.Err(), rows.Close()
	if rowErr != nil || closeErr != nil {
		return nil, "", expensePairUnavailable()
	}
	if addedID == "" {
		return nil, "", expensePairUnsupported()
	}
	return out, addedID, nil
}

func (s *Store) expensePairReadAnchor(tx *sql.Tx, sourceID, transactionID string, requireCommitted bool) (expensePairNativeAnchor, error) {
	var a expensePairNativeAnchor
	var normalized, preview, envelope, disposition string
	var committed, resultJSON sql.NullString
	err := tx.QueryRow(`SELECT sr.id,sr.import_id,sr.line,sr.disposition,sr.normalized_json,i.content_hash,i.parser_version,i.committed_at,i.preview_json,i.raw_csv,i.result_json
	FROM source_records sr JOIN imports i ON i.id=sr.import_id WHERE i.ledger_id=? AND sr.id=? AND sr.transaction_id=?`, s.ledgerID, sourceID, transactionID).Scan(&a.SourceRecordID, &a.ImportID, &a.Line, &disposition, &normalized, &a.ImportContentHash, &a.EnvelopeParserVersion, &committed, &preview, &envelope, &resultJSON)
	if err != nil {
		return a, expensePairUnavailable()
	}
	if disposition != "added" || a.EnvelopeParserVersion != 2 {
		return expensePairNativeAnchor{}, expensePairUnsupported()
	}
	var p Preview
	if !committed.Valid || !nativeEvidenceTimestamp(committed.String) || !expensePairHash(a.ImportContentHash) || hash(envelope) != a.ImportContentHash || !expensePairCanonical(preview, &p) || p.ID != a.ImportID || !nativeEvidenceValid(p) || !expensePairEnvelopeValid(envelope, p) || !expensePairCanonical(normalized, &a.Original) || a.Original.ID != transactionID || a.Original.Version != 1 || a.Original.Type != "expense" || a.Original.RefundOf != "" || a.Original.ToAccount != "" {
		return expensePairNativeAnchor{}, expensePairUnavailable()
	}
	var result CommitResult
	if p.AlreadyCommitted || p.Undone || p.LedgerVersion < 0 || len(p.Errors) != 0 || !resultJSON.Valid || !expensePairCanonical(resultJSON.String, &result) || result.ImportID != a.ImportID || result.Pending != len(p.Pending) {
		return expensePairNativeAnchor{}, expensePairUnavailable()
	}
	added, skipped := 0, 0
	for _, row := range p.Rows {
		if row.Status == "duplicate" {
			skipped++
		} else {
			added++
		}
	}
	if result.Added != added || result.Skipped != skipped {
		return expensePairNativeAnchor{}, expensePairUnavailable()
	}
	state, err := nativeEvidenceUndoState(tx, s.ledgerID, a.ImportID, true)
	if err != nil {
		return expensePairNativeAnchor{}, expensePairUnavailable()
	}
	if requireCommitted && state != "committed" {
		return expensePairNativeAnchor{}, expensePairUnsupported()
	}
	if err = nativeEvidenceCommittedRows(tx, s.ledgerID, p); err != nil {
		return expensePairNativeAnchor{}, expensePairUnavailable()
	}
	found := false
	for _, row := range p.Rows {
		if row.Line == a.Line {
			if row.Record != a.Original || (row.Status != "new" && row.Status != "similar") {
				return expensePairNativeAnchor{}, expensePairUnavailable()
			}
			found = true
		}
	}
	if !found {
		return expensePairNativeAnchor{}, expensePairUnavailable()
	}
	var record ingestion.Record
	for _, r := range p.SourceDocument.Records {
		if r.Line == a.Line {
			record = r
		}
	}
	if !s.expensePairNativeOriginal(p, record, a.Original) {
		return expensePairNativeAnchor{}, expensePairUnsupported()
	}
	d := p.SourceDocument
	a.Ordinal, a.DocumentContentHash, a.DocumentParser, a.DocumentParserVersion = record.Ordinal, d.ContentHash, d.Parser, d.ParserVersion
	a.PreviewHash, a.NormalizedHash, a.CommittedAt = hash(preview), hash(normalized), committed.String
	return a, nil
}

// Reconcile the native extraction with its committed normalized row without
// nativeRows (which generates IDs), reparsing bytes, or rewriting either source.
func (s *Store) expensePairNativeOriginal(p Preview, r ingestion.Record, original Transaction) bool {
	f := r.Fields
	if !expensePairTransactionText(original) {
		return false
	}
	amount, err := parseAmount(f.Amount)
	if err != nil || original.AmountMinor != strconv.FormatInt(-amount, 10) || f.Type != "支出" || f.Currency != "CNY" || original.Source != f.Source || original.Merchant != f.Merchant || original.Note != f.Note {
		return false
	}
	date, err := time.Parse("2006-01-02 15:04:05", f.Date)
	if err != nil || original.Date != date.Format("2006-01-02") {
		return false
	}
	key := ""
	count := 0
	for _, identifier := range r.Identifiers {
		if identifier.Kind == p.Format+"_payment_trace" {
			count++
			key, err = identifier.MatchKey(s.ledgerID)
			if err != nil {
				return false
			}
		}
	}
	if count != 1 || key == "" {
		return false
	}
	prefix, payment, category := "alipay-native-v1:", strings.TrimSpace(r.Raw["收/付款方式"]), f.Category
	if p.Format == "wechat" {
		prefix, payment, category = "wechat-native-v1:", strings.TrimSpace(r.Raw["支付方式"]), "未分类"
		if strings.TrimSpace(r.Raw["当前状态"]) != "支付成功" || f.Category != "商户消费" {
			return false
		}
	} else {
		if strings.TrimSpace(r.Raw["交易状态"]) != "交易成功" {
			return false
		}
		known := false
		for _, name := range []string{"餐饮美食", "日用百货", "交通出行", "服饰装扮", "数码电器", "家居家装", "运动户外", "美容美发", "母婴亲子", "宠物", "文化休闲", "生活服务", "教育培训", "医疗健康", "酒店旅游", "住房物业", "商业服务", "公益捐赠"} {
			if f.Category == name {
				known = true
			}
		}
		if !known {
			return false
		}
	}
	business := f.Category + " " + f.Note + " " + f.Merchant
	for _, word := range []string{"退款", "转账", "还款", "充值", "提现", "理财", "基金", "借款", "借呗", "余额宝", "红包", "代付", "分期", "外币", "美元", "港币", "转入", "转出", "信用借还", "借还", "收款", "金融", "利息", "手续费"} {
		if strings.Contains(business, word) {
			return false
		}
	}
	for _, other := range p.SourceDocument.Records {
		if other.Fields.Merchant == f.Merchant && (strings.Contains(other.Raw["交易状态"]+other.Raw["当前状态"], "退") || strings.Contains(other.Fields.Category, "退款")) {
			return false
		}
	}
	if payment == "" || payment == "/" || payment == "-" || payment == "待核实资金账户" {
		// A shared fallback account is not complete original payment evidence.
		return false
	}
	return original.ExternalID == prefix+key && original.Account == payment && original.Category == category
}

func (s *Store) expensePairReadEvidence(tx *sql.Tx, current Transaction) (expensePairEvidenceDocument, error) {
	occurrences, added, err := s.expensePairOccurrences(tx, current.ID)
	if err != nil {
		return expensePairEvidenceDocument{}, err
	}
	anchor, err := s.expensePairReadAnchor(tx, added, current.ID, true)
	if err != nil {
		return expensePairEvidenceDocument{}, err
	}
	if !expensePairOriginalMatchesCurrent(anchor.Original, current) {
		return expensePairEvidenceDocument{}, expensePairUnsupported()
	}
	for _, occurrence := range occurrences {
		var raw string
		var original Transaction
		if err = tx.QueryRow(`SELECT sr.normalized_json FROM source_records sr JOIN imports i ON i.id=sr.import_id WHERE i.ledger_id=? AND sr.id=? AND sr.transaction_id=?`, s.ledgerID, occurrence.SourceRecordID, current.ID).Scan(&raw); err != nil || !expensePairCanonical(raw, &original) || !expensePairOriginalMatchesCurrent(anchor.Original, original) {
			return expensePairEvidenceDocument{}, expensePairUnavailable()
		}
	}
	return expensePairEvidenceDocument{ContractVersion: 1, Anchor: anchor, Occurrences: occurrences}, nil
}

func (s *Store) expensePairVerifyHistoricalEvidence(tx *sql.Tx, member expensePairMemberSnapshot) error {
	e := member.Evidence
	if e.ContractVersion != 1 || len(e.Occurrences) == 0 || !expensePairOriginalMatchesCurrent(e.Anchor.Original, member.Facts.Transaction) {
		return expensePairUnavailable()
	}
	anchor, err := s.expensePairReadAnchor(tx, e.Anchor.SourceRecordID, member.TransactionID, false)
	if err != nil || anchor != e.Anchor {
		return expensePairUnavailable()
	}
	added, previous := 0, ""
	for _, o := range e.Occurrences {
		if o.SourceRecordID <= previous {
			return expensePairUnavailable()
		}
		previous = o.SourceRecordID
		var found expensePairSourceOccurrence
		var normalized string
		var committed sql.NullString
		err = tx.QueryRow(`SELECT sr.id,sr.import_id,sr.line,sr.disposition,sr.normalized_json,i.content_hash,i.parser_version,i.committed_at
		FROM source_records sr JOIN imports i ON i.id=sr.import_id WHERE i.ledger_id=? AND sr.id=? AND sr.transaction_id=?`, s.ledgerID, o.SourceRecordID, member.TransactionID).Scan(&found.SourceRecordID, &found.ImportID, &found.Line, &found.Disposition, &normalized, &found.ImportContentHash, &found.EnvelopeParserVersion, &committed)
		if err != nil || !committed.Valid {
			return expensePairUnavailable()
		}
		var original Transaction
		if !expensePairCanonical(normalized, &original) || original.ID != member.TransactionID || !expensePairOriginalMatchesCurrent(e.Anchor.Original, original) {
			return expensePairUnavailable()
		}
		found.NormalizedHash, found.CommittedAt = hash(normalized), committed.String
		if found != o || o.Line < 1 || !associationAccountingID(o.SourceRecordID) || !associationAccountingID(o.ImportID) || !expensePairHash(o.ImportContentHash) || !nativeEvidenceTimestamp(o.CommittedAt) || (o.EnvelopeParserVersion != 1 && o.EnvelopeParserVersion != 2) {
			return expensePairUnavailable()
		}
		if o.Disposition == "added" {
			added++
			if o.SourceRecordID != e.Anchor.SourceRecordID {
				return expensePairUnavailable()
			}
		} else if o.Disposition != "duplicate" {
			return expensePairUnavailable()
		}
	}
	if added != 1 {
		return expensePairUnavailable()
	}
	return nil
}

func expensePairMembersEqual(a, b []expensePairMemberSnapshot) bool { return reflect.DeepEqual(a, b) }
