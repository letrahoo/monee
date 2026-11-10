package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/dirlease"
)

func TestApplicationRequiresLiveMatchingLease(t *testing.T) {
	dir := t.TempDir()
	wrong, err := dirlease.Acquire(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()
	closed, err := dirlease.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	for _, lease := range []*dirlease.Lease{nil, wrong, closed} {
		l, a, err := openApplication(dir, auth.Config{}, lease)
		if l != nil || a != nil || !errors.Is(err, dirlease.ErrUnavailable) {
			t.Fatal(l, a, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "service.lock" {
			t.Fatal("invalid lease touched database", entry.Name())
		}
	}
	if _, err = os.Stat(filepath.Join(dir, "application.db")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
