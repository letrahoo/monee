package main

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
	"github.com/letrahoo/monee/server/internal/storage"
)

func TestCurrentSchema5RejectsFixedSchema6(t *testing.T) {
	// This is deliberately pinned: raising production support requires replacing
	// this check with an actual retained-v5 compatibility fixture, not changing
	// a moving SchemaVersion+1 sentinel and silently losing the old boundary.
	if storage.SchemaVersion != 5 {
		t.Fatal("retain real schema5 compatibility coverage before enabling a new version")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "future.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE marker(value TEXT); INSERT INTO marker VALUES('unchanged'); PRAGMA user_version=6"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	opened, err := ledger.Open(path)
	var p *ledger.Problem
	if opened != nil {
		opened.Close()
		t.Fatal("ledger accepted fixed schema6")
	}
	if !errors.As(err, &p) || p.Code != "future_schema" {
		t.Fatal(err)
	}
	access, err := auth.Open(path, nil)
	if access != nil {
		access.Close()
		t.Fatal("auth accepted fixed schema6")
	}
	if err == nil || !strings.Contains(err.Error(), "newer application") {
		t.Fatal(err)
	}
	if v, e := storage.Check(path); e != nil || v != 6 {
		t.Fatal(v, e)
	}
	bundle := filepath.Join(dir, "backup")
	if err = storage.Snapshot(path, bundle); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored.db")
	if err = storage.Restore(bundle, target); err == nil || !strings.Contains(err.Error(), "newer application") {
		t.Fatal(err)
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("future restore left target", err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables int
	var value string
	if err = db.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil || tables != 1 {
		t.Fatal(tables, err)
	}
	if err = db.QueryRow("SELECT value FROM marker").Scan(&value); err != nil || value != "unchanged" {
		t.Fatal(value, err)
	}
	matches, err := filepath.Glob(path + ".backup-before-*")
	if err != nil || len(matches) != 0 {
		t.Fatal("old startup attempted migration backup", matches, err)
	}
}
