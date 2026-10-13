package ledger

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

type AnnotationInput struct {
	Version  int64  `json:"version"`
	Category string `json:"category"`
	Note     string `json:"note"`
}
type AnnotationRevision struct {
	Before    Transaction `json:"before"`
	After     Transaction `json:"after"`
	CreatedAt string      `json:"createdAt"`
}

func (s *Store) Annotate(id string, in AnnotationInput) (Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Transaction
	tx, e := s.db.Begin()
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = s.authorize(tx, true); e != nil {
		return out, e
	}
	before, e := scanRecord(tx.QueryRow("SELECT "+transactionColumns+" FROM transactions WHERE ledger_id=? AND id=? AND deleted_at IS NULL", s.ledgerID, id))
	if errors.Is(e, sql.ErrNoRows) {
		return out, problem("not_found", "找不到该交易")
	}
	if e != nil {
		return out, e
	}
	if before.Version != in.Version {
		return out, problem("conflict", "这笔交易已被修改，请重新打开详情")
	}
	in.Category = strings.TrimSpace(in.Category)
	in.Note = strings.TrimSpace(in.Note)
	if in.Category == "" {
		in.Category = "未分类"
	}
	// Match creation/import limits so editing one field never requires truncating
	// another valid field already saved on the transaction.
	if len([]rune(in.Category)) > 200 || strings.ContainsAny(in.Category, "\x00\r\n") {
		return out, problem("invalid", "分类最多 200 字，不支持换行或控制字符")
	}
	if len([]rune(in.Note)) > 1000 || strings.ContainsRune(in.Note, 0) {
		return out, problem("invalid", "备注最多 1000 字，不支持空字符")
	}
	out = before
	out.Category = in.Category
	out.Note = in.Note
	if before.Type == "transfer" && out.Category != before.Category {
		return out, problem("invalid", "本人转账不参与收支分类；只能修改备注")
	}
	if out.Category != before.Category {
		total, err := s.refundTotal(tx, id)
		if err != nil {
			return out, err
		}
		if before.Type == "refund" || total > 0 {
			return out, problem("invalid", "已有退款关联，暂不支持单独修改关联交易分类；备注仍可修改")
		}
	}
	if out == before {
		return out, nil
	}
	out.Version++
	// Keep the source fingerprint immutable so a user annotation never turns a
	// repeated source import into a conflicting new payment.
	if _, e = tx.Exec("UPDATE transactions SET category=?,note=?,version=?,updated_at=? WHERE ledger_id=? AND id=? AND version=?", out.Category, out.Note, out.Version, now(), s.ledgerID, id, in.Version); e != nil {
		return out, e
	}
	if out.Type != "transfer" {
		account, err := s.account(tx, categoryKind(out), out.Category)
		if err != nil {
			return out, err
		}
		if _, e = tx.Exec("UPDATE postings SET account_id=? WHERE transaction_id=? AND account_id IN (SELECT id FROM accounts WHERE ledger_id=? AND kind=?)", account, id, s.ledgerID, categoryKind(out)); e != nil {
			return out, e
		}
	}
	if _, e = tx.Exec("UPDATE ledgers SET version=version+1 WHERE id=?", s.ledgerID); e != nil {
		return out, e
	}
	revision := AnnotationRevision{Before: before, After: out, CreatedAt: now()}
	if _, e = tx.Exec("INSERT INTO change_log(change_id,ledger_id,device_id,entity_id,entity_type,base_version,entity_version,operation,payload_version,payload_json,created_at,actor_id) VALUES(?,?,?,?,?,?,?,?,1,?,?,?)", newID(), s.ledgerID, s.deviceID, id, "transaction", before.Version, out.Version, "annotate", encode(revision), revision.CreatedAt, nullActor(s.actorID)); e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func (s *Store) AnnotationHistory(id string) ([]AnnotationRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []AnnotationRevision{}
	if e := s.authorize(s.db, false); e != nil {
		return out, e
	}
	var found int
	if e := s.db.QueryRow("SELECT COUNT(*) FROM transactions WHERE ledger_id=? AND id=?", s.ledgerID, id).Scan(&found); e != nil {
		return out, e
	}
	if found == 0 {
		return out, problem("not_found", "找不到该交易")
	}
	rows, e := s.db.Query("SELECT payload_json FROM change_log WHERE ledger_id=? AND entity_id=? AND entity_type='transaction' AND operation='annotate' ORDER BY sequence DESC LIMIT 100", s.ledgerID, id)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var r AnnotationRevision
		if e = rows.Scan(&raw); e != nil {
			return out, e
		}
		if e = json.Unmarshal([]byte(raw), &r); e != nil {
			return out, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
