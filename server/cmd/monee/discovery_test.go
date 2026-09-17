package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceConnectionFreshIdentity(t *testing.T) {
	first, err := newServiceConnection("http://127.0.0.1:4173")
	if err != nil {
		t.Fatal(err)
	}
	second, err := newServiceConnection(first.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(first.InstanceID)
	if err != nil || len(decoded) != 32 || first.InstanceID == second.InstanceID {
		t.Fatal("service instances must receive distinct random 256-bit identities")
	}
	if first.ServiceProtocol != 1 || first.BaseURL != "http://127.0.0.1:4173" {
		t.Fatal("unexpected discovery contract")
	}
}

func TestPublishConnectionPrivateAndReplacesStaleDiscovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connection.json")
	if err := os.WriteFile(path, []byte(`{"baseUrl":"http://127.0.0.1:4173"}`), 0644); err != nil {
		t.Fatal(err)
	}
	connection, err := newServiceConnection("http://127.0.0.1:4173")
	if err != nil {
		t.Fatal(err)
	}
	if err := publishConnection(dir, connection); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("discovery must be private: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var actual serviceConnection
	if err := json.Unmarshal(data, &actual); err != nil || actual != connection {
		t.Fatalf("discovery was not published intact: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 3 {
		t.Fatal("discovery must contain only URL, process identity, and protocol")
	}
	temps, _ := filepath.Glob(filepath.Join(dir, ".connection-*.tmp"))
	if len(temps) != 0 {
		t.Fatal("publication left temporary discovery files")
	}
}

func TestPublishFailureLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	// A directory cannot be replaced by a regular discovery file.
	if err := os.Mkdir(filepath.Join(dir, "connection.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := publishConnection(dir, serviceConnection{}); err == nil {
		t.Fatal("expected publication failure")
	}
	temps, _ := filepath.Glob(filepath.Join(dir, ".connection-*.tmp"))
	if len(temps) != 0 {
		t.Fatal("failed publication left a temporary file")
	}
}
