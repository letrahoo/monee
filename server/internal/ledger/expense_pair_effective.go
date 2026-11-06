package ledger

import "database/sql"

// Effective financial projection and writer guards for confirmed expense pairs.
// Normal Open still refuses populated association tables, so these paths are
// reachable only from isolated tests until the complete API/UI delivery.

func expensePairLocked() error {
	return problem("expense_pair_locked", "这笔交易已确认与另一条记录是同一笔消费，请先撤回关联再修改")
}

// A pre-schema5 database (only seen before Open migrates it) cannot hold any
// decision. A partial table set is not a valid schema and fails closed.
func expensePairSchemaPresent(q queryer) (bool, error) {
	var tables int
	if err := q.QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name IN ('expense_pair_decisions','expense_pair_members','expense_pair_revisions')`).Scan(&tables); err != nil {
		return false, expensePairUnavailable()
	}
	if tables != 0 && tables != 3 {
		return false, expensePairUnavailable()
	}
	if tables == 0 {
		var version int
		var residue bool
		if err := q.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 4 {
			return false, expensePairUnavailable()
		}
		if err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name IN ('expense_pair_decisions','expense_pair_members','expense_pair_revisions')) OR EXISTS(SELECT 1 FROM change_log WHERE entity_type='expense_pair_decision')`).Scan(&residue); err != nil || residue {
			return false, expensePairUnavailable()
		}
	}
	return tables == 3, nil
}

// Validate the complete decision history in the caller's transaction before
// trusting any member marker. Damaged metadata never unlocks frozen evidence.
func (s *Store) expensePairMemberLocked(q *sql.Tx, transactionID string) error {
	if _, err := s.expensePairSuppressed(q); err != nil {
		return err
	}
	present, err := expensePairSchemaPresent(q)
	if err != nil || !present {
		return err
	}
	var locked bool
	if err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM expense_pair_members WHERE ledger_id=? AND transaction_id=? AND active IS NOT 0)`, s.ledgerID, transactionID).Scan(&locked); err != nil {
		return err
	}
	if locked {
		return expensePairLocked()
	}
	return nil
}

// Any source line of the batch, including duplicate links, can be part of an
// active member's frozen occurrence set; changing the batch state would
// invalidate that evidence, so the whole batch is blocked.
func (s *Store) expensePairImportLocked(tx *sql.Tx, importID string) (map[string]bool, error) {
	out := map[string]bool{}
	if _, err := s.expensePairSuppressed(tx); err != nil {
		return nil, err
	}
	present, err := expensePairSchemaPresent(tx)
	if err != nil || !present {
		return out, err
	}
	rows, err := tx.Query(`SELECT DISTINCT sr.transaction_id FROM source_records sr JOIN expense_pair_members m ON m.ledger_id=? AND m.transaction_id=sr.transaction_id AND m.active IS NOT 0 WHERE sr.import_id=?`, s.ledgerID, importID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// expensePairSuppressed returns the secondary members of every active decision.
// It uses the strict loader, so damaged or drifted metadata fails closed rather
// than silently falling back to double-counted raw totals.
func (s *Store) expensePairSuppressed(tx *sql.Tx) (map[string]bool, error) {
	out := map[string]bool{}
	present, err := expensePairSchemaPresent(tx)
	if err != nil {
		return nil, err
	}
	if !present {
		return out, nil
	}
	var populated bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM expense_pair_decisions WHERE ledger_id=?) OR EXISTS(SELECT 1 FROM expense_pair_members WHERE ledger_id=?) OR EXISTS(SELECT 1 FROM expense_pair_revisions WHERE ledger_id=?) OR EXISTS(SELECT 1 FROM change_log WHERE ledger_id=? AND entity_type='expense_pair_decision')`, s.ledgerID, s.ledgerID, s.ledgerID, s.ledgerID).Scan(&populated); err != nil {
		return nil, expensePairUnavailable()
	}
	if !populated {
		return out, nil
	}
	decisions, err := s.loadExpensePairLedger(tx)
	if err != nil {
		return nil, err
	}
	for _, d := range decisions {
		if d.Status == "active" {
			out[d.Members[1].TransactionID] = true
		}
	}
	return out, nil
}
