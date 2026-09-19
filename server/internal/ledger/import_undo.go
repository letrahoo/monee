package ledger

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
)

// ImportUndoPreview is calculated from the current ledger, never client supplied IDs.
// Amounts are positive minor-unit totals of transactions affected by this action.
type ImportUndoPreview struct {
	ImportID       string          `json:"importId"`
	LedgerVersion  int64           `json:"ledgerVersion"`
	State          string          `json:"state"`
	Action         string          `json:"action"`
	ChangeCount    int             `json:"changeCount"`
	PreservedCount int             `json:"preservedCount"`
	BlockedCount   int             `json:"blockedCount"`
	IncomeMinor    string          `json:"incomeMinor"`
	ExpenseMinor   string          `json:"expenseMinor"`
	Rows           []ImportUndoRow `json:"rows"`
	AlreadyApplied bool            `json:"alreadyApplied"`
}
type ImportUndoRow struct {
	TransactionID string `json:"transactionId"`
	Date          string `json:"date"`
	Merchant      string `json:"merchant"`
	Type          string `json:"type"`
	AmountMinor   string `json:"amountMinor"`
	Action        string `json:"action"`
	Reason        string `json:"reason"`
}
type importUndoChange struct {
	Version   int64  `json:"version"`
	DeletedAt string `json:"deletedAt,omitempty"`
}
type importUndoState struct {
	State   string                      `json:"state"`
	Changes map[string]importUndoChange `json:"changes"`
}

