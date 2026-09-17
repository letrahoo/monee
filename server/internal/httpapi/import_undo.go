package httpapi

import "net/http"

func (a API) registerImportUndo(mux *http.ServeMux) {
	for _, restore := range []bool{false, true} {
		action := "undo"
		if restore {
			action = "restore"
		}
		mux.HandleFunc("GET /api/v1/imports/{id}/"+action+"-preview", func(w http.ResponseWriter, r *http.Request) {
			s, ok := a.ledgerForRequest(w, r)
			if !ok {
				return
			}
			out, e := s.PreviewImportUndo(r.PathValue("id"), restore)
			if e != nil {
				fail(w, e)
				return
			}
			writeJSON(w, 200, out)
		})
		mux.HandleFunc("POST /api/v1/imports/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			s, ok := a.ledgerForRequest(w, r)
			if !ok {
				return
			}
			var in struct {
				LedgerVersion int64 `json:"ledgerVersion"`
			}
			if !decode(w, r, &in) {
				return
			}
			operation := s.UndoImport
			if restore {
				operation = s.RestoreImport
			}
			out, e := operation(r.PathValue("id"), in.LedgerVersion)
			if e != nil {
				fail(w, e)
				return
			}
			writeJSON(w, 200, out)
		})
	}
}
