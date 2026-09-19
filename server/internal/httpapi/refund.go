package httpapi

import (
	"github.com/letrahoo/monee/server/internal/ledger"
	"net/http"
)

func (a API) registerRefunds(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/transactions/{id}/refunds", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		out, err := s.Refunds(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("POST /api/v1/transactions/{id}/refunds", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var in ledger.RefundInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.CreateRefund(r.PathValue("id"), in, r.Header.Get("Idempotency-Key"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 201, out)
	})
}
