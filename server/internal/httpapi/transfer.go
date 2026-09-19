package httpapi

import (
	"net/http"

	"github.com/letrahoo/monee/server/internal/ledger"
)

func (a API) registerTransfers(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/transfers", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var in ledger.TransferInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.CreateTransfer(in, r.Header.Get("Idempotency-Key"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	})
}
