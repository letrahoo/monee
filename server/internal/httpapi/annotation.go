package httpapi

import (
	"github.com/letrahoo/monee/server/internal/ledger"
	"net/http"
)

func (a API) registerAnnotations(mux *http.ServeMux) {
	mux.HandleFunc("PATCH /api/v1/transactions/{id}/annotation", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var in ledger.AnnotationInput
		if !decode(w, r, &in) {
			return
		}
		out, e := s.Annotate(r.PathValue("id"), in)
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /api/v1/transactions/{id}/annotations", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		out, e := s.AnnotationHistory(r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, out)
	})
}
