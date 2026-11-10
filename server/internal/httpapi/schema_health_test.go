package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/dirlease"
	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestSchemaHealthReportsActualVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "application.db")
	s, err := ledger.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	checkSchemaHealth(t, s, 5)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	lease, err := dirlease.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, upgradeErr := ledger.UpgradeExpensePairOffline(context.Background(), lease, dir, "before-expense-pairs")
	closeErr := lease.Close()
	if upgradeErr != nil || closeErr != nil {
		t.Fatal(upgradeErr, closeErr)
	}
	s, err = ledger.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	checkSchemaHealth(t, s, 6)
}

func checkSchemaHealth(t *testing.T, s *ledger.Store, version int) {
	t.Helper()
	h := API{Store: s, Host: host, InstanceID: "synthetic-instance", ServiceProtocol: 1}.Handler()
	w := request(h, http.MethodGet, "/api/v1/health", "", nil)
	var result struct {
		Status   string `json:"status"`
		Schema   int    `json:"schemaVersion"`
		Max      int    `json:"maxSupportedSchemaVersion"`
		Instance string `json:"instanceId"`
		Protocol int    `json:"serviceProtocol"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || result.Status != "ok" || result.Schema != version || result.Max != 6 || result.Instance != "synthetic-instance" || result.Protocol != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSchemaHealthUnavailableForClosedStore(t *testing.T) {
	s, err := ledger.Open(filepath.Join(t.TempDir(), "application.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, store := range []*ledger.Store{s, nil} {
		h := API{Store: store, Host: host}.Handler()
		w := request(h, http.MethodGet, "/api/v1/health", "", nil)
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusServiceUnavailable || result["status"] == "ok" || result["code"] != "schema_unavailable" {
			t.Fatal(w.Code, w.Body.String())
		}
		if _, ok := result["schemaVersion"]; ok {
			t.Fatal("unavailable health fabricated a schema version", result)
		}
	}
}
