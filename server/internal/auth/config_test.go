package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateConfigValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth.json")
	if c, err := LoadConfig(p); err != nil || c.GitHub.ClientID != "" {
		t.Fatal("missing configuration must disable providers", err)
	}
	for _, tc := range []struct {
		name, body string
		mode       os.FileMode
		valid      bool
	}{
		{"github-app", `{"github":{"clientId":"synthetic-client","clientSecret":"synthetic-secret","appType":"github-app"}}`, 0600, true},
		{"oauth-app-default", `{"github":{"clientId":"synthetic-client","clientSecret":"synthetic-secret"}}`, 0600, true},
		{"readable-by-others", `{}`, 0644, false},
		{"partial-credentials", `{"google":{"clientId":"synthetic-client"}}`, 0600, false},
		{"service-account", `{"type":"service_account","private_key":"synthetic-not-a-key"}`, 0600, false},
		{"unknown-type", `{"github":{"appType":"installation-token"}}`, 0600, false},
		{"trailing-json", `{} {}`, 0600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(p, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(p, tc.mode); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(p)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
