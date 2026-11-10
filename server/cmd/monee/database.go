package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/dirlease"
	"github.com/letrahoo/monee/server/internal/ledger"
	"github.com/letrahoo/monee/server/internal/storage"
)

// Called only while holding the data-directory lock. Both legacy databases are
// backed up before conversion, and the new application database is published only
// after every migration and integrity check succeeds.
func openApplication(dir string, config auth.Config, lease *dirlease.Lease) (*ledger.Store, *auth.Store, error) {
	// The startup owner must retain this lease until both returned stores close.
	if err := lease.Within(dir, func(owned string) error { dir = owned; return nil }); err != nil {
		return nil, nil, err
	}
	target := filepath.Join(dir, "application.db")
	if _, err := os.Stat(target); err == nil {
		l, e := ledger.Open(target)
		if e != nil {
			return nil, nil, e
		}
		a, e := auth.Open(target, config.Superadmins)
		if e != nil {
			l.Close()
			return nil, nil, e
		}
		return l, a, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	stage := filepath.Join(dir, "application-migration-"+time.Now().UTC().Format("20060102T150405.000000000")+".db")
	defer os.Remove(stage)
	defer os.Remove(stage + "-wal")
	defer os.Remove(stage + "-shm")
	for _, name := range []string{"monee.db", "auth.db"} {
		source := filepath.Join(dir, name)
		if _, e := os.Stat(source); errors.Is(e, os.ErrNotExist) {
			continue
		} else if e != nil {
			return nil, nil, e
		}
		backup := source + ".backup-" + time.Now().UTC().Format("20060102T150405.000000000")
		if e := storage.Snapshot(source, backup); e != nil {
			return nil, nil, e
		}
		if name == "monee.db" {
			if e := storage.RestoreWithLease(lease, backup, stage); e != nil {
				return nil, nil, e
			}
		}
	}
	l, e := ledger.Open(stage)
	if e != nil {
		return nil, nil, e
	}
	l.Close()
	oldAuth := filepath.Join(dir, "auth.db")
	if _, e = os.Stat(oldAuth); e == nil {
		if e = auth.ImportLegacy(stage, oldAuth); e != nil {
			return nil, nil, e
		}
	}
	a, e := auth.Open(stage, config.Superadmins)
	if e != nil {
		return nil, nil, e
	}
	a.Close()
	if _, e = storage.Check(stage); e != nil {
		return nil, nil, e
	}
	if e = os.Rename(stage, target); e != nil {
		return nil, nil, e
	}
	l, e = ledger.Open(target)
	if e != nil {
		return nil, nil, e
	}
	a, e = auth.Open(target, config.Superadmins)
	if e != nil {
		l.Close()
		return nil, nil, e
	}
	return l, a, nil
}
