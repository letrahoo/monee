package ledger

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/dirlease"
	"github.com/letrahoo/monee/server/internal/storage"
)

func upgradeFixture(t *testing.T) (string, *dirlease.Lease) {
	t.Helper()
	dir := t.TempDir()
	s, e := Open(filepath.Join(dir, "application.db"))
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	l, e := dirlease.Acquire(dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	return dir, l
}
func upgradeVersion(t *testing.T, dir string, want int) {
	t.Helper()
	v, e := storage.Check(filepath.Join(dir, "application.db"))
	if e != nil || v != want {
		t.Fatal(v, e)
	}
}
func TestExpensePairOfflineUpgradeSuccess(t *testing.T) {
	dir, l := upgradeFixture(t)
	original, err := sql.Open("sqlite", filepath.Join(dir, "application.db"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := expensePairUpgradeDigest(original)
	original.Close()
	if err != nil {
		t.Fatal(err)
	}
	backup, e := upgradeExpensePairOffline(context.Background(), l, dir, "before-upgrade")
	if e != nil {
		t.Fatal(e)
	}
	upgradeVersion(t, dir, 6)
	if v, e := storage.VerifySnapshot(backup); e != nil || v != 5 {
		t.Fatal(v, e)
	}
	if next, e := dirlease.Acquire(dir); e == nil {
		next.Close()
		t.Fatal("upgrade released caller lease")
	}
	if s, e := Open(filepath.Join(dir, "application.db")); e == nil {
		s.Close()
		t.Fatal("normal Open enabled version6")
	}
	// Repeat rejects before creating a second backup.
	if _, e = upgradeExpensePairOffline(context.Background(), l, dir, "second"); e == nil {
		t.Fatal("repeated upgrade")
	}
	if _, e = os.Stat(filepath.Join(dir, "second")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	restored := filepath.Join(t.TempDir(), "restored.db")
	if e = storage.Restore(backup, restored); e != nil {
		t.Fatal(e)
	}
	s, e := Open(restored)
	if e != nil {
		t.Fatal(e)
	}
	after, err := expensePairUpgradeDigest(s.db)
	s.Close()
	if err != nil || before != after {
		t.Fatal("restored backup differs from original", err)
	}
}
func TestExpensePairOfflineUpgradeFailuresPreserveFive(t *testing.T) {
	for _, mode := range []string{"snapshot-failure", "wrong-valid-backup", "tamper-manifest", "cancel-after-backup", "concurrent-writer", "unknown-ddl", "wrong-version", "no-lease", "backup-exists"} {
		t.Run(mode, func(t *testing.T) {
			dir, l := upgradeFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			snapshot := storage.Snapshot
			name := "before-upgrade"
			switch mode {
			case "wrong-valid-backup":
				other, _ := upgradeFixture(t)
				snapshot = func(_, dst string) error { return storage.Snapshot(filepath.Join(other, "application.db"), dst) }
			case "snapshot-failure":
				snapshot = func(string, string) error { return errors.New("synthetic failure") }
			case "tamper-manifest":
				snapshot = func(src, dst string) error {
					if e := storage.Snapshot(src, dst); e != nil {
						return e
					}
					return os.WriteFile(filepath.Join(dst, "manifest.json"), []byte(`{}`), 0600)
				}
			case "cancel-after-backup":
				snapshot = func(src, dst string) error { e := storage.Snapshot(src, dst); cancel(); return e }
			case "concurrent-writer":
				snapshot = func(src, dst string) error {
					if e := storage.Snapshot(src, dst); e != nil {
						return e
					}
					db, e := sql.Open("sqlite", src)
					if e != nil {
						return e
					}
					defer db.Close()
					_, e = db.Exec("CREATE TABLE noncooperating_writer(value TEXT)")
					if e != nil {
						t.Fatalf("noncooperating writer did not commit: %v", e)
					}
					return nil
				}
			case "unknown-ddl":
				db, e := sql.Open("sqlite", filepath.Join(dir, "application.db"))
				if e != nil {
					t.Fatal(e)
				}
				_, e = db.Exec("CREATE INDEX expense_pair_unknown ON expense_pair_members(transaction_id)")
				db.Close()
				if e != nil {
					t.Fatal(e)
				}
			case "wrong-version":
				db, e := sql.Open("sqlite", filepath.Join(dir, "application.db"))
				if e != nil {
					t.Fatal(e)
				}
				_, e = db.Exec("PRAGMA user_version=4")
				db.Close()
				if e != nil {
					t.Fatal(e)
				}
			case "no-lease":
				l = nil
			case "backup-exists":
				if e := os.Mkdir(filepath.Join(dir, name), 0700); e != nil {
					t.Fatal(e)
				}
			}
			result, e := upgradeExpensePairWithSnapshot(ctx, l, dir, name, snapshot)
			if e == nil || result != "" {
				t.Fatal(result, e)
			}
			want := 5
			if mode == "wrong-version" {
				want = 4
			}
			upgradeVersion(t, dir, want)
			if mode == "unknown-ddl" || mode == "wrong-version" || mode == "no-lease" {
				if _, e = os.Stat(filepath.Join(dir, name)); !os.IsNotExist(e) {
					t.Fatal("preflight created backup", e)
				}
			}
		})
	}
}
