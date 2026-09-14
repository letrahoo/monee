package auth

import (
	"database/sql"
	"errors"
)

func ensureAccess(tx *sql.Tx, id string) error {
	_, err := tx.Exec(`INSERT INTO user_access(user_id,enabled,role)
 SELECT ?,EXISTS(SELECT 1 FROM user_identities i JOIN allowlist a ON a.provider=i.provider AND a.subject=i.subject WHERE i.user_id=? AND a.enabled=1),
 CASE WHEN EXISTS(SELECT 1 FROM user_identities i JOIN allowlist a ON a.provider=i.provider AND a.subject=i.subject WHERE i.user_id=? AND a.role='superadmin' AND a.enabled=1) THEN 'superadmin' ELSE 'member' END
 ON CONFLICT(user_id) DO NOTHING`, id, id, id)
	return err
}
func initializeAccess(tx *sql.Tx) error {
	rows, err := tx.Query("SELECT id FROM app_users")
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = ensureAccess(tx, id); err != nil {
			return err
		}
	}
	return nil
}
func grantIdentity(tx *sql.Tx, provider, subject, role string) error {
	var id string
	err := tx.QueryRow("SELECT user_id FROM user_identities WHERE provider=? AND subject=?", provider, subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = ensureAccess(tx, id); err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE user_access SET enabled=1,role=CASE WHEN role='superadmin' OR ?='superadmin' THEN 'superadmin' ELSE 'member' END,version=version+1 WHERE user_id=?", role, id)
	return err
}

type AccountEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Role    string `json:"role"`
	Version int64  `json:"version"`
}

func (s *Store) Accounts(actor Identity) ([]AccountEntry, error) {
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
	rows, err := tx.Query("SELECT p.id,p.display_name,a.enabled,a.role,a.version FROM app_users p JOIN user_access a ON a.user_id=p.id WHERE p.status='active' ORDER BY p.created_at,p.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AccountEntry{}
	for rows.Next() {
		var e AccountEntry
		if err = rows.Scan(&e.ID, &e.Name, &e.Enabled, &e.Role, &e.Version); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
func (s *Store) SetAccountEnabled(actor Identity, id string, enabled bool, version int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = requireAdmin(tx, actor); err != nil {
		return err
	}
	var role string
	var v int64
	if err = tx.QueryRow("SELECT role,version FROM user_access WHERE user_id=?", id).Scan(&role, &v); err != nil {
		return errors.New("账号不存在")
	}
	if role == "superadmin" {
		return errors.New("初始超管不可停用")
	}
	if v != version {
		return errors.New("账号状态已变化，请刷新")
	}
	if _, err = tx.Exec("UPDATE user_access SET enabled=?,version=version+1 WHERE user_id=?", enabled, id); err != nil {
		return err
	}
	if err = audit(tx, actor.Provider+":"+actor.Subject, "set_account_access", id); err != nil {
		return err
	}
	return tx.Commit()
}
