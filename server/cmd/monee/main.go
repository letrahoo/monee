package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
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
	listen := flag.String("listen", "127.0.0.1:4173", "IPv4 listen address")
	publicURL := flag.String("public-url", os.Getenv("MONEE_PUBLIC_URL"), "browser-visible origin, required when listening outside loopback")
	webDir := flag.String("web-dir", "", "built Web asset directory")
	authConfigPath := flag.String("auth-config", "", "private OAuth configuration file (defaults to auth.json in data directory)")
	flag.Parse()
	host, port, err := net.SplitHostPort(*listen)
	if err != nil || (host != "127.0.0.1" && host != "0.0.0.0") || port == "0" {
		return errors.New("listen must use 127.0.0.1 or 0.0.0.0 with a fixed non-zero port")
	}
	baseURL, requestHost, err := deploymentURLs(host, port, *publicURL)
	if err != nil {
		return err
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
	api := httpapi.API{Redaction: projector, Store: store, Auth: access, Host: requestHost, Origin: baseURL, WebDir: *webDir,
		InstanceID: connection.InstanceID, ServiceProtocol: connection.ServiceProtocol}
	server := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	log.Printf("Monee ledger ready at %s", baseURL)
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

func deploymentURLs(listenHost, listenPort, configured string) (string, string, error) {
	if configured == "" {
		if listenHost != "127.0.0.1" {
			return "", "", errors.New("public-url is required when listening outside loopback")
		}
		configured = "http://" + net.JoinHostPort(listenHost, listenPort)
	}
	u, err := url.Parse(configured)
	if err != nil || !u.IsAbs() || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", "", errors.New("public-url must be an absolute origin without credentials, path, query, or fragment")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "https" && !(u.Scheme == "http" && u.Hostname() == "127.0.0.1") {
		return "", "", errors.New("public-url must use HTTPS except for 127.0.0.1")
	}
	// Browsers serialize origins with lowercase hosts and no default port.
	// Use the same spelling for request validation, OAuth and health checks.
	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return "", "", errors.New("public-url must have a hostname")
	}
	port := u.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", "", errors.New("public-url has an invalid port")
		}
		port = strconv.Itoa(number)
	}
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	u.Host = hostname
	if port != "" {
		u.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		u.Host = "[" + hostname + "]"
	}
	u.Path = ""
	return u.String(), u.Host, nil
}
func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if err := checkHealth(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func checkHealth() error {
	publicURL := os.Getenv("MONEE_PUBLIC_URL")
	_, requestHost, err := deploymentURLs("0.0.0.0", "4173", publicURL)
	if err != nil {
		return errors.New("MONEE_PUBLIC_URL is required for the container healthcheck")
	}
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:4173/api/v1/health", nil)
	if err != nil {
		return err
	}
	req.Host = requestHost
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("unexpected redirect") }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", resp.Status)
	}
	return nil
}
