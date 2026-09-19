package ledger

import (
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/storage"
)

func TestTransferMigrationPreservesV3RefundAndBackup(t *testing.T) {
	s, path := schema2Store(t)
	if err := migrateRefundSchema(s.db); err != nil {
		t.Fatal(err)
	}
	original, err := s.Create(testInput(), "transfer-migration-original")
	if err != nil {
		t.Fatal(err)
	}
	refund := original
	refund.ID, refund.Type, refund.AmountMinor, refund.RefundOf = "synthetic-refund", "refund", "100", original.ID
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.insert(tx, refund); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	changes, postings := count(t, s, "change_log"), count(t, s, "postings")
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	summary, err := s.Refunds(original.ID)
	if err != nil || summary.Original != original || len(summary.Refunds) != 1 || summary.Refunds[0] != refund || summary.RemainingMinor != "1134" {
		t.Fatal(summary, err)
	}
	if count(t, s, "change_log") != changes || count(t, s, "postings") != postings {
		t.Fatal("migration rewrote accounting")
	}
	if v, e := storage.Check(path); e != nil || v != 4 {
		t.Fatal(v, e)
	}
	backups, err := filepath.Glob(path + ".backup-before-v4-*")
	if err != nil || len(backups) != 1 {
		t.Fatal(backups, err)
	}
	restored := filepath.Join(t.TempDir(), "old.db")
	if err = storage.Restore(backups[0], restored); err != nil {
		t.Fatal(err)
	}
	if v, e := storage.Check(restored); e != nil || v != 3 {
		t.Fatal("backup not v3", v, e)
	}
	if _, err = s.CreateTransfer(syntheticTransfer(), "transfer-after-migration"); err != nil {
		t.Fatal(err)
	}
}

func TestTransferMigrationFailureRollsBackAndRestoresFK(t *testing.T) {
	s, _ := schema2Store(t)
	if err := migrateRefundSchema(s.db); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA foreign_keys=OFF; INSERT INTO postings VALUES('broken','missing','missing',1,'CNY'); PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := migrateTransferSchema(s.db); err == nil {
		t.Fatal("accepted broken FK")
	}
	var version, fk, staged int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='transactions_v4'").Scan(&staged); err != nil {
		t.Fatal(err)
	}
	if version != 3 || fk != 1 || staged != 0 || count(t, s, "postings") != 1 {
		t.Fatal(version, fk, staged)
	}
}
