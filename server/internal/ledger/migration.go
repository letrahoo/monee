package ledger

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
)

//go:embed migrations/003.sql
var schema3 string

func migrateRefundSchema(db *sql.DB) (result error) {
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
	if _, err = tx.Exec(schema3); err != nil {
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
		return fmt.Errorf("refund schema migration failed foreign key validation")
	}
	return tx.Commit()
}
