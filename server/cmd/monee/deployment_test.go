package main

import "testing"

func TestDeploymentURLs(t *testing.T) {
	tests := []struct {
		name, host, port, configured, wantURL, wantHost string
		wantErr                                         bool
	}{
		{"local default", "127.0.0.1", "4173", "", "http://127.0.0.1:4173", "127.0.0.1:4173", false},
		{"production", "0.0.0.0", "4173", "https://finance.letra.xin", "https://finance.letra.xin", "finance.letra.xin", false},
		{"production port", "0.0.0.0", "4173", "https://example.com:8443", "https://example.com:8443", "example.com:8443", false},
		{"canonical HTTPS", "0.0.0.0", "4173", "HTTPS://Finance.Example:443/", "https://finance.example", "finance.example", false},
		{"canonical local HTTP", "127.0.0.1", "80", "HTTP://127.0.0.1:80", "http://127.0.0.1", "127.0.0.1", false},
		{"canonical nondefault port", "0.0.0.0", "4173", "https://Finance.Example:08443", "https://finance.example:8443", "finance.example:8443", false},
		{"IPv6 HTTPS", "0.0.0.0", "4173", "https://[2001:DB8::1]:443", "https://[2001:db8::1]", "[2001:db8::1]", false},
		{"invalid port", "0.0.0.0", "4173", "https://example.com:65536", "", "", true},
		{"zero port", "0.0.0.0", "4173", "https://example.com:0", "", "", true},
		{"empty query rejected", "0.0.0.0", "4173", "https://example.com?", "", "", true},
		{"missing public URL", "0.0.0.0", "4173", "", "", "", true},
		{"insecure production", "0.0.0.0", "4173", "http://finance.letra.xin", "", "", true},
		{"path rejected", "0.0.0.0", "4173", "https://finance.letra.xin/app", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotHost, err := deploymentURLs(tt.host, tt.port, tt.configured)
			if (err != nil) != tt.wantErr || gotURL != tt.wantURL || gotHost != tt.wantHost {
				t.Fatalf("deploymentURLs() = %q, %q, %v", gotURL, gotHost, err)
			}
		})
	}
}
