package ledger

import (
	"errors"
	"testing"
)

func TestExpensePairEffectiveSchema4Boundary(t *testing.T) {
	for _, mode := range []string{"clean", "view", "audit", "schema5", "future"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := expensePairV4Fixture(t)
			switch mode {
			case "view":
				foundationExec(t, s, "CREATE VIEW expense_pair_members AS SELECT 1 AS active")
			case "audit":
				tx, err := s.db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				if err = s.change(tx, "expense_pair_decision", "synthetic-orphan", map[string]string{"reason": "synthetic"}); err != nil {
					tx.Rollback()
					t.Fatal(err)
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
			case "schema5":
				foundationExec(t, s, "PRAGMA user_version=5")
			case "future":
				foundationExec(t, s, "PRAGMA user_version=6")
			}
			tx, err := s.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			present, err := expensePairSchemaPresent(tx)
			if mode == "clean" {
				if err != nil || present {
					t.Fatal(present, err)
				}
				return
			}
			var p *Problem
			if present || !errors.As(err, &p) || p.Code != "expense_pair_unavailable" {
				t.Fatal(present, err)
			}
		})
	}
}
