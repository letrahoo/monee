package ledger

import (
	"context"
	"database/sql"
)

// Validate the whole database before selecting the default ledger or exposing
// normal runtime6. Every SELECT shares one snapshot; no repair or seed writes.
func checkExpensePairRuntime(db *sql.DB) error {
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return expensePairUnavailable()
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
		return expensePairUnavailable()
	}
	if err = checkExpensePairStructureTx(tx); err != nil {
		return expensePairUnavailable()
	}
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return expensePairUnavailable()
	}
	bad := rows.Next()
	rowErr := rows.Err()
	closeErr := rows.Close()
	if bad || rowErr != nil || closeErr != nil {
		return expensePairUnavailable()
	}
	rows, err = tx.Query("SELECT id FROM ledgers ORDER BY id")
	if err != nil {
		return expensePairUnavailable()
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return expensePairUnavailable()
		}
		ids = append(ids, id)
	}
	rowErr = rows.Err()
	closeErr = rows.Close()
	if rowErr != nil || closeErr != nil {
		return expensePairUnavailable()
	}
	// Close collection rows before strict loading: the DB intentionally has only
	// one connection and nested open cursors cannot be relied upon across drivers.
	for _, id := range ids {
		scoped := &Store{ledgerID: id}
		if _, err = scoped.loadExpensePairLedger(tx); err != nil {
			return expensePairUnavailable()
		}
	}
	return nil
}

// SchemaVersion reports this opened database, not the default migration target
// or the binary's maximum supported version. Failure must not become health OK.
func (s *Store) SchemaVersion() (int, error) {
	var version int
	err := s.db.QueryRow("PRAGMA user_version").Scan(&version)
	return version, err
}
