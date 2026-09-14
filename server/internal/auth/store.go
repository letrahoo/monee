package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const sessionLifetime = 12 * time.Hour

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func digest(value string) string {
	b := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func csrfToken(token string) string { return digest("monee-csrf-v1:" + token) }
func timestamp() string             { return time.Now().UTC().Format(time.RFC3339Nano) }

type Entry struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	Subject   string `json:"subject"`
	Note      string `json:"note"`
	Role      string `json:"role"`
	Enabled   bool   `json:"enabled"`
	Protected bool   `json:"protected"`
	Version   int64  `json:"version"`
	CreatedAt string `json:"createdAt"`
}
type User struct {
	Identity
	Allowed bool   `json:"allowed"`
	Role    string `json:"role"`
}
type Audit struct {
	ID        int64  `json:"id"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	EntryID   string `json:"entryId"`
	CreatedAt string `json:"createdAt"`
}
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

var errDenied = errors.New("无数据访问权限")
var errAdmin = errors.New("仅超管可以管理白名单")
var subjectPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,255}$`)

const authSchema = `
CREATE TABLE auth_meta(key TEXT PRIMARY KEY,value TEXT NOT NULL);
CREATE TABLE users(provider TEXT NOT NULL,subject TEXT NOT NULL,username TEXT NOT NULL,email TEXT NOT NULL,display_name TEXT NOT NULL,PRIMARY KEY(provider,subject));
CREATE TABLE allowlist(
 id TEXT PRIMARY KEY,provider TEXT NOT NULL CHECK(provider IN ('google','github')),kind TEXT NOT NULL,value TEXT NOT NULL,subject TEXT,
 note TEXT NOT NULL,role TEXT NOT NULL CHECK(role IN ('member','superadmin')),enabled INTEGER NOT NULL,protected INTEGER NOT NULL,version INTEGER NOT NULL,
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL,UNIQUE(provider,kind,value),UNIQUE(provider,subject));
CREATE TABLE sessions(token_hash TEXT PRIMARY KEY,provider TEXT NOT NULL,subject TEXT NOT NULL,expires_at INTEGER NOT NULL,FOREIGN KEY(provider,subject) REFERENCES users(provider,subject));
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE access_audit(id INTEGER PRIMARY KEY AUTOINCREMENT,actor TEXT NOT NULL,action TEXT NOT NULL,entry_id TEXT NOT NULL,created_at TEXT NOT NULL);
PRAGMA user_version=1;`

