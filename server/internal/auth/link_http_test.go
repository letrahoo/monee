package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func startBinding(t *testing.T, h http.Handler, token string) startedFlow {
	t.Helper()
	w := call(h, "POST", "/api/v1/auth/start", map[string]string{"provider": "github", "client": "web", "intent": "link"}, nil, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var start struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	w = call(h, "GET", start.URL, nil, nil, nil)
	dest, _ := url.Parse(w.Header().Get("Location"))
	return startedFlow{start.ID, dest.Query().Get("state"), w.Result().Cookies()[0]}
}
func TestBindingRequiresSessionAndExistingAccountConfirmation(t *testing.T) {
	s, h := serviceForTest(t)
	target := Identity{Provider: "github", Subject: "2002", Username: "second"}
	s.providers["github"] = fakeProvider{target}
	rootToken := sessionFor(t, s.store, rootIdentity)
	targetToken := sessionFor(t, s.store, target)
	root := mustUser(t, s.store, rootToken, true)
	oldID, _ := s.store.AccountID(target)
	w := call(h, "POST", "/api/v1/auth/start", map[string]string{"provider": "github", "client": "web", "intent": "link"}, nil, nil)
	if w.Code != 401 {
		t.Fatal("anonymous binding")
	}
	flow := startBinding(t, h, rootToken)
	w = callbackFlow(h, flow, []*http.Cookie{flow.cookie})
	if w.Header().Get("Location") != "/auth/result?status=linked" {
		t.Fatal(w.Header())
	}
	if id, _ := s.store.AccountID(target); id != oldID {
		t.Fatal("merged without confirmation")
	}
	w = call(h, "POST", "/api/v1/auth/account/merge", map[string]string{"id": flow.id}, nil, map[string]string{"Authorization": "Bearer " + targetToken})
	if w.Code != 409 {
		t.Fatal("other session consumed merge")
	}
	w = call(h, "POST", "/api/v1/auth/account/merge", map[string]string{"id": flow.id}, nil, map[string]string{"Authorization": "Bearer " + rootToken})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if id, _ := s.store.AccountID(target); id != root.ID {
		t.Fatal("not linked")
	}
	w = call(h, "POST", "/api/v1/auth/account/merge", map[string]string{"id": flow.id}, nil, map[string]string{"Authorization": "Bearer " + rootToken})
	if w.Code != 409 {
		t.Fatal("merge replay")
	}
}
func TestBindingCannotCompleteAfterInitiatorLogout(t *testing.T) {
	s, h := serviceForTest(t)
	target := Identity{Provider: "github", Subject: "2002"}
	s.providers["github"] = fakeProvider{target}
	token := sessionFor(t, s.store, rootIdentity)
	rootID, _ := s.store.AccountID(rootIdentity)
	flow := startBinding(t, h, token)
	if err := s.store.Logout(token); err != nil {
		t.Fatal(err)
	}
	w := callbackFlow(h, flow, []*http.Cookie{flow.cookie})
	if w.Header().Get("Location") != "/auth/result?status=failed" {
		t.Fatal("stale binding session accepted")
	}
	if id, _ := s.store.AccountID(target); id == rootID {
		t.Fatal("linked after logout")
	}
}
