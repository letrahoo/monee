package httpapi

// This file is compiled only by go test. The production binary has no fake login mode.
import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

type browserFixtureProvider struct{ base, name string }

func (p browserFixtureProvider) AuthorizationURL(state, nonce, verifier string) string {
	return p.base + "/fixture/choose?" + url.Values{"state": {state}, "provider": {p.name}}.Encode()
}
func (p browserFixtureProvider) Exchange(_ context.Context, code, verifier, nonce string) (auth.Identity, error) {
	if verifier == "" || nonce == "" {
		return auth.Identity{}, errors.New("missing proof")
	}
	var id, name string
	switch code {
	case "fixture-admin":
		id, name = "1001", "synthetic-admin"
	case "fixture-member":
		id, name = "2002", "synthetic-member"
	case "fixture-denied":
		id, name = "3003", "synthetic-denied"
	default:
		return auth.Identity{}, errors.New("invalid fixture account")
	}
	return auth.Identity{Provider: p.name, Subject: id, Username: map[bool]string{true: name}[p.name == "github"], Email: map[bool]string{true: name + "@gmail.com"}[p.name == "google"], EmailTrusted: true, DisplayName: name}, nil
}
func TestBrowserAuthFixture(t *testing.T) {
	if os.Getenv("MONEE_AUTH_UI_TEST") != "1" {
		t.Skip("explicit isolated browser fixture only")
	}
	const base = "http://127.0.0.1:4175"
	dir := t.TempDir()
	data, e := ledger.Open(filepath.Join(dir, "application.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer data.Close()
	access, e := auth.Open(filepath.Join(dir, "application.db"), []auth.Selector{{Provider: "github", Kind: "subject", Value: "1001"}, {Provider: "google", Kind: "email", Value: "synthetic-admin@gmail.com"}})
	if e != nil {
		t.Fatal(e)
	}
	defer access.Close()
	for _, provider := range []string{"github", "google"} {
		i, e := (browserFixtureProvider{base, provider}).Exchange(context.Background(), "fixture-admin", "proof", "nonce")
		if e != nil {
			t.Fatal(e)
		}
		if e = access.RememberIdentity(i); e != nil {
			t.Fatal(e)
		}
		token, e := access.CreateSession(i)
		if e != nil {
			t.Fatal(e)
		}
		u, e := access.Session(token)
		if e != nil {
			t.Fatal(e)
		}
		l, e := data.CreateLedger(u.ID, provider+" 合成账本")
		if e != nil {
			t.Fatal(e)
		}
		scoped, e := data.Scoped(u.ID, l.ID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = scoped.Create(ledger.Input{Date: "2026-09-14", Type: "expense", Amount: "12.34", Currency: "CNY", Merchant: "合成权限验证", Source: "合成测试", Category: "测试"}, "auth-ui-fixture-0001"); e != nil {
			t.Fatal(e)
		}
	}
	providers := map[string]auth.Provider{"github": browserFixtureProvider{base, "github"}, "google": browserFixtureProvider{base, "google"}}
	handler := API{Store: data, Auth: auth.NewService(access, providers, base), Host: "127.0.0.1:4175", WebDir: os.Getenv("MONEE_AUTH_UI_WEB_DIR")}.Handler()
	chooser := template.Must(template.New("choose").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>合成登录验证</title><h1>隔离测试：选择合成账号</h1><p>仅验证应用内数据权限，不会登录 Google / GitHub。</p><ul><li><a href="{{.Admin}}">合成超管</a></li><li><a href="{{.Member}}">合成成员</a></li><li><a href="{{.Denied}}">合成未授权账号</a></li></ul></html>`))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fixture/choose", func(w http.ResponseWriter, r *http.Request) {
		link := func(code string) string {
			return "/auth/callback/" + r.URL.Query().Get("provider") + "?" + url.Values{"state": {r.URL.Query().Get("state")}, "code": {code}}.Encode()
		}
		chooser.Execute(w, map[string]string{"Admin": link("fixture-admin"), "Member": link("fixture-member"), "Denied": link("fixture-denied")})
	})
	mux.Handle("/", handler)
	listener, e := net.Listen("tcp", "127.0.0.1:4175")
	if e != nil {
		t.Fatal(e)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	fmt.Println("Isolated browser auth fixture ready at " + base)
	go server.Serve(listener)
	<-time.After(14 * time.Minute)
}
