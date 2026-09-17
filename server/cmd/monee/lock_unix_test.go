//go:build darwin || linux

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDirectoryLockRelease(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := lockDirectory(dir); err == nil {
		other()
		unlock()
		t.Fatal("duplicate directory ownership was permitted")
	}
	unlock()
	unlock, err = lockDirectory(dir)
	if err != nil {
		t.Fatalf("normal shutdown did not release directory lock: %v", err)
	}
	unlock()
}

func TestDirectoryLockSubprocess(t *testing.T) {
	if os.Getenv("MONEE_TEST_LOCK_HELPER") == "1" {
		dir := os.Getenv("MONEE_TEST_LOCK_DIR")
		// Deliberately do not unlock: process exit must release the kernel lock.
		heldUnlock, err := lockDirectory(dir)
		if err != nil {
			os.Exit(2)
		}
		connection, err := newServiceConnection("http://127.0.0.1:4173")
		if err != nil || publishConnection(dir, connection) != nil {
			os.Exit(3)
		}
		fmt.Println("locked")
		_, _ = io.Copy(io.Discard, os.Stdin)
		runtime.KeepAlive(heldUnlock)
		os.Exit(0)
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDirectoryLockSubprocess$")
	cmd.Env = append(os.Environ(), "MONEE_TEST_LOCK_HELPER=1", "MONEE_TEST_LOCK_DIR="+dir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill() })
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "locked\n" {
		t.Fatalf("helper did not acquire lock: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "connection.json"))
	if err != nil {
		t.Fatal(err)
	}
	if unlock, err := lockDirectory(dir); err == nil {
		unlock()
		t.Fatal("a separate process already owns this data directory")
	}
	after, err := os.ReadFile(filepath.Join(dir, "connection.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("competing startup changed discovery")
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockDirectory(dir)
	if err != nil {
		t.Fatalf("process exit did not release lock: %v", err)
	}
	unlock()
}
