package ledger

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
	"github.com/letrahoo/monee/server/internal/storage"
)

func schema2Store(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "application.db")
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(schema + storage.IdentitySchema + schema2); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO ledgers(id,currency,timezone,version,device_id,created_at) VALUES('test-ledger','CNY','Asia/Shanghai',0,'test-device','2026-09-01')"); err != nil {
		t.Fatal(err)
	}
	return &Store{db: db, mu: &sync.Mutex{}, ledgerID: "test-ledger", deviceID: "test-device"}, path
}

func TestRefundMigrationPreservesDataAndBackup(t *testing.T) {
	old, path := schema2Store(t)
	p := preview(t, old, sampleCSV)
	if _, err := old.Commit(p.ID, p.LedgerVersion, false); err != nil {
		t.Fatal(err)
	}
	before := dashboard(t, old)
	sources := count(t, old, "source_records")
	changes := count(t, old, "change_log")
	old.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after := dashboard(t, s)
	if after.TotalCount != before.TotalCount || after.ExpenseMinor != before.ExpenseMinor || after.IncomeMinor != before.IncomeMinor || after.Transactions[0].ID != before.Transactions[0].ID || after.Version != before.Version {
		t.Fatalf("migration changed ledger: before=%+v after=%+v", before, after)
	}
	if count(t, s, "source_records") != sources || count(t, s, "change_log") != changes || count(t, s, "postings") != 6 {
		t.Fatal("migration changed provenance, audit or postings")
	}
	if v, err := storage.Check(path); err != nil || v != 3 {
		t.Fatal("schema or integrity", v, err)
	}
	var fk int
	if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatal("foreign keys not re-enabled", fk, err)
	}
	// Initializing identity in the combined DB must preserve version 3, including
	// the first run where authSchema itself still declares its legacy version 1.
	a, err := auth.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	if v, err := storage.Check(path); err != nil || v != 3 {
		t.Fatal("identity downgraded schema", v, err)
	}
	backups, err := filepath.Glob(path + ".backup-before-v3-*")
	if err != nil || len(backups) != 1 {
		t.Fatal("missing upgrade backup", backups, err)
	}
	restored := filepath.Join(t.TempDir(), "restored.db")
	if err := storage.Restore(backups[0], restored); err != nil {
		t.Fatal(err)
	}
	if v, err := storage.Check(restored); err != nil || v != 2 {
		t.Fatal("backup did not preserve old schema", v, err)
	}
	backupDB, err := sql.Open("sqlite", restored)
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	var n int
	if err := backupDB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&n); err != nil || n != before.TotalCount {
		t.Fatal("backup lost data", n, err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal("restart", err)
	}
	s.Close()
	backups, _ = filepath.Glob(path + ".backup-before-v3-*")
	if len(backups) != 1 {
		t.Fatal("restart made redundant backup")
	}
}

func TestRefundMigrationRollsBackOnBrokenForeignKey(t *testing.T) {
	s, _ := schema2Store(t)
	if _, err := s.db.Exec("PRAGMA foreign_keys=OFF; INSERT INTO postings VALUES('broken','missing-transaction','missing-account',1,'CNY'); PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := migrateRefundSchema(s.db); err == nil {
		t.Fatal("accepted broken foreign key")
	}
	var version, fk, staged int
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='transactions_v3'").Scan(&staged)
	if version != 2 || fk != 1 || staged != 0 || count(t, s, "postings") != 1 {
		t.Fatal("migration was not fully rolled back", version, fk, staged)
	}
}

func TestRefundUpgradeStopsWhenBackupValidationFails(t *testing.T) {
	s, path := schema2Store(t)
	if _, err := s.db.Exec("PRAGMA foreign_keys=OFF; INSERT INTO postings VALUES('broken','missing-transaction','missing-account',1,'CNY'); PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(path); err == nil {
		reopened.Close()
		t.Fatal("upgraded without a verified backup")
	}
	var version, newColumns int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('transactions') WHERE name='refund_of'").Scan(&newColumns); err != nil {
		t.Fatal(err)
	}
	if version != 2 || newColumns != 0 || count(t, s, "postings") != 1 {
		t.Fatal("failed backup modified the source database", version, newColumns)
	}
	backups, _ := filepath.Glob(path + ".backup-before-v3-*")
	if len(backups) != 0 {
		t.Fatal("left an unverified backup")
	}
}

