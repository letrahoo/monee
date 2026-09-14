package auth

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
)

// ImportLegacy copies only known authentication tables into a staged application
// database. The old database is attached read-only; old sessions are not migrated.
func ImportLegacy(target, source string) error {
	u := url.URL{Scheme: "file", Path: target}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	old := url.URL{Scheme: "file", Path: source}
	if _, err = db.Exec("ATTACH DATABASE ? AS old_auth", old.String()+"?mode=ro"); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA old_auth.user_version").Scan(&version); err != nil {
		return err
	}
	if version != 1 {
		return fmt.Errorf("legacy authentication schema must be version 1")
	}
	if _, err = tx.Exec(strings.ReplaceAll(authSchema, "PRAGMA user_version=1;", "")); err != nil {
		return err
	}
	for _, table := range []string{"auth_meta", "users", "allowlist", "access_audit"} {
		if _, err = tx.Exec("INSERT INTO " + table + " SELECT * FROM old_auth." + table); err != nil {
			return err
		}
	}
	return tx.Commit()
}
