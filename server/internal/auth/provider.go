package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Identity struct {
	Provider     string `json:"provider"`
	Subject      string `json:"subject"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	DisplayName  string `json:"displayName"`
	EmailTrusted bool   `json:"-"`
}

type Provider interface {
	AuthorizationURL(state, nonce, verifier string) string
	Exchange(context.Context, string, string, string) (Identity, error)
}

type remoteProvider struct {
	name     string
	config   oauth2.Config
	client   *http.Client
	verifier *oidc.IDTokenVerifier
	userURL  string
}

func NewProviders(c Config, baseURL string) map[string]Provider {
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("unexpected identity provider redirect") }}
	providers := map[string]Provider{}
	if c.Google.ClientID != "" {
		ctx := oidc.ClientContext(context.Background(), client)
		keys := oidc.NewRemoteKeySet(ctx, "https://www.googleapis.com/oauth2/v3/certs")
		providers["google"] = &remoteProvider{name: "google", client: client, config: oauth2.Config{
			ClientID: c.Google.ClientID, ClientSecret: c.Google.ClientSecret, RedirectURL: baseURL + "/auth/callback/google", Scopes: []string{"openid", "email", "profile"},
			Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams},
		}, verifier: oidc.NewVerifier("https://accounts.google.com", keys, &oidc.Config{ClientID: c.Google.ClientID, SupportedSigningAlgs: []string{"RS256"}})}
	}
	if c.GitHub.ClientID != "" {
		scopes := []string{"read:user"}
		// GitHub Apps use registration permissions, not OAuth App scopes.
		if c.GitHub.AppType == "github-app" {
			scopes = nil
		}
		providers["github"] = &remoteProvider{name: "github", client: client, userURL: "https://api.github.com/user", config: oauth2.Config{
			ClientID: c.GitHub.ClientID, ClientSecret: c.GitHub.ClientSecret, RedirectURL: baseURL + "/auth/callback/github", Scopes: scopes,
			Endpoint: oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams},
		}}
	}
	return providers
}
func (p *remoteProvider) AuthorizationURL(state, nonce, verifier string) string {
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "select_account")}
	if p.name == "google" {
		opts = append(opts, oidc.Nonce(nonce))
	}
	return p.config.AuthCodeURL(state, opts...)
}
func (p *remoteProvider) Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.client)
	token, err := p.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, errors.New("identity provider rejected authorization")
	}
	if p.name == "google" {
		raw, ok := token.Extra("id_token").(string)
		if !ok {
			return Identity{}, errors.New("missing Google ID token")
		}
		verified, err := p.verifier.Verify(ctx, raw)
		if err != nil {
			return Identity{}, errors.New("invalid Google ID token")
		}
		if subtle.ConstantTimeCompare([]byte(verified.Nonce), []byte(nonce)) != 1 {
			return Identity{}, errors.New("invalid Google nonce")
		}
		var claims struct {
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
			Name          string `json:"name"`
			HD            string `json:"hd"`
			AZP           string `json:"azp"`
		}
		if err = verified.Claims(&claims); err != nil {
			return Identity{}, errors.New("invalid Google claims")
		}
		if claims.AZP != "" && claims.AZP != p.config.ClientID {
			return Identity{}, errors.New("invalid Google authorized party")
		}
		if verified.AccessTokenHash != "" && verified.VerifyAccessToken(token.AccessToken) != nil {
			return Identity{}, errors.New("Google token hash mismatch")
		}
		email := strings.ToLower(strings.TrimSpace(claims.Email))
		trusted := claims.EmailVerified && (strings.HasSuffix(email, "@gmail.com") || strings.HasSuffix(email, "@googlemail.com") || claims.HD != "")
		return Identity{Provider: "google", Subject: verified.Subject, Email: email, DisplayName: claims.Name, EmailTrusted: trusted}, nil
	}
	var user struct {
		ID    json.Number `json:"id"`
		Login string      `json:"login"`
		Name  string      `json:"name"`
		Type  string      `json:"type"`
	}
	req, err := http.NewRequestWithContext(ctx, "GET", p.userURL, nil)
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	if err = readGitHub(p.client, req, &user); err != nil {
		return Identity{}, err
	}
	if !numericID.MatchString(user.ID.String()) || user.Login == "" || user.Type != "User" {
		return Identity{}, errors.New("invalid GitHub identity")
	}
	return Identity{Provider: "github", Subject: user.ID.String(), Username: user.Login, DisplayName: user.Name}, nil
}

var numericID = regexp.MustCompile(`^[1-9][0-9]{0,254}$`)
var githubLogin = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

func readGitHub(client *http.Client, req *http.Request, target any) error {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Monee")
	response, err := client.Do(req)
	if err != nil {
		return errors.New("无法连接 GitHub，请稍后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("GitHub 账号查询失败（%d），请检查用户名或稍后重试", response.StatusCode)
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target) != nil {
		return errors.New("GitHub 返回无效账号信息")
	}
	return nil
}
func ResolveGitHub(ctx context.Context, name string) (string, string, error) {
	if !githubLogin.MatchString(name) {
		return "", "", errors.New("GitHub 用户名格式无效")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/users/"+url.PathEscape(name), nil)
	var user struct {
		ID    json.Number `json:"id"`
		Login string      `json:"login"`
		Type  string      `json:"type"`
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("请使用当前 GitHub 用户名") }}
	if err := readGitHub(client, req, &user); err != nil {
		return "", "", err
	}
	if user.Type != "User" || !numericID.MatchString(user.ID.String()) || !strings.EqualFold(user.Login, name) {
		return "", "", errors.New("请选择有效 GitHub 个人账号")
	}
	return user.ID.String(), user.Login, nil
}
