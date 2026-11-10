package ledger

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/letrahoo/monee/server/internal/dirlease"
	"github.com/letrahoo/monee/server/internal/storage"
)

type runtimePairFixture struct {
	dir, path                   string
	root, owner, viewer         *Store
	in                          expensePairConfirmInput
	ownerID, viewerID, ledgerID string
}

// No PRAGMA unlock: native imports on5, all connections closed, verified real
// offline upgrade, lease released, and only normal Open6 supplies runtime stores.
func runtimePair(t *testing.T) *runtimePairFixture {
	t.Helper()
	f := &runtimePairFixture{dir: t.TempDir()}
	f.path = filepath.Join(f.dir, "application.db")
	root, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.root = root
	t.Cleanup(func() { f.root.Close() })
	_, f.owner, f.in = lifecycleFixtureInStore(t, root, nativeExpense, "synthetic-lifecycle-wallet")
	f.viewer = lifecycleMember(t, root, "viewer")
	f.ownerID, f.viewerID, f.ledgerID = f.owner.actorID, f.viewer.actorID, root.ledgerID
	lifecycleRefresh(t, root, &f.in)
	if err = root.Close(); err != nil {
		t.Fatal(err)
	}
	lease, err := dirlease.Acquire(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	_, upgradeErr := UpgradeExpensePairOffline(context.Background(), lease, f.dir, "before-runtime6")
	closeErr := lease.Close()
	if upgradeErr != nil || closeErr != nil {
		t.Fatal(upgradeErr, closeErr)
	}
	runtimeReopen(t, f)
	return f
}
func runtimeReopen(t *testing.T, f *runtimePairFixture) {
	t.Helper()
	f.root.Close()
	root, err := Open(f.path)
	if err != nil {
		t.Fatal("normal reopen6", err)
	}
	f.root = root
	f.owner, err = root.Scoped(f.ownerID, f.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	f.viewer, err = root.Scoped(f.viewerID, f.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	if v, e := root.SchemaVersion(); e != nil || v != 6 {
		t.Fatal(v, e)
	}
}
func runtimeDigest(t *testing.T, path string) [32]byte {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := expensePairUpgradeDigest(db)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	return hash
}
func runtimeConfirm(t *testing.T, f *runtimePairFixture) ExpensePairReceipt {
	t.Helper()
	_, err := f.owner.PreviewExpensePair(pairAPIPreviewInput(f.in))
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.owner.ConfirmExpensePair(f.in, "runtime6-confirm-stable-key")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func runtimeWithdraw(t *testing.T, f *runtimePairFixture, r ExpensePairReceipt) ExpensePairReceipt {
	t.Helper()
	detail, err := f.owner.ExpensePairs(f.in.PrimaryTransactionID)
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.owner.WithdrawExpensePair(ExpensePairWithdrawInput{LedgerVersion: detail.LedgerVersion, DecisionID: r.ID, DecisionVersion: r.Version, Reason: "合成runtime6撤回"}, "runtime6-withdraw-stable-key")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestExpensePairRuntimeNormalUpgradeReopenLifecycle(t *testing.T) {
	f := runtimePair(t)
	before := nativeEvidenceTables(t, f.root)
	r := runtimeConfirm(t, f)
	lifecycleOnlyMetadataChanged(t, before, nativeEvidenceTables(t, f.root))
	hash := runtimeDigest(t, f.path)
	runtimeReopen(t, f)
	if hash != runtimeDigest(t, f.path) {
		t.Fatal("startup changed active data")
	}
	for _, s := range []*Store{f.owner, f.viewer} {
		if d := dashboard(t, s); d.TotalCount != 1 || d.ExpenseMinor != "1234" || len(d.ExpensePairs) != 1 {
			t.Fatal(d)
		}
		for _, id := range []string{f.in.PrimaryTransactionID, f.in.SecondaryTransactionID} {
			detail, err := s.ExpensePairs(id)
			if err != nil || len(detail.Decisions) != 1 || detail.Decisions[0] != r {
				t.Fatal(detail, err)
			}
			sources, err := s.TransactionSources(id)
			if err != nil || len(sources) != 1 {
				t.Fatal(sources, err)
			}
		}
	}
	same, err := f.owner.ConfirmExpensePair(f.in, "runtime6-confirm-stable-key")
	if err != nil || same != r {
		t.Fatal(same, err)
	}
	if hash != runtimeDigest(t, f.path) {
		t.Fatal("replay after reopen duplicated audit")
	}
	for _, id := range []string{f.in.PrimaryTransactionID, f.in.SecondaryTransactionID} {
		version := effectiveVersion(t, f.root, id)
		effectiveLocked(t, f.root, func() error {
			_, e := f.owner.Annotate(id, AnnotationInput{Version: version, Category: "餐饮美食", Note: "blocked"})
			return e
		})
		effectiveLocked(t, f.root, func() error {
			_, e := f.owner.Correct(id, CorrectionInput{Version: version, Type: "expense", AmountMinor: "1000", Reason: "blocked"})
			return e
		})
		effectiveLocked(t, f.root, func() error {
			_, e := f.owner.CreateRefund(id, RefundInput{Version: version, Date: "2026-09-16", Amount: "1.00", Currency: "CNY", Account: "合成退款账户"}, "runtime6-locked-refund-"+id)
			return e
		})
	}
	_, err = f.viewer.WithdrawExpensePair(ExpensePairWithdrawInput{LedgerVersion: r.LedgerVersion, DecisionID: r.ID, DecisionVersion: r.Version, Reason: "denied"}, "runtime6-viewer-denied-key")
	pairAPIProblem(t, err, "ledger_readonly")
	r = runtimeWithdraw(t, f, r)
	hash = runtimeDigest(t, f.path)
	runtimeReopen(t, f)
	if hash != runtimeDigest(t, f.path) {
		t.Fatal("startup changed withdrawn data")
	}
	if d := dashboard(t, f.viewer); d.TotalCount != 2 || d.ExpenseMinor != "2468" || len(d.ExpensePairs) != 0 {
		t.Fatal(d)
	}
	detail, err := f.viewer.ExpensePairs(f.in.SecondaryTransactionID)
	if err != nil || len(detail.Decisions) != 1 || detail.Decisions[0] != r {
		t.Fatal(detail, err)
	}
	// Retraction freezes history, not current facts. Later legitimate mutations
	// must stay valid across startup; otherwise runtime6 would strand edited books.
	version := effectiveVersion(t, f.root, f.in.SecondaryTransactionID)
	if _, err = f.owner.Annotate(f.in.SecondaryTransactionID, AnnotationInput{Version: version, Category: "餐饮美食", Note: "withdrawn editable"}); err != nil {
		t.Fatal(err)
	}
	version = effectiveVersion(t, f.root, f.in.PrimaryTransactionID)
	if _, err = f.owner.CreateRefund(f.in.PrimaryTransactionID, RefundInput{Version: version, Date: "2026-09-16", Amount: "1.00", Currency: "CNY", Account: "合成退款账户"}, "runtime6-refund-after-withdraw"); err != nil {
		t.Fatal(err)
	}
	hash = runtimeDigest(t, f.path)
	runtimeReopen(t, f)
	if hash != runtimeDigest(t, f.path) {
		t.Fatal("reopen modified later valid facts")
	}
	if _, err = f.viewer.ExpensePairs(f.in.PrimaryTransactionID); err != nil {
		t.Fatal(err)
	}
}

func TestExpensePairRuntimeWithdrawUndoAndSourceGuardReopen(t *testing.T) {
	f := runtimePair(t)
	r := runtimeConfirm(t, f)
	runtimeReopen(t, f)
	var importID string
	if err := f.root.db.QueryRow("SELECT import_id FROM source_records WHERE transaction_id=?", f.in.SecondaryTransactionID).Scan(&importID); err != nil {
		t.Fatal(err)
	}
	preview, err := f.owner.PreviewImportUndo(importID, false)
	if err != nil || preview.BlockedCount != 1 {
		t.Fatal(preview, err)
	}
	effectiveLocked(t, f.root, func() error { _, e := f.owner.UndoImport(importID, preview.LedgerVersion); return e })
	runtimeWithdraw(t, f, r)
	preview, err = f.owner.PreviewImportUndo(importID, false)
	if err != nil || preview.BlockedCount != 0 || preview.ChangeCount != 1 {
		t.Fatal(preview, err)
	}
	if _, err = f.owner.UndoImport(importID, preview.LedgerVersion); err != nil {
		t.Fatal(err)
	}
	hash := runtimeDigest(t, f.path)
	runtimeReopen(t, f)
	if hash != runtimeDigest(t, f.path) {
		t.Fatal("reopen changed legitimate undo")
	}
	if d := dashboard(t, f.owner); d.TotalCount != 1 || d.ExpenseMinor != "1234" {
		t.Fatal(d)
	}
}

func TestExpensePairRuntimeCorruptionRefusesWithoutRepair(t *testing.T) {
	for _, retracted := range []bool{false, true} {
		state := "active"
		if retracted {
			state = "retracted"
		}
		for _, damage := range []string{"marker", "missing-member", "extra-member", "missing-revision", "revision-json", "missing-audit", "audit-json", "evidence-json", "facts-json", "source-json", "import-hash", "unknown-table", "unknown-trigger", "unprefixed-trigger", "missing-index", "missing-table", "orphan-ledger"} {
			t.Run(state+"/"+damage, func(t *testing.T) {
				f := runtimePair(t)
				r := runtimeConfirm(t, f)
				if retracted {
					r = runtimeWithdraw(t, f, r)
				}
				var extraID string
				if damage == "extra-member" {
					tr, e := f.owner.Create(testInput(), "runtime6-extra-normal-transaction")
					if e != nil {
						t.Fatal(e)
					}
					extraID = tr.ID
				}
				if err := f.root.Close(); err != nil {
					t.Fatal(err)
				}
				db, err := sql.Open("sqlite", f.path)
				if err != nil {
					t.Fatal(err)
				}
				var query string
				args := []any{}
				switch damage {
				case "marker":
					query = "UPDATE expense_pair_members SET active=1-active"
				case "missing-member":
					query = "DELETE FROM expense_pair_members WHERE role='secondary'"
				case "extra-member":
					query = "PRAGMA ignore_check_constraints=ON; INSERT INTO expense_pair_members SELECT ledger_id,decision_id,?,'extra',transaction_version,facts_json,evidence_json,active FROM expense_pair_members WHERE role='primary'"
					args = []any{extraID}
				case "missing-revision":
					query = "DELETE FROM expense_pair_revisions WHERE version=1"
				case "revision-json":
					query = "UPDATE expense_pair_revisions SET snapshot_json='{}'"
				case "missing-audit":
					query = "DELETE FROM change_log WHERE entity_type='expense_pair_decision'"
				case "audit-json":
					query = "UPDATE change_log SET payload_json='{}' WHERE entity_type='expense_pair_decision'"
				case "evidence-json":
					query = "UPDATE expense_pair_members SET evidence_json='{}'"
				case "facts-json":
					query = "UPDATE expense_pair_members SET facts_json='{}'"
				case "source-json":
					query = "UPDATE source_records SET normalized_json='{}'"
				case "import-hash":
					query = "UPDATE imports SET content_hash='tampered-'||id"
				case "unknown-table":
					query = "CREATE TABLE expense_pair_unknown(value TEXT)"
				case "unknown-trigger":
					query = "CREATE TRIGGER expense_pair_unknown AFTER INSERT ON transactions BEGIN SELECT 1; END"
				case "unprefixed-trigger":
					query = "CREATE TRIGGER hidden_pair_trigger AFTER UPDATE ON expense_pair_decisions BEGIN SELECT 1; END"
				case "missing-index":
					query = "DROP INDEX expense_pair_members_one_active_transaction"
				case "missing-table":
					query = "ALTER TABLE expense_pair_members RENAME TO missing_members"
				case "orphan-ledger":
					query = "UPDATE change_log SET ledger_id=? WHERE entity_type='expense_pair_decision'"
					args = []any{newID()}
				}
				if _, err = db.Exec(query, args...); err != nil {
					db.Close()
					t.Fatal(err)
				}
				db.Close()
				before := runtimeDigest(t, f.path)
				opened, err := Open(f.path)
				if opened != nil {
					opened.Close()
					t.Fatal("accepted corrupted6", damage)
				}
				var p *Problem
				if !errors.As(err, &p) || p.Code != "expense_pair_unavailable" {
					t.Fatal(err)
				}
				if before != runtimeDigest(t, f.path) {
					t.Fatal("startup repaired or wrote corrupt6")
				}
				if version, e := storage.Check(f.path); damage != "orphan-ledger" && damage != "missing-table" && (e != nil || version != 6) {
					t.Fatal(version, e)
				}
				matches, e := filepath.Glob(f.path + ".backup-before-*")
				if e != nil || len(matches) != 0 {
					t.Fatal(matches, e)
				}
			})
		}
	}
}

func TestExpensePairRuntimeValidatesNonDefaultLedger(t *testing.T) {
	f := runtimePair(t)
	secondUser := lifecycleAdmit(t, f.root, "member")
	other, err := f.root.CreateLedger(secondUser, "合成第二账本")
	if err != nil {
		t.Fatal(err)
	}
	otherRoot, err := f.root.Scoped(secondUser, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, otherOwner, in := lifecycleFixtureOwned(t, otherRoot, otherRoot, nativeExpense, "synthetic-lifecycle-wallet")
	if _, err = otherOwner.ConfirmExpensePair(in, "runtime6-nondefault-confirm-key"); err != nil {
		t.Fatal(err)
	}
	var first string
	if err = f.root.db.QueryRow("SELECT id FROM ledgers ORDER BY created_at,id LIMIT 1").Scan(&first); err != nil || first != f.ledgerID {
		t.Fatal(first, err)
	}
	foundationExec(t, f.root, "UPDATE expense_pair_members SET active=0 WHERE ledger_id=?", other.ID)
	f.root.Close()
	before := runtimeDigest(t, f.path)
	opened, err := Open(f.path)
	if opened != nil {
		opened.Close()
		t.Fatal("only default ledger validated")
	}
	pairAPIProblem(t, err, "expense_pair_unavailable")
	if before != runtimeDigest(t, f.path) {
		t.Fatal("startup modified damaged nondefault ledger")
	}
}

func TestExpensePairDefaultFiveNeverAutoActivates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "application.db")
	for i := 0; i < 3; i++ {
		root, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if v, e := root.SchemaVersion(); e != nil || v != 5 {
			t.Fatal(v, e)
		}
		_, err = root.ExpensePairs(newID())
		if err == nil {
			t.Fatal("default5 enabled pair API")
		}
		root.Close()
	}
	matches, e := filepath.Glob(path + ".backup-before-*")
	if e != nil || len(matches) != 0 {
		t.Fatal(matches, e)
	}
	if _, e = os.Stat(filepath.Join(dir, "before-runtime6")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}

func TestExpensePairRuntimeRestoreSixPreservesCapability(t *testing.T) {
	f := runtimePair(t)
	runtimeConfirm(t, f)
	f.root.Close()
	bundle := filepath.Join(t.TempDir(), "active-six")
	if err := storage.Snapshot(f.path, bundle); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "application.db")
	if err := storage.Restore(bundle, target); err != nil {
		t.Fatal(err)
	}
	root, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	owner, err := root.Scoped(f.ownerID, f.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	if d := dashboard(t, owner); d.TotalCount != 1 || d.ExpenseMinor != "1234" {
		t.Fatal(d)
	}
	if !reflect.DeepEqual(root.ledgerID, f.ledgerID) {
		t.Fatal(root.ledgerID)
	}
}
