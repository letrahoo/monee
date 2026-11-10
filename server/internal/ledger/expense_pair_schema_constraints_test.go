package ledger

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Direct SQL in an isolated, already-open schema5 test store verifies storage
// constraints only. It is not a decision writer, authorization or eligibility.
type pairSchemaFixture struct {
	store                                                         *Store
	actor, ledger, otherLedger, first, second, foreignTransaction string
}

func newPairSchemaFixture(t *testing.T) pairSchemaFixture {
	t.Helper()
	s := testStore(t)
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatal("constraint fixtures require schema5", version, err)
	}
	f := pairSchemaFixture{store: s, actor: newID(), ledger: s.ledgerID, otherLedger: newID(), foreignTransaction: newID()}
	if _, err := s.db.Exec("INSERT INTO app_users(id,display_name,created_at) VALUES(?,?,?)", f.actor, "合成约束测试身份", now()); err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{"pair-schema-expense-first", "pair-schema-expense-second"} {
		in := testInput()
		in.ExternalID = key
		r, err := s.Create(in, key)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			f.first = r.ID
		} else {
			f.second = r.ID
		}
	}
	if _, err := s.db.Exec("INSERT INTO ledgers(id,currency,timezone,version,device_id,created_at,name) VALUES(?,?,?,?,?,?,?)", f.otherLedger, "CNY", "Asia/Shanghai", 0, newID(), now(), "合成其他约束账本"); err != nil {
		t.Fatal(err)
	}
	// A valid transaction row in another real ledger isolates the composite FK
	// from the simpler missing-transaction case. It has no claim of bookability.
	if _, err := s.db.Exec(`INSERT INTO transactions(id,ledger_id,occurred_on,kind,amount_minor,currency,merchant,category,source,account,external_id,note,identity_key,fingerprint,similarity_key,version,device_id,created_at,updated_at)
 SELECT ?,?,occurred_on,kind,amount_minor,currency,merchant,category,source,account,external_id,note,NULL,fingerprint,similarity_key,version,device_id,created_at,updated_at FROM transactions WHERE id=?`, f.foreignTransaction, f.otherLedger, f.first); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f pairSchemaFixture) root(id string) []any {
	return []any{f.ledger, id, "active", int64(1), int64(1), f.actor, now(), now()}
}
func (f pairSchemaFixture) member(decision, transaction, role string, active int64) []any {
	return []any{f.ledger, decision, transaction, role, int64(1), `{}`, `{}`, active}
}
func (f pairSchemaFixture) revision(decision, id string, version int64) []any {
	return []any{f.ledger, decision, id, version, "confirm", f.actor, "合成约束理由", `{}`, int64(1), now()}
}

const pairSchemaRootSQL = `INSERT INTO expense_pair_decisions(ledger_id,id,status,version,contract_version,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`
const pairSchemaMemberSQL = `INSERT INTO expense_pair_members(ledger_id,decision_id,transaction_id,role,transaction_version,facts_json,evidence_json,active) VALUES(?,?,?,?,?,?,?,?)`
const pairSchemaRevisionSQL = `INSERT INTO expense_pair_revisions(ledger_id,decision_id,revision_id,version,operation,actor_id,reason,snapshot_json,contract_version,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`

func pairSchemaMustInsert(t *testing.T, tx *sql.Tx, statement string, args []any) {
	t.Helper()
	if _, err := tx.Exec(statement, args...); err != nil {
		t.Fatal("valid schema fixture rejected", err)
	}
}
func pairSchemaMustReject(t *testing.T, tx *sql.Tx, statement string, args []any) {
	t.Helper()
	_, err := tx.Exec(statement, args...)
	if err == nil {
		t.Fatal("invalid schema row accepted")
	}
	var sqliteError *sqlite.Error
	if !errors.As(err, &sqliteError) || sqliteError.Code()&0xff != sqlite3.SQLITE_CONSTRAINT {
		// Some SQLite expression evaluators report malformed JSON as SQLITE_ERROR
		// rather than CHECK failure. No unrelated SQL/name/locking error qualifies.
		if !errors.As(err, &sqliteError) || sqliteError.Code() != sqlite3.SQLITE_ERROR || !strings.Contains(strings.ToLower(err.Error()), "malformed json") {
			t.Fatal("expected a declared constraint failure, not unrelated SQL failure", err)
		}
	}
}

// Every constraint exercise rolls back, including Fatal paths, and compares ALL
// application tables to the seeded fixture. Financial/source rows remain intact.
func pairSchemaRollback(t *testing.T, f pairSchemaFixture, run func(*sql.Tx)) {
	t.Helper()
	before := nativeEvidenceTables(t, f.store)
	tx, err := f.store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
		if !reflect.DeepEqual(before, nativeEvidenceTables(t, f.store)) {
			t.Error("schema exercise changed application or financial tables after rollback")
		}
	}()
	run(tx)
}

