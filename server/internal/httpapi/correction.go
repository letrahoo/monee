package httpapi

import (
	"github.com/letrahoo/monee/server/internal/ledger"
	"net/http"
)

func (a API) registerCorrections(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/transactions/{id}/correction/preview", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var in ledger.CorrectionInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.PreviewCorrection(r.PathValue("id"), in)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("PATCH /api/v1/transactions/{id}/correction", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var in ledger.CorrectionInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.Correct(r.PathValue("id"), in)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /api/v1/transactions/{id}/corrections", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		out, err := s.CorrectionHistory(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
}
