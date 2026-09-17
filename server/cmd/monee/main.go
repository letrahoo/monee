package main

import (
	"context"
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

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/httpapi"
	"github.com/letrahoo/monee/server/internal/redaction"
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
	authConfigPath := flag.String("auth-config", "", "private OAuth configuration file (defaults to auth.json in data directory)")
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
	if *authConfigPath == "" {
		*authConfigPath = filepath.Join(*dataDir, "auth.json")
	}
	config, err := auth.LoadConfig(*authConfigPath)
	if err != nil {
		return err
	}
	store, accessStore, err := openApplication(*dataDir, config)
	if err != nil {
		return err
	}
	defer accessStore.Close()
	defer store.Close()
	baseURL := "http://" + listener.Addr().String()
	access := auth.NewService(accessStore, auth.NewProviders(config, baseURL), baseURL)
	connection, err := newServiceConnection(baseURL)
	if err != nil {
		return err
	}
	// Discovery contains no account credentials. Both clients must complete OAuth.
	if err = publishConnection(*dataDir, connection); err != nil {
		return err
	}
	projector, keyErr := redaction.LoadOrCreate(*dataDir)
	if keyErr != nil {
		// Failure disables only this optional capability; never log the key or path.
		log.Print("Redaction preview unavailable: private key could not be loaded")
	}
	api := httpapi.API{Redaction: projector, Store: store, Auth: access, Host: listener.Addr().String(), WebDir: *webDir,
		InstanceID: connection.InstanceID, ServiceProtocol: connection.ServiceProtocol}
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
