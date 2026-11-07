//go:build retained_v5

package ledger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/letrahoo/monee/server/internal/dirlease"
	"github.com/letrahoo/monee/server/internal/storage"
)

const retainedV5Revision = "dab74a5fdcfb403c47900dbb21023540517978d2"
const retainedV5Tree = "e883e849f2359262b6a6b7c9b3f15b85026e60fa"

type retainedV5Manifest struct {
	Format    int    `json:"format"`
	Revision  string `json:"revision"`
	Tree      string `json:"tree"`
	GoVersion string `json:"goVersion"`
	Artifacts []struct {
		Name      string `json:"name"`
		SHA256    string `json:"sha256"`
		BuildInfo string `json:"buildInfo"`
	} `json:"artifacts"`
}

func retainedV5Programs(t *testing.T) map[string]string {
	t.Helper()
	root := os.Getenv("MONEE_RETAINED_V5_DIR")
	if root == "" {
		t.Fatal("required genuine-v5 fixture missing; run scripts/verify-retained-v5.sh (never skip this suite)")
	}
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest retainedV5Manifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Format != 1 || manifest.Revision != retainedV5Revision || manifest.Tree != retainedV5Tree || !strings.HasPrefix(manifest.GoVersion, "go version ") || len(manifest.Artifacts) != 3 {
		t.Fatal("wrong retained fixture identity")
	}
	programs := map[string]string{}
	for _, artifact := range manifest.Artifacts {
		switch artifact.Name {
		case "monee-v5", "database-backup-v5", "fixed-schema5-tests":
		default:
			t.Fatal("unexpected artifact name")
		}
		if _, duplicate := programs[artifact.Name]; duplicate {
			t.Fatal("duplicate retained artifact")
		}
		path := filepath.Join(root, artifact.Name)
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() {
			t.Fatal("artifact is not a regular file", e)
		}
		binary, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		digest := sha256.Sum256(binary)
		if hex.EncodeToString(digest[:]) != artifact.SHA256 || artifact.BuildInfo == "" {
			t.Fatal("retained binary digest mismatch")
		}
		embedded, e := buildinfo.ReadFile(path)
		if e != nil || embedded.GoVersion == "" {
			t.Fatal("missing embedded build information", e)
		}
		settings := map[string]string{}
		for _, setting := range embedded.Settings {
			settings[setting.Key] = setting.Value
		}
		if embedded.Main.Path != "github.com/letrahoo/monee/server" || settings["vcs.revision"] != retainedV5Revision || settings["vcs.modified"] != "false" {
			t.Fatal("binary is not an unmodified pinned checkout", artifact.Name)
		}
		programs[artifact.Name] = path
	}
	return programs
}

func retainedV5Command(t *testing.T, program string, args ...string) (*exec.Cmd, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, program, args...)
	// Do not inherit provider configuration, tokens, real data paths or discovery.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "XDG_CONFIG_HOME=" + t.TempDir(), "TMPDIR=" + os.TempDir()}
	return cmd, cancel
}

func retainedV5Address(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	// The old CLI requires a nonzero port. A port race is a test failure, never a
	// passing negative control: negative runs must return the exact schema error.
	return address
}

