package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/letrahoo/monee/server/internal/ledger"
)

// Association candidates are current-value hints, never permission to merge or
// change accounting. Authenticate and select the ledger before parsing input.
func (a API) registerAssociationCandidates(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/transactions/{id}/association-candidates", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		page := 1
		query, err := url.ParseQuery(r.URL.RawQuery)
		invalid := err != nil
		for key, values := range query {
			if key != "page" || len(values) != 1 {
				invalid = true
				continue
			}
			value := values[0]
			page, err = strconv.Atoi(value)
			if err != nil || page < 1 || page > 1_000_000 || strconv.Itoa(page) != value {
				invalid = true
			}
		}
		if invalid {
			fail(w, &ledger.Problem{Code: "invalid", Message: "分页须为规范正整数，且仅接受一个 page 参数"})
			return
		}
		out, err := store.TransactionAssociationCandidates(r.PathValue("id"), page)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}
