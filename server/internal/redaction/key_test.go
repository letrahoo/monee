package redaction

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPersistentKeyAcrossRestartsAndConcurrentCreation(t *testing.T) {
	dir := privateDir(t)
	const workers = 8
	payloads := make(chan []byte, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := LoadOrCreate(dir)
			if err != nil {
				errs <- err
				return
			}
			preview, err := p.Project("ledger", []Input{fixture()})
			if err != nil {
				errs <- err
				return
			}
			payloads <- preview.Bytes()
		}()
	}
	wg.Wait()
	close(errs)
	close(payloads)
	for err := range errs {
		t.Fatal(err)
	}
	var first []byte
	for data := range payloads {
		if first == nil {
			first = data
		} else if !bytes.Equal(first, data) {
			t.Fatal("concurrent key rotation")
		}
	}
	p, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := p.Project("ledger", []Input{fixture()})
	if err != nil || !bytes.Equal(first, restarted.Bytes()) {
		t.Fatal("restart changed aliases", err)
	}
	info, err := os.Stat(filepath.Join(dir, "redaction.key"))
	if err != nil || info.Mode().Perm() != 0600 || info.Size() != 32 {
		t.Fatal("key permissions or length", info, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".redaction-key-*"))
	if len(leftovers) != 0 {
		t.Fatal("temporary keys retained")
	}
	other, err := LoadOrCreate(privateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	distinct, _ := other.Project("ledger", []Input{fixture()})
	if bytes.Equal(first, distinct.Bytes()) {
		t.Fatal("independent installs reuse key")
	}
}

func TestPersistentKeyRejectsInvalidFilesWithoutReplacing(t *testing.T) {
	for _, name := range []string{"short", "long", "public", "symlink", "directory"} {
		t.Run(name, func(t *testing.T) {
			dir := privateDir(t)
			path := filepath.Join(dir, "redaction.key")
			key := bytes.Repeat([]byte{77}, 32)
			switch name {
			case "short":
				key = key[:31]
			case "long":
				key = append(key, 1)
			}
			if name == "directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if name == "symlink" {
				target := filepath.Join(dir, "target")
				if err := os.WriteFile(target, key, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, key, 0600); err != nil {
					t.Fatal(err)
				}
				if name == "public" {
					if err := os.Chmod(path, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, _ := os.Lstat(path)
			if p, err := LoadOrCreate(dir); p != nil || err == nil {
				t.Fatal("unsafe key accepted")
			}
			after, _ := os.Lstat(path)
			if !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("bad key silently replaced")
			}
			if name != "directory" {
				saved, _ := os.ReadFile(path)
				if !bytes.Equal(saved, key) {
					t.Fatal("key mutated")
				}
			}
		})
	}
	dir := privateDir(t)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("public directory accepted")
	}
}
