package auth

import (
	"github.com/letrahoo/monee/server/internal/ledger"
	"path/filepath"
	"testing"
)

func TestUnifiedIdentityMergePreservesLedgersAndRevokesSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	data, err := ledger.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	s, err := Open(path, []Selector{{Provider: "github", Kind: "subject", Value: "1001"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := rootIdentity
	b := Identity{Provider: "google", Subject: "google-b", Email: "b@gmail.com", EmailTrusted: true}
	c := Identity{Provider: "github", Subject: "3003", Username: "third"}
	at := sessionFor(t, s, a)
	bt := sessionFor(t, s, b)
	ct := sessionFor(t, s, c)
	if _, err = s.Add(a, Selector{Provider: "google", Kind: "subject", Value: b.Subject}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Add(a, Selector{Provider: "github", Kind: "subject", Value: c.Subject}, ""); err != nil {
		t.Fatal(err)
	}
	au := mustUser(t, s, at, true)
	bu := mustUser(t, s, bt, true)
	cu := mustUser(t, s, ct, true)
	if au.ID == bu.ID {
		t.Fatal("identities automatically merged")
	}
	l1, err := data.CreateLedger(au.ID, "A")
	if err != nil {
		t.Fatal(err)
	}
	l2, err := data.CreateLedger(bu.ID, "B")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := data.Scoped(au.ID, l1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.Invite(bu.ID, "viewer"); err != nil {
		t.Fatal(err)
	}
	invitations, _ := data.Invitations(bu.ID)
	if err = data.RespondInvitation(bu.ID, invitations[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err = s.LinkVerified(au.ID, b, false); err == nil {
		t.Fatal("merged without explicit confirmation")
	}
	if err = s.LinkVerified(au.ID, b, true); err != nil {
		t.Fatal(err)
	}
	if u, err := s.Session(bt); err != nil || u != nil {
		t.Fatal("old target session survived", err)
	}
	linked, err := s.Identities(au.ID)
	if err != nil || len(linked) != 2 {
		t.Fatal(err, linked)
	}
	newToken, err := s.CreateSession(b)
	if err != nil {
		t.Fatal(err)
	}
	if u := mustUser(t, s, newToken, true); u.ID != au.ID || u.Role != "superadmin" {
		t.Fatal("linked login did not resolve unified account", u)
	}
	list, err := data.ListLedgers(au.ID)
	if err != nil || len(list) != 2 {
		t.Fatal("lost memberships", err, list)
	}
	for _, l := range list {
		if l.Role != "owner" {
			t.Fatal("owner downgraded")
		}
	}
	if _, err = data.Scoped(cu.ID, l2.ID); err == nil {
		t.Fatal("outsider gained access")
	}
	if err = s.Unlink(au.ID, a.Provider, a.Subject); err == nil {
		t.Fatal("unlinked protected administrator")
	}
	if err = s.Unlink(au.ID, b.Provider, b.Subject); err != nil {
		t.Fatal(err)
	}
	if u, err := s.Session(newToken); err != nil || u != nil {
		t.Fatal("unlinked session survived")
	}
	if err = s.Unlink(au.ID, a.Provider, a.Subject); err == nil {
		t.Fatal("removed final login")
	}
}
func TestAccountDisableAppliesToEveryLinkedChannel(t *testing.T) {
	s := testStore(t)
	member := Identity{Provider: "github", Subject: "2002"}
	second := Identity{Provider: "google", Subject: "second", Email: "same@gmail.com", EmailTrusted: true}
	mt := sessionFor(t, s, member)
	_ = sessionFor(t, s, second)
	if _, err := s.Add(rootIdentity, Selector{Provider: "github", Kind: "subject", Value: member.Subject}, ""); err != nil {
		t.Fatal(err)
	}
	m := mustUser(t, s, mt, true)
	if err := s.LinkVerified(m.ID, second, true); err != nil {
		t.Fatal(err)
	}
	st, err := s.CreateSession(second)
	if err != nil {
		t.Fatal(err)
	}
	users, err := s.Accounts(rootIdentity)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.ID == m.ID {
			if err = s.SetAccountEnabled(rootIdentity, u.ID, false, u.Version); err != nil {
				t.Fatal(err)
			}
		}
	}
	mustUser(t, s, mt, false)
	mustUser(t, s, st, false)
	if err = s.RememberIdentity(member); err != nil {
		t.Fatal(err)
	}
	mustUser(t, s, mt, false) // login cannot revive admission
}
func TestSameEmailDoesNotMergeAccounts(t *testing.T) {
	s := testStore(t)
	one := Identity{Provider: "google", Subject: "one", Email: "shared@gmail.com", EmailTrusted: true}
	two := Identity{Provider: "google", Subject: "two", Email: "shared@gmail.com", EmailTrusted: true}
	a := mustUser(t, s, sessionFor(t, s, one), false)
	b := mustUser(t, s, sessionFor(t, s, two), false)
	if a.ID == b.ID {
		t.Fatal("merged by email")
	}
}
