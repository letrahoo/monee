package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotRestoreWALAndTampering(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	db, e := sql.Open("sqlite", source)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("PRAGMA journal_mode=WAL; CREATE TABLE items(id TEXT PRIMARY KEY); INSERT INTO items VALUES('retained'); PRAGMA user_version=2"); e != nil {
		t.Fatal(e)
	}
	bundle := filepath.Join(root, "backup")
	if e = Snapshot(source, bundle); e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(root, "restored.db")
	if e = Restore(bundle, dest); e != nil {
		t.Fatal(e)
	}
	restored, e := openRead(dest)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	var value string
	if e = restored.QueryRow("SELECT id FROM items").Scan(&value); e != nil || value != "retained" {
		t.Fatal(e, value)
	}
	if e = Restore(bundle, dest); e == nil {
		t.Fatal("overwrote database")
	}
	if e = Snapshot(source, bundle); e == nil {
		t.Fatal("overwrote backup")
	}
	if e = os.WriteFile(filepath.Join(bundle, "database.sqlite3"), []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = Restore(bundle, filepath.Join(root, "bad.db")); e == nil {
		t.Fatal("accepted corrupted backup")
	}
}
