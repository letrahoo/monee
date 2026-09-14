package auth

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/letrahoo/monee/server/internal/storage"
)

func migrateAccounts(tx *sql.Tx) error {
	if _, err := tx.Exec(storage.IdentitySchema + `
 CREATE TABLE IF NOT EXISTS user_identities(
 provider TEXT NOT NULL,subject TEXT NOT NULL,user_id TEXT NOT NULL REFERENCES app_users(id),
 PRIMARY KEY(provider,subject),FOREIGN KEY(provider,subject) REFERENCES users(provider,subject));
 CREATE INDEX IF NOT EXISTS identities_user ON user_identities(user_id);
 `); err != nil {
		return err
	}
	rows, err := tx.Query("SELECT provider,subject,display_name FROM users WHERE NOT EXISTS(SELECT 1 FROM user_identities i WHERE i.provider=users.provider AND i.subject=users.subject)")
	if err != nil {
		return err
	}
	var identities []Identity
	for rows.Next() {
		var i Identity
		if err = rows.Scan(&i.Provider, &i.Subject, &i.DisplayName); err != nil {
			rows.Close()
			return err
		}
		identities = append(identities, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range identities {
		if err = ensureAccount(tx, i); err != nil {
			return err
		}
	}
	if err = initializeAccess(tx); err != nil {
		return err
	}
	_, err = tx.Exec("INSERT OR REPLACE INTO auth_meta(key,value) VALUES('account_schema','2')")
	return err
}
func ensureAccount(tx *sql.Tx, i Identity) error {
	var id string
	err := tx.QueryRow("SELECT user_id FROM user_identities WHERE provider=? AND subject=?", i.Provider, i.Subject).Scan(&id)
	if err == nil {
		return ensureAccess(tx, id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	id = uuid.NewString()
	name := i.DisplayName
	if name == "" {
		name = i.Username
	}
	if name == "" {
		name = "Monee 用户"
	}
	if _, err = tx.Exec("INSERT INTO app_users(id,display_name,created_at) VALUES(?,?,?)", id, name, timestamp()); err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO user_identities VALUES(?,?,?)", i.Provider, i.Subject, id)
	if err != nil {
		return err
	}
	return ensureAccess(tx, id)
}
func (s *Store) AccountID(i Identity) (string, error) {
	var id string
	err := s.db.QueryRow("SELECT user_id FROM user_identities WHERE provider=? AND subject=?", i.Provider, i.Subject).Scan(&id)
	return id, err
}

type LinkedIdentity struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

func (s *Store) Identities(user string) ([]LinkedIdentity, error) {
	rows, err := s.db.Query("SELECT u.provider,u.subject,u.username,u.email FROM users u JOIN user_identities i ON i.provider=u.provider AND i.subject=u.subject WHERE i.user_id=? ORDER BY u.provider,u.subject", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []LinkedIdentity{}
	for rows.Next() {
		var i LinkedIdentity
		if err = rows.Scan(&i.Provider, &i.Subject, &i.Username, &i.Email); err != nil {
			return nil, err
		}
		result = append(result, i)
	}
	return result, rows.Err()
}
func (s *Store) Rename(user, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 || strings.ContainsAny(name, "\x00\r\n") {
		return errors.New("昵称须为 1–80 字")
	}
	_, err := s.db.Exec("UPDATE app_users SET display_name=? WHERE id=? AND status='active'", name, user)
	return err
}

// LinkVerified is called only after completing an OAuth binding flow attached to
// a live initiating session. Already registered targets require a separate merge
// confirmation, never matching names or email addresses.
func (s *Store) LinkVerified(user string, i Identity, merge bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var target, status string
	if err = tx.QueryRow("SELECT i.user_id,p.status FROM user_identities i JOIN app_users p ON p.id=i.user_id WHERE i.provider=? AND i.subject=?", i.Provider, i.Subject).Scan(&target, &status); err != nil {
		return err
	}
	if target == user {
		return nil
	}
	if status != "active" {
		return errors.New("目标账号不可合并")
	}
	var sourceStatus string
	if err = tx.QueryRow("SELECT status FROM app_users WHERE id=?", user).Scan(&sourceStatus); err != nil || sourceStatus != "active" {
		return errors.New("当前账号已失效")
	}
	// An identity created just for binding still has its own account, so the caller
	// must distinguish it from an existing registration before RememberIdentity.
	if !merge {
		return errors.New("此身份已注册，请核实后确认合并")
	}
	var ledgerTables int
	_ = tx.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='ledger_members'").Scan(&ledgerTables)
	if ledgerTables > 0 {
		// Remove weaker overlapping memberships first to preserve the unique owner.
		if _, err = tx.Exec(`DELETE FROM ledger_members WHERE user_id=? AND ledger_id IN(SELECT ledger_id FROM ledger_members WHERE user_id=? AND role='owner')`, user, target); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO ledger_members(ledger_id,user_id,role,version,created_at)
   SELECT ledger_id,?,role,version+1,created_at FROM ledger_members WHERE user_id=? AND role!='owner'
   ON CONFLICT(ledger_id,user_id) DO UPDATE SET role=CASE WHEN ledger_members.role='owner' THEN 'owner' WHEN ledger_members.role='editor' OR excluded.role='editor' THEN 'editor' ELSE 'viewer' END, version=ledger_members.version+1`, user, target); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE ledger_members SET user_id=?,version=version+1 WHERE user_id=? AND role='owner'", user, target); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM ledger_members WHERE user_id=?", target); err != nil {
			return err
		}
		// Pending invitations remain traceable and are recreated for the survivor.
		if _, err = tx.Exec(`INSERT OR IGNORE INTO ledger_invitations SELECT lower(hex(randomblob(16))),ledger_id,?,role,invited_by,status,created_at FROM ledger_invitations WHERE user_id=? AND status='pending'`, user, target); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM ledger_invitations WHERE user_id=? AND status='pending'", target); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE EXISTS(SELECT 1 FROM user_identities i WHERE i.provider=sessions.provider AND i.subject=sessions.subject AND i.user_id=?)", target); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE user_access SET enabled=MAX(enabled,COALESCE((SELECT enabled FROM user_access WHERE user_id=?),0)), role=CASE WHEN role='superadmin' OR EXISTS(SELECT 1 FROM user_access WHERE user_id=? AND role='superadmin') THEN 'superadmin' ELSE 'member' END, version=version+1 WHERE user_id=?`, target, target, user); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE user_identities SET user_id=? WHERE user_id=?", user, target); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE app_users SET status='merged',merged_into=? WHERE id=?", user, target); err != nil {
		return err
	}
	if err = audit(tx, user, "merge_account", target); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Unlink(user, provider, subject string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow("SELECT COUNT(*) FROM user_identities WHERE user_id=?", user).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return errors.New("至少保留一种登录方式")
	}
	var protected int
	if err = tx.QueryRow("SELECT COUNT(*) FROM allowlist WHERE provider=? AND subject=? AND protected=1", provider, subject).Scan(&protected); err != nil {
		return err
	}
	if protected > 0 {
		return errors.New("初始超管身份不能解绑")
	}
	result, err := tx.Exec("DELETE FROM user_identities WHERE user_id=? AND provider=? AND subject=?", user, provider, subject)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errors.New("绑定不存在")
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE provider=? AND subject=?", provider, subject); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM users WHERE provider=? AND subject=?", provider, subject); err != nil {
		return err
	}
	if err = audit(tx, user, "unlink_identity", provider+":"+subject); err != nil {
		return err
	}
	return tx.Commit()
}
