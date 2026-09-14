package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/letrahoo/monee/server/internal/httpapi"
	"github.com/letrahoo/monee/server/internal/ledger"
)

func run() error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	defaultData := os.Getenv("MONEE_DATA_DIR")
	if defaultData == "" {
		defaultData = filepath.Join(base, "Monee")
	}
	dataDir := flag.String("data-dir", defaultData, "private local ledger directory")
	listen := flag.String("listen", "127.0.0.1:4173", "IPv4 loopback address")
	webDir := flag.String("web-dir", "", "built Web asset directory")
	flag.Parse()
	host, port, err := net.SplitHostPort(*listen)
	if err != nil || host != "127.0.0.1" || port == "0" {
		return errors.New("listen must be 127.0.0.1 with a fixed non-zero port")
	}
	*dataDir, err = filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(*dataDir, 0700); err != nil {
		return err
	}
	if err = os.Chmod(*dataDir, 0700); err != nil {
		return err
	}
	unlock, err := lockDirectory(*dataDir)
	if err != nil {
		return err
	}
	defer unlock()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("local address is already in use: %w", err)
	}
	defer listener.Close()
	store, err := ledger.Open(filepath.Join(*dataDir, "monee.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	tokenBytes := make([]byte, 32)
	if _, err = rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)
	connection, _ := json.Marshal(map[string]string{"baseUrl": "http://" + listener.Addr().String(), "token": token})
	// Rotate on startup. Same-user native clients rediscover credentials on reconnect.
	path := filepath.Join(*dataDir, "connection.json")
	if err = os.WriteFile(path+".tmp", connection, 0600); err != nil {
		return err
	}
	if err = os.Chmod(path+".tmp", 0600); err != nil {
		return err
	}
	if err = os.Rename(path+".tmp", path); err != nil {
		return err
	}
	api := httpapi.API{Store: store, Token: token, Host: listener.Addr().String(), WebDir: *webDir}
	server := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	log.Printf("Monee local ledger ready at http://%s", listener.Addr())
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