func TestExpensePairSchemaRootConstraints(t *testing.T) {
	f := newPairSchemaFixture(t)
	cases := []struct {
		name  string
		index int
		value any
	}{
		{"missing ledger", 0, "synthetic-missing-ledger"}, {"empty id", 1, ""}, {"blank id", 1, "   "},
		{"unknown status", 2, "confirmed"}, {"null status", 2, nil},
		{"zero version", 3, int64(0)}, {"negative version", 3, int64(-1)}, {"real version", 3, 1.5}, {"text version", 3, "invalid"},
		{"wrong contract", 4, int64(2)}, {"real contract", 4, 1.5}, {"null contract", 4, nil},
		{"missing creator", 5, "synthetic-missing-actor"}, {"null creator", 5, nil},
		{"empty created time", 6, ""}, {"empty updated time", 7, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pairSchemaRollback(t, f, func(tx *sql.Tx) {
				args := f.root("synthetic-root")
				args[tc.index] = tc.value
				pairSchemaMustReject(t, tx, pairSchemaRootSQL, args)
			})
		})
	}
	t.Run("duplicate ledger scoped id", func(t *testing.T) {
		pairSchemaRollback(t, f, func(tx *sql.Tx) {
			args := f.root("synthetic-root")
			pairSchemaMustInsert(t, tx, pairSchemaRootSQL, args)
			pairSchemaMustReject(t, tx, pairSchemaRootSQL, args)
		})
	})
}

func TestExpensePairSchemaMemberConstraints(t *testing.T) {
	f := newPairSchemaFixture(t)
	cases := []struct {
		name  string
		index int
		value any
	}{
		{"missing root", 1, "synthetic-missing-root"}, {"missing transaction", 2, "synthetic-missing-transaction"},
		{"other ledger transaction", 2, f.foreignTransaction}, {"unknown role", 3, "winner"}, {"null role", 3, nil},
		{"zero transaction version", 4, int64(0)}, {"negative transaction version", 4, int64(-1)}, {"real transaction version", 4, 1.5},
		{"invalid facts JSON", 5, `{broken`}, {"array facts JSON", 5, `[]`}, {"null facts JSON", 5, `null`}, {"scalar facts JSON", 5, `1`}, {"null facts column", 5, nil},
		{"invalid evidence JSON", 6, `{broken`}, {"array evidence JSON", 6, `[]`}, {"null evidence JSON", 6, `null`}, {"string evidence JSON", 6, `"synthetic"`},
		{"invalid active", 7, int64(2)}, {"negative active", 7, int64(-1)}, {"real active", 7, 1.5}, {"null active", 7, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pairSchemaRollback(t, f, func(tx *sql.Tx) {
				pairSchemaMustInsert(t, tx, pairSchemaRootSQL, f.root("synthetic-root"))
				args := f.member("synthetic-root", f.first, "primary", 1)
				args[tc.index] = tc.value
				pairSchemaMustReject(t, tx, pairSchemaMemberSQL, args)
			})
		})
	}
	t.Run("root from different ledger", func(t *testing.T) {
		pairSchemaRollback(t, f, func(tx *sql.Tx) {
			root := f.root("synthetic-foreign-root")
			root[0] = f.otherLedger
			pairSchemaMustInsert(t, tx, pairSchemaRootSQL, root)
			pairSchemaMustReject(t, tx, pairSchemaMemberSQL, f.member("synthetic-foreign-root", f.first, "primary", 1))
		})
	})
	t.Run("duplicate member primary key", func(t *testing.T) {
		pairSchemaRollback(t, f, func(tx *sql.Tx) {
			pairSchemaMustInsert(t, tx, pairSchemaRootSQL, f.root("synthetic-root"))
			pairSchemaMustInsert(t, tx, pairSchemaMemberSQL, f.member("synthetic-root", f.first, "primary", 0))
			pairSchemaMustReject(t, tx, pairSchemaMemberSQL, f.member("synthetic-root", f.first, "secondary", 0))
		})
	})
	t.Run("one role per group", func(t *testing.T) {
		pairSchemaRollback(t, f, func(tx *sql.Tx) {
			pairSchemaMustInsert(t, tx, pairSchemaRootSQL, f.root("synthetic-root"))
			pairSchemaMustInsert(t, tx, pairSchemaMemberSQL, f.member("synthetic-root", f.first, "primary", 0))
			pairSchemaMustReject(t, tx, pairSchemaMemberSQL, f.member("synthetic-root", f.second, "primary", 0))
		})
	})
	t.Run("one active group per transaction", func(t *testing.T) {
		pairSchemaRollback(t, f, func(tx *sql.Tx) {
			for _, id := range []string{"synthetic-root-a", "synthetic-root-b"} {
				pairSchemaMustInsert(t, tx, pairSchemaRootSQL, f.root(id))
			}
			pairSchemaMustInsert(t, tx, pairSchemaMemberSQL, f.member("synthetic-root-a", f.first, "primary", 1))
			pairSchemaMustReject(t, tx, pairSchemaMemberSQL, f.member("synthetic-root-b", f.first, "primary", 1))
		})
	})
}