func (s *Store) importUndoState(tx queryer, id string) (importUndoState, error) {
	out := importUndoState{State: "committed", Changes: map[string]importUndoChange{}}
	var raw string
	err := tx.QueryRow(`SELECT payload_json FROM change_log WHERE ledger_id=? AND entity_id=? AND entity_type='import' AND operation IN ('import_undo','import_restore') ORDER BY sequence DESC LIMIT 1`, s.ledgerID, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal([]byte(raw), &out)
	return out, err
}
func (s *Store) PreviewImportUndo(id string, restore bool) (ImportUndoPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return ImportUndoPreview{}, e
	}
	defer tx.Rollback()
	if e = s.authorize(tx, false); e != nil {
		return ImportUndoPreview{}, e
	}
	out, _, e := s.previewImportUndo(tx, id, restore)
	return out, e
}
func (s *Store) previewImportUndo(tx *sql.Tx, id string, restore bool) (ImportUndoPreview, importUndoState, error) {
	out := ImportUndoPreview{ImportID: id, Action: "undo", Rows: []ImportUndoRow{}, IncomeMinor: "0", ExpenseMinor: "0"}
	if restore {
		out.Action = "restore"
	}
	var committed sql.NullString
	e := tx.QueryRow("SELECT committed_at FROM imports WHERE ledger_id=? AND id=?", s.ledgerID, id).Scan(&committed)
	if errors.Is(e, sql.ErrNoRows) {
		return out, importUndoState{}, problem("not_found", "找不到此账本的导入记录")
	}
	if e != nil {
		return out, importUndoState{}, e
	}
	if !committed.Valid {
		return out, importUndoState{}, problem("invalid", "本批尚未入账，无需撤销")
	}
	state, e := s.importUndoState(tx, id)
	if e != nil {
		return out, state, e
	}
	out.State = state.State
	if e = tx.QueryRow("SELECT version FROM ledgers WHERE id=?", s.ledgerID).Scan(&out.LedgerVersion); e != nil {
		return out, state, e
	}
	out.AlreadyApplied = (!restore && state.State == "undone") || (restore && state.State == "committed")
	// Collapse multiple source lines referring to the same transaction. A duplicate
	// within the creating batch must not hide its original 'added' disposition.
	rows, e := tx.Query(`SELECT sr.transaction_id,MAX(CASE WHEN sr.disposition='added' THEN 1 ELSE 0 END) FROM source_records sr JOIN imports i ON i.id=sr.import_id WHERE i.ledger_id=? AND i.id=? GROUP BY sr.transaction_id ORDER BY MIN(sr.line),sr.transaction_id`, s.ledgerID, id)
	if e != nil {
		return out, state, e
	}
	type source struct {
		id    string
		added int
	}
	sources := []source{}
	for rows.Next() {
		var src source
		if e = rows.Scan(&src.id, &src.added); e != nil {
			rows.Close()
			return out, state, e
		}
		sources = append(sources, src)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, state, e
	}
	var income, expense int64
	for _, src := range sources {
		t, e := scanRecord(tx.QueryRow("SELECT "+transactionColumns+" FROM transactions WHERE ledger_id=? AND id=?", s.ledgerID, src.id))
		if e != nil {
			return out, state, e
		}
		var deleted sql.NullString
		if e = tx.QueryRow("SELECT deleted_at FROM transactions WHERE ledger_id=? AND id=?", s.ledgerID, src.id).Scan(&deleted); e != nil {
			return out, state, e
		}
		row := ImportUndoRow{TransactionID: t.ID, Date: t.Date, Merchant: t.Merchant, Type: t.Type, AmountMinor: t.AmountMinor, Action: "preserve"}
		prior, owned := state.Changes[t.ID]
		refunded, e := s.refundTotal(tx, t.ID)
		if e != nil {
			return out, state, e
		}
		switch {
		case out.AlreadyApplied:
			row.Reason = "本次操作已完成，无需重复处理"
		case restore:
			if !owned {
				row.Reason = "未由本批撤销，保持不变"
			} else if !deleted.Valid || deleted.String != prior.DeletedAt || t.Version != prior.Version {
				row.Reason = "撤销后记录已变化，无法自动恢复"
				out.BlockedCount++
			} else {
				row.Action = "restore"
				row.Reason = "恢复本批撤销的交易"
			}
		case src.added == 0:
			row.Reason = "本批仅关联已有交易，保持不变"
		case deleted.Valid:
			row.Reason = "已被其他操作移除，保持不变"
		case refunded > 0:
			row.Reason = "存在退款关联，保留原消费及退款"
		case t.Version != 1 && !(owned && prior.DeletedAt == "" && t.Version == prior.Version):
			row.Reason = "交易已修改，保留当前记录"
		default:
			var shared int
			e = tx.QueryRow(`SELECT COUNT(*) FROM source_records sr JOIN imports i ON i.id=sr.import_id WHERE i.ledger_id=? AND sr.transaction_id=? AND i.id<>? AND i.committed_at IS NOT NULL`, s.ledgerID, t.ID, id).Scan(&shared)
			if e != nil {
				return out, state, e
			}
			if shared > 0 {
				row.Reason = "其他已入账批次也关联此交易，保持不变"
			} else {
				row.Action = "undo"
				row.Reason = "仅由本批新增且未修改"
			}
		}
		if row.Action == "preserve" {
			out.PreservedCount++
		} else {
			out.ChangeCount++
			amount, e := strconv.ParseInt(t.AmountMinor, 10, 64)
			if e != nil {
				return out, state, e
			}
			if amount < 0 {
				expense -= amount
			} else {
				income += amount
			}
		}
		out.Rows = append(out.Rows, row)
	}
	out.IncomeMinor = strconv.FormatInt(income, 10)
	out.ExpenseMinor = strconv.FormatInt(expense, 10)
	return out, state, nil
}
func (s *Store) UndoImport(id string, version int64) (ImportUndoPreview, error) {
	return s.applyImportUndo(id, version, false)
}
func (s *Store) RestoreImport(id string, version int64) (ImportUndoPreview, error) {
	return s.applyImportUndo(id, version, true)
}
func (s *Store) applyImportUndo(id string, version int64, restore bool) (ImportUndoPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return ImportUndoPreview{}, e
	}
	defer tx.Rollback()
	if e = s.authorize(tx, true); e != nil {
		return ImportUndoPreview{}, e
	}
	out, prior, e := s.previewImportUndo(tx, id, restore)
	if e != nil {
		return out, e
	}
	if out.AlreadyApplied {
		return out, nil
	}
	if version != out.LedgerVersion {
		return out, problem("conflict", "账本已变化，请重新查看影响后确认")
	}
	if restore && out.ChangeCount != len(prior.Changes) {
		return out, problem("conflict", "撤销后的记录已变化，请核实后再恢复")
	}
	next := importUndoState{State: "undone", Changes: map[string]importUndoChange{}}
	operation := "import_undo"
	if restore {
		next.State = "committed"
		operation = "import_restore"
	}
	timestamp := now()
	for _, row := range out.Rows {
		if row.Action == "preserve" {
			continue
		}
		before, e := scanRecord(tx.QueryRow("SELECT "+transactionColumns+" FROM transactions WHERE ledger_id=? AND id=?", s.ledgerID, row.TransactionID))
		if e != nil {
			return out, e
		}
		var deleted any = timestamp
		if restore {
			deleted = nil
		}
		result, e := tx.Exec("UPDATE transactions SET deleted_at=?,updated_at=?,version=version+1 WHERE ledger_id=? AND id=? AND version=?", deleted, timestamp, s.ledgerID, row.TransactionID, before.Version)
		if e != nil {
			return out, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return out, e
		}
		if n != 1 {
			return out, problem("conflict", "交易已变化，请重新预览")
		}
		after := before
		after.Version++
		change := importUndoChange{Version: after.Version}
		if !restore {
			change.DeletedAt = timestamp
		}
		next.Changes[row.TransactionID] = change
		payload := map[string]any{"importId": id, "before": before, "after": after, "deletedAt": deleted}
		if _, e = tx.Exec("INSERT INTO change_log(change_id,ledger_id,device_id,entity_id,entity_type,base_version,entity_version,operation,payload_version,payload_json,created_at,actor_id) VALUES(?,?,?,?,?,?,?,?,1,?,?,?)", newID(), s.ledgerID, s.deviceID, row.TransactionID, "transaction", before.Version, after.Version, operation, encode(payload), timestamp, nullActor(s.actorID)); e != nil {
			return out, e
		}
	}
	if _, e = tx.Exec("UPDATE ledgers SET version=version+1 WHERE id=? AND version=?", s.ledgerID, version); e != nil {
		return out, e
	}
	if _, e = tx.Exec("INSERT INTO change_log(change_id,ledger_id,device_id,entity_id,entity_type,base_version,entity_version,operation,payload_version,payload_json,created_at,actor_id) VALUES(?,?,?,?,?,?,?,?,1,?,?,?)", newID(), s.ledgerID, s.deviceID, id, "import", version, version+1, operation, encode(next), timestamp, nullActor(s.actorID)); e != nil {
		return out, e
	}
	out.State = next.State
	out.LedgerVersion++
	return out, tx.Commit()
}
