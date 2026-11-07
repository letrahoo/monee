package httpapi

import "net/http"

func (a API) registerAnnual(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/annual", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		params, valid := queryParameters(w, r, "year")
		if !valid {
			return
		}
		out, err := s.Annual(params.Get("year"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}
