package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundlePrivacyAndNoOverwrite(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.json")
	out := filepath.Join(root, "bundle")
	// An archived row still has to survive intact in the source archive.
	raw := `{"schemaVersion":"1.0","audit":{"totalTransactions":1,"transactions":[{"transactionId":"synthetic-1","amount":1.00,"isCanonical":false}]},"unknownFutureEvidence":{"kept":true}}`
	if err := os.WriteFile(source, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(source, out); err != nil {
		t.Fatal(err)
	}
	copy, err := os.ReadFile(filepath.Join(out, "source-snapshot.json"))
	if err != nil || string(copy) != raw {
		t.Fatal("source archive changed", err)
	}
	for _, name := range []string{"manifest.json", "source-snapshot.json", "README.txt"} {
		info, err := os.Stat(filepath.Join(out, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("private file mode", name, err)
		}
	}
	info, err := os.Stat(out)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("private directory mode", err)
	}
	if err := run(source, out); err == nil {
		t.Fatal("overwrote existing bundle")
	}
	after, _ := os.ReadFile(filepath.Join(out, "source-snapshot.json"))
	if string(after) != raw {
		t.Fatal("existing bundle was changed")
	}
	if err := run(source, filepath.Join(root, "../missing-parent/bundle")); err == nil {
		t.Fatal("unexpected missing parent success")
	}
	if err := run("", out); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatal("missing flags accepted")
	}
}
