package httpapi

import (
	"github.com/letrahoo/monee/server/internal/ledger"
	"net/http"
	"strconv"
)

func (a API) registerReviewQueue(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/review-queue", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		page := 1
		if value := r.URL.Query().Get("page"); value != "" {
			var e error
			page, e = strconv.Atoi(value)
			if e != nil {
				fail(w, &ledger.Problem{Code: "invalid", Message: "分页参数无效"})
				return
			}
		}
		out, e := store.ReviewQueue(page, r.URL.Query().Get("format"))
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}
