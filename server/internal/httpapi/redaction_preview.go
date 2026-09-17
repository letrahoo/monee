package httpapi

import "net/http"

func (a API) registerRedactionPreview(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/imports/{id}/redaction-preview", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		result, err := store.ImportRedactionPreview(r.PathValue("id"), a.Redaction)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
