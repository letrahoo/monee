package ledger

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type foundationSchemaState struct {
	Version int
	Objects [][4]string
}

func foundationSchemaSignature(t *testing.T, db *sql.DB) foundationSchemaState {
	t.Helper()
	var out foundationSchemaState
	if e := db.QueryRow("PRAGMA user_version").Scan(&out.Version); e != nil {
		t.Fatal(e)
	}
	rows, e := db.Query("SELECT type,name,tbl_name,COALESCE(sql,'') FROM sqlite_master ORDER BY type,name,tbl_name")
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	for rows.Next() {
		var object [4]string
		if e = rows.Scan(&object[0], &object[1], &object[2], &object[3]); e != nil {
			t.Fatal(e)
		}
		out.Objects = append(out.Objects, object)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	return out
}

func foundationExec(t *testing.T, s *Store, q string, args ...any) {
	t.Helper()
	if _, e := s.db.Exec(q, args...); e != nil {
		t.Fatal(e)
	}
}
func foundationActor(t *testing.T, s *Store) {
	t.Helper()
	foundationExec(t, s, "INSERT INTO app_users(id,display_name,created_at) VALUES('foundation-actor','合成决策人','synthetic')")
}
func foundationRoot(t *testing.T, s *Store, status string) {
	t.Helper()
	foundationActor(t, s)
	foundationExec(t, s, "INSERT INTO expense_pair_decisions(ledger_id,id,status,version,contract_version,created_by,created_at,updated_at) VALUES(?,?,?,2,1,'foundation-actor','synthetic','synthetic')", s.ledgerID, "foundation-decision", status)
}
func foundationMember(t *testing.T, s *Store, id, role string, active int) {
	t.Helper()
	foundationExec(t, s, "INSERT INTO expense_pair_members(ledger_id,decision_id,transaction_id,role,transaction_version,facts_json,evidence_json,active) VALUES(?,?,?, ?,1,'{}','{}',?)", s.ledgerID, "foundation-decision", id, role, active)
}
func foundationRevision(t *testing.T, s *Store, version int, op string) {
	t.Helper()
	foundationExec(t, s, "INSERT INTO expense_pair_revisions(ledger_id,decision_id,revision_id,version,operation,actor_id,reason,snapshot_json,contract_version,created_at) VALUES(?,?,?,?,?,'foundation-actor','synthetic reason','{}',1,'synthetic')", s.ledgerID, "foundation-decision", "foundation-revision-"+op, version, op)
}
func foundationWithoutFK(t *testing.T, s *Store, fn func()) {
	t.Helper()
	foundationExec(t, s, "PRAGMA foreign_keys=OFF")
	defer func() {
		if _, e := s.db.Exec("PRAGMA foreign_keys=ON"); e != nil {
			t.Error(e)
		}
	}()
	fn()
}

func TestExpensePairFoundationStartupRejectsEveryPopulatedTable(t *testing.T) {
	cases := map[string]func(*testing.T, *Store){
		"active root only":    func(t *testing.T, s *Store) { foundationRoot(t, s, "active") },
		"retracted root only": func(t *testing.T, s *Store) { foundationRoot(t, s, "retracted") },
		"member only": func(t *testing.T, s *Store) {
			r := accountingFactCreate(t, s, "合成账户", "orphan-member")
			foundationWithoutFK(t, s, func() { foundationMember(t, s, r.ID, "primary", 0) })
		},
		"revision only": func(t *testing.T, s *Store) {
			foundationWithoutFK(t, s, func() { foundationRevision(t, s, 1, "confirm") })
		},
		"complete inactive history": func(t *testing.T, s *Store) {
			a := accountingFactCreate(t, s, "合成账户", "history-a")
			b := accountingFactCreate(t, s, "合成账户", "history-b")
			foundationRoot(t, s, "retracted")
			foundationMember(t, s, a.ID, "primary", 0)
			foundationMember(t, s, b.ID, "secondary", 0)
			foundationRevision(t, s, 1, "confirm")
			foundationRevision(t, s, 2, "withdraw")
		},
		"unknown contract": func(t *testing.T, s *Store) {
			foundationRoot(t, s, "active")
			foundationExec(t, s, "PRAGMA ignore_check_constraints=ON")
			defer func() {
				if _, e := s.db.Exec("PRAGMA ignore_check_constraints=OFF"); e != nil {
					t.Error(e)
				}
			}()
			foundationExec(t, s, "UPDATE expense_pair_decisions SET contract_version=99")
		},
	}
	for name, seed := range cases {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			var path string
			if e := s.db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); e != nil {
				t.Fatal(e)
			}
			seed(t, s)
			before := accountingFactTables(t, s)
			beforeSchema := foundationSchemaSignature(t, s.db)
			s.Close()
			out, e := Open(path)
			if out != nil {
				out.Close()
			}
			var p *Problem
			if e == nil || out != nil || !errors.As(e, &p) || p.Code != "association_foundation_unavailable" || strings.Contains(e.Error(), "foundation-decision") {
				t.Fatal("Populated foundation must fail with a static capability error", out, e)
			}
			db := expensePairDirectDB(t, path)
			if !reflect.DeepEqual(beforeSchema, foundationSchemaSignature(t, db)) {
				t.Fatal("Refused Open repaired or changed schema objects/version")
			}
			if !reflect.DeepEqual(before, accountingFactTables(t, &Store{db: db})) {
				t.Fatal("Refused Open changed stored application rows")
			}
			tx, e := db.Begin()
			if e != nil {
				t.Fatal("Failed Open retained a database lock", e)
			}
			defer tx.Rollback()
			if _, e = tx.Exec("UPDATE ledgers SET version=version"); e != nil {
				t.Fatal(e)
			}
			tx.Rollback()
		})
	}
}
func TestExpensePairFoundationStartupRejectsMissingOrAlteredSchema(t *testing.T) {
	cases := map[string]func(*testing.T, *Store){
		"missing decisions": func(t *testing.T, s *Store) { foundationExec(t, s, "DROP TABLE expense_pair_decisions") },
		"missing members":   func(t *testing.T, s *Store) { foundationExec(t, s, "DROP TABLE expense_pair_members") },
		"missing revisions": func(t *testing.T, s *Store) { foundationExec(t, s, "DROP TABLE expense_pair_revisions") },
		"view impersonates revisions": func(t *testing.T, s *Store) {
			foundationExec(t, s, "DROP TABLE expense_pair_revisions")
			foundationExec(t, s, "CREATE VIEW expense_pair_revisions AS SELECT 'synthetic' AS revision_id WHERE 0")
		},
		"missing active-member index": func(t *testing.T, s *Store) {
			foundationExec(t, s, "DROP INDEX expense_pair_members_one_active_transaction")
		},
		"extra member column": func(t *testing.T, s *Store) {
			foundationExec(t, s, "ALTER TABLE expense_pair_members ADD COLUMN unexpected TEXT")
		},
		"unknown trigger": func(t *testing.T, s *Store) {
			foundationExec(t, s, "CREATE TRIGGER unexpected_pair_trigger AFTER INSERT ON expense_pair_decisions BEGIN SELECT 1; END")
		},
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			var path string
			if e := s.db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); e != nil {
				t.Fatal(e)
			}
			alter(t, s)
			beforeSchema := foundationSchemaSignature(t, s.db)
			s.Close()
			out, e := Open(path)
			if out != nil {
				out.Close()
			}
			var p *Problem
			if e == nil || out != nil || !errors.As(e, &p) || p.Code != "association_foundation_unavailable" {
				t.Fatal("Unknown foundation shape must not be repaired or ignored", out, e)
			}
			db := expensePairDirectDB(t, path)
			if !reflect.DeepEqual(beforeSchema, foundationSchemaSignature(t, db)) {
				t.Fatal("Unknown schema was repaired before refusal")
			}
		})
	}
}
