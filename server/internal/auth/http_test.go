package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeProvider struct{ identity Identity }

func (p fakeProvider) AuthorizationURL(state, nonce, verifier string) string {
	return "https://identity.invalid/authorize?" + url.Values{"state": {state}, "nonce": {nonce}, "code_challenge": {digest(verifier)}, "code_challenge_method": {"S256"}}.Encode()
}
func (p fakeProvider) Exchange(_ context.Context, code, verifier, nonce string) (Identity, error) {
	if code != "test-code" || verifier == "" || nonce == "" {
		return Identity{}, errDenied
	}
	return p.identity, nil
}
func serviceForTest(t *testing.T) (*Service, http.Handler) {
	s := NewService(testStore(t), map[string]Provider{"github": fakeProvider{rootIdentity}}, "http://127.0.0.1:4173")
	mux := http.NewServeMux()
	s.Register(mux)
	return s, mux
}

func TestProductionServiceRequiresSecureCookies(t *testing.T) {
	if NewService(testStore(t), nil, "https://finance.letra.xin").secureCookies != true {
		t.Fatal("HTTPS service did not enable secure cookies")
	}
	if NewService(testStore(t), nil, "http://127.0.0.1:4173").secureCookies {
		t.Fatal("loopback HTTP unexpectedly enabled secure cookies")
	}
}
func call(h http.Handler, method, path string, body any, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	data := ""
	if body != nil {
		b, _ := json.Marshal(body)
		data = string(b)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type startedFlow struct {
	id, state string
	cookie    *http.Cookie
}

func startFlow(t *testing.T, h http.Handler, client, challenge string) startedFlow {
	t.Helper()
	w := call(h, "POST", "/api/v1/auth/start", map[string]string{"provider": "github", "client": client, "challenge": challenge}, nil, nil)
	if w.Code != 200 {
		t.Fatalf("start %d %s", w.Code, w.Body.String())
	}
	var start struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	json.Unmarshal(w.Body.Bytes(), &start)
	w = call(h, "GET", start.URL, nil, nil, nil)
	if w.Code != 303 {
		t.Fatalf("begin %d", w.Code)
	}
	dest, _ := url.Parse(w.Header().Get("Location"))
	if dest.Query().Get("code_challenge_method") != "S256" || len(dest.Query().Get("code_challenge")) != 43 {
		t.Fatal("missing PKCE")
	}
	return startedFlow{start.ID, dest.Query().Get("state"), w.Result().Cookies()[0]}
}
func callbackFlow(h http.Handler, f startedFlow, cookies []*http.Cookie) *httptest.ResponseRecorder {
	return call(h, "GET", "/auth/callback/github?code=test-code&state="+f.state, nil, cookies, nil)
}
func TestOAuthWebFlowCookieBindingReplayAndCSRF(t *testing.T) {
	s, h := serviceForTest(t)
	f := startFlow(t, h, "web", "")
	for _, cookies := range [][]*http.Cookie{nil, {{Name: f.cookie.Name, Value: "wrong"}}} {
		w := callbackFlow(h, f, cookies)
		if w.Header().Get("Location") != "/auth/result?status=invalid" {
			t.Fatal("callback cookie bypass")
		}
	}
	// A callback at the wrong provider cannot consume a valid login attempt.
	w := call(h, "GET", "/auth/callback/google?code=test-code&state="+f.state, nil, []*http.Cookie{f.cookie}, nil)
	if w.Header().Get("Location") != "/auth/result?status=invalid" {
		t.Fatal("provider mix-up")
	}
	w = callbackFlow(h, f, []*http.Cookie{f.cookie})
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatal("callback failed", w.Body.String())
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == s.sessionCookie() {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || session.Path != "/api/" || session.SameSite != http.SameSiteLaxMode {
		t.Fatal("session cookie protections")
	}
	cookies := []*http.Cookie{session}
	w = call(h, "GET", "/api/v1/auth/me", nil, cookies, nil)
	var me struct {
		User *User  `json:"user"`
		CSRF string `json:"csrfToken"`
	}
	json.Unmarshal(w.Body.Bytes(), &me)
	if me.User == nil || me.User.Role != "superadmin" || me.CSRF == "" {
		t.Fatal("wrong session status")
	}
	if w = callbackFlow(h, f, []*http.Cookie{f.cookie}); w.Header().Get("Location") != "/auth/result?status=invalid" {
		t.Fatal("callback replay accepted")
	}
	if w = call(h, "POST", "/api/v1/auth/logout", map[string]any{}, cookies, nil); w.Code != 403 {
		t.Fatal("CSRF missing accepted")
	}
	if w = call(h, "POST", "/api/v1/auth/logout", map[string]any{}, cookies, map[string]string{"X-Monee-CSRF": me.CSRF}); w.Code != 200 {
		t.Fatal("logout failed")
	}
	if u, _ := s.store.Session(session.Value); u != nil {
		t.Fatal("session not revoked")
	}
}
func TestNativeAuthorizationBoundToClientProofAndOneUse(t *testing.T) {
	s, h := serviceForTest(t)
	proof := randomToken()
	f := startFlow(t, h, "desktop", digest(proof))
	w := callbackFlow(h, f, []*http.Cookie{f.cookie})
	if w.Header().Get("Location") != "/auth/result?status=desktop" {
		t.Fatal("native callback failed")
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == s.sessionCookie() {
			t.Fatal("native flow leaked browser session")
		}
	}
	if w = call(h, "POST", "/api/v1/auth/native/poll", map[string]string{"id": f.id, "verifier": randomToken()}, nil, nil); w.Code != 401 {
		t.Fatal("wrong native proof accepted")
	}
	w = call(h, "POST", "/api/v1/auth/native/poll", map[string]string{"id": f.id, "verifier": proof}, nil, nil)
	var result struct{ Status, Token string }
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Status != "complete" || result.Token == "" {
		t.Fatal("native exchange failed")
	}
	mustUser(t, s.store, result.Token, true)
	if w = call(h, "POST", "/api/v1/auth/native/poll", map[string]string{"id": f.id, "verifier": proof}, nil, nil); w.Code != 401 {
		t.Fatal("native exchange replay")
	}
}
func TestDeniedIdentityCannotManageAndClientCannotSelectRole(t *testing.T) {
	s, h := serviceForTest(t)
	stranger := Identity{Provider: "github", Subject: "2002", Username: "no-access"}
	token := sessionFor(t, s.store, stranger)
	headers := map[string]string{"Authorization": "Bearer " + token}
	for _, method := range []string{"GET", "POST"} {
		if w := call(h, method, "/api/v1/admin/allowlist", map[string]string{}, nil, headers); w.Code != 403 {
			t.Fatal("denied identity admin access")
		}
	}
	admin := sessionFor(t, s.store, rootIdentity)
	headers["Authorization"] = "Bearer " + admin
	if w := call(h, "POST", "/api/v1/admin/allowlist", map[string]string{"provider": "github", "kind": "subject", "value": "2002", "role": "superadmin"}, nil, headers); w.Code != 400 {
		t.Fatal("client-specified role accepted")
	}
	s.resolveGitHub = func(context.Context, string) (string, string, error) { return "2002", "no-access", nil }
	if w := call(h, "POST", "/api/v1/admin/allowlist", Selector{Provider: "github", Kind: "username", Value: "no-access"}, nil, headers); w.Code != 201 {
		t.Fatal("admin add failed", w.Body.String())
	}
	mustUser(t, s.store, token, true)
	// The public local connection endpoint cannot mint credentials anymore.
	if w := call(h, "GET", "/api/v1/session", nil, nil, nil); w.Code != 404 {
		t.Fatal("legacy login bypass exists")
	}
}
func TestCancelledExpiredAndUnconfiguredLogin(t *testing.T) {
	s, h := serviceForTest(t)
	if w := call(h, "POST", "/api/v1/auth/start", map[string]string{"provider": "google", "client": "web"}, nil, nil); w.Code != 503 {
		t.Fatal("missing provider not closed")
	}
	proof := randomToken()
	f := startFlow(t, h, "desktop", digest(proof))
	w := call(h, "GET", "/auth/callback/github?error=access_denied&state="+f.state, nil, []*http.Cookie{f.cookie}, nil)
	if w.Header().Get("Location") != "/auth/result?status=cancelled" {
		t.Fatal("cancel handling")
	}
	w = call(h, "POST", "/api/v1/auth/native/poll", map[string]string{"id": f.id, "verifier": proof}, nil, nil)
	if !strings.Contains(w.Body.String(), `"status":"failed"`) {
		t.Fatal("cancelled login still pending")
	}
	f = startFlow(t, h, "desktop", digest(proof))
	s.mu.Lock()
	s.flows[f.id].Expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if w = call(h, "POST", "/api/v1/auth/native/poll", map[string]string{"id": f.id, "verifier": proof}, nil, nil); w.Code != 401 {
		t.Fatal("expired flow accepted")
	}
}

func TestDesktopResultOnlyOffersFixedWakeLink(t *testing.T) {
	_, h := serviceForTest(t)
	for _, status := range []string{"desktop", "cancelled", "failed", "invalid", ""} {
		t.Run(status, func(t *testing.T) {
			q := url.Values{"status": {status}, "redirectTo": {"https://attacker.invalid"}, "token": {"untrusted-secret"}}
			w := call(h, "GET", "/auth/result?"+q.Encode(), nil, nil, nil)
			body := w.Body.String()
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("result page must remain uncached")
			}
			if strings.Contains(body, `href="monee://auth/complete"`) != (status == "desktop") {
				t.Fatal("wrong desktop return link")
			}
			for _, forbidden := range []string{"attacker.invalid", "untrusted-secret", "#ZgotmplZ", "<script"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("unsafe result content: %s", forbidden)
				}
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("result page must not issue a session")
			}
		})
	}
}
