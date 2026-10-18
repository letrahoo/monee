package httpapi

import (
	"net/http"
	"strconv"

	"github.com/letrahoo/monee/server/internal/ledger"
)

// Metadata describes retained source evidence. It never authorizes a decision or
// provides a financial mutation route, even when the source is an ordinary candidate.
func (a API) registerReviewItemMetadata(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/imports/{id}/review-items/{line}/metadata", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		lineText := r.PathValue("line")
		line, err := strconv.Atoi(lineText)
		if err != nil || line < 1 || line > 1_000_000 || strconv.Itoa(line) != lineText || r.URL.RawQuery != "" {
			fail(w, &ledger.Problem{Code: "invalid", Message: "来源行号须为规范正整数，且不接受查询参数"})
			return
		}
		out, err := store.ReviewItemMetadata(r.PathValue("id"), line)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}
