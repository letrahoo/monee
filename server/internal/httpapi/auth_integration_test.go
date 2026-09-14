package httpapi

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestEveryDataEndpointRequiresAllowlist(t *testing.T) {
	data, e := ledger.Open(filepath.Join(t.TempDir(), "data.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer data.Close()
	access, e := auth.Open(filepath.Join(t.TempDir(), "auth.db"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer access.Close()
	identity := auth.Identity{Provider: "github", Subject: "2002", Username: "denied-fixture"}
	if e = access.RememberIdentity(identity); e != nil {
		t.Fatal(e)
	}
	token, e := access.CreateSession(identity)
	if e != nil {
		t.Fatal(e)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/dashboard"}, {"GET", "/api/v1/template"}, {"POST", "/api/v1/transactions"}, {"POST", "/api/v1/imports/preview"}, {"POST", "/api/v1/imports/any/commit"}} {
		for _, loggedIn := range []bool{false, true} {
			headers := map[string]string{"Content-Type": "application/json"}
			want := http.StatusUnauthorized
			if loggedIn {
				headers["Authorization"] = "Bearer " + token
				want = http.StatusForbidden
			}
			w := request(h, route.method, route.path, `{}`, headers)
			if w.Code != want {
				t.Fatalf("%s %s loggedIn=%v got=%d", route.method, route.path, loggedIn, w.Code)
			}
			if loggedIn && !strings.Contains(w.Body.String(), "access_denied") {
				t.Fatal("missing access denial")
			}
		}
	}
	d, e := data.Dashboard("2026-09", "", 1)
	if e != nil || d.TotalCount != 0 {
		t.Fatal("denied write reached ledger")
	}
	// Reading the previous discovery token, or forging a browser session marker, gives no data access.
	w := request(h, "GET", "/api/v1/session", "", map[string]string{"Sec-Fetch-Site": "same-origin"})
	if w.Code != 401 {
		t.Fatal("legacy discovery route bypass")
	}
}
