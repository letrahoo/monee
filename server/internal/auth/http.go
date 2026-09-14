package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

type flow struct {
	ID, Ticket, State, Binding, Verifier, Nonce, Provider, Client, Challenge, Status string
	Identity                                                                         Identity
	Expires                                                                          time.Time
	LinkUser, LinkSession                                                            string
}
type Service struct {
	store         *Store
	providers     map[string]Provider
	baseURL       string
	mu            sync.Mutex
	flows         map[string]*flow
	resolveGitHub func(context.Context, string) (string, string, error)
}

func NewService(store *Store, providers map[string]Provider, baseURL string) *Service {
	return &Service{store: store, providers: providers, baseURL: baseURL, flows: map[string]*flow{}, resolveGitHub: ResolveGitHub}
}
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func errorResponse(w http.ResponseWriter, status int, code, message string) {
	jsonResponse(w, status, map[string]string{"code": code, "message": message})
}
func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		errorResponse(w, 415, "content_type", "请使用 JSON 请求")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		errorResponse(w, 400, "invalid", "请求格式无效")
		return false
	}
	return true
}
func (s *Service) requestToken(r *http.Request) (string, bool) {
	if header := r.Header.Get("Authorization"); header != "" {
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok {
			return "", false
		}
		return token, false
	}
	c, err := r.Cookie(s.sessionCookie())
	if err != nil {
		return "", true
	}
	return c.Value, true
}
func unsafe(method string) bool { return method != "GET" && method != "HEAD" && method != "OPTIONS" }
func (s *Service) user(w http.ResponseWriter, r *http.Request, required bool) (*User, bool) {
	token, cookie := s.requestToken(r)
	u, err := s.store.Session(token)
	if err != nil {
		errorResponse(w, 500, "internal", "无法验证登录状态")
		return nil, false
	}
	if u == nil && required {
		errorResponse(w, 401, "unauthenticated", "请先登录")
		return nil, false
	}
	if u != nil && cookie && unsafe(r.Method) && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Monee-CSRF")), []byte(csrfToken(token))) != 1 {
		errorResponse(w, 403, "csrf", "登录验证已变化，请刷新页面后重试")
		return nil, false
	}
	return u, true
}
func (s *Service) CurrentUser(w http.ResponseWriter, r *http.Request) (*User, bool) {
	return s.user(w, r, true)
}

func (s *Service) AuthorizeData(w http.ResponseWriter, r *http.Request) bool {
	u, ok := s.user(w, r, true)
	if !ok {
		return false
	}
	if !u.Allowed {
		errorResponse(w, 403, "access_denied", "无数据访问权限，请联系超管添加白名单")
		return false
	}
	return true
}
func (s *Service) admin(w http.ResponseWriter, r *http.Request) (*User, bool) {
	u, ok := s.user(w, r, true)
	if !ok {
		return nil, false
	}
	if !u.Allowed || u.Role != "superadmin" {
		errorResponse(w, 403, "admin_required", errAdmin.Error())
		return nil, false
	}
	return u, true
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/me", s.me)
	s.registerAccount(mux)
	mux.HandleFunc("POST /api/v1/auth/start", s.start)
	mux.HandleFunc("POST /api/v1/auth/native/poll", s.poll)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /auth/begin", s.begin)
	mux.HandleFunc("GET /auth/callback/{provider}", s.callback)
	mux.HandleFunc("GET /auth/result", s.result)
	mux.HandleFunc("GET /api/v1/admin/allowlist", s.list)
	mux.HandleFunc("POST /api/v1/admin/allowlist", s.add)
	mux.HandleFunc("PATCH /api/v1/admin/allowlist/{id}", s.setEnabled)
	mux.HandleFunc("GET /api/v1/admin/audit", s.audit)
}
func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	u, ok := s.user(w, r, false)
	if !ok {
		return
	}
	providers := []map[string]any{}
	for _, name := range []string{"google", "github"} {
		_, enabled := s.providers[name]
		providers = append(providers, map[string]any{"id": name, "enabled": enabled})
	}
	csrf := ""
	if u != nil {
		token, _ := s.requestToken(r)
		csrf = csrfToken(token)
	}
	jsonResponse(w, 200, map[string]any{"user": u, "providers": providers, "csrfToken": csrf})
}

var proofPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func (s *Service) cleanupLocked() {
	for id, f := range s.flows {
		if time.Now().After(f.Expires) {
			delete(s.flows, id)
		}
	}
}
func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Provider  string `json:"provider"`
		Client    string `json:"client"`
		Challenge string `json:"challenge"`
		Intent    string `json:"intent"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if _, ok := s.providers[input.Provider]; !ok {
		errorResponse(w, 503, "provider_unconfigured", "此登录方式尚未配置，请联系超管")
		return
	}
	if (input.Client != "web" && input.Client != "desktop") || (input.Client == "desktop" && !proofPattern.MatchString(input.Challenge)) || (input.Client == "web" && input.Challenge != "") {
		errorResponse(w, 422, "invalid", "登录客户端信息无效")
		return
	}
	var linkUser, linkSession string
	if input.Intent != "" && input.Intent != "login" && input.Intent != "link" {
		errorResponse(w, 422, "invalid", "无效操作")
		return
	}
	if input.Intent == "link" {
		u, ok := s.user(w, r, true)
		if !ok {
			return
		}
		if !u.Allowed {
			errorResponse(w, 403, "access_denied", "账号尚未获准")
			return
		}
		linkUser = u.ID
		linkSession, _ = s.requestToken(r)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	if len(s.flows) >= 128 {
		errorResponse(w, 429, "rate_limited", "登录请求过多，请稍后重试")
		return
	}
	f := &flow{ID: randomToken(), Ticket: randomToken(), State: randomToken(), Verifier: randomToken(), Nonce: randomToken(), Provider: input.Provider, Client: input.Client, Challenge: input.Challenge, Status: "pending", Expires: time.Now().Add(10 * time.Minute)}
	f.LinkUser = linkUser
	f.LinkSession = linkSession
	s.flows[f.ID] = f
	jsonResponse(w, 200, map[string]any{"id": f.ID, "url": s.baseURL + "/auth/begin?ticket=" + f.Ticket, "expiresIn": 600})
}
func cookieName(state string) string { return "monee_flow_" + digest(state)[:12] }
func (s *Service) begin(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("ticket")
	if !proofPattern.MatchString(ticket) {
		s.redirectResult(w, r, "invalid")
		return
	}
	s.mu.Lock()
	s.cleanupLocked()
	var found *flow
	for _, f := range s.flows {
		if f.Ticket == ticket && f.Status == "pending" {
			found = f
			break
		}
	}
	if found == nil {
		s.mu.Unlock()
		s.redirectResult(w, r, "invalid")
		return
	}
	found.Ticket = ""
	found.Binding = randomToken()
	f := *found
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName(f.State), Value: f.Binding, Path: "/auth/callback/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, s.providers[f.Provider].AuthorizationURL(f.State, f.Nonce, f.Verifier), http.StatusSeeOther)
}
func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	provider := r.PathValue("provider")
	if !proofPattern.MatchString(state) {
		s.redirectResult(w, r, "invalid")
		return
	}
	cookie, err := r.Cookie(cookieName(state))
	if err != nil {
		s.redirectResult(w, r, "invalid")
		return
	}
	s.mu.Lock()
	s.cleanupLocked()
	var found *flow
	for _, f := range s.flows {
		if f.State == state && f.Provider == provider && f.Status == "pending" && f.Binding != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(f.Binding)) == 1 {
			found = f
			break
		}
	}
	if found == nil {
		s.mu.Unlock()
		s.redirectResult(w, r, "invalid")
		return
	}
	found.Status = "exchanging"
	f := *found
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName(state), Value: "", Path: "/auth/callback/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if r.URL.Query().Get("error") != "" {
		s.failFlow(f.ID)
		s.redirectResult(w, r, "cancelled")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 4096 {
		s.failFlow(f.ID)
		s.redirectResult(w, r, "invalid")
		return
	}
	identity, err := s.providers[f.Provider].Exchange(r.Context(), code, f.Verifier, f.Nonce)
	if err != nil || identity.Provider != f.Provider {
		s.failFlow(f.ID)
		s.redirectResult(w, r, "failed")
		return
	}
	_, priorErr := s.store.AccountID(identity)
	if priorErr != nil && !errors.Is(priorErr, sql.ErrNoRows) {
		s.failFlow(f.ID)
		s.redirectResult(w, r, "failed")
		return
	}
	if err = s.store.RememberIdentity(identity); err != nil {
		s.failFlow(f.ID)
		s.redirectResult(w, r, "failed")
		return
	}
	if f.LinkUser != "" {
		current, e := s.store.Session(f.LinkSession)
		if e != nil || current == nil || !current.Allowed || current.ID != f.LinkUser {
			s.failFlow(f.ID)
			s.redirectResult(w, r, "failed")
			return
		}
		target, e := s.store.AccountID(identity)
		if e != nil {
			s.failFlow(f.ID)
			s.redirectResult(w, r, "failed")
			return
		}
		status := "linked"
		if target != f.LinkUser && priorErr == nil {
			status = "merge_required"
		} else if target != f.LinkUser {
			if e = s.store.LinkVerified(f.LinkUser, identity, true); e != nil {
				s.failFlow(f.ID)
				s.redirectResult(w, r, "failed")
				return
			}
		}
		s.mu.Lock()
		if current, ok := s.flows[f.ID]; ok {
			current.Status = status
			current.Identity = identity
		}
		s.mu.Unlock()
		s.redirectResult(w, r, "linked")
		return
	}
	if f.Client == "desktop" {
		s.mu.Lock()
		if current, ok := s.flows[f.ID]; ok {
			current.Status = "complete"
			current.Identity = identity
		}
		s.mu.Unlock()
		s.redirectResult(w, r, "desktop")
		return
	}
	token, err := s.store.CreateSession(identity)
	if err != nil {
		s.failFlow(f.ID)
		s.redirectResult(w, r, "failed")
		return
	}
	s.mu.Lock()
	delete(s.flows, f.ID)
	s.mu.Unlock()
	// Loopback HTTP only. Keep credentials HttpOnly; JSON writes additionally require CSRF proof.
	http.SetCookie(w, &http.Cookie{Name: s.sessionCookie(), Value: token, Path: "/api/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionLifetime.Seconds())})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (s *Service) failFlow(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.flows[id]; ok {
		f.Status = "failed"
	}
}
func (s *Service) poll(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID       string `json:"id"`
		Verifier string `json:"verifier"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !proofPattern.MatchString(input.ID) || !proofPattern.MatchString(input.Verifier) {
		errorResponse(w, 400, "invalid", "登录验证信息无效")
		return
	}
	s.mu.Lock()
	s.cleanupLocked()
	f, ok := s.flows[input.ID]
	if !ok || f.Client != "desktop" || subtle.ConstantTimeCompare([]byte(digest(input.Verifier)), []byte(f.Challenge)) != 1 {
		s.mu.Unlock()
		errorResponse(w, 401, "login_expired", "登录已过期，请重新发起")
		return
	}
	switch f.Status {
	case "linked", "merge_required":
		status := f.Status
		s.mu.Unlock()
		jsonResponse(w, 200, map[string]string{"status": status, "token": ""})
		return
	case "complete":
		identity := f.Identity
		delete(s.flows, f.ID)
		s.mu.Unlock()
		token, err := s.store.CreateSession(identity)
		if err != nil {
			errorResponse(w, 500, "internal", "登录未完成，请重新发起")
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "complete", "token": token})
	case "failed":
		delete(s.flows, f.ID)
		s.mu.Unlock()
		jsonResponse(w, 200, map[string]string{"status": "failed", "token": ""})
	default:
		s.mu.Unlock()
		jsonResponse(w, 200, map[string]string{"status": "pending", "token": ""})
	}
}
func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.user(w, r, false); !ok {
		return
	}
	token, _ := s.requestToken(r)
	if err := s.store.Logout(token); err != nil {
		errorResponse(w, 500, "internal", "退出登录失败，请重试")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.sessionCookie(), Value: "", Path: "/api/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	u, ok := s.admin(w, r)
	if !ok {
		return
	}
	entries, err := s.store.List(u.Identity)
	if err != nil {
		errorResponse(w, 500, "internal", "读取白名单失败")
		return
	}
	jsonResponse(w, 200, map[string]any{"entries": entries})
}
func (s *Service) add(w http.ResponseWriter, r *http.Request) {
	u, ok := s.admin(w, r)
	if !ok {
		return
	}
	var input Selector
	if !readJSON(w, r, &input) {
		return
	}
	if _, err := entryFor(input); err != nil {
		errorResponse(w, 422, "invalid", err.Error())
		return
	}
	subject := ""
	if input.Kind == "username" {
		var err error
		subject, _, err = s.resolveGitHub(r.Context(), strings.TrimSpace(input.Value))
		if err != nil {
			errorResponse(w, 422, "invalid", err.Error())
			return
		}
	}
	entry, err := s.store.Add(u.Identity, input, subject)
	if err != nil {
		errorResponse(w, 409, "conflict", err.Error())
		return
	}
	jsonResponse(w, 201, entry)
}
func (s *Service) setEnabled(w http.ResponseWriter, r *http.Request) {
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
	entry, err := s.store.SetEnabled(u.Identity, r.PathValue("id"), *input.Enabled, input.Version)
	if err != nil {
		errorResponse(w, 409, "conflict", err.Error())
		return
	}
	jsonResponse(w, 200, entry)
}
func (s *Service) audit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.admin(w, r)
	if !ok {
		return
	}
	events, err := s.store.Audit(u.Identity)
	if err != nil {
		errorResponse(w, 500, "internal", "读取操作记录失败")
		return
	}
	jsonResponse(w, 200, map[string]any{"events": events})
}
func (s *Service) redirectResult(w http.ResponseWriter, r *http.Request, status string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/auth/result?status="+status, http.StatusSeeOther)
}

