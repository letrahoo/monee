package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/dirlease"
	"github.com/letrahoo/monee/server/internal/httpapi"
	"github.com/letrahoo/monee/server/internal/ledger"
	"github.com/letrahoo/monee/server/internal/storage"
)

// Exercise the normal startup coordinator, not a private ledger test opener or
// a PRAGMA-only capability switch. Every pre-upgrade connection is closed.
func upgradeApplicationForRuntimeTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	lease, err := dirlease.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	l, a, err := openApplication(dir, auth.Config{}, lease)
	if err != nil {
		lease.Close()
		t.Fatal(err)
	}
	if v, err := l.SchemaVersion(); err != nil || v != 5 {
		t.Fatal(v, err)
	}
	identity := auth.Identity{Provider: "github", Subject: "7001", Username: "synthetic-runtime"}
	if err = a.RememberIdentity(identity); err != nil {
		t.Fatal(err)
	}
	if _, err = a.CreateSession(identity); err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	lease, err = dirlease.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	backup, upgradeErr := ledger.UpgradeExpensePairOffline(context.Background(), lease, dir, "before-runtime6")
	closeErr := lease.Close()
	if upgradeErr != nil || closeErr != nil {
		t.Fatal(upgradeErr, closeErr)
	}
	if v, err := storage.VerifySnapshot(backup); err != nil || v != 5 {
		t.Fatal(v, err)
	}
	return dir
}

func TestExpensePairNormalApplicationRuntime6(t *testing.T) {
	dir := upgradeApplicationForRuntimeTest(t)
	lease, err := dirlease.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	l, a, err := openApplication(dir, auth.Config{}, lease)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	defer a.Close()
	if v, err := l.SchemaVersion(); err != nil || v != 6 {
		t.Fatal("auth downgraded runtime schema", v, err)
	}
	const host = "127.0.0.1:4173"
	h := httpapi.API{Store: l, Host: host}.Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/health", nil))
	var result struct {
		Status string `json:"status"`
		Schema int    `json:"schemaVersion"`
		Max    int    `json:"maxSupportedSchemaVersion"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || result.Status != "ok" || result.Schema != 6 || result.Max != 6 {
		t.Fatal(w.Code, w.Body.String())
	}
	// A second normal startup also preserves 6 and validates the same DB.
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	l, a, err = openApplication(dir, auth.Config{}, lease)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	defer a.Close()
	if v, err := l.SchemaVersion(); err != nil || v != 6 {
		t.Fatal(v, err)
	}
}

func TestExpensePairBadRuntime6FailsBeforeAuthWrites(t *testing.T) {
	dir := upgradeApplicationForRuntimeTest(t)
	path := filepath.Join(dir, "application.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// If auth initialization were reached it would both bootstrap the supplied
	// administrator and remove this expired session. Structural corruption is
	// introduced only after a real, completed offline transition to schema 6.
	if _, err = db.Exec("DROP INDEX expense_pair_members_one_active_transaction; UPDATE sessions SET expires_at=1"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	before := filepath.Join(t.TempDir(), "before")
	if err = storage.Snapshot(path, before); err != nil {
		t.Fatal(err)
	}
	state := runtimeAuthSnapshotState(t, before)
	lease, err := dirlease.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	config := auth.Config{Superadmins: []auth.Selector{{Provider: "github", Kind: "subject", Value: "8002"}}}
	l, a, err := openApplication(dir, config, lease)
	if l != nil {
		l.Close()
		t.Fatal("bad runtime6 returned ledger")
	}
	if a != nil {
		a.Close()
		t.Fatal("bad runtime6 returned auth")
	}
	var p *ledger.Problem
	if !errors.As(err, &p) || p.Code != "expense_pair_unavailable" {
		t.Fatal("expected ledger validation failure before auth", err)
	}
	after := filepath.Join(t.TempDir(), "after")
	if err = storage.Snapshot(path, after); err != nil {
		t.Fatal(err)
	}
	if actual := runtimeAuthSnapshotState(t, after); actual != state {
		t.Fatal("failed ledger startup changed auth tables", state, actual)
	}
	if v, err := storage.VerifySnapshot(after); err != nil || v != 6 {
		t.Fatal(v, err)
	}
}

func runtimeAuthSnapshotState(t *testing.T, bundle string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(bundle, "database.sqlite3")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state := make(map[string][][]any)
	for _, table := range []string{"auth_meta", "users", "allowlist", "sessions", "access_audit", "app_users", "user_identities", "user_access"} {
		rows, err := db.Query("SELECT * FROM " + table + " ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			state[table] = append(state[table], values)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
