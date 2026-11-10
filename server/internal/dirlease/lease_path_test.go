//go:build darwin || linux

package dirlease

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithinSuppliesCanonicalDirectoryDespiteAliasRetarget(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	links := t.TempDir()
	alias := filepath.Join(links, "alias")
	if err := os.Symlink(a, alias); err != nil {
		t.Fatal(err)
	}
	lease, err := Acquire(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err = lease.Within(alias, func(owned string) error {
		// Swap only a synthetic directory alias after lease validation.
		replacement := filepath.Join(links, "replacement")
		if e := os.Symlink(b, replacement); e != nil {
			return e
		}
		if e := os.Rename(replacement, alias); e != nil {
			return e
		}
		return os.WriteFile(filepath.Join(owned, "marker"), []byte("owned"), 0600)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(a, "marker")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(b, "marker")); !os.IsNotExist(err) {
		t.Fatal("operation followed retargeted alias", err)
	}
}
