package httpapi

import (
	"net/http"
	"strconv"

	"github.com/letrahoo/monee/server/internal/ledger"
)

func (a API) registerImports(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/imports", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		page := 1
		if value := r.URL.Query().Get("page"); value != "" {
			var err error
			page, err = strconv.Atoi(value)
			if err != nil {
				fail(w, &ledger.Problem{Code: "invalid", Message: "分页参数无效"})
				return
			}
		}
		result, err := store.ImportHistory(page)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/imports/{id}", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		result, err := store.ImportDetail(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
