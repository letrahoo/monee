package ledger

import (
	"database/sql"
	"errors"
	"strings"
)

type queryer interface{ QueryRow(string, ...any) *sql.Row }

func nullActor(id string) any {
	if id == "" {
		return nil
	}
	return id
}
func admitted(q queryer, user string) error {
	var count int
	err := q.QueryRow(`SELECT COUNT(*) FROM app_users p JOIN user_access a ON a.user_id=p.id WHERE p.id=? AND p.status='active' AND a.enabled=1`, user).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return problem("access_denied", "账号未获准或已停用")
	}
	return nil
}
func (s *Store) authorize(q queryer, write bool) error {
	// Only internal initialization and isolated ledger tests use the root store.
	// HTTP always obtains a user-scoped store; it cannot select this mode.
	if s.actorID == "" {
		return nil
	}
	if err := admitted(q, s.actorID); err != nil {
		return err
	}
	var role string
	err := q.QueryRow("SELECT role FROM ledger_members WHERE ledger_id=? AND user_id=?", s.ledgerID, s.actorID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return problem("ledger_denied", "无此账本访问权限")
	}
	if err != nil {
		return err
	}
	if write && role == "viewer" {
		return problem("ledger_readonly", "此账本仅可查看")
	}
	return nil
}
func (s *Store) owner(q queryer) error {
	if s.actorID == "" {
		return problem("ledger_denied", "缺少操作账号")
	}
	if err := s.authorize(q, true); err != nil {
		return err
	}
	var role string
	if err := q.QueryRow("SELECT role FROM ledger_members WHERE ledger_id=? AND user_id=?", s.ledgerID, s.actorID).Scan(&role); err != nil {
		return err
	}
	if role != "owner" {
		return problem("ledger_denied", "仅所有者可以管理账本")
	}
	return nil
}
func (s *Store) Scoped(user, id string) (*Store, error) {
	if user == "" {
		return nil, problem("unauthenticated", "请先登录")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		rows, err := s.db.Query("SELECT ledger_id FROM ledger_members WHERE user_id=? ORDER BY ledger_id", user)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for rows.Next() {
			var v string
			if err = rows.Scan(&v); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(ids) != 1 {
			return nil, problem("ledger_required", "请选择账本")
		}
		id = ids[0]
	}
	scoped := &Store{db: s.db, mu: s.mu, ledgerID: id, actorID: user}
	if err := scoped.authorize(s.db, false); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow("SELECT device_id FROM ledgers WHERE id=?", id).Scan(&scoped.deviceID); err != nil {
		return nil, err
	}
	return scoped, nil
}

type LedgerInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Version int64  `json:"version"`
}
type Member struct {
	UserID  string `json:"userId"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Version int64  `json:"version"`
}
type Invitation struct {
	ID         string `json:"id"`
	LedgerID   string `json:"ledgerId"`
	LedgerName string `json:"ledgerName"`
	Role       string `json:"role"`
	InvitedBy  string `json:"invitedBy"`
}

func (s *Store) ListLedgers(user string) ([]LedgerInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := admitted(s.db, user); err != nil {
		return nil, err
	}
	rows, err := s.db.Query("SELECT l.id,l.name,m.role,l.version FROM ledgers l JOIN ledger_members m ON m.ledger_id=l.id WHERE m.user_id=? ORDER BY l.created_at,l.id", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []LedgerInfo{}
	for rows.Next() {
		var l LedgerInfo
		if err = rows.Scan(&l.ID, &l.Name, &l.Role, &l.Version); err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, rows.Err()
}
func validName(name string) bool {
	return strings.TrimSpace(name) != "" && len([]rune(name)) <= 80 && !strings.ContainsAny(name, "\x00\r\n")
}
func (s *Store) CreateLedger(user, name string) (LedgerInfo, error) {
	l := LedgerInfo{ID: newID(), Name: strings.TrimSpace(name), Role: "owner"}
	if !validName(l.Name) {
		return l, problem("invalid", "账本名称须为 1–80 字")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return l, err
	}
	defer tx.Rollback()
	if err = admitted(tx, user); err != nil {
		return l, err
	}
	if _, err = tx.Exec("INSERT INTO ledgers(id,currency,timezone,version,device_id,created_at,name) VALUES(?,'CNY','Asia/Shanghai',0,?,?,?)", l.ID, newID(), now(), l.Name); err != nil {
		return l, err
	}
	if _, err = tx.Exec("INSERT INTO ledger_members(ledger_id,user_id,role,created_at) VALUES(?,?,'owner',?)", l.ID, user, now()); err != nil {
		return l, err
	}
	if _, err = tx.Exec("INSERT INTO ledger_audit VALUES(?,?,?,?,?,?)", newID(), l.ID, user, "create_ledger", l.ID, now()); err != nil {
		return l, err
	}
	return l, tx.Commit()
}
func (s *Store) Members() ([]Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.owner(s.db); err != nil {
		return nil, err
	}
	rows, err := s.db.Query("SELECT p.id,p.display_name,m.role,m.version FROM ledger_members m JOIN app_users p ON p.id=m.user_id WHERE m.ledger_id=? ORDER BY m.created_at,p.id", s.ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Member{}
	for rows.Next() {
		var m Member
		if err = rows.Scan(&m.UserID, &m.Name, &m.Role, &m.Version); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
func (s *Store) memberAudit(tx *sql.Tx, action, target string) error {
	_, err := tx.Exec("INSERT INTO ledger_audit VALUES(?,?,?,?,?,?)", newID(), s.ledgerID, s.actorID, action, target, now())
	return err
}
func (s *Store) Invite(user, role string) error {
	if role != "editor" && role != "viewer" {
		return problem("invalid", "邀请角色须为可编辑或只读")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.owner(tx); err != nil {
		return err
	}
	if err = admitted(tx, user); err != nil {
		return problem("invalid", "对方账号不存在或尚未获准使用系统")
	}
	var count int
	if err = tx.QueryRow("SELECT COUNT(*) FROM ledger_members WHERE ledger_id=? AND user_id=?", s.ledgerID, user).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return problem("conflict", "对方已是账本成员")
	}
	_, err = tx.Exec("INSERT INTO ledger_invitations VALUES(?,?,?,?,?,'pending',?) ON CONFLICT(ledger_id,user_id,status) DO UPDATE SET role=excluded.role,invited_by=excluded.invited_by", newID(), s.ledgerID, user, role, s.actorID, now())
	if err != nil {
		return err
	}
	if err = s.memberAudit(tx, "invite", user); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Invitations(user string) ([]Invitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := admitted(s.db, user); err != nil {
		return nil, err
	}
	rows, err := s.db.Query("SELECT i.id,i.ledger_id,l.name,i.role,p.display_name FROM ledger_invitations i JOIN ledgers l ON l.id=i.ledger_id JOIN app_users p ON p.id=i.invited_by WHERE i.user_id=? AND i.status='pending' ORDER BY i.created_at", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Invitation{}
	for rows.Next() {
		var i Invitation
		if err = rows.Scan(&i.ID, &i.LedgerID, &i.LedgerName, &i.Role, &i.InvitedBy); err != nil {
			return nil, err
		}
		result = append(result, i)
	}
	return result, rows.Err()
}
func (s *Store) RespondInvitation(user, id string, accept bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = admitted(tx, user); err != nil {
		return err
	}
	var ledgerID, role, inviter string
	err = tx.QueryRow("SELECT ledger_id,role,invited_by FROM ledger_invitations WHERE id=? AND user_id=? AND status='pending'", id, user).Scan(&ledgerID, &role, &inviter)
	if errors.Is(err, sql.ErrNoRows) {
		return problem("not_found", "邀请已失效")
	}
	if err != nil {
		return err
	}
	if accept {
		var currentOwner string
		if err = tx.QueryRow("SELECT user_id FROM ledger_members WHERE ledger_id=? AND role='owner'", ledgerID).Scan(&currentOwner); err != nil {
			return err
		}
		if currentOwner != inviter {
			return problem("conflict", "账本所有者已变化，请重新邀请")
		}
		if _, err = tx.Exec("INSERT INTO ledger_members(ledger_id,user_id,role,created_at) VALUES(?,?,?,?) ON CONFLICT(ledger_id,user_id) DO NOTHING", ledgerID, user, role, now()); err != nil {
			return err
		}
	}
	// Remove consumed invitations; immutable audit retains the response.
	if _, err = tx.Exec("DELETE FROM ledger_invitations WHERE id=?", id); err != nil {
		return err
	}
	action := "decline_invitation"
	if accept {
		action = "accept_invitation"
	}
	if _, err = tx.Exec("INSERT INTO ledger_audit VALUES(?,?,?,?,?,?)", newID(), ledgerID, user, action, id, now()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) ChangeMember(user, role string, version int64) error {
	if role != "editor" && role != "viewer" && role != "remove" {
		return problem("invalid", "成员角色无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.owner(tx); err != nil {
		return err
	}
	var current string
	var v int64
	err = tx.QueryRow("SELECT role,version FROM ledger_members WHERE ledger_id=? AND user_id=?", s.ledgerID, user).Scan(&current, &v)
	if errors.Is(err, sql.ErrNoRows) {
		return problem("not_found", "成员不存在")
	}
	if err != nil {
		return err
	}
	if current == "owner" {
		return problem("invalid", "不能移除或降级所有者")
	}
	if v != version {
		return problem("conflict", "成员权限已变化，请刷新")
	}
	if role == "remove" {
		_, err = tx.Exec("DELETE FROM ledger_members WHERE ledger_id=? AND user_id=?", s.ledgerID, user)
	} else {
		_, err = tx.Exec("UPDATE ledger_members SET role=?,version=version+1 WHERE ledger_id=? AND user_id=?", role, s.ledgerID, user)
	}
	if err != nil {
		return err
	}
	if err = s.memberAudit(tx, "member_"+role, user); err != nil {
		return err
	}
	return tx.Commit()
}