func TestExpensePairSchemaRevisionConstraints(t *testing.T) {
	f := newPairSchemaFixture(t)
	cases := []struct {
		name  string
		index int
		value any
	}{
		{"missing decision", 1, "synthetic-missing-root"}, {"empty revision id", 2, ""}, {"blank revision id", 2, " "},
		{"zero revision", 3, int64(0)}, {"negative revision", 3, int64(-1)}, {"real revision", 3, 1.5},
		{"unknown operation", 4, "merge"}, {"null operation", 4, nil}, {"missing actor", 5, "synthetic-missing-actor"}, {"null actor", 5, nil},
		{"empty reason", 6, ""}, {"blank reason", 6, "  "},
		{"invalid snapshot JSON", 7, `{broken`}, {"array snapshot JSON", 7, `[]`}, {"null snapshot JSON", 7, `null`}, {"scalar snapshot JSON", 7, `true`},
		{"wrong contract", 8, int64(2)}, {"real contract", 8, 1.5}, {"empty created time", 9, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pairSchemaRollback(t, f, func(tx *sql.Tx) {
				pairSchemaMustInsert(t, tx, pairSchemaRootSQL, f.root("synthetic-root"))
				args := f.revision("synthetic-root", "synthetic-revision", 1)
				args[tc.index] = tc.value
				pairSchemaMustReject(t, tx, pairSchemaRevisionSQL, args)
			})
		})
	}
	t.Run("root from different ledger", func(t *testing.T) {
		pairSchemaRollback(t, f, func(tx *sql.Tx) {
			root := f.root("synthetic-foreign-root")
			root[0] = f.otherLedger
			pairSchemaMustInsert(t, tx, pairSchemaRootSQL, root)
			pairSchemaMustReject(t, tx, pairSchemaRevisionSQL, f.revision("synthetic-foreign-root", "synthetic-revision", 1))
		})
	})
	t.Run("unique revision version", func(t *testing.T) {
		pairSchemaRollback(t, f, func(tx *sql.Tx) {
			pairSchemaMustInsert(t, tx, pairSchemaRootSQL, f.root("synthetic-root"))
			pairSchemaMustInsert(t, tx, pairSchemaRevisionSQL, f.revision("synthetic-root", "synthetic-revision-a", 1))
			pairSchemaMustReject(t, tx, pairSchemaRevisionSQL, f.revision("synthetic-root", "synthetic-revision-b", 1))
		})
	})
	t.Run("unique revision id", func(t *testing.T) {
		pairSchemaRollback(t, f, func(tx *sql.Tx) {
			pairSchemaMustInsert(t, tx, pairSchemaRootSQL, f.root("synthetic-root"))
			pairSchemaMustInsert(t, tx, pairSchemaRevisionSQL, f.revision("synthetic-root", "synthetic-revision", 1))
			pairSchemaMustReject(t, tx, pairSchemaRevisionSQL, f.revision("synthetic-root", "synthetic-revision", 2))
		})
	})
}

func TestExpensePairSchemaPositiveHistoryAndOpaqueJSON(t *testing.T) {
	f := newPairSchemaFixture(t)
	pairSchemaRollback(t, f, func(tx *sql.Tx) {
		for _, id := range []string{"synthetic-active", "synthetic-old-a", "synthetic-old-b"} {
			root := f.root(id)
			if id != "synthetic-active" {
				root[2] = "retracted"
			}
			pairSchemaMustInsert(t, tx, pairSchemaRootSQL, root)
		}
		for _, roleTx := range []struct{ role, id string }{{"primary", f.first}, {"secondary", f.second}} {
			pairSchemaMustInsert(t, tx, pairSchemaMemberSQL, f.member("synthetic-active", roleTx.id, roleTx.role, 1))
			for _, old := range []string{"synthetic-old-a", "synthetic-old-b"} {
				pairSchemaMustInsert(t, tx, pairSchemaMemberSQL, f.member(old, roleTx.id, roleTx.role, 0))
			}
		}
		// JSON is constrained to an object, not validated accounting evidence. No
		// typed account FK exists inside these payloads, nor does this grant rights.
		if _, err := tx.Exec(`UPDATE expense_pair_members SET facts_json=?,evidence_json=? WHERE ledger_id=?`, `{"accountId":"synthetic-nonexistent-json-account"}`, `{"sourceRole":"synthetic-unverified-claim"}`, f.ledger); err != nil {
			t.Fatal(err)
		}
		pairSchemaMustInsert(t, tx, pairSchemaRevisionSQL, f.revision("synthetic-active", "synthetic-confirm", 1))
		withdraw := f.revision("synthetic-active", "synthetic-withdraw", 2)
		withdraw[4] = "withdraw"
		pairSchemaMustInsert(t, tx, pairSchemaRevisionSQL, withdraw)
		// Same opaque root ID may exist in another ledger; composite references
		// remain ledger-scoped rather than accidentally granting cross-ledger access.
		other := f.root("synthetic-active")
		other[0] = f.otherLedger
		pairSchemaMustInsert(t, tx, pairSchemaRootSQL, other)
		var problems int
		rows, err := tx.Query("PRAGMA foreign_key_check")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			problems++
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		if problems != 0 {
			t.Fatal("valid constrained fixtures violate FKs", problems)
		}
	})
}
