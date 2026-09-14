package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/oauth2"
)

func TestGoogleSignedTokenValidation(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name   string
		change func(map[string]any)
		nonce  string
		valid  bool
	}{
		{"valid", func(map[string]any) {}, "nonce", true},
		{"audience", func(m map[string]any) { m["aud"] = "wrong-client" }, "nonce", false},
		{"issuer", func(m map[string]any) { m["iss"] = "https://attacker.invalid" }, "nonce", false},
		{"expired", func(m map[string]any) { m["exp"] = time.Now().Add(-time.Hour).Unix() }, "nonce", false},
		{"nonce", func(map[string]any) {}, "wrong-nonce", false},
		{"authorized-party", func(m map[string]any) { m["azp"] = "wrong-client" }, "nonce", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"iss": "https://accounts.google.com", "aud": "client", "sub": "9009", "nonce": "nonce", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "email": "owner.fixture@gmail.com", "email_verified": true, "name": "Synthetic Owner"}
			tc.change(claims)
			signed, e := jwt.Signed(signer).Claims(claims).Serialize()
			if e != nil {
				t.Fatal(e)
			}
			var form url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				form = r.Form
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"access_token": "test-provider-token", "token_type": "Bearer", "id_token": signed})
			}))
			defer server.Close()
			p := &remoteProvider{name: "google", client: server.Client(), config: oauth2.Config{ClientID: "client", ClientSecret: "synthetic-secret", RedirectURL: "http://127.0.0.1:4173/auth/callback/google", Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/authorize", TokenURL: server.URL, AuthStyle: oauth2.AuthStyleInParams}}, verifier: oidc.NewVerifier("https://accounts.google.com", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: "client", SupportedSigningAlgs: []string{"RS256"}})}
			identity, e := p.Exchange(context.Background(), "code", "verifier", tc.nonce)
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, e)
			}
			if tc.valid && (identity.Subject != "9009" || !identity.EmailTrusted) {
				t.Fatal("Google identity incorrect")
			}
			if form.Get("code_verifier") != "verifier" || form.Get("redirect_uri") != p.config.RedirectURL {
				t.Fatal("exchange not bound to verifier and callback")
			}
		})
	}
	// Cryptographic key mismatch is rejected independently of claim validation.
	other, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	signed, _ := jwt.Signed(signer).Claims(map[string]any{"iss": "https://accounts.google.com", "aud": "client", "sub": "9009", "exp": time.Now().Add(time.Hour).Unix()}).Serialize()
	v := oidc.NewVerifier("https://accounts.google.com", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&other.PublicKey}}, &oidc.Config{ClientID: "client"})
	if _, e = v.Verify(context.Background(), signed); e == nil {
		t.Fatal("bad signature accepted")
	}
}
func TestGitHubIdentityComesFromAuthenticatedAPI(t *testing.T) {
	for _, kind := range []string{"User", "Organization"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/token" {
					r.ParseForm()
					if r.Form.Get("code_verifier") != "proof" {
						t.Error("missing PKCE")
					}
					json.NewEncoder(w).Encode(map[string]string{"access_token": "synthetic-provider-token", "token_type": "bearer"})
					return
				}
				if r.Header.Get("Authorization") != "Bearer synthetic-provider-token" {
					t.Error("identity request not authenticated")
				}
				json.NewEncoder(w).Encode(map[string]any{"id": 2002, "login": "verified-name", "name": "Fixture", "type": kind})
			}))
			defer server.Close()
			p := &remoteProvider{name: "github", client: server.Client(), userURL: server.URL + "/user", config: oauth2.Config{ClientID: "client", ClientSecret: "secret", Endpoint: oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize", TokenURL: server.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}}}
			identity, e := p.Exchange(context.Background(), "code", "proof", "nonce")
			if kind == "User" {
				if e != nil || identity.Subject != "2002" || identity.Username != "verified-name" {
					t.Fatal(identity, e)
				}
			} else if e == nil {
				t.Fatal("organization accepted")
			}
			if !strings.Contains(p.AuthorizationURL("state", "nonce", "proof"), "code_challenge_method=S256") {
				t.Fatal("no PKCE")
			}
		})
	}
}

func TestRegisteredGitHubClientTypes(t *testing.T) {
	for _, kind := range []string{"", "oauth-app", "github-app"} {
		t.Run(kind, func(t *testing.T) {
			providers := NewProviders(Config{GitHub: GitHubConfig{ClientConfig: ClientConfig{ClientID: "synthetic-client", ClientSecret: "synthetic-secret"}, AppType: kind}}, "http://127.0.0.1:4173")
			u, err := url.Parse(providers["github"].AuthorizationURL("state", "nonce", "proof"))
			if err != nil {
				t.Fatal(err)
			}
			want := "read:user"
			if kind == "github-app" {
				want = ""
			}
			q := u.Query()
			if q.Get("scope") != want || q.Get("client_id") != "synthetic-client" || q.Get("redirect_uri") != "http://127.0.0.1:4173/auth/callback/github" || q.Get("code_challenge_method") != "S256" {
				t.Fatal("wrong authorization parameters", q)
			}
			if q.Has("client_secret") {
				t.Fatal("client secret exposed in redirect")
			}
		})
	}
}