func retainedV5PositiveControl(t *testing.T, program, dir string) {
	t.Helper()
	address := retainedV5Address(t)
	cmd, cancel := retainedV5Command(t, program, "-data-dir", dir, "-listen", address)
	defer cancel()
	var log bytes.Buffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	defer func() {
		if !finished {
			cancel()
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	client := http.Client{Timeout: 500 * time.Millisecond}
	ready := false
	for !ready {
		select {
		case err := <-done:
			finished = true
			t.Fatalf("old v5 positive control exited: %v %s", err, log.String())
		case <-deadline.C:
			t.Fatal("old v5 positive control did not become healthy")
		case <-ticker.C:
			response, err := client.Get("http://" + address + "/api/v1/health")
			if err != nil {
				continue
			}
			var health struct {
				Status        string `json:"status"`
				SchemaVersion int    `json:"schemaVersion"`
			}
			err = json.NewDecoder(response.Body).Decode(&health)
			response.Body.Close()
			ready = err == nil && response.StatusCode == 200 && health.Status == "ok" && health.SchemaVersion == 5
		}
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		finished = true
		t.Fatalf("old v5 shutdown: %v %s", err, log.String())
	}
	finished = true
	if !strings.Contains(log.String(), "Monee ledger ready at ") {
		t.Fatal("positive control never announced readiness")
	}
}

func retainedV5Digest(t *testing.T, path string) [32]byte {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := expensePairUpgradeDigest(db)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	return digest
}

func TestRetainedV5BinaryBoundary(t *testing.T) {
	programs := retainedV5Programs(t)
	t.Run("immutable-old-ledger-auth-restore-fences", func(t *testing.T) {
		cmd, cancel := retainedV5Command(t, programs["fixed-schema5-tests"], "-test.run", "^TestCurrentSchema5RejectsFixedSchema6$", "-test.v")
		defer cancel()
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "--- PASS: TestCurrentSchema5RejectsFixedSchema6") {
			t.Fatalf("genuine old fence test: %v %s", err, output)
		}
	})
	t.Run("genuine-service-rejects-real-offline-upgrade", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "application.db")
		store, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.Close(); err != nil {
			t.Fatal(err)
		}
		retainedV5PositiveControl(t, programs["monee-v5"], dir)
		discoveryBefore, err := os.ReadFile(filepath.Join(dir, "connection.json"))
		if err != nil {
			t.Fatal(err)
		}
		lease, err := dirlease.Acquire(dir)
		if err != nil {
			t.Fatal(err)
		}
		backup, err := upgradeExpensePairOffline(context.Background(), lease, dir, "before-upgrade")
		closeErr := lease.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		upgradeVersion(t, dir, 6)
		before := retainedV5Digest(t, path)
		// Explicitly prove the lease is released. A retained service failing on its
		// flock would say nothing about its schema6 protection.
		probe, err := dirlease.Acquire(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err = probe.Close(); err != nil {
			t.Fatal(err)
		}
		cmd, cancel := retainedV5Command(t, programs["monee-v5"], "-data-dir", dir, "-listen", retainedV5Address(t))
		defer cancel()
		output, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "数据库来自更新版本，拒绝以旧程序写入") || strings.Contains(string(output), "Monee ledger ready at ") {
			t.Fatalf("not the old schema fence: %v %s", err, output)
		}
		if after := retainedV5Digest(t, path); before != after {
			t.Fatal("old service changed upgraded schema/data/audit")
		}
		upgradeVersion(t, dir, 6)
		discoveryAfter, err := os.ReadFile(filepath.Join(dir, "connection.json"))
		if err != nil || !bytes.Equal(discoveryBefore, discoveryAfter) {
			t.Fatal("old rejection replaced discovery", err)
		}
		matches, err := filepath.Glob(path + ".backup-before-*")
		if err != nil || len(matches) != 0 {
			t.Fatal("old program created migration backup", matches, err)
		}
		archived := filepath.Join(t.TempDir(), "archive-six")
		snapshot, stop := retainedV5Command(t, programs["database-backup-v5"], "-source", path, "-out", archived)
		output, err = snapshot.CombinedOutput()
		stop()
		if err != nil {
			t.Fatalf("old read-only archival: %v %s", err, output)
		}
		if version, e := storage.VerifySnapshot(archived); e != nil || version != 6 {
			t.Fatal(version, e)
		}
		target := filepath.Join(t.TempDir(), "rejected.db")
		restore, stop := retainedV5Command(t, programs["database-backup-v5"], "-restore", archived, "-out", target)
		output, err = restore.CombinedOutput()
		stop()
		if err == nil || !strings.Contains(string(output), "newer application") {
			t.Fatalf("old restore future fence: %v %s", err, output)
		}
		if _, e := os.Stat(target); !os.IsNotExist(e) {
			t.Fatal("old restore left a target", e)
		}
		if retainedV5Digest(t, path) != before {
			t.Fatal("old archival/restore changed source")
		}
		// Rollback is a v5 backup restored into a fresh target, never lowering the
		// upgraded source's version or overwriting it with an old binary.
		rollbackDir := t.TempDir()
		if err = storage.Restore(backup, filepath.Join(rollbackDir, "application.db")); err != nil {
			t.Fatal(err)
		}
		retainedV5PositiveControl(t, programs["monee-v5"], rollbackDir)
		upgradeVersion(t, dir, 6)
		t.Log(fmt.Sprintf("pinned revision %s: v5 positive, upgraded6 rejection, archive6, restore rejection, v5 rollback", retainedV5Revision))
	})
}
