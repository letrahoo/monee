package httpapi

import (
	"net/http"
	"net/url"

	"github.com/letrahoo/monee/server/internal/ledger"
)

// Reject malformed/ambiguous input rather than silently dropping a filter.
func queryParameters(w http.ResponseWriter, r *http.Request, allowed ...string) (url.Values, bool) {
	params, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		fail(w, &ledger.Problem{Code: "invalid", Message: "查询参数格式无效"})
		return nil, false
	}
	names := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		names[name] = true
	}
	for name, values := range params {
		if !names[name] || len(values) != 1 {
			fail(w, &ledger.Problem{Code: "invalid", Message: "查询参数无效或重复"})
			return nil, false
		}
	}
	return params, true
}
