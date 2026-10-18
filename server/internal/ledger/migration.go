package ledger

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

//go:embed migrations/003.sql
var schema3 string

//go:embed migrations/004.sql
var schema4 string

//go:embed migrations/005.sql
var schema5 string

func migrateRefundSchema(db *sql.DB) (result error) {
	return migrateTransactionSchema(db, schema3)
}

func migrateTransferSchema(db *sql.DB) error {
	return migrateTransactionSchema(db, schema4)
}

func migrateTransactionSchema(db *sql.DB, ddl string) (result error) {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// SQLite ignores this PRAGMA inside a transaction. Pin the connection so the
	// disabled setting and its restoration cannot land on different connections.
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	defer func() {
		_, restoreErr := conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
		result = errors.Join(result, restoreErr)
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(ddl); err != nil {
		return err
	}
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	bad := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if bad {
		return fmt.Errorf("transaction schema migration failed foreign key validation")
	}
	return tx.Commit()
}

func expensePairFoundationUnavailable() error {
	return problem("association_foundation_unavailable", "当前版本不支持已有关联决策数据或未知关联结构，拒绝打开账本")
}

// This migration adds only foundation storage. It does not change any existing
// transaction, source record, posting, identity, actor, or financial version.
func migrateExpensePairSchema(db *sql.DB) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return expensePairFoundationUnavailable()
	}
	defer conn.Close()
	var foreignKeys int
	if err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		return expensePairFoundationUnavailable()
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return expensePairFoundationUnavailable()
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 4 {
		return expensePairFoundationUnavailable()
	}
	if err = rejectPrematureExpensePairObjects(tx); err != nil {
		return err
	}
	if _, err = tx.Exec(schema5); err != nil {
		return expensePairFoundationUnavailable()
	}
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return expensePairFoundationUnavailable()
	}
	bad := rows.Next()
	rowErr := rows.Err()
	closeErr := rows.Close()
	if bad || rowErr != nil || closeErr != nil {
		return expensePairFoundationUnavailable()
	}
	if err = tx.Commit(); err != nil {
		return expensePairFoundationUnavailable()
	}
	return nil
}

// An older version number with association objects is not a migration source to
// repair. Refuse it before any normal Open migration or backup path is attempted.
func rejectPrematureExpensePairObjects(q queryer) error {
	var count int
	err := q.QueryRow(`SELECT COUNT(*) FROM main.sqlite_master WHERE lower(name) GLOB 'expense_pair_*' OR lower(tbl_name) IN ('expense_pair_decisions','expense_pair_members','expense_pair_revisions')`).Scan(&count)
	if err != nil || count != 0 {
		return expensePairFoundationUnavailable()
	}
	return nil
}

type expensePairSchemaObject struct{ kind, table, sql string }

// Derive the exact expected definitions from the one embedded migration rather
// than maintaining a second, potentially divergent schema declaration. Only our
// static DDL is split here; no user-supplied SQL or JSON is parsed or repaired.
func expensePairSchemaObjects() map[string]expensePairSchemaObject {
	var lines []string
	for _, line := range strings.Split(schema5, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	expected := map[string]expensePairSchemaObject{}
	for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
		statement = strings.TrimSpace(statement)
		fields := strings.Fields(statement)
		if strings.HasPrefix(statement, "CREATE TABLE ") && len(fields) > 2 {
			name := fields[2]
			expected[name] = expensePairSchemaObject{kind: "table", table: name, sql: strings.Join(fields, " ")}
		} else if strings.HasPrefix(statement, "CREATE UNIQUE INDEX ") && len(fields) > 5 {
			name := fields[3]
			table := strings.SplitN(fields[5], "(", 2)[0]
			expected[name] = expensePairSchemaObject{kind: "index", table: table, sql: strings.Join(fields, " ")}
		}
	}
	return expected
}

// This is an Open/startup capability fence, not a decision semantic validator or
// protection against an external/future writer after this process has opened.
// Remove it only with complete effective readers and all mutation guards, using
// an exclusive version upgrade. Even retracted or corrupt rows are unsupported.
func checkExpensePairFoundation(db *sql.DB) error {
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return expensePairFoundationUnavailable()
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		return expensePairFoundationUnavailable()
	}
	expected := expensePairSchemaObjects()
	if len(expected) != 4 {
		return expensePairFoundationUnavailable()
	}
	seen := map[string]bool{}
	rows, err := tx.Query(`SELECT name,type,tbl_name,sql FROM main.sqlite_master WHERE lower(name) GLOB 'expense_pair_*' OR lower(tbl_name) IN ('expense_pair_decisions','expense_pair_members','expense_pair_revisions') ORDER BY name`)
	if err != nil {
		return expensePairFoundationUnavailable()
	}
	for rows.Next() {
		var name, kind, table string
		var sqlText sql.NullString
		if err = rows.Scan(&name, &kind, &table, &sqlText); err != nil {
			rows.Close()
			return expensePairFoundationUnavailable()
		}
		if kind == "index" && !sqlText.Valid && strings.HasPrefix(name, "sqlite_autoindex_expense_pair_") {
			if object, ok := expected[table]; ok && object.kind == "table" {
				continue
			}
		}
		object, ok := expected[name]
		if !ok || object.kind != kind || object.table != table || !sqlText.Valid || strings.Join(strings.Fields(sqlText.String), " ") != object.sql {
			rows.Close()
			return expensePairFoundationUnavailable()
		}
		seen[name] = true
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if rowErr != nil || closeErr != nil || len(seen) != len(expected) {
		return expensePairFoundationUnavailable()
	}
	var anyRow bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM expense_pair_decisions) OR EXISTS(SELECT 1 FROM expense_pair_members) OR EXISTS(SELECT 1 FROM expense_pair_revisions)`).Scan(&anyRow); err != nil || anyRow {
		return expensePairFoundationUnavailable()
	}
	return nil
}
