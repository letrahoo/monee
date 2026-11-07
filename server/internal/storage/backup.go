// Package storage provides versioned, verified SQLite snapshots. Credentials are
// not part of database backups. Restore is offline into a new destination only.
package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/letrahoo/monee/server/internal/dirlease"
	_ "modernc.org/sqlite"
)

type Manifest struct {
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
	Schema  int    `json:"schema"`
}

func openRead(path string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path}
	return sql.Open("sqlite", u.String()+"?mode=ro")
}
func Check(path string) (int, error) {
	db, e := openRead(path)
	if e != nil {
		return 0, e
	}
	defer db.Close()
	var integrity string
	if e = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); e != nil {
		return 0, e
	}
	if integrity != "ok" {
		return 0, fmt.Errorf("database integrity check failed")
	}
	rows, e := db.Query("PRAGMA foreign_key_check")
	if e != nil {
		return 0, e
	}
	bad := rows.Next()
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return 0, rowErr
	}
	if bad {
		return 0, fmt.Errorf("database foreign key check failed")
	}
	var v int
	e = db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, e
}
func checksum(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func Snapshot(source, directory string) error {
	if e := os.Mkdir(directory, 0700); e != nil {
		return e
	}
	done := false
	defer func() {
		if !done {
			os.RemoveAll(directory)
		}
	}()
	db, e := openRead(source)
	if e != nil {
		return e
	}
	defer db.Close()
	target := filepath.Join(directory, "database.sqlite3")
	if _, e = db.Exec("VACUUM INTO '" + strings.ReplaceAll(target, "'", "''") + "'"); e != nil {
		return e
	}
	if e = os.Chmod(target, 0600); e != nil {
		return e
	}
	v, e := Check(target)
	if e != nil {
		return e
	}
	hash, e := checksum(target)
	if e != nil {
		return e
	}
	b, _ := json.MarshalIndent(Manifest{1, hash, v}, "", "  ")
	if e = os.WriteFile(filepath.Join(directory, "manifest.json"), b, 0600); e != nil {
		return e
	}
	done = true
	return nil
}

// Restore acquires the same directory lease as the service before inspecting
// the backup or creating a new target. Online Snapshot remains read-only.
func Restore(directory, target string) error {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	lease, err := dirlease.Acquire(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	defer lease.Close()
	return RestoreWithLease(lease, directory, absolute)
}

// RestoreWithLease lets a future offline coordinator retain one lease across
// backup/restore/upgrade steps, without nesting flock acquisitions.
func RestoreWithLease(lease *dirlease.Lease, directory, target string) error {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	return lease.Within(filepath.Dir(absolute), func(ownedDirectory string) error {
		return restore(directory, filepath.Join(ownedDirectory, filepath.Base(absolute)))
	})
}

// VerifySnapshot validates the manifest, checksum, integrity and declared schema.
// It permits future versions for read-only archival, not application startup.
func VerifySnapshot(directory string) (int, error) {
	return verifySnapshot(directory)
}

func verifySnapshot(directory string) (int, error) {
	raw, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return 0, err
	}
	var m Manifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return 0, err
	}
	if m.Version != 1 {
		return 0, fmt.Errorf("unsupported backup version")
	}
	source := filepath.Join(directory, "database.sqlite3")
	hash, err := checksum(source)
	if err != nil {
		return 0, err
	}
	if hash != m.SHA256 {
		return 0, fmt.Errorf("backup checksum mismatch")
	}
	v, err := Check(source)
	if err != nil {
		return 0, err
	}
	if v != m.Schema {
		return 0, fmt.Errorf("backup schema mismatch")
	}
	return v, nil
}

func restore(directory, target string) error {
	v, e := VerifySnapshot(directory)
	if e != nil {
		return e
	}
	if v > SchemaVersion {
		return fmt.Errorf("backup requires a newer application")
	}
	return copyRestoredDatabase(filepath.Join(directory, "database.sqlite3"), target)
}

func copyRestoredDatabase(source, target string) error {
	in, e := os.Open(source)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	done := false
	defer func() {
		out.Close()
		if !done {
			os.Remove(target)
		}
	}()
	if _, e = io.Copy(out, in); e != nil {
		return e
	}
	if e = out.Sync(); e != nil {
		return e
	}
	done = true
	return nil
}
