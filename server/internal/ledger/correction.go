package ledger

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// AmountMinor is a positive magnitude; Type determines the transaction sign.
type CorrectionInput struct {
	Version     int64  `json:"version"`
	Type        string `json:"type"`
	AmountMinor string `json:"amountMinor"`
	Reason      string `json:"reason"`
}

type CorrectionPreview struct {
	Before            Transaction `json:"before"`
	After             Transaction `json:"after"`
	IncomeDeltaMinor  string      `json:"incomeDeltaMinor"`
	ExpenseDeltaMinor string      `json:"expenseDeltaMinor"`
	NetDeltaMinor     string      `json:"netDeltaMinor"`
}

type CorrectionRevision struct {
	Before    Transaction `json:"before"`
	After     Transaction `json:"after"`
	Reason    string      `json:"reason"`
	CreatedAt string      `json:"createdAt"`
}

var correctionAmountPattern = regexp.MustCompile(`^[1-9][0-9]{0,12}$`)

func correctionPreview(before Transaction, in CorrectionInput) (CorrectionPreview, error) {
	out := CorrectionPreview{Before: before, After: before}
	if before.Version != in.Version {
		return out, problem("conflict", "这笔交易已被修改，请重新打开详情")
	}
	if (before.Type != "income" && before.Type != "expense") || (in.Type != "income" && in.Type != "expense") {
		return out, problem("invalid", "当前只能更正普通收入或支出，退款、转账和还款须单独处理")
	}
	if !correctionAmountPattern.MatchString(in.AmountMinor) {
		return out, problem("invalid", "金额须为大于零的整数分值")
	}
	amount, err := strconv.ParseInt(in.AmountMinor, 10, 64)
	if err != nil || amount > 1_000_000_000_000 {
		return out, problem("invalid", "金额须大于 0 且不超过 100 亿元")
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" || len([]rune(reason)) > 500 || strings.ContainsRune(reason, 0) {
		return out, problem("invalid", "请填写 1–500 字的更正原因")
	}
	oldAmount, err := strconv.ParseInt(before.AmountMinor, 10, 64)
	if err != nil || oldAmount == 0 || oldAmount < -1_000_000_000_000 || oldAmount > 1_000_000_000_000 || (before.Type == "income") != (oldAmount > 0) {
		return out, problem("invalid", "原交易金额异常，无法更正")
	}
	if in.Type == "expense" {
		amount = -amount
	}
	out.After.Type = in.Type
	out.After.AmountMinor = strconv.FormatInt(amount, 10)
	if out.After.Type != before.Type || out.After.AmountMinor != before.AmountMinor {
		out.After.Version++
	}
	var oldIncome, oldExpense, newIncome, newExpense int64
	if oldAmount > 0 {
		oldIncome = oldAmount
	} else {
		oldExpense = -oldAmount
	}
	if amount > 0 {
		newIncome = amount
	} else {
		newExpense = -amount
	}
	out.IncomeDeltaMinor = strconv.FormatInt(newIncome-oldIncome, 10)
	out.ExpenseDeltaMinor = strconv.FormatInt(newExpense-oldExpense, 10)
	out.NetDeltaMinor = strconv.FormatInt(amount-oldAmount, 10)
	return out, nil
}

