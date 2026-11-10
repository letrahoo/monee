//go:build darwin || linux

package dirlease

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLeaseLifetimeAndScope(t *testing.T) {
	dir := t.TempDir()
	l, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if other, err := Acquire(dir); err == nil {
		other.Close()
		t.Fatal("duplicate lease")
	}
	called := false
	if err = l.Within(t.TempDir(), func(string) error { called = true; return nil }); !errors.Is(err, ErrUnavailable) || called {
		t.Fatal(err)
	}
	var absent *Lease
	if err = absent.Within(dir, func(string) error { called = true; return nil }); !errors.Is(err, ErrUnavailable) || called {
		t.Fatal(err)
	}
	want := errors.New("operation failed")
	if err = l.Within(dir, func(string) error { called = true; return want }); !errors.Is(err, want) || !called {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if err = l.Within(dir, func(string) error { t.Fatal("closed lease ran callback"); return nil }); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	next, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
	if _, err = os.Stat(filepath.Join(dir, "service.lock")); err != nil {
		t.Fatal("must retain lock inode", err)
	}
}

func TestLeaseLegacyProtocolAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	f, err := os.OpenFile(filepath.Join(dir, "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if l, err := Acquire(dir); err == nil {
		l.Close()
		t.Fatal("old service lock ignored")
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err = os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err = l.Within(dir, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		t.Fatal("new lease ignored by old service")
	}
}

func TestLeaseRejectsSymlinkLockFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "service.lock")); err != nil {
		t.Fatal(err)
	}
	if l, err := Acquire(dir); err == nil {
		l.Close()
		t.Fatal("symlink lock accepted")
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "unchanged" {
		t.Fatal(string(raw), err)
	}
}
