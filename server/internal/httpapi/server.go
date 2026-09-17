package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/ledger"
)

type API struct {
	Store        *ledger.Store
	Host, WebDir string
	Auth         *auth.Service
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	status, code, message := 500, "internal", "本地服务处理失败，操作未确认，请重试"
	var p *ledger.Problem
	if errors.As(err, &p) {
		code, message = p.Code, p.Message
		status = 422
		if code == "conflict" {
			status = 409
		}
		if code == "ledger_denied" || code == "ledger_readonly" || code == "access_denied" {
			status = 403
		}
		if code == "not_found" {
			status = 404
		}
	} else {
		log.Printf("request failed: %v", err)
	}
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		writeJSON(w, 415, map[string]string{"code": "content_type", "message": "请使用 JSON 请求"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		writeJSON(w, 400, map[string]string{"code": "invalid", "message": "请求格式无效或超过大小限制"})
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		writeJSON(w, 400, map[string]string{"code": "invalid", "message": "请求只允许一个 JSON 对象"})
		return false
	}
	return true
}

func (a API) Handler() http.Handler {
	mux := http.NewServeMux()
	a.registerLedgers(mux)
	a.registerImports(mux)
	a.registerAnnotations(mux)
	a.registerCorrections(mux)
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "apiVersion": 1, "schemaVersion": 2})
	})
	if a.Auth != nil {
		a.Auth.Register(mux)
	}
	mux.HandleFunc("GET /api/v1/dashboard", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		page := 1
		var err error
		if value := r.URL.Query().Get("page"); value != "" {
			page, err = strconv.Atoi(value)
		}
		if err != nil {
			fail(w, &ledger.Problem{Code: "invalid", Message: "分页参数无效"})
			return
		}
		result, err := store.Dashboard(r.URL.Query().Get("month"), r.URL.Query().Get("q"), page)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("POST /api/v1/transactions", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var input ledger.Input
		if !decode(w, r, &input) {
			return
		}
		result, err := store.Create(input, r.Header.Get("Idempotency-Key"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 201, result)
	})
	mux.HandleFunc("POST /api/v1/imports/preview", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var input struct {
			Filename string `json:"filename"`
			CSV      string `json:"csv"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := store.Preview(input.Filename, input.CSV)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("POST /api/v1/imports/{id}/commit", func(w http.ResponseWriter, r *http.Request) {
		store, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var input struct {
			LedgerVersion  int64 `json:"ledgerVersion"`
			ConfirmSimilar bool  `json:"confirmSimilar"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := store.Commit(r.PathValue("id"), input.LedgerVersion, input.ConfirmSimilar)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("GET /api/v1/template", func(w http.ResponseWriter, r *http.Request) {
		text := "date,type,amount,currency,merchant,category,source,account,external_id,note\n"
		if r.URL.Query().Get("sample") == "true" {
			date := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
			text += fmt.Sprintf("%s,expense,28.50,CNY,示例咖啡,餐饮,示例支付宝,待核实资金账户,SAMPLE-001,合成示例\n%s,expense,96.00,CNY,示例书店,学习,示例微信,待核实资金账户,SAMPLE-002,合成示例\n%s,income,25000.00,CNY,示例工资,工资,示例银行,待核实资金账户,SAMPLE-003,合成示例\n", date, date, date)
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, text)
	})
	files := http.FileServer(http.Dir(a.WebDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		if a.WebDir == "" {
			http.Error(w, "Web assets are not configured", 503)
			return
		}
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		for _, part := range strings.Split(filepath.ToSlash(r.URL.Path), "/") {
			if strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		origin := r.Header.Get("Origin")
		site := r.Header.Get("Sec-Fetch-Site")
		callback := r.Method == "GET" && a.Auth != nil && a.Auth.IsCallback(r.URL.Path)
		navigation := r.Method == "GET" && r.Header.Get("Sec-Fetch-Mode") == "navigate" && (r.URL.Path == "/" || r.URL.Path == "/auth/begin" || r.URL.Path == "/auth/result")
		if r.Host != a.Host || (!callback && origin != "" && origin != "http://"+a.Host) || ((site == "cross-site" || site == "same-site") && !callback && !navigation) {
			writeJSON(w, 403, map[string]string{"code": "origin", "message": "拒绝非本地同源访问"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/health" && !strings.HasPrefix(r.URL.Path, "/api/v1/auth/") && !strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
			if a.Auth == nil {
				writeJSON(w, 503, map[string]string{"code": "auth_unavailable", "message": "登录服务尚未配置"})
				return
			}
			if !a.Auth.AuthorizeData(w, r) {
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
