package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/letrahoo/monee/server/internal/ledger"
)

func TestWorkbenchQueriesRejectMalformedAmbiguousAndUnknownParameters(t *testing.T) {
	h, token := apiForTest(t)
	headers := map[string]string{"Authorization": "Bearer " + token}
	for _, path := range []string{
		"/api/v1/dashboard?month=2026-09&type=%ZZ",
		"/api/v1/dashboard?month=2026-09&type=expense;ignored=x",
		"/api/v1/dashboard?month=2026-09&query=ignored",
		"/api/v1/dashboard?month=2026-09&month=2026-08",
		"/api/v1/dashboard?month=2026-09&type=expense&type=refund",
		"/api/v1/annual?year=%ZZ",
		"/api/v1/annual?year=2026;ignored=x",
		"/api/v1/annual?year=2026&year=2025",
		"/api/v1/annual?year=2026&unexpected=x",
	} {
		w := request(h, "GET", path, "", headers)
		if w.Code != 422 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	// Exercise Annual through the real Handler and auth middleware, not a hand-registered mux.
	w := request(h, "GET", "/api/v1/annual?year=2026", "", headers)
	var report ledger.AnnualReport
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &report) != nil || report.Year != 2026 || len(report.Months) != 12 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(h, "GET", "/api/v1/annual?year=2026", "", nil); w.Code != 401 {
		t.Fatal("annual bypassed login", w.Code)
	}
}