func Open(path string, superadmins []Selector) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err = s.initialize(superadmins); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) initialize(seeds []Selector) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return errors.New("auth database requires a newer application")
	}
	if version == 0 {
		if _, err = tx.Exec(authSchema); err != nil {
			return err
		}
	}
	var initialized int
	if err = tx.QueryRow("SELECT COUNT(*) FROM auth_meta WHERE key='superadmins_initialized'").Scan(&initialized); err != nil {
		return err
	}
	if initialized == 0 && len(seeds) > 0 {
		for _, seed := range seeds {
			if seed.Kind == "username" {
				return errors.New("bootstrap GitHub administrators must use a stable subject ID")
			}
			e, err := entryFor(seed)
			if err != nil {
				return err
			}
			e.Role = "superadmin"
			e.Protected = true
			if err = insertEntry(tx, e, "bootstrap"); err != nil {
				return err
			}
		}
		if _, err = tx.Exec("INSERT INTO auth_meta VALUES('superadmins_initialized','1')"); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func entryFor(in Selector) (Entry, error) {
	in.Value = strings.TrimSpace(in.Value)
	in.Note = strings.TrimSpace(in.Note)
	if in.Provider != "google" && in.Provider != "github" {
		return Entry{}, errors.New("请选择 Google 或 GitHub")
	}
	if len([]rune(in.Note)) > 200 || strings.ContainsAny(in.Note, "\x00\r\n") {
		return Entry{}, errors.New("备注最多 200 字且不能换行")
	}
	e := Entry{ID: randomToken(), Provider: in.Provider, Kind: in.Kind, Value: in.Value, Note: in.Note, Role: "member", Enabled: true, Version: 1, CreatedAt: timestamp()}
	switch in.Kind {
	case "subject":
		if !subjectPattern.MatchString(in.Value) || (in.Provider == "github" && !numericID.MatchString(in.Value)) {
			return e, errors.New("账号 ID 格式无效")
		}
		e.Subject = in.Value
	case "email":
		a, err := mail.ParseAddress(in.Value)
		if in.Provider != "google" || err != nil || a.Address != in.Value || len(in.Value) > 254 {
			return e, errors.New("请填写有效 Google 登录邮箱")
		}
		e.Value = strings.ToLower(in.Value)
	case "username":
		if in.Provider != "github" || !githubLogin.MatchString(in.Value) {
			return e, errors.New("用户名仅支持 GitHub；Google 请使用账号 ID 或邮箱")
		}
		e.Value = strings.ToLower(in.Value)
	default:
		return e, errors.New("请选择账号 ID、GitHub 用户名或 Google 邮箱")
	}
	return e, nil
}
func insertEntry(tx *sql.Tx, e Entry, actor string) error {
	var sub any
	if e.Subject != "" {
		sub = e.Subject
	}
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM allowlist WHERE provider=? AND ((kind=? AND value=?) OR subject=?)", e.Provider, e.Kind, e.Value, sub).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("该账号已在白名单中，请修改现有条目")
	}
	_, err := tx.Exec("INSERT INTO allowlist VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", e.ID, e.Provider, e.Kind, e.Value, sub, e.Note, e.Role, e.Enabled, e.Protected, e.Version, e.CreatedAt, e.CreatedAt)
	if err != nil {
		return err
	}
	return audit(tx, actor, "add", e.ID)
}
func audit(tx *sql.Tx, actor, action, id string) error {
	_, err := tx.Exec("INSERT INTO access_audit(actor,action,entry_id,created_at) VALUES(?,?,?,?)", actor, action, id, timestamp())
	return err
}
func requireAdmin(tx *sql.Tx, actor Identity) error {
	var count int
	err := tx.QueryRow("SELECT COUNT(*) FROM allowlist WHERE provider=? AND subject=? AND enabled=1 AND role='superadmin'", actor.Provider, actor.Subject).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return errAdmin
	}
	return nil
}
func (s *Store) RememberIdentity(i Identity) error {
	if (i.Provider != "google" && i.Provider != "github") || !subjectPattern.MatchString(i.Subject) {
		return errors.New("无效的登录身份")
	}
	if len(i.Username) > 255 || len(i.Email) > 320 || len([]rune(i.DisplayName)) > 500 {
		return errors.New("无效的账号信息")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO users VALUES(?,?,?,?,?) ON CONFLICT(provider,subject) DO UPDATE SET username=excluded.username,email=excluded.email,display_name=excluded.display_name`, i.Provider, i.Subject, i.Username, i.Email, i.DisplayName)
	if err != nil {
		return err
	}
	if i.Provider == "google" && i.EmailTrusted && i.Email != "" {
		// An email invitation binds once to a verified subject. It never follows a recycled email.
		var bound int
		if err = tx.QueryRow("SELECT COUNT(*) FROM allowlist WHERE provider=? AND subject=?", i.Provider, i.Subject).Scan(&bound); err != nil {
			return err
		}
		if bound == 0 {
			var id string
			err = tx.QueryRow("SELECT id FROM allowlist WHERE provider='google' AND kind='email' AND value=? AND subject IS NULL AND enabled=1", strings.ToLower(i.Email)).Scan(&id)
			if err == nil {
				if _, err = tx.Exec("UPDATE allowlist SET subject=?,version=version+1,updated_at=? WHERE id=?", i.Subject, timestamp(), id); err != nil {
					return err
				}
				if err = audit(tx, "google:"+i.Subject, "bind", id); err != nil {
					return err
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
	}
	return tx.Commit()
}
func (s *Store) CreateSession(i Identity) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := randomToken()
	_, err := s.db.Exec("INSERT INTO sessions VALUES(?,?,?,?)", digest(token), i.Provider, i.Subject, time.Now().Add(sessionLifetime).Unix())
	return token, err
}
func (s *Store) Session(token string) (*User, error) {
	if len(token) != 43 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var u User
	var role sql.NullString
	var enabled sql.NullBool
	err := s.db.QueryRow(`SELECT u.provider,u.subject,u.username,u.email,u.display_name,a.role,a.enabled
 FROM sessions s JOIN users u ON u.provider=s.provider AND u.subject=s.subject
 LEFT JOIN allowlist a ON a.provider=u.provider AND a.subject=u.subject WHERE s.token_hash=? AND s.expires_at>?`, digest(token), time.Now().Unix()).Scan(&u.Provider, &u.Subject, &u.Username, &u.Email, &u.DisplayName, &role, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.Allowed = enabled.Valid && enabled.Bool
	if u.Allowed {
		u.Role = role.String
	}
	return &u, nil
}
func (s *Store) Logout(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM sessions WHERE token_hash=?", digest(token))
	return err
}

const entryColumns = "id,provider,kind,value,COALESCE(subject,''),note,role,enabled,protected,version,created_at"

func scanEntry(row interface{ Scan(...any) error }) (Entry, error) {
	var e Entry
	err := row.Scan(&e.ID, &e.Provider, &e.Kind, &e.Value, &e.Subject, &e.Note, &e.Role, &e.Enabled, &e.Protected, &e.Version, &e.CreatedAt)
	return e, err
}
func (s *Store) List(actor Identity) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireAdmin(tx, actor); err != nil {
		return nil, err
	}
	rows, err := tx.Query("SELECT " + entryColumns + " FROM allowlist ORDER BY protected DESC,created_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
func (s *Store) Add(actor Identity, selector Selector, resolvedSubject string) (Entry, error) {
	e, err := entryFor(selector)
	if err != nil {
		return e, err
	}
	if e.Kind == "username" {
		if !numericID.MatchString(resolvedSubject) {
			return e, errors.New("GitHub 用户名尚未验证")
		}
		e.Subject = resolvedSubject
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	if err = requireAdmin(tx, actor); err != nil {
		return e, err
	}
	if err = insertEntry(tx, e, actor.Provider+":"+actor.Subject); err != nil {
		return e, err
	}
	return e, tx.Commit()
}
func (s *Store) SetEnabled(actor Identity, id string, enabled bool, version int64) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	if err = requireAdmin(tx, actor); err != nil {
		return Entry{}, err
	}
	e, err := scanEntry(tx.QueryRow("SELECT "+entryColumns+" FROM allowlist WHERE id=?", id))
	if err != nil {
		return e, errors.New("白名单条目不存在")
	}
	if e.Protected || e.Role == "superadmin" {
		return e, errors.New("初始超管受保护，不能在白名单页面停用或修改")
	}
	if e.Version != version {
		return e, errors.New("白名单已变化，请刷新后重试")
	}
	if _, err = tx.Exec("UPDATE allowlist SET enabled=?,version=version+1,updated_at=? WHERE id=?", enabled, timestamp(), id); err != nil {
		return e, err
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	if err = audit(tx, actor.Provider+":"+actor.Subject, action, id); err != nil {
		return e, err
	}
	e.Enabled = enabled
	e.Version++
	return e, tx.Commit()
}
func (s *Store) Audit(actor Identity) ([]Audit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireAdmin(tx, actor); err != nil {
		return nil, err
	}
	rows, err := tx.Query("SELECT id,actor,action,entry_id,created_at FROM access_audit ORDER BY id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Audit{}
	for rows.Next() {
		var a Audit
		if err = rows.Scan(&a.ID, &a.Actor, &a.Action, &a.EntryID, &a.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
