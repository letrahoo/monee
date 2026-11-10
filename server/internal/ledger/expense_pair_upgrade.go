package ledger

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/letrahoo/monee/server/internal/dirlease"
	"github.com/letrahoo/monee/server/internal/storage"
)

// UpgradeExpensePairOffline is an explicit offline capability transition. It
// never runs from normal startup or HTTP. Default creation/migration stays5;
// normal Open6 validates the entire database before making it available.
// The owner must first stop every old connection; a lease cannot enforce that
// against noncooperating clients. backupName is a new child of the leased dir.
func UpgradeExpensePairOffline(ctx context.Context, lease *dirlease.Lease, dir, backupName string) (string, error) {
	return upgradeExpensePairOffline(ctx, lease, dir, backupName)
}

func upgradeExpensePairOffline(ctx context.Context, lease *dirlease.Lease, dir, backupName string) (string, error) {
	return upgradeExpensePairWithSnapshot(ctx, lease, dir, backupName, storage.Snapshot)
}

func expensePairUpgradeUnavailable() error {
	return problem("expense_pair_upgrade_unavailable", "关联功能升级未完成，原数据库未切换；请保持服务停止并检查备份")
}

// snapshot is a private dependency seam for deterministic cancellation/failure
// tests. Normal private coordination always supplies storage.Snapshot above.
func upgradeExpensePairWithSnapshot(ctx context.Context, lease *dirlease.Lease, dir, backupName string, snapshot func(string, string) error) (string, error) {
	if ctx == nil || snapshot == nil || backupName == "" || backupName == "." || backupName == ".." || filepath.Base(backupName) != backupName || strings.ContainsAny(backupName, "\\\x00\r\n") {
		return "", expensePairUpgradeUnavailable()
	}
	var backup string
	err := lease.Within(dir, func(owned string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		source := filepath.Join(owned, "application.db")
		info, e := os.Lstat(source)
		if e != nil || !info.Mode().IsRegular() {
			return expensePairUpgradeUnavailable()
		}
		u := url.URL{Scheme: "file", Path: source}
		db, e := sql.Open("sqlite", u.String()+"?mode=rw&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
		if e != nil {
			return e
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = checkExpensePairFoundationTx(tx); e != nil {
			return e
		}
		// Empty decision tables alone do not excuse orphan decision audit evidence.
		var residue bool
		if e = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM change_log WHERE entity_type='expense_pair_decision')").Scan(&residue); e != nil || residue {
			return expensePairUpgradeUnavailable()
		}
		candidate := filepath.Join(owned, backupName)
		if e = snapshot(source, candidate); e != nil {
			return e
		}
		if version, e := storage.VerifySnapshot(candidate); e != nil || version != 5 {
			return expensePairUpgradeUnavailable()
		}
		originalDigest, e := expensePairUpgradeDigest(tx)
		if e != nil {
			return e
		}
		backupURL := url.URL{Scheme: "file", Path: filepath.Join(candidate, "database.sqlite3")}
		copyDB, e := sql.Open("sqlite", backupURL.String()+"?mode=ro")
		if e != nil {
			return e
		}
		copyDigest, digestErr := expensePairUpgradeDigest(copyDB)
		closeErr := copyDB.Close()
		if digestErr != nil || closeErr != nil || originalDigest != copyDigest {
			return expensePairUpgradeUnavailable()
		}
		// Readback alone is not durability. Seal files, then directory entries,
		// before committing a source version the currently shipped app rejects.
		for _, path := range []string{filepath.Join(candidate, "database.sqlite3"), filepath.Join(candidate, "manifest.json"), candidate, owned} {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			err = f.Sync()
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		// Recheck in the same snapshot. A noncooperating writer changing the source
		// after our reads must make the SQLite write upgrade fail, not be overwritten.
		if e = checkExpensePairFoundationTx(tx); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "PRAGMA user_version=6"); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		backup = candidate
		return nil
	})
	if err != nil {
		return "", expensePairUpgradeUnavailable()
	}
	return backup, nil
}
