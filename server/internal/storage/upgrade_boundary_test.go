//go:build darwin || linux

package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/dirlease"
)

// This deliberately demonstrates why an offline upgrade must close all old
// connections. Neither advisory ownership nor user_version revokes SQLite FDs.
func TestUpgradeVersionAndLeaseDoNotRevokeExistingConnection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "synthetic.db")
	old, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	old.SetMaxOpenConns(1)
	if _, err = old.Exec("CREATE TABLE items(id TEXT PRIMARY KEY); PRAGMA user_version=5"); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = old.QueryRow("PRAGMA user_version").Scan(&before); err != nil || before != 5 {
		t.Fatal(before, err)
	}
	lease, err := dirlease.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if other, e := dirlease.Acquire(dir); !errors.Is(e, dirlease.ErrUnavailable) {
		if other != nil {
			other.Close()
		}
		t.Fatal(e)
	}
	newer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer newer.Close()
	if _, err = newer.Exec("PRAGMA user_version=6"); err != nil {
		t.Fatal(err)
	}
	// The already-open, noncooperating connection still writes successfully.
	if _, err = old.Exec("INSERT INTO items VALUES('old-connection-still-writes')"); err != nil {
		t.Fatal(err)
	}
	var count, version int
	if err = newer.QueryRow("SELECT COUNT(*) FROM items").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err = old.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
		t.Fatal(version, err)
	}
	// Snapshot is intentionally online/read-only, including a future-version DB.
	if err = Snapshot(path, filepath.Join(dir, "backup")); err != nil {
		t.Fatal(err)
	}
	if other, e := dirlease.Acquire(dir); !errors.Is(e, dirlease.ErrUnavailable) {
		if other != nil {
			other.Close()
		}
		t.Fatal("snapshot released owner lease", e)
	}
}