func (s *Store) correctionRecord(q queryer, id string) (Transaction, error) {
	before, err := scanRecord(q.QueryRow("SELECT "+transactionColumns+" FROM transactions WHERE ledger_id=? AND id=? AND deleted_at IS NULL", s.ledgerID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return before, problem("not_found", "找不到该交易")
	}
	return before, err
}

// PreviewCorrection never changes transactions, accounts, or the audit log.
func (s *Store) PreviewCorrection(id string, in CorrectionInput) (CorrectionPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.authorize(s.db, true); err != nil {
		return CorrectionPreview{}, err
	}
	before, err := s.correctionRecord(s.db, id)
	if err != nil {
		return CorrectionPreview{}, err
	}
	return correctionPreview(before, in)
}

func (s *Store) Correct(id string, in CorrectionInput) (Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Transaction
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = s.authorize(tx, true); err != nil {
		return out, err
	}
	before, err := s.correctionRecord(tx, id)
	if err != nil {
		return out, err
	}
	preview, err := correctionPreview(before, in)
	if err != nil {
		return out, err
	}
	out = preview.After
	if out == before {
		return out, nil
	}
	// Preserve both posting identities for future synchronization. Ordinary
	// transactions must have exactly one funding and one category posting.
	rows, err := tx.Query(`SELECT p.id,a.kind,p.amount_minor,p.currency,a.ledger_id FROM postings p JOIN accounts a ON a.id=p.account_id WHERE p.transaction_id=?`, id)
	if err != nil {
		return out, err
	}
	var fundID, categoryID string
	var balance int64
	var n int
	invalidPosting := false
	oldAmount, _ := strconv.ParseInt(before.AmountMinor, 10, 64)
	for rows.Next() {
		var postingID, kind, currency, ledgerID string
		var amount int64
		if err = rows.Scan(&postingID, &kind, &amount, &currency, &ledgerID); err != nil {
			rows.Close()
			return out, err
		}
		n++
		invalidPosting = invalidPosting || currency != before.Currency || ledgerID != s.ledgerID
		balance += amount
		switch kind {
		case "clearing":
			fundID = postingID
			invalidPosting = invalidPosting || amount != oldAmount
		case before.Type:
			categoryID = postingID
			invalidPosting = invalidPosting || amount != -oldAmount
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if invalidPosting || n != 2 || balance != 0 || fundID == "" || categoryID == "" {
		return out, problem("unbalanced", "原交易分录异常，更正已取消")
	}
	category, err := s.account(tx, out.Type, out.Category)
	if err != nil {
		return out, err
	}
	amount, _ := strconv.ParseInt(out.AmountMinor, 10, 64) // validated above
	// The fingerprint and identity key remain the immutable imported evidence.
	// Only the similarity index follows the currently effective transaction.
	result, err := tx.Exec("UPDATE transactions SET kind=?,amount_minor=?,similarity_key=?,version=?,updated_at=? WHERE ledger_id=? AND id=? AND version=? AND deleted_at IS NULL", out.Type, amount, similarity(out), out.Version, now(), s.ledgerID, id, before.Version)
	if err != nil {
		return out, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return out, err
	}
	if affected != 1 {
		return out, problem("conflict", "这笔交易已被修改，请重新打开详情")
	}
	if _, err = tx.Exec("UPDATE postings SET amount_minor=? WHERE id=?", amount, fundID); err != nil {
		return out, err
	}
	if _, err = tx.Exec("UPDATE postings SET account_id=?,amount_minor=? WHERE id=?", category, -amount, categoryID); err != nil {
		return out, err
	}
	if _, err = tx.Exec("UPDATE ledgers SET version=version+1 WHERE id=?", s.ledgerID); err != nil {
		return out, err
	}
	revision := CorrectionRevision{Before: before, After: out, Reason: strings.TrimSpace(in.Reason), CreatedAt: now()}
	if _, err = tx.Exec("INSERT INTO change_log(change_id,ledger_id,device_id,entity_id,entity_type,base_version,entity_version,operation,payload_version,payload_json,created_at,actor_id) VALUES(?,?,?,?,?,?,?,?,1,?,?,?)", newID(), s.ledgerID, s.deviceID, id, "transaction", before.Version, out.Version, "correct", encode(revision), revision.CreatedAt, nullActor(s.actorID)); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) CorrectionHistory(id string) ([]CorrectionRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []CorrectionRevision{}
	if err := s.authorize(s.db, false); err != nil {
		return out, err
	}
	var found int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM transactions WHERE ledger_id=? AND id=?", s.ledgerID, id).Scan(&found); err != nil {
		return out, err
	}
	if found == 0 {
		return out, problem("not_found", "找不到该交易")
	}
	rows, err := s.db.Query("SELECT payload_json FROM change_log WHERE ledger_id=? AND entity_id=? AND entity_type='transaction' AND operation='correct' ORDER BY sequence DESC LIMIT 100", s.ledgerID, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var revision CorrectionRevision
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &revision); err != nil {
			return out, err
		}
		out = append(out, revision)
	}
	return out, rows.Err()
}
