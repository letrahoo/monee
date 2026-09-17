package httpapi

import (
	"net/http"
	"strconv"

	"github.com/letrahoo/monee/server/internal/ledger"
)

func (a API) registerImports(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/transactions/{id}/sources", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		out, e := s.TransactionSources(r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("POST /api/v1/imports/alipay/preview", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var input struct {
			Filename      string `json:"filename"`
			ContentBase64 string `json:"contentBase64"`
			Account       string `json:"account"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := store.PreviewAlipay(input.Filename, input.ContentBase64, input.Account)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/v1/imports/wechat/preview", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var input struct {
			Filename      string `json:"filename"`
			ContentBase64 string `json:"contentBase64"`
			Account       string `json:"account"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := store.PreviewWeChat(input.Filename, input.ContentBase64, input.Account)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

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