func TestRefundSchemaRequiresSameLedgerTarget(t *testing.T) {
	s := testStore(t)
	original, err := s.Create(testInput(), "original-request-001")
	if err != nil {
		t.Fatal(err)
	}
	// Copy only synthetic test fields to isolate database link constraints.
	insert := `INSERT INTO transactions SELECT ?,ledger_id,occurred_on,'refund',1,currency,merchant,category,source,account,external_id,note,NULL,fingerprint,similarity_key,1,device_id,created_at,updated_at,NULL,created_by,? FROM transactions WHERE id=?`
	for _, target := range []any{nil, "missing", "self"} {
		if _, err := s.db.Exec(insert, "self", target, original.ID); err == nil {
			t.Fatalf("accepted invalid refund target %v", target)
		}
	}
	if _, err := s.db.Exec(insert, "refund", original.ID, original.ID); err != nil {
		t.Fatal("valid link rejected", err)
	}
	if _, err := s.db.Exec("INSERT INTO ledgers(id,currency,timezone,version,device_id,created_at) VALUES('other','CNY','Asia/Shanghai',0,'device','2026-09-01')"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE transactions SET ledger_id='other' WHERE id='refund'"); err == nil {
		t.Fatal("cross-ledger refund accepted")
	}
}

func TestRefundMigrationPreservesIdentityAndDeletedRecords(t *testing.T) {
	s, path := schema2Store(t)
	a, err := auth.Open(path, []auth.Selector{{Provider: "github", Kind: "subject", Value: "1001"}})
	if err != nil {
		t.Fatal(err)
	}
	identity := auth.Identity{Provider: "github", Subject: "1001", Username: "synthetic"}
	if err := a.RememberIdentity(identity); err != nil {
		t.Fatal(err)
	}
	token, err := a.CreateSession(identity)
	if err != nil {
		t.Fatal(err)
	}
	user, err := a.Session(token)
	if err != nil || user == nil || !user.Allowed {
		t.Fatal("test identity", err)
	}
	l, err := s.CreateLedger(user.ID, "Synthetic migration")
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := s.Scoped(user.ID, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	record, err := scoped.Create(testInput(), "migration-created-by-01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE transactions SET deleted_at='2026-09-18',version=2 WHERE id=?", record.ID); err != nil {
		t.Fatal(err)
	}
	// Compare every pre-existing column, not only the public transaction DTO.
	queries := []string{
		"SELECT id,ledger_id,occurred_on,kind,amount_minor,currency,merchant,category,source,account,external_id,note,identity_key,fingerprint,similarity_key,version,device_id,created_at,updated_at,deleted_at,created_by FROM transactions ORDER BY id",
		"SELECT * FROM app_users ORDER BY id", "SELECT * FROM user_identities ORDER BY user_id",
		"SELECT * FROM user_access ORDER BY user_id", "SELECT * FROM ledger_members ORDER BY ledger_id,user_id",
		"SELECT * FROM ledger_audit ORDER BY id", "SELECT * FROM change_log ORDER BY sequence",
	}
	dump := func(db *sql.DB, query string) string {
		t.Helper()
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		data := [][]any{}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			data = append(data, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	before := make([]string, len(queries))
	for i, query := range queries {
		before[i] = dump(s.db, query)
	}
	a.Close()
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err = auth.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for i, query := range queries {
		if dump(s.db, query) != before[i] {
			t.Fatalf("upgrade changed existing data: %s", query)
		}
	}
	current, err := a.Session(token)
	if err != nil || current == nil || current.ID != user.ID || !current.Allowed {
		t.Fatal("session lost on upgrade", err)
	}
	if _, err := s.Scoped(user.ID, l.ID); err != nil {
		t.Fatal("ledger access lost on upgrade", err)
	}
}
