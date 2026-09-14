package auth

import (
	"net/http"
)

func (s *Service) registerAccount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/users", s.listAccounts)
	mux.HandleFunc("PATCH /api/v1/admin/users/{id}", s.setAccountAccess)
	mux.HandleFunc("GET /api/v1/auth/account", s.account)
	mux.HandleFunc("PATCH /api/v1/auth/account", s.renameAccount)
	mux.HandleFunc("POST /api/v1/auth/account/unlink", s.unlink)
	mux.HandleFunc("POST /api/v1/auth/account/merge", s.merge)
}
func (s *Service) account(w http.ResponseWriter, r *http.Request) {
	u, ok := s.user(w, r, true)
	if !ok {
		return
	}
	identities, err := s.store.Identities(u.ID)
	if err != nil {
		errorResponse(w, 500, "internal", "读取账号失败")
		return
	}
	pending := []map[string]string{}
	s.mu.Lock()
	s.cleanupLocked()
	for _, f := range s.flows {
		if f.LinkUser == u.ID && f.Status == "merge_required" {
			pending = append(pending, map[string]string{"id": f.ID, "provider": f.Identity.Provider, "name": f.Identity.DisplayName, "username": f.Identity.Username})
		}
	}
	s.mu.Unlock()
	jsonResponse(w, 200, map[string]any{"user": u, "identities": identities, "pendingMerges": pending})
}
func (s *Service) renameAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := s.user(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if err := s.store.Rename(u.ID, input.Name); err != nil {
		errorResponse(w, 422, "invalid", err.Error())
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *Service) unlink(w http.ResponseWriter, r *http.Request) {
	u, ok := s.user(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Provider string `json:"provider"`
		Subject  string `json:"subject"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if err := s.store.Unlink(u.ID, input.Provider, input.Subject); err != nil {
		errorResponse(w, 409, "conflict", err.Error())
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *Service) merge(w http.ResponseWriter, r *http.Request) {
	u, ok := s.user(w, r, true)
	if !ok {
		return
	}
	var input struct {
		ID string `json:"id"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	f, exists := s.flows[input.ID]
	token, _ := s.requestToken(r)
	if !exists || f.LinkUser != u.ID || f.Status != "merge_required" || digest(token) != digest(f.LinkSession) {
		errorResponse(w, 409, "conflict", "合并验证已失效，请重新绑定")
		return
	}
	if err := s.store.LinkVerified(u.ID, f.Identity, true); err != nil {
		errorResponse(w, 409, "conflict", err.Error())
		return
	}
	delete(s.flows, input.ID)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (s *Service) listAccounts(w http.ResponseWriter, r *http.Request) {
	u, ok := s.admin(w, r)
	if !ok {
		return
	}
	list, err := s.store.Accounts(u.Identity)
	if err != nil {
		errorResponse(w, 500, "internal", "读取注册账号失败")
		return
	}
	jsonResponse(w, 200, map[string]any{"users": list})
}
func (s *Service) setAccountAccess(w http.ResponseWriter, r *http.Request) {
	u, ok := s.admin(w, r)
	if !ok {
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
		Version int64 `json:"version"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		errorResponse(w, 422, "invalid", "缺少启用状态")
		return
	}
	if err := s.store.SetAccountEnabled(u.Identity, r.PathValue("id"), *input.Enabled, input.Version); err != nil {
		errorResponse(w, 409, "conflict", err.Error())
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
