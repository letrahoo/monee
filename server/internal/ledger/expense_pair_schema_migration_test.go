package ledger

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/letrahoo/monee/server/internal/storage"
)

func expensePairV4Fixture(t *testing.T) (*Store, string) {
	t.Helper()
	s, path := schema2Store(t)
	if e := migrateRefundSchema(s.db); e != nil {
		t.Fatal(e)
	}
	if e := migrateTransferSchema(s.db); e != nil {
		t.Fatal(e)
	}
	var v int
	if e := s.db.QueryRow("PRAGMA user_version").Scan(&v); e != nil || v != 4 {
		t.Fatal(v, e)
	}
	return s, path
}
func expensePairDirectDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, e := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(500)")
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
func TestExpensePairSchemaMigrationPreservesV4AndBackup(t *testing.T) {
	s, path := expensePairV4Fixture(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense), "synthetic-v4-wallet")
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	r, e := s.Create(testInput(), "expense-pair-v4-original")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateRefund(r.ID, RefundInput{Version: r.Version, Date: "2026-09-15", Amount: "1.00", Currency: "CNY", Account: "合成退款账户"}, "expense-pair-v4-refund"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateTransfer(syntheticTransfer(), "expense-pair-v4-transfer"); e != nil {
		t.Fatal(e)
	}
	before := accountingFactTables(t, s)
	d := dashboard(t, s)
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	upgraded, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { upgraded.Close() })
	var v int
	if e = upgraded.db.QueryRow("PRAGMA user_version").Scan(&v); e != nil || v != 5 {
		t.Fatal(v, e)
	}
	after := accountingFactTables(t, upgraded)
	for _, name := range []string{"expense_pair_decisions", "expense_pair_members", "expense_pair_revisions"} {
		if len(after[name]) != 0 {
			t.Fatal(name, after[name])
		}
		delete(after, name)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(d, dashboard(t, upgraded)) {
		t.Fatal("Migration modified original financial/source/identity/application rows")
	}
	if checked, e := storage.Check(path); e != nil || checked != 5 {
		t.Fatal(checked, e)
	}
	backups, e := filepath.Glob(path + ".backup-before-v5-*")
	if e != nil || len(backups) != 1 {
		t.Fatal(backups, e)
	}
	restored := filepath.Join(t.TempDir(), "restored-v4.db")
	if e = storage.Restore(backups[0], restored); e != nil {
		t.Fatal(e)
	}
	if checked, e := storage.Check(restored); e != nil || checked != 4 {
		t.Fatal("Pre-migration backup did not retain actual schema4", checked, e)
	}
	restoredDB := expensePairDirectDB(t, restored)
	if !reflect.DeepEqual(before, accountingFactTables(t, &Store{db: restoredDB})) {
		t.Fatal("Restored schema4 backup changed source/refund/transfer snapshots")
	}
	migrated := accountingFactTables(t, upgraded)
	if e = upgraded.Close(); e != nil {
		t.Fatal(e)
	}
	again, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	if !reflect.DeepEqual(migrated, accountingFactTables(t, again)) {
		t.Fatal("Empty schema5 reopen changed any row")
	}
}
func TestExpensePairSchemaMigrationRejectsExistingObjectsWithoutWrites(t *testing.T) {
	for _, collision := range []string{"expense_pair_members", "expense_pair_revisions"} {
		t.Run(collision, func(t *testing.T) {
			s, _ := expensePairV4Fixture(t)
			if _, e := s.db.Exec("CREATE TABLE " + collision + " (synthetic INTEGER)"); e != nil {
				t.Fatal(e)
			}
			before := accountingFactTables(t, s)
			beforeSchema := foundationSchemaSignature(t, s.db)
			defer func() {
				if !reflect.DeepEqual(beforeSchema, foundationSchemaSignature(t, s.db)) {
					t.Error("Preflight refusal repaired or changed the schema")
				}
			}()
			if e := migrateExpensePairSchema(s.db); e == nil {
				t.Fatal("Expected legacy-object preflight refusal")
			}
			var v int
			if e := s.db.QueryRow("PRAGMA user_version").Scan(&v); e != nil || v != 4 {
				t.Fatal(v, e)
			}
			if !reflect.DeepEqual(before, accountingFactTables(t, s)) {
				t.Fatal("Failed migration left partial tables or changed old rows")
			}
			var roots int
			if e := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='expense_pair_decisions'").Scan(&roots); e != nil || roots != 0 {
				t.Fatal("Preflight refusal unexpectedly created foundation objects", roots, e)
			}
		})
	}
}
func TestExpensePairSchemaMigrationForeignKeyFailureRollsBackDDL(t *testing.T) {
	s, _ := expensePairV4Fixture(t)
	r := accountingFactCreate(t, s, "合成账户", "broken-v4-fk")
	foundationWithoutFK(t, s, func() {
		foundationExec(t, s, "UPDATE postings SET account_id='synthetic-missing-v4-account' WHERE transaction_id=? AND amount_minor<0", r.ID)
	})
	before := accountingFactTables(t, s)
	beforeSchema := foundationSchemaSignature(t, s.db)
	defer func() {
		if !reflect.DeepEqual(beforeSchema, foundationSchemaSignature(t, s.db)) {
			t.Error("Failed migration changed old schema objects/version")
		}
	}()
	if e := migrateExpensePairSchema(s.db); e == nil {
		t.Fatal("Expected FK check failure after DDL execution")
	}
	var v, objects int
	if e := s.db.QueryRow("PRAGMA user_version").Scan(&v); e != nil || v != 4 {
		t.Fatal(v, e)
	}
	if e := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'expense_pair_%'").Scan(&objects); e != nil || objects != 0 {
		t.Fatal("DDL/user_version did not roll back", objects, e)
	}
	if !reflect.DeepEqual(before, accountingFactTables(t, s)) {
		t.Fatal("Rollback modified original application rows")
	}
}
func TestExpensePairSchemaOpenRefusesFutureVersion(t *testing.T) {
	s := testStore(t)
	var path string
	if e := s.db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec(fmt.Sprintf("PRAGMA user_version=%d", storage.SchemaVersion+1)); e != nil {
		t.Fatal(e)
	}
	s.Close()
	out, e := Open(path)
	var p *Problem
	if e == nil || out != nil || !errors.As(e, &p) || p.Code != "future_schema" {
		if out != nil {
			out.Close()
		}
		t.Fatal("Future schema opened", out, e)
	}
}
