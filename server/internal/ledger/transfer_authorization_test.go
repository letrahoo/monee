package ledger

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/auth"
)

// Read directly rather than through Dashboard, which correctly becomes forbidden
// when access is revoked. Include all tables that a transfer could write.
func transferWriteState(t *testing.T, s *Store) [6]int64 {
	t.Helper()
	var state [6]int64
	for i, table := range []string{"transactions", "postings", "accounts", "idempotency", "change_log"} {
		state[i] = int64(count(t, s, table))
	}
	if err := s.db.QueryRow("SELECT version FROM ledgers WHERE id=?", s.ledgerID).Scan(&state[5]); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestTransferRechecksAuthorizationBeforeReplayAndCreate(t *testing.T) {
	for _, tc := range []struct {
		name, revoke, restore, code string
	}{
		{"admission disabled", "UPDATE user_access SET enabled=0 WHERE user_id=?", "UPDATE user_access SET enabled=1 WHERE user_id=?", "access_denied"},
		{"user disabled", "UPDATE app_users SET status='disabled' WHERE id=?", "UPDATE app_users SET status='active' WHERE id=?", "access_denied"},
		{"member downgraded", "UPDATE ledger_members SET role='viewer' WHERE user_id=? AND ledger_id=?", "UPDATE ledger_members SET role='owner' WHERE user_id=? AND ledger_id=?", "ledger_readonly"},
		{"member removed", "DELETE FROM ledger_members WHERE user_id=? AND ledger_id=?", "INSERT INTO ledger_members(user_id,ledger_id,role,created_at) VALUES(?,?,'owner','2026-09-01T00:00:00Z')", "ledger_denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "application.db")
			// Match service initialization: ledger migrations own the shared schema.
			root, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			a, err := auth.Open(path, []auth.Selector{{Provider: "github", Kind: "subject", Value: "1001"}})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			identity := auth.Identity{Provider: "github", Subject: "1001", Username: "synthetic-transfer-authorization"}
			if err := a.RememberIdentity(identity); err != nil {
				t.Fatal(err)
			}
			userID, err := a.AccountID(identity)
			if err != nil {
				t.Fatal(err)
			}
			l, err := root.CreateLedger(userID, "Synthetic transfer authorization")
			if err != nil {
				t.Fatal(err)
			}
			s, err := root.Scoped(userID, l.ID)
			if err != nil {
				t.Fatal(err)
			}
			in := syntheticTransfer()
			const originalKey = "transfer-authorized-original-001"
			original, err := s.CreateTransfer(in, originalKey)
			if err != nil {
				t.Fatal(err)
			}
			before := transferWriteState(t, s)
			// Mutate permissions on an independent connection after acquiring the
			// scoped Store, so cached authorization would incorrectly pass.
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			args := []any{userID}
			if tc.code != "access_denied" {
				args = append(args, l.ID)
			}
			if result, err := db.Exec(tc.revoke, args...); err != nil {
				t.Fatal(err)
			} else if n, err := result.RowsAffected(); err != nil || n != 1 {
				t.Fatalf("revoke affected %d rows: %v", n, err)
			}
			fresh := in
			fresh.FromAccount = "Synthetic new source"
			fresh.ToAccount = "Synthetic new destination"
			const freshKey = "transfer-authorized-fresh-001"
			for _, attempt := range []struct {
				name, key string
				input     TransferInput
			}{{"existing key replay", originalKey, in}, {"new key first submission", freshKey, fresh}} {
				if got := transferWriteState(t, s); got != before {
					t.Fatalf("%s changed write state before rejection: got %v want %v", attempt.name, got, before)
				}
				tr, err := s.CreateTransfer(attempt.input, attempt.key)
				var denied *Problem
				if !errors.As(err, &denied) || denied.Code != tc.code || tr != (Transaction{}) {
					t.Fatalf("%s: got transaction %+v, error %v; want %s", attempt.name, tr, err, tc.code)
				}
				if got := transferWriteState(t, s); got != before {
					t.Fatalf("%s changed write state after rejection: got %v want %v", attempt.name, got, before)
				}
			}
			if _, err := db.Exec(tc.restore, args...); err != nil {
				t.Fatal(err)
			}
			if replay, err := s.CreateTransfer(in, originalKey); err != nil || replay != original {
				t.Fatalf("restored original key lost original result: %+v %v", replay, err)
			}
			if got := transferWriteState(t, s); got != before {
				t.Fatalf("restored replay changed write state: got %v want %v", got, before)
			}
			// The denied first submission must not consume its key either.
			created, err := s.CreateTransfer(fresh, freshKey)
			if err != nil || created.ID == "" || created.ID == original.ID {
				t.Fatalf("restored new key: %+v %v", created, err)
			}
			settled := transferWriteState(t, s)
			if replay, err := s.CreateTransfer(fresh, freshKey); err != nil || replay != created {
				t.Fatalf("restored new key replay: %+v %v", replay, err)
			}
			if got := transferWriteState(t, s); got != settled {
				t.Fatalf("new key replay changed write state: got %v want %v", got, settled)
			}
		})
	}
}
