package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

const serviceProtocol = 1

// serviceConnection identifies a running service, not an authenticated account.
// Clients must match its identity against health before using the advertised URL.
type serviceConnection struct {
	BaseURL         string `json:"baseUrl"`
	InstanceID      string `json:"instanceId"`
	ServiceProtocol int    `json:"serviceProtocol"`
}

func newServiceConnection(baseURL string) (serviceConnection, error) {
	var identity [32]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return serviceConnection{}, err
	}
	return serviceConnection{BaseURL: baseURL, InstanceID: hex.EncodeToString(identity[:]), ServiceProtocol: serviceProtocol}, nil
}

// publishConnection is called only while owning the directory lock and listening
// socket. A competing startup therefore cannot overwrite the owner's discovery.
// A crash may leave stale discovery; instance matching makes it safe to reject.
func publishConnection(dir string, connection serviceConnection) error {
	data, err := json.Marshal(connection)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".connection-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = file.Chmod(0600); err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(dir, "connection.json"))
}
