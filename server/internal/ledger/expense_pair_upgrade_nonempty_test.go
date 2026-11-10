package ledger

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/dirlease"
	"github.com/letrahoo/monee/server/internal/storage"
)

func TestExpensePairOfflineUpgradeRejectsActiveAndWithdrawnData(t *testing.T) {
	for _, withdrawn := range []bool{false, true} {
		name := "active"
		if withdrawn {
			name = "withdrawn"
		}
		t.Run(name, func(t *testing.T) {
			root, s, _, receipt := effectiveConfirmed(t)
			if withdrawn {
				effectiveWithdraw(t, s, receipt)
			}
			var sequence int
			var database, path string
			if e := root.db.QueryRow("PRAGMA database_list").Scan(&sequence, &database, &path); e != nil {
				t.Fatal(e)
			}
			dir := t.TempDir()
			bundle := filepath.Join(t.TempDir(), "source")
			if e := storage.Snapshot(path, bundle); e != nil {
				t.Fatal(e)
			}
			if e := storage.Restore(bundle, filepath.Join(dir, "application.db")); e != nil {
				t.Fatal(e)
			}
			lease, e := dirlease.Acquire(dir)
			if e != nil {
				t.Fatal(e)
			}
			defer lease.Close()
			if _, e = upgradeExpensePairOffline(context.Background(), lease, dir, "before-upgrade"); e == nil {
				t.Fatal("nonempty decisions were upgraded")
			}
			upgradeVersion(t, dir, 5)
			if _, e = os.Stat(filepath.Join(dir, "before-upgrade")); !os.IsNotExist(e) {
				t.Fatal("nonempty preflight created backup", e)
			}
		})
	}
}
