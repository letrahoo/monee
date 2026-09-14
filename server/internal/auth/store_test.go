package auth

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var rootIdentity = Identity{Provider: "github", Subject: "1001", Username: "synthetic-admin"}

func testStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "auth.db"), []Selector{{Provider: "github", Kind: "subject", Value: rootIdentity.Subject}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	if e = s.RememberIdentity(rootIdentity); e != nil {
		t.Fatal(e)
	}
	return s
}
func sessionFor(t *testing.T, s *Store, i Identity) string {
	t.Helper()
	if e := s.RememberIdentity(i); e != nil {
		t.Fatal(e)
	}
	token, e := s.CreateSession(i)
	if e != nil {
		t.Fatal(e)
	}
	return token
}
func mustUser(t *testing.T, s *Store, token string, allowed bool) *User {
	t.Helper()
	u, e := s.Session(token)
	if e != nil || u == nil || u.Allowed != allowed {
		t.Fatalf("session allowed=%v user=%+v err=%v", allowed, u, e)
	}
	return u
}

func TestAllowlistManagementAndImmediateRevocation(t *testing.T) {
	s := testStore(t)
	rootToken := sessionFor(t, s, rootIdentity)
	if mustUser(t, s, rootToken, true).Role != "superadmin" {
		t.Fatal("bootstrap role")
	}
	member := Identity{Provider: "github", Subject: "2002", Username: "synthetic-member"}
	token := sessionFor(t, s, member)
	mustUser(t, s, token, false)
	entry, e := s.Add(rootIdentity, Selector{Provider: "github", Kind: "username", Value: member.Username}, member.Subject)
	if e != nil {
		t.Fatal(e)
	}
	if mustUser(t, s, token, true).Role != "member" {
		t.Fatal("role escalation")
	}
	if _, e = s.Add(member, Selector{Provider: "github", Kind: "subject", Value: "3003"}, ""); !errors.Is(e, errAdmin) {
		t.Fatal("member added access", e)
	}
	if _, e = s.List(member); !errors.Is(e, errAdmin) {
		t.Fatal("member read list", e)
	}
	disabled, e := s.SetEnabled(rootIdentity, entry.ID, false, entry.Version)
	if e != nil {
		t.Fatal(e)
	}
	mustUser(t, s, token, false)
	if _, e = s.SetEnabled(rootIdentity, entry.ID, true, entry.Version); e == nil {
		t.Fatal("stale version accepted")
	}
	if _, e = s.SetEnabled(rootIdentity, entry.ID, true, disabled.Version); e != nil {
		t.Fatal(e)
	}
	mustUser(t, s, token, true)
	entries, _ := s.List(rootIdentity)
	for _, v := range entries {
		if v.Protected {
			if _, e = s.SetEnabled(rootIdentity, v.ID, false, v.Version); e == nil {
				t.Fatal("root disabled")
			}
		}
	}
	events, e := s.Audit(rootIdentity)
	if e != nil || len(events) != 4 {
		t.Fatalf("audit %+v %v", events, e)
	}
}
func TestGoogleEmailBindsOnceAndProvidersStaySeparate(t *testing.T) {
	s := testStore(t)
	_, e := s.Add(rootIdentity, Selector{Provider: "google", Kind: "email", Value: "member.fixture@gmail.com"}, "")
	if e != nil {
		t.Fatal(e)
	}
	i := Identity{Provider: "google", Subject: "google-1001", Email: "member.fixture@gmail.com"}
	token := sessionFor(t, s, i)
	mustUser(t, s, token, false)
	i.EmailTrusted = true
	if e = s.RememberIdentity(i); e != nil {
		t.Fatal(e)
	}
	mustUser(t, s, token, true)
	recycled := i
	recycled.Subject = "google-1002"
	other := sessionFor(t, s, recycled)
	mustUser(t, s, other, false)
	i.Email = "new.fixture@gmail.com"
	if e = s.RememberIdentity(i); e != nil {
		t.Fatal(e)
	}
	mustUser(t, s, token, true)
	collision := sessionFor(t, s, Identity{Provider: "google", Subject: "1001", Username: rootIdentity.Username})
	mustUser(t, s, collision, false)
}
func TestBootstrapAndSessionsPersistWithoutReapplyingSeeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")
	s, e := Open(path, []Selector{{Provider: "github", Kind: "subject", Value: "1001"}, {Provider: "google", Kind: "email", Value: "owner.fixture@gmail.com"}})
	if e != nil {
		t.Fatal(e)
	}
	token := sessionFor(t, s, rootIdentity)
	google := Identity{Provider: "google", Subject: "9009", Email: "owner.fixture@gmail.com", EmailTrusted: true}
	googleToken := sessionFor(t, s, google)
	if mustUser(t, s, googleToken, true).Role != "superadmin" {
		t.Fatal("Google bootstrap failed")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path, []Selector{{Provider: "github", Kind: "subject", Value: "6666"}})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	mustUser(t, s, token, true)
	mustUser(t, s, googleToken, true)
	attacker := sessionFor(t, s, Identity{Provider: "github", Subject: "6666"})
	mustUser(t, s, attacker, false)
	var hash string
	if e = s.db.QueryRow("SELECT token_hash FROM sessions WHERE provider='github' AND subject='1001'").Scan(&hash); e != nil || hash == token {
		t.Fatal("plaintext session stored", e)
	}
	if e = s.Logout(token); e != nil {
		t.Fatal(e)
	}
	if u, e := s.Session(token); e != nil || u != nil {
		t.Fatal("logout ineffective")
	}
	if _, e = s.db.Exec("UPDATE sessions SET expires_at=?", time.Now().Add(-time.Second).Unix()); e != nil {
		t.Fatal(e)
	}
	if u, e := s.Session(googleToken); e != nil || u != nil {
		t.Fatal("expired session accepted")
	}
}
func TestConcurrentDuplicateAddAndValidation(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	success := make(chan bool, 12)
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Add(rootIdentity, Selector{Provider: "github", Kind: "subject", Value: "2002"}, "")
			success <- e == nil
		}()
	}
	wg.Wait()
	close(success)
	count := 0
	for ok := range success {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate additions", count)
	}
	for _, input := range []Selector{{Provider: "unknown", Kind: "subject", Value: "2002"}, {Provider: "google", Kind: "username", Value: "name"}, {Provider: "github", Kind: "subject", Value: "abc"}, {Provider: "google", Kind: "email", Value: "not-an-email"}} {
		if _, e := s.Add(rootIdentity, input, ""); e == nil {
			t.Fatal("invalid selector accepted")
		}
	}
}
func TestAuthFutureSchemaIsRejected(t *testing.T) {
	s := testStore(t)
	if _, e := s.db.Exec("PRAGMA user_version=2"); e != nil {
		t.Fatal(e)
	}
	if e := s.initialize(nil); e == nil {
		t.Fatal("future schema accepted")
	}
}
