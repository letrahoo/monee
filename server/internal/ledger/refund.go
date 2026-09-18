package ledger

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

type RefundInput struct {
	Version  int64  `json:"version"`
	Date     string `json:"date"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Account  string `json:"account"`
	Note     string `json:"note"`
}

type RefundSummary struct {
	Original       Transaction   `json:"original"`
	RefundedMinor  string        `json:"refundedMinor"`
	RemainingMinor string        `json:"remainingMinor"`
	Refunds        []Transaction `json:"refunds"`
}

func categoryKind(t Transaction) string {
	if t.Type == "refund" {
		return "expense"
	}
	return t.Type
}

func (s *Store) refundTotal(q queryer, original string) (int64, error) {
	var total int64
	err := q.QueryRow("SELECT COALESCE(SUM(amount_minor),0) FROM transactions WHERE ledger_id=? AND refund_of=? AND kind='refund' AND deleted_at IS NULL", s.ledgerID, original).Scan(&total)
	return total, err
}

// Refunds accepts an original expense or a refund ID, making the original link
// navigable from either side. Authorization is checked before looking up IDs.
func (s *Store) Refunds(id string) (RefundSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := RefundSummary{Refunds: []Transaction{}}
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = s.authorize(tx, false); err != nil {
		return out, err
	}
	original, err := s.correctionRecord(tx, id)
	if err != nil {
		return out, err
	}
	if original.Type == "refund" {
		original, err = s.correctionRecord(tx, original.RefundOf)
		if err != nil {
			return out, err
		}
	}
	if original.Type != "expense" {
		return out, problem("invalid", "只有消费支出可以关联退款")
	}
	out.Original = original
	total, err := s.refundTotal(tx, original.ID)
	if err != nil {
		return out, err
	}
	amount, err := strconv.ParseInt(original.AmountMinor, 10, 64)
	if err != nil {
		return out, err
	}
	out.RefundedMinor = strconv.FormatInt(total, 10)
	out.RemainingMinor = strconv.FormatInt(-amount-total, 10)
	rows, err := tx.Query("SELECT "+transactionColumns+" FROM transactions WHERE ledger_id=? AND refund_of=? AND deleted_at IS NULL ORDER BY occurred_on,created_at,id", s.ledgerID, original.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return out, err
		}
		out.Refunds = append(out.Refunds, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateRefund(id string, in RefundInput, key string) (Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Transaction
	if len(key) < 16 || len(key) > 128 || strings.ContainsAny(key, "\x00\r\n") {
		return out, problem("invalid", "缺少有效的幂等请求标识")
	}
	// Normalize caller-controlled fields independently of mutable original data,
	// so a lost-response retry still succeeds after the original version changes.
	n, err := normalize(Input{Date: in.Date, Type: "income", Amount: in.Amount, Currency: in.Currency, Merchant: "退款", Category: "退款", Source: "手动退款", Account: in.Account, Note: in.Note})
	if err != nil {
		return out, err
	}
	requestHash := hash(encode([]any{"refund-v1", id, in.Version, n}))
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = s.authorize(tx, true); err != nil {
		return out, err
	}
	var previousHash, previousJSON string
	err = tx.QueryRow("SELECT request_hash,response_json FROM idempotency WHERE ledger_id=? AND key=?", s.ledgerID, key).Scan(&previousHash, &previousJSON)
	if err == nil {
		if previousHash != requestHash {
			return out, problem("conflict", "该请求标识已用于另一笔数据")
		}
		err = json.Unmarshal([]byte(previousJSON), &out)
		return out, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	original, err := s.correctionRecord(tx, id)
	if err != nil {
		return out, err
	}
	if original.Type != "expense" {
		return out, problem("invalid", "只有消费支出可以关联退款")
	}
	if in.Version != original.Version {
		return out, problem("conflict", "原消费或退款记录已变化，请刷新后重试")
	}
	if n.Date < original.Date {
		return out, problem("invalid", "退款日期不能早于消费日期")
	}
	if n.Currency != original.Currency {
		return out, problem("invalid", "退款币种必须与原消费一致")
	}
	total, err := s.refundTotal(tx, id)
	if err != nil {
		return out, err
	}
	amount, _ := strconv.ParseInt(n.AmountMinor, 10, 64)
	originalAmount, err := strconv.ParseInt(original.AmountMinor, 10, 64)
	if err != nil {
		return out, err
	}
	if originalAmount >= 0 || total < 0 || amount > -originalAmount-total {
		return out, problem("invalid", "累计退款不能超过原消费金额")
	}
	out = n
	out.ID, out.Type, out.RefundOf = newID(), "refund", id
	out.Merchant, out.Category = original.Merchant, original.Category
	if err = s.insert(tx, out); err != nil {
		return out, err
	}
	// A link change advances the original's version too. Stale refund forms and
	// stale correction/undo previews must not silently overwrite this relation.
	result, err := tx.Exec("UPDATE transactions SET version=version+1,updated_at=? WHERE ledger_id=? AND id=? AND version=? AND deleted_at IS NULL", now(), s.ledgerID, id, original.Version)
	if err != nil {
		return out, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return out, err
	}
	if affected != 1 {
		return out, problem("conflict", "原消费已变化，请刷新后重试")
	}
	after := original
	after.Version++
	if _, err = tx.Exec("INSERT INTO change_log(change_id,ledger_id,device_id,entity_id,entity_type,base_version,entity_version,operation,payload_version,payload_json,created_at,actor_id) VALUES(?,?,?,?,?,?,?,?,1,?,?,?)", newID(), s.ledgerID, s.deviceID, id, "transaction", original.Version, after.Version, "refund_linked", encode(map[string]any{"before": original, "after": after, "refundId": out.ID}), now(), nullActor(s.actorID)); err != nil {
		return out, err
	}
	if _, err = tx.Exec("UPDATE ledgers SET version=version+1 WHERE id=?", s.ledgerID); err != nil {
		return out, err
	}
	if _, err = tx.Exec("INSERT INTO idempotency VALUES(?,?,?,?,?)", s.ledgerID, key, requestHash, encode(out), now()); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) validateRefundCorrection(q queryer, id string, after Transaction) error {
	total, err := s.refundTotal(q, id)
	if err != nil {
		return err
	}
	if total == 0 {
		return nil
	}
	amount, err := strconv.ParseInt(after.AmountMinor, 10, 64)
	if err != nil {
		return err
	}
	if after.Type != "expense" || -amount < total {
		return problem("invalid", "已有退款关联，不能改为收入或将消费金额改到已退款金额以下")
	}
	return nil
}