var resultPage = template.Must(template.New("result").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Monee 登录</title><style>body{font-family:system-ui;background:#f6f7f2;color:#20372f;margin:0;padding:10vh 24px}main{max-width:520px;margin:auto;padding:32px;background:white;border-radius:20px}h1{color:#165b4a}a{color:#165b4a;display:inline-block;margin-top:16px}</style><main><h1>Monee</h1><h2>{{.Title}}</h2><p>{{.Message}}</p>{{if .Desktop}}<a href="monee://auth/complete">返回 Monee</a><p>如有提示，请选择打开 Monee。</p><p>若已退出应用，请重新登录。</p>{{end}}<a href="/">返回首页</a></main></html>`))

func (s *Service) result(w http.ResponseWriter, r *http.Request) {
	title, message := "登录未完成", "登录请求已失效，请返回应用重新发起。"
	switch r.URL.Query().Get("status") {
	case "linked":
		title = "身份验证已完成"
		message = "请返回应用查看绑定结果；已注册账号需要确认合并。"
	case "desktop":
		title = "已完成账号验证"
		message = "正在返回 Monee。未自动返回时，请点击下方按钮。"
	case "cancelled":
		title = "已取消登录"
		message = "你可以返回应用，重新选择登录账号。"
	case "failed":
		message = "账号验证失败，请重试。"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	resultPage.Execute(w, map[string]any{"Title": title, "Message": message, "Desktop": r.URL.Query().Get("status") == "desktop"})
}
func (s *Service) sessionCookie() string { return "monee_session_" + digest(s.baseURL)[:12] }

// Callback exceptions are restricted to actually configured adapters.
func (s *Service) IsCallback(path string) bool {
	provider, ok := strings.CutPrefix(path, "/auth/callback/")
	if !ok {
		return false
	}
	_, enabled := s.providers[provider]
	return enabled
}
