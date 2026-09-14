package main

import (
	"database/sql"
	"github.com/letrahoo/monee/server/internal/auth"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyDatabasesAreBackedUpAndConsolidated(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "monee.db")
	db, err := sql.Open("sqlite", oldPath)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../../internal/ledger/migrations/001.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(schema) + `INSERT INTO ledgers VALUES('legacy-ledger','CNY','Asia/Shanghai',0,'legacy-device','2026-01-01');`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	oldAuth := filepath.Join(dir, "auth.db")
	s, err := auth.Open(oldAuth, []auth.Selector{{Provider: "github", Kind: "subject", Value: "1001"}})
	if err != nil {
		t.Fatal(err)
	}
	i := auth.Identity{Provider: "github", Subject: "1001", Username: "legacy"}
	if err = s.RememberIdentity(i); err != nil {
		t.Fatal(err)
	}
	oldToken, err := s.CreateSession(i)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	// Recreate the previous schema, before unified account tables existed.
	db, err = sql.Open("sqlite", oldAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DROP TABLE user_access; DROP TABLE user_identities; DROP TABLE app_users; DELETE FROM auth_meta WHERE key='account_schema'; PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	l, a, err := openApplication(dir, auth.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if u, err := a.Session(oldToken); err != nil || u != nil {
		t.Fatal("legacy session was migrated", err)
	}
	if err = a.RememberIdentity(i); err != nil {
		t.Fatal(err)
	}
	newToken, err := a.CreateSession(i)
	if err != nil {
		t.Fatal(err)
	}
	u, err := a.Session(newToken)
	if err != nil || u == nil || !u.Allowed {
		t.Fatal("legacy access lost", err, u)
	}
	if ledgers, err := l.ListLedgers(u.ID); err != nil || len(ledgers) != 0 {
		t.Fatal("automatically granted old shared data", err, ledgers)
	}
	d, err := l.Dashboard("2026-01", "", 1)
	if err != nil || d.LedgerID != "legacy-ledger" {
		t.Fatal("legacy ID not retained", err, d)
	}
	l.Close()
	a.Close()
	for _, name := range []string{"monee.db", "auth.db"} {
		matches, _ := filepath.Glob(filepath.Join(dir, name+".backup-*", "manifest.json"))
		if len(matches) != 1 {
			t.Fatal("missing backup", name, matches)
		}
	}
	l, a, err = openApplication(dir, auth.Config{})
	if err != nil {
		t.Fatal("restart", err)
	}
	l.Close()
	a.Close()
}
