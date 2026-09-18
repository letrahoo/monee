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
		{"expanded IPv6", "0.0.0.0", "4173", "https://[2001:0DB8:0:0:0:0:0:1]:443", "https://[2001:db8::1]", "[2001:db8::1]", false},
		{"IPv6 nondefault port", "0.0.0.0", "4173", "https://[2001:0db8::1]:8443", "https://[2001:db8::1]:8443", "[2001:db8::1]:8443", false},
		{"Unicode IDN rejected", "0.0.0.0", "4173", "https://bücher.example", "", "", true},
		{"Unicode case fold rejected", "0.0.0.0", "4173", "https://İ.example", "", "", true},
		{"invalid ACE rejected", "0.0.0.0", "4173", "https://xn--a.example", "", "", true},
		{"punycode accepted", "0.0.0.0", "4173", "https://XN--BCHER-KVA.Example:443", "https://xn--bcher-kva.example", "xn--bcher-kva.example", false},
		{"IPv4 mapped rejected", "0.0.0.0", "4173", "https://[::ffff:192.0.2.1]", "", "", true},
		{"IPv6 zone rejected", "0.0.0.0", "4173", "https://[fe80::1%25en0]", "", "", true},
		{"canonical IPv4", "0.0.0.0", "4173", "https://192.0.2.1", "https://192.0.2.1", "192.0.2.1", false},
		{"abbreviated IPv4 rejected", "0.0.0.0", "4173", "https://127.1", "", "", true},
		{"octal IPv4 rejected", "0.0.0.0", "4173", "https://0177.0.0.1", "", "", true},
		{"hex IPv4 rejected", "0.0.0.0", "4173", "https://0x7f000001", "", "", true},
		{"overflow numeric host rejected", "0.0.0.0", "4173", "https://99999999999999999999999999999999999999", "", "", true},
		{"invalid DNS rejected", "0.0.0.0", "4173", "https://-example.com", "", "", true},
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
