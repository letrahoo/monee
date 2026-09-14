package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/ledger"
)

const token = "test-token-that-is-only-a-fixture"
const host = "127.0.0.1:4173"

func apiForTest(t *testing.T) http.Handler {
	t.Helper()
	s, e := ledger.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return API{Store: s, Token: token, Host: host}.Handler()
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
	h := apiForTest(t)
	for _, tc := range []struct {
		path    string
		headers map[string]string
		status  int
	}{
		{"/api/v1/dashboard", nil, 401},
		{"/api/v1/dashboard", map[string]string{"Authorization": token}, 401},
		{"/api/v1/session", nil, 403},
		{"/api/v1/session", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"/api/v1/session", map[string]string{"Sec-Fetch-Site": "same-origin"}, 200},
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
func TestRealAPIWriteReadAndValidation(t *testing.T) {
	h := apiForTest(t)
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
