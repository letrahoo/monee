package httpapi

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestNativeEvidencePreviewHTTPAuthorizationAndConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-evidence.db")
	data, e := ledger.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer data.Close()
	access, e := auth.Open(path, []auth.Selector{{Provider: "github", Kind: "subject", Value: "1001"}})
	if e != nil {
		t.Fatal(e)
	}
	defer access.Close()
	identities := []auth.Identity{{Provider: "github", Subject: "1001", Username: "synthetic-owner"}, {Provider: "github", Subject: "2002", Username: "synthetic-viewer"}, {Provider: "github", Subject: "3003", Username: "synthetic-editor"}, {Provider: "github", Subject: "4004", Username: "synthetic-other"}}
	var tokens, ids []string
	var editorEntry auth.Entry
	for i, identity := range identities {
		if e = access.RememberIdentity(identity); e != nil {
			t.Fatal(e)
		}
		if i > 0 {
			entry, err := access.Add(identities[0], auth.Selector{Provider: "github", Kind: "subject", Value: identity.Subject}, "")
			if err != nil {
				t.Fatal(err)
			}
			if i == 2 {
				editorEntry = entry
			}
		}
		token, err := access.CreateSession(identity)
		if err != nil {
			t.Fatal(err)
		}
		u, err := access.Session(token)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
		ids = append(ids, u.ID)
	}
	first, e := data.CreateLedger(ids[0], "合成证据甲")
	if e != nil {
		t.Fatal(e)
	}
	second, e := data.CreateLedger(ids[3], "合成证据乙")
	if e != nil {
		t.Fatal(e)
	}
	owner, e := data.Scoped(ids[0], first.ID)
	if e != nil {
		t.Fatal(e)
	}
	for i, role := range map[int]string{1: "viewer", 2: "editor"} {
		if e = owner.Invite(ids[i], role); e != nil {
			t.Fatal(e)
		}
		invitations, err := data.Invitations(ids[i])
		if err != nil || len(invitations) != 1 {
			t.Fatal(err, invitations)
		}
		if e = data.RespondInvitation(ids[i], invitations[0].ID, true); e != nil {
			t.Fatal(e)
		}
	}
	editorScope, e := data.Scoped(ids[2], first.ID)
	if e != nil {
		t.Fatal(e)
	}
	h := API{Store: data, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler()
	body, _ := json.Marshal(map[string]string{"filename": "synthetic-native.csv", "account": "synthetic-wallet", "contentBase64": base64.StdEncoding.EncodeToString([]byte(reviewQueueCSV))})
	headers := func(i int, l string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + tokens[i], "X-Monee-Ledger": l, "Content-Type": "application/json"}
	}
	call := func(i int, l string, want int) ledger.Preview {
		t.Helper()
		w := request(h, "POST", "/api/v1/imports/alipay/preview", string(body), headers(i, l))
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("actor%d got%d want%d: %s", i, w.Code, want, w.Body.String())
		}
		var p ledger.Preview
		if want == 200 && json.Unmarshal(w.Body.Bytes(), &p) != nil {
			t.Fatal(w.Body.String())
		}
		return p
	}
	if w := request(h, "POST", "/api/v1/imports/alipay/preview", string(body), nil); w.Code != 401 {
		t.Fatal("anonymous source write accepted", w.Code)
	}
	original := call(0, first.ID, 200)
	if original.ID == "" || len(original.Pending) != 1 {
		t.Fatal(original)
	}
	again := call(2, first.ID, 200)
	if again.ID != original.ID {
		t.Fatal("editor repeat created new source", again)
	}
	call(1, first.ID, 403)
	call(0, second.ID, 403)
	call(3, first.ID, 403)
	other := call(3, second.ID, 200)
	if other.ID == original.ID {
		t.Fatal("same content collapsed across ledgers")
	}
	if w := request(h, "GET", "/api/v1/imports/"+original.ID, "", headers(3, second.ID)); w.Code != 404 {
		t.Fatal("cross-tenant original disclosed", w.Code)
	}
	if e = owner.ChangeMember(ids[1], "remove", 1); e != nil {
		t.Fatal(e)
	}
	call(1, first.ID, 403)
	if _, e = access.SetEnabled(identities[0], editorEntry.ID, false, editorEntry.Version); e != nil {
		t.Fatal(e)
	}
	call(2, first.ID, 403)
	if _, e = editorScope.PreviewAlipay("stale-editor.csv", base64.StdEncoding.EncodeToString([]byte(reviewQueueCSV)), "synthetic-wallet"); e == nil {
		t.Fatal("previously scoped editor bypassed current admission")
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var stored string
	if e = db.QueryRow("SELECT preview_json FROM imports WHERE id=?", original.ID).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	var corrupted map[string]any
	if e = json.Unmarshal([]byte(stored), &corrupted); e != nil {
		t.Fatal(e)
	}
	delete(corrupted, "sourceDocument")
	corrupted["syntheticUnexpected"] = "synthetic-secret"
	b, _ := json.Marshal(corrupted)
	if _, e = db.Exec("UPDATE imports SET preview_json=? WHERE id=?", string(b), original.ID); e != nil {
		t.Fatal(e)
	}
	row := func() []any {
		t.Helper()
		v := make([]any, 8)
		p := make([]any, 8)
		for i := range v {
			p[i] = &v[i]
		}
		if err := db.QueryRow("SELECT preview_json,raw_csv,ledger_version,created_by,filename,created_at,result_json,committed_at FROM imports WHERE id=?", original.ID).Scan(p...); err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := row()
	w := request(h, "POST", "/api/v1/imports/alipay/preview", string(body), headers(0, first.ID))
	if w.Code != http.StatusConflict || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "synthetic-secret") {
		t.Fatal("bad snapshot was repaired or exposed", w.Code, w.Body.String())
	}
	var failure map[string]string
	if e = json.Unmarshal(w.Body.Bytes(), &failure); e != nil || failure["code"] != "conflict" || failure["message"] != "原生来源快照已变化或证据不完整，无法自动刷新此批预览" {
		t.Fatal("conflict payload must remain static and evidence-free", w.Body.String(), e)
	}
	if !reflect.DeepEqual(before, row()) {
		t.Fatal("HTTP conflict overwrote first evidence")
	}
	if d, err := owner.Dashboard("2026-09", "", 1); err != nil || d.TotalCount != 0 {
		t.Fatal("preview/denial changed finance", d, err)
	}
}
