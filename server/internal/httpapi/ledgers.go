package httpapi

import (
	"github.com/letrahoo/monee/server/internal/ledger"
	"net/http"
)

func (a API) ledgerForRequest(w http.ResponseWriter, r *http.Request) (*ledger.Store, bool) {
	if a.Auth == nil {
		writeJSON(w, 503, map[string]string{"code": "auth_unavailable"})
		return nil, false
	}
	user, ok := a.Auth.CurrentUser(w, r)
	if !ok {
		return nil, false
	}
	id := r.PathValue("ledger")
	if id == "" {
		id = r.Header.Get("X-Monee-Ledger")
	}
	store, err := a.Store.Scoped(user.ID, id)
	if err != nil {
		fail(w, err)
		return nil, false
	}
	return store, true
}
func (a API) registerLedgers(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/ledgers", func(w http.ResponseWriter, r *http.Request) {
		u, ok := a.Auth.CurrentUser(w, r)
		if !ok {
			return
		}
		list, err := a.Store.ListLedgers(u.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ledgers": list})
	})
	mux.HandleFunc("POST /api/v1/ledgers", func(w http.ResponseWriter, r *http.Request) {
		u, ok := a.Auth.CurrentUser(w, r)
		if !ok {
			return
		}
		var input struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &input) {
			return
		}
		l, err := a.Store.CreateLedger(u.ID, input.Name)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 201, l)
	})
	mux.HandleFunc("GET /api/v1/ledgers/{ledger}/members", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		members, err := s.Members()
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"members": members})
	})
	mux.HandleFunc("POST /api/v1/ledgers/{ledger}/invitations", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var input struct {
			UserID string `json:"userId"`
			Role   string `json:"role"`
		}
		if !decode(w, r, &input) {
			return
		}
		if err := s.Invite(input.UserID, input.Role); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 201, map[string]bool{"ok": true})
	})
	mux.HandleFunc("PATCH /api/v1/ledgers/{ledger}/members/{user}", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var input struct {
			Role    string `json:"role"`
			Version int64  `json:"version"`
		}
		if !decode(w, r, &input) {
			return
		}
		if err := s.ChangeMember(r.PathValue("user"), input.Role, input.Version); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/v1/invitations", func(w http.ResponseWriter, r *http.Request) {
		u, ok := a.Auth.CurrentUser(w, r)
		if !ok {
			return
		}
		list, err := a.Store.Invitations(u.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"invitations": list})
	})
	mux.HandleFunc("POST /api/v1/invitations/{id}/respond", func(w http.ResponseWriter, r *http.Request) {
		u, ok := a.Auth.CurrentUser(w, r)
		if !ok {
			return
		}
		var input struct {
			Accept *bool `json:"accept"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.Accept == nil {
			writeJSON(w, 422, map[string]string{"code": "invalid", "message": "请选择接受或拒绝"})
			return
		}
		if err := a.Store.RespondInvitation(u.ID, r.PathValue("id"), *input.Accept); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
}
