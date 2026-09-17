package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

const host = "127.0.0.1:4173"

func apiForTest(t *testing.T) (http.Handler, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "application.db")
	s, e := ledger.Open(dbPath)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	access, e := auth.Open(dbPath, []auth.Selector{{Provider: "github", Kind: "subject", Value: "1001"}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { access.Close() })
	identity := auth.Identity{Provider: "github", Subject: "1001", Username: "test-admin"}
	if e = access.RememberIdentity(identity); e != nil {
		t.Fatal(e)
	}
	token, e := access.CreateSession(identity)
	if e != nil {
		t.Fatal(e)
	}
	u, e := access.Session(token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateLedger(u.ID, "测试账本"); e != nil {
		t.Fatal(e)
	}
	return API{Store: s, Auth: auth.NewService(access, nil, "http://"+host), Host: host}.Handler(), token
}
func request(h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestLoopbackAuthAndOriginBoundary(t *testing.T) {
	h, token := apiForTest(t)
	for _, tc := range []struct {
		path    string
		headers map[string]string
		status  int
	}{
		{"/api/v1/dashboard", nil, 401},
		{"/api/v1/dashboard", map[string]string{"Authorization": token}, 401},
		{"/api/v1/session", nil, 401},
		{"/api/v1/session", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"/api/v1/auth/me", map[string]string{"Sec-Fetch-Site": "same-origin"}, 200},
		{"/api/v1/dashboard", map[string]string{"Authorization": "Bearer " + token, "Origin": "https://evil.example"}, 403},
		{"/api/v1/dashboard", map[string]string{"Authorization": "Bearer " + token, "Sec-Fetch-Site": "same-site"}, 403},
		{"/api/v1/dashboard", map[string]string{"Authorization": "Bearer " + token}, 200},
	} {
		w := request(h, "GET", tc.path, "", tc.headers)
		if w.Code != tc.status {
			t.Fatalf("%s: got %d want %d", tc.path, w.Code, tc.status)
		}
		if tc.status != 200 && strings.Contains(w.Body.String(), token) {
			t.Fatal("token leaked")
		}
	}
	r := httptest.NewRequest("GET", "http://evil.example/api/v1/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("DNS rebinding host accepted")
	}
}

func TestProductionOriginBoundary(t *testing.T) {
	h := API{Host: "finance.letra.xin", Origin: "https://finance.letra.xin"}.Handler()
	for _, tc := range []struct {
		host, origin string
		status       int
	}{
		{"finance.letra.xin", "https://finance.letra.xin", 200},
		{"finance.letra.xin", "http://finance.letra.xin", 403},
		{"evil.example", "https://finance.letra.xin", 403},
	} {
		r := httptest.NewRequest("GET", "http://"+tc.host+"/api/v1/health", nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("host=%q origin=%q: got %d want %d", tc.host, tc.origin, w.Code, tc.status)
		}
	}
}
func TestRealAPIWriteReadAndValidation(t *testing.T) {
	h, token := apiForTest(t)
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json", "Idempotency-Key": "http-test-request-0001"}
	body := `{"date":"2026-09-14","type":"expense","amount":"12.34","currency":"CNY","merchant":"测试中文","source":"手动"}`
	w := request(h, "POST", "/api/v1/transactions", body, headers)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/transactions", body, headers)
	if w.Code != 201 {
		t.Fatal("retry failed")
	}
	w = request(h, "GET", "/api/v1/dashboard?month=2026-09&q=%E4%B8%AD%E6%96%87", "", headers)
	var d ledger.Dashboard
	if e := json.Unmarshal(w.Body.Bytes(), &d); e != nil {
		t.Fatal(e)
	}
	if w.Code != 200 || d.ExpenseMinor != "1234" || d.TotalCount != 1 || d.FilteredCount != 1 {
		t.Fatalf("wrong dashboard %+v", d)
	}
	if !strings.Contains(w.Body.String(), `"amountMinor":"-1234"`) {
		t.Fatal("amount not encoded as string")
	}
	for _, body := range []string{`{}`, `{"unknown":1}`, body + `{}`, strings.Repeat("x", (3<<20)+1)} {
		w = request(h, "POST", "/api/v1/transactions", body, headers)
		if w.Code < 400 {
			t.Fatal("invalid JSON accepted")
		}
	}
	delete(headers, "Content-Type")
	if w = request(h, "POST", "/api/v1/transactions", body, headers); w.Code != 415 {
		t.Fatal("simple cross-site form content type accepted")
	}
}
