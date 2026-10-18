package ledger

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

// Test-only adapter: main schema has NO postings.ledger_id. The posting's
// transaction ledger is obtained from its real transaction join; account ledger
// comes from the actual referenced account, not a display name or an input label.
func accountingFactsFromTempDB(t *testing.T, s *Store, id string) associationExpenseAccountingInput {
	t.Helper()
	tx, e := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	return accountingFactsFromTempTx(t, tx, id)
}

func accountingFactsFromTempTx(t *testing.T, tx *sql.Tx, id string) associationExpenseAccountingInput {
	t.Helper()
	var in associationExpenseAccountingInput
	e := tx.QueryRow(`SELECT ledger_id,id,version,kind,currency,amount_minor,typeof(amount_minor),amount_minor FROM transactions WHERE id=?`, id).Scan(&in.LedgerID, &in.TransactionID, &in.Version, &in.Kind, &in.Currency, &in.AmountMinor, &in.AmountStorageClass, &in.AmountRaw)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := tx.Query(`SELECT p.id,t.ledger_id,p.transaction_id,p.account_id,COALESCE(a.ledger_id,''),COALESCE(a.kind,''),p.currency,typeof(p.amount_minor),p.amount_minor FROM postings p JOIN transactions t ON t.id=p.transaction_id LEFT JOIN accounts a ON a.id=p.account_id WHERE p.transaction_id=? ORDER BY p.id`, id)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	for rows.Next() {
		var p associationExpensePostingInput
		if e = rows.Scan(&p.ID, &p.TransactionLedgerID, &p.TransactionID, &p.AccountID, &p.AccountLedgerID, &p.AccountKind, &p.Currency, &p.AmountStorageClass, &p.AmountRaw); e != nil {
			t.Fatal(e)
		}
		in.Postings = append(in.Postings, p)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	return in
}
func accountingFactCreate(t *testing.T, s *Store, account, key string) Transaction {
	t.Helper()
	in := testInput()
	in.Account = account
	in.ExternalID = key
	r, e := s.Create(in, "accounting-facts-"+key)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func accountingFactTables(t *testing.T, s *Store) map[string][][]any {
	t.Helper()
	r, e := s.db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for r.Next() {
		var name string
		if e = r.Scan(&name); e != nil {
			t.Fatal(e)
		}
		names = append(names, name)
	}
	if e = r.Err(); e != nil {
		t.Fatal(e)
	}
	r.Close()
	out := map[string][][]any{}
	for _, name := range names {
		rows, e := s.db.Query(`SELECT * FROM "` + strings.ReplaceAll(name, `"`, `""`) + `"`)
		if e != nil {
			t.Fatal(e)
		}
		columns, e := rows.Columns()
		if e != nil {
			t.Fatal(e)
		}
		var data [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			p := make([]any, len(columns))
			for i := range values {
				p[i] = &values[i]
			}
			if e = rows.Scan(p...); e != nil {
				t.Fatal(e)
			}
			data = append(data, values)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		rows.Close()
		out[name] = data
	}
	return out
}
func inspectTempAccountingFacts(t *testing.T, s *Store, id string) associationExpenseAccountingFacts {
	t.Helper()
	f, e := inspectAssociationExpenseAccountingFacts(s.ledgerID, accountingFactsFromTempDB(t, s, id))
	if e != nil {
		t.Fatal(e)
	}
	return f
}

func TestAssociationAccountingFactsStoreCurrentProjectionIsReadOnly(t *testing.T) {
	s := testStore(t)
	r := accountingFactCreate(t, s, "合成账户A", "initial")
	in := accountingFactsFromTempDB(t, s, r.ID)
	before := accountingFactTables(t, s)
	want := associationExpenseAccountingFacts{LedgerID: s.ledgerID, TransactionID: r.ID, Version: r.Version, AmountMinor: -1234}
	for _, leg := range in.Postings {
		if leg.AccountKind == "clearing" {
			want.FundPostingID = leg.ID
			want.FundAccountID = leg.AccountID
		} else if leg.AccountKind == "expense" {
			want.ExpensePostingID = leg.ID
			want.ExpenseAccountID = leg.AccountID
		}
	}
	for i := 0; i < 3; i++ {
		f, e := inspectAssociationExpenseAccountingFacts(s.ledgerID, in)
		if e != nil || f != want || want.FundAccountID == "" || want.ExpenseAccountID == "" || want.FundPostingID == "" || want.ExpensePostingID == "" {
			t.Fatal(f, e)
		}
	}
	if !reflect.DeepEqual(before, accountingFactTables(t, s)) {
		t.Fatal("Pure accounting fact inspection mutated application tables")
	}
	corrected, e := s.Correct(r.ID, CorrectionInput{Version: r.Version, Type: "expense", AmountMinor: "1250", Reason: "合成金额更正"})
	if e != nil {
		t.Fatal(e)
	}
	afterCorrect := inspectTempAccountingFacts(t, s, r.ID)
	if afterCorrect.AmountMinor != -1250 || afterCorrect.Version != corrected.Version {
		t.Fatal(afterCorrect)
	}
	annotated, e := s.Annotate(r.ID, AnnotationInput{Version: corrected.Version, Category: "合成新分类", Note: "synthetic note not an accounting fact"})
	if e != nil {
		t.Fatal(e)
	}
	afterAnnotation := inspectTempAccountingFacts(t, s, r.ID)
	if afterAnnotation.FundAccountID != afterCorrect.FundAccountID || afterAnnotation.ExpenseAccountID == afterCorrect.ExpenseAccountID || afterAnnotation.Version != annotated.Version {
		t.Fatal(afterAnnotation)
	}
	// The helper has no live-DB authority: a coherent old snapshot is still coherent.
	old, e := inspectAssociationExpenseAccountingFacts(s.ledgerID, in)
	if e != nil || old.AmountMinor != -1234 || old.Version != 1 {
		t.Fatal("Facts validation must not pretend to refresh, authorize or establish freshness", old, e)
	}
}
func TestAssociationAccountingFactsStoreDoesNotClaimEligibility(t *testing.T) {
	s := testStore(t)
	r := accountingFactCreate(t, s, "合成账户A", "refund-parent")
	_, e := s.CreateRefund(r.ID, RefundInput{Version: 1, Date: "2026-09-15", Amount: "1.00", Currency: "CNY", Account: "合成账户A"}, "accounting-facts-refund")
	if e != nil {
		t.Fatal(e)
	}
	withRefund := inspectTempAccountingFacts(t, s, r.ID)
	if withRefund.AmountMinor != -1234 || withRefund.Version <= 1 {
		t.Fatal(withRefund)
	}
	if _, e = s.db.Exec("UPDATE transactions SET deleted_at=? WHERE id=?", now(), r.ID); e != nil {
		t.Fatal(e)
	}
	deleted := inspectTempAccountingFacts(t, s, r.ID)
	if deleted != withRefund {
		t.Fatal("Input intentionally omits active/deletion policy, not a membership permission", deleted, withRefund)
	}
	other := accountingFactCreate(t, s, "合成账户B", "other-account")
	otherFacts := inspectTempAccountingFacts(t, s, other.ID)
	if otherFacts.FundAccountID == withRefund.FundAccountID {
		t.Fatal("Different actual accounting accounts collapsed by merchant/source labels")
	}
}
func TestAssociationAccountingFactsStoreRejectsCorruptPostingEvidence(t *testing.T) {
	cases := map[string]func(*testing.T, *Store, Transaction){
		"balanced legs with wrong amount": func(t *testing.T, s *Store, r Transaction) {
			_, e := s.db.Exec("UPDATE postings SET amount_minor=CASE WHEN amount_minor<0 THEN -1200 ELSE 1200 END WHERE transaction_id=?", r.ID)
			if e != nil {
				t.Fatal(e)
			}
		},
		"real posting": func(t *testing.T, s *Store, r Transaction) {
			_, e := s.db.Exec("UPDATE postings SET amount_minor=-1234.5 WHERE transaction_id=? AND amount_minor<0", r.ID)
			if e != nil {
				t.Fatal(e)
			}
		},
		"real transaction": func(t *testing.T, s *Store, r Transaction) {
			_, e := s.db.Exec("UPDATE transactions SET amount_minor=-1234.5 WHERE id=?", r.ID)
			if e != nil {
				t.Fatal(e)
			}
		},
		"missing leg": func(t *testing.T, s *Store, r Transaction) {
			_, e := s.db.Exec("UPDATE postings SET transaction_id=? WHERE transaction_id=? AND amount_minor<0", accountingFactCreate(t, s, "合成账户B", "leg-holder").ID, r.ID)
			if e != nil {
				t.Fatal(e)
			}
		},
		"two clearing accounts": func(t *testing.T, s *Store, r Transaction) {
			_, e := s.db.Exec("UPDATE accounts SET kind='clearing' WHERE id IN (SELECT account_id FROM postings WHERE transaction_id=? AND amount_minor>0)", r.ID)
			if e != nil {
				t.Fatal(e)
			}
		},
		"cross ledger account": func(t *testing.T, s *Store, r Transaction) {
			ledgerID := newID()
			_, e := s.db.Exec("INSERT INTO ledgers(id,currency,timezone,version,device_id,created_at,name) VALUES(?,?,?,?,?,?,?)", ledgerID, "CNY", "Asia/Shanghai", 0, newID(), now(), "合成其他账本")
			if e != nil {
				t.Fatal(e)
			}
			accountID := newID()
			_, e = s.db.Exec("INSERT INTO accounts(id,ledger_id,name,kind,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", accountID, ledgerID, "合成同名账户", "clearing", 1, now(), now())
			if e != nil {
				t.Fatal(e)
			}
			_, e = s.db.Exec("UPDATE postings SET account_id=? WHERE transaction_id=? AND amount_minor<0", accountID, r.ID)
			if e != nil {
				t.Fatal(e)
			}
		},
		"missing referenced account": func(t *testing.T, s *Store, r Transaction) {
			if _, e := s.db.Exec("PRAGMA foreign_keys=OFF"); e != nil {
				t.Fatal(e)
			}
			defer func() {
				if _, e := s.db.Exec("PRAGMA foreign_keys=ON"); e != nil {
					t.Error(e)
				}
			}()
			if _, e := s.db.Exec("UPDATE postings SET account_id='synthetic-missing-account' WHERE transaction_id=? AND amount_minor<0", r.ID); e != nil {
				t.Fatal(e)
			}

		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			r := accountingFactCreate(t, s, "合成账户A", "corrupt-fixture")
			corrupt(t, s, r)
			in := accountingFactsFromTempDB(t, s, r.ID)
			before := accountingFactTables(t, s)
			out, e := inspectAssociationExpenseAccountingFacts(s.ledgerID, in)
			if e == nil || out != (associationExpenseAccountingFacts{}) {
				t.Fatal("Corrupt accounting data yielded usable facts", out, e)
			}
			if !reflect.DeepEqual(before, accountingFactTables(t, s)) {
				t.Fatal("Rejected facts inspection changed any table")
			}
		})
	}
}
