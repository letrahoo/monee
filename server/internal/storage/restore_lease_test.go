//go:build darwin || linux

package storage

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/letrahoo/monee/server/internal/dirlease"
)

func TestRestoreLeaseScopeAndLifetime(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new.db")
	missing := filepath.Join(dir, "missing-backup")
	wrong, err := dirlease.Acquire(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()
	for _, lease := range []*dirlease.Lease{nil, wrong} {
		if err = RestoreWithLease(lease, missing, target); !errors.Is(err, dirlease.ErrUnavailable) {
			t.Fatal("read backup before checking lease", err)
		}
	}
	held, err := dirlease.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = Restore(missing, target); !errors.Is(err, dirlease.ErrUnavailable) {
		t.Fatal(err)
	}
	held.Close()
	if err = RestoreWithLease(held, missing, target); !errors.Is(err, dirlease.ErrUnavailable) {
		t.Fatal("closed lease accepted", err)
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("failed lease created target", err)
	}
	// Failure while holding a newly acquired lease must release it.
	if err = Restore(missing, target); err == nil || errors.Is(err, dirlease.ErrUnavailable) {
		t.Fatal(err)
	}
	next, err := dirlease.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}

func TestRestoreRejectsLegacyServiceSubprocess(t *testing.T) {
	if os.Getenv("MONEE_RESTORE_LEASE_HELPER") == "1" {
		f, err := os.OpenFile(filepath.Join(os.Getenv("MONEE_RESTORE_LEASE_DIR"), "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			os.Exit(2)
		}
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			os.Exit(3)
		}
		fmt.Println("locked")
		io.Copy(io.Discard, os.Stdin)
		runtime.KeepAlive(f)
		os.Exit(0)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRestoreRejectsLegacyServiceSubprocess$")
	cmd.Env = append(os.Environ(), "MONEE_RESTORE_LEASE_HELPER=1", "MONEE_RESTORE_LEASE_DIR="+dir)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		input.Close()
		if !waited {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatal(line, err)
	}
	target := filepath.Join(dir, "restored.db")
	if err = Restore(filepath.Join(dir, "does-not-exist"), target); !errors.Is(err, dirlease.ErrUnavailable) {
		t.Fatal("restore bypassed legacy process or inspected backup first", err)
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	input.Close()
	err = cmd.Wait()
	waited = true
	if err != nil {
		t.Fatal(err)
	}
	l, err := dirlease.Acquire(dir)
	if err != nil {
		t.Fatal("process exit did not release lease", err)
	}
	l.Close()
}
