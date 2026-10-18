package ledger

import (
	"database/sql"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

func expensePairKey(key string) bool {
	return len(key) >= 16 && len(key) <= 128 && utf8.ValidString(key) && !strings.ContainsFunc(key, unicode.IsControl)
}

func (s *Store) expensePairAuthorize(tx *sql.Tx) error {
	if !associationAccountingID(s.actorID) {
		return problem("unauthenticated", "请先登录")
	}
	if !associationAccountingID(s.ledgerID) {
		return expensePairUnavailable()
	}
	if err := s.authorize(tx, true); err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(`SELECT role FROM ledger_members WHERE ledger_id=? AND user_id=?`, s.ledgerID, s.actorID).Scan(&role); err != nil || (role != "owner" && role != "editor") {
		return problem("ledger_denied", "无此账本操作权限")
	}
	return nil
}

func (s *Store) expensePairLedgerVersion(tx *sql.Tx) (int64, error) {
	var raw any
	var kind string
	if err := tx.QueryRow(`SELECT version,typeof(version) FROM ledgers WHERE id=?`, s.ledgerID).Scan(&raw, &kind); err != nil {
		return 0, expensePairUnavailable()
	}
	version, ok := expensePairInteger(raw, kind)
	if !ok || version < 0 {
		return 0, expensePairUnavailable()
	}
	return version, nil
}

func (s *Store) expensePairReplay(tx *sql.Tx, key, requestHash string) (*expensePairReceipt, error) {
	var savedHash, raw string
	err := tx.QueryRow(`SELECT request_hash,response_json FROM idempotency WHERE ledger_id=? AND key=?`, s.ledgerID, key).Scan(&savedHash, &raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, expensePairUnavailable()
	}
	if savedHash != requestHash {
		return nil, expensePairConflict()
	}
	var receipt expensePairReceipt
	if !expensePairCanonical(raw, &receipt) {
		return nil, expensePairUnavailable()
	}
	return &receipt, nil
}

func (s *Store) expensePairReserveVersion(tx *sql.Tx, expected int64) (int64, error) {
	if expected < 0 || expected == math.MaxInt64 {
		return 0, expensePairConflict()
	}
	result, err := tx.Exec(`UPDATE ledgers SET version=version+1 WHERE id=? AND version=?`, s.ledgerID, expected)
	if err != nil {
		return 0, expensePairUnavailable()
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, expensePairUnavailable()
	}
	if changed != 1 {
		return 0, expensePairConflict()
	}
	return expected + 1, nil
}

func expensePairHasActiveMember(decisions map[string]expensePairStoredDecision, id string) bool {
	for _, decision := range decisions {
		if decision.Status == "active" {
			for _, member := range decision.Members {
				if member.TransactionID == id {
					return true
				}
			}
		}
	}
	return false
}

func (s *Store) expensePairWriteRevision(tx *sql.Tx, snapshot expensePairRevisionSnapshot) error {
	if _, err := tx.Exec(`INSERT INTO expense_pair_revisions(ledger_id,decision_id,revision_id,version,operation,actor_id,reason,snapshot_json,contract_version,created_at) VALUES(?,?,?,?,?,?,?,?,1,?)`, s.ledgerID, snapshot.Receipt.ID, snapshot.RevisionID, snapshot.Receipt.Version, snapshot.Operation, s.actorID, snapshot.Reason, encode(snapshot), snapshot.CreatedAt); err != nil {
		return expensePairUnavailable()
	}
	if _, err := tx.Exec(`INSERT INTO change_log(change_id,ledger_id,device_id,entity_id,entity_type,base_version,entity_version,operation,payload_version,payload_json,created_at,actor_id) VALUES(?,?,?,?,'expense_pair_decision',?,?,?,1,?,?,?)`, newID(), s.ledgerID, s.deviceID, snapshot.Receipt.ID, snapshot.Receipt.Version-1, snapshot.Receipt.Version, "expense_pair_"+snapshot.Operation, encode(snapshot), snapshot.CreatedAt, s.actorID); err != nil {
		return expensePairUnavailable()
	}
	return nil
}

func (s *Store) expensePairFinish(tx *sql.Tx, key, requestHash string, receipt expensePairReceipt) (expensePairReceipt, error) {
	// Validate our complete metadata write before exposing a receipt. This is not
	// the Open/startup fence: that fence still rejects every populated store.
	decisions, err := s.loadExpensePairLedger(tx)
	if err != nil {
		return expensePairReceipt{}, err
	}
	decision, ok := decisions[receipt.ID]
	if !ok || len(decision.Revisions) == 0 || decision.Revisions[len(decision.Revisions)-1].Receipt != receipt {
		return expensePairReceipt{}, expensePairUnavailable()
	}
	// Deliberately last: a late replay-storage failure rolls back the root,
	// members, revision, audit and ledger CAS together.
	if _, err = tx.Exec(`INSERT INTO idempotency(ledger_id,key,request_hash,response_json,created_at) VALUES(?,?,?,?,?)`, s.ledgerID, key, requestHash, encode(receipt), now()); err != nil {
		return expensePairReceipt{}, expensePairUnavailable()
	}
	if err = tx.Commit(); err != nil {
		return expensePairReceipt{}, expensePairUnavailable()
	}
	return receipt, nil
}

// Private metadata transaction only. It changes no original transaction,
// posting, source link, payment identity, source snapshot or transaction version.
// No route calls it; effective readers and mutation guards are NOT activated.
func (s *Store) confirmExpensePair(in expensePairConfirmInput, key string) (expensePairReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !associationAccountingID(s.actorID) {
		return expensePairReceipt{}, problem("unauthenticated", "请先登录")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return expensePairReceipt{}, expensePairUnavailable()
	}
	defer tx.Rollback()
	if err = s.expensePairAuthorize(tx); err != nil {
		return expensePairReceipt{}, err
	}
	reason, validReason := expensePairReason(in.Reason)
	if !expensePairKey(key) || !validReason || in.LedgerVersion < 0 || in.PrimaryVersion < 1 || in.SecondaryVersion < 1 || !associationAccountingID(in.PrimaryTransactionID) || !associationAccountingID(in.SecondaryTransactionID) || in.PrimaryTransactionID == in.SecondaryTransactionID {
		return expensePairReceipt{}, expensePairInvalid()
	}
	in.Reason = reason
	requestHash := hash(encode([]any{"expense-pair-confirm-v1", s.ledgerID, s.actorID, in}))
	replayed, err := s.expensePairReplay(tx, key, requestHash)
	if err != nil {
		return expensePairReceipt{}, err
	}
	decisions, err := s.loadExpensePairLedger(tx)
	if err != nil {
		return expensePairReceipt{}, err
	}
	if replayed != nil {
		d, ok := decisions[replayed.ID]
		if !ok {
			return expensePairReceipt{}, expensePairUnavailable()
		}
		if d.Status != "active" {
			return expensePairReceipt{}, expensePairConflict()
		}
		if len(d.Revisions) != 1 || d.Revisions[0].Receipt != *replayed || replayed.PrimaryID != in.PrimaryTransactionID || replayed.SecondaryID != in.SecondaryTransactionID || in.LedgerVersion == math.MaxInt64 || replayed.LedgerVersion != in.LedgerVersion+1 || d.Revisions[0].ActorID != s.actorID || d.Revisions[0].Reason != reason || d.Members[0].TransactionVersion != in.PrimaryVersion || d.Members[1].TransactionVersion != in.SecondaryVersion {
			return expensePairReceipt{}, expensePairUnavailable()
		}
		return *replayed, nil
	}
	version, err := s.expensePairLedgerVersion(tx)
	if err != nil {
		return expensePairReceipt{}, err
	}
	if version != in.LedgerVersion || expensePairHasActiveMember(decisions, in.PrimaryTransactionID) || expensePairHasActiveMember(decisions, in.SecondaryTransactionID) {
		return expensePairReceipt{}, expensePairConflict()
	}
	primary, err := s.expensePairReadMember(tx, in.PrimaryTransactionID)
	if err != nil {
		return expensePairReceipt{}, err
	}
	secondary, err := s.expensePairReadMember(tx, in.SecondaryTransactionID)
	if err != nil {
		return expensePairReceipt{}, err
	}
	if primary.Transaction.Version != in.PrimaryVersion || secondary.Transaction.Version != in.SecondaryVersion {
		return expensePairReceipt{}, expensePairConflict()
	}
	if !expensePairFactsValid(s.ledgerID, primary) || !expensePairFactsValid(s.ledgerID, secondary) {
		return expensePairReceipt{}, expensePairUnavailable()
	}
	if _, err = planAssociationExpensePairProjection(s.ledgerID, version, expensePairInput(primary), expensePairInput(secondary), in.PrimaryTransactionID); err != nil {
		return expensePairReceipt{}, expensePairUnsupported()
	}
	primaryEvidence, err := s.expensePairReadEvidence(tx, primary.Transaction)
	if err != nil {
		return expensePairReceipt{}, err
	}
	secondaryEvidence, err := s.expensePairReadEvidence(tx, secondary.Transaction)
	if err != nil {
		return expensePairReceipt{}, err
	}
	members := []expensePairMemberSnapshot{
		{Role: "primary", TransactionID: in.PrimaryTransactionID, TransactionVersion: in.PrimaryVersion, Active: 1, Facts: primary, Evidence: primaryEvidence},
		{Role: "secondary", TransactionID: in.SecondaryTransactionID, TransactionVersion: in.SecondaryVersion, Active: 1, Facts: secondary, Evidence: secondaryEvidence},
	}
	nextVersion, err := s.expensePairReserveVersion(tx, version)
	if err != nil {
		return expensePairReceipt{}, err
	}
	id, timestamp := newID(), now()
	receipt := expensePairReceipt{ID: id, Version: 1, Status: "active", LedgerVersion: nextVersion, PrimaryID: in.PrimaryTransactionID, SecondaryID: in.SecondaryTransactionID}
	if _, err = tx.Exec(`INSERT INTO expense_pair_decisions(ledger_id,id,status,version,contract_version,created_by,created_at,updated_at) VALUES(?,?,'active',1,1,?,?,?)`, s.ledgerID, id, s.actorID, timestamp, timestamp); err != nil {
		return expensePairReceipt{}, expensePairUnavailable()
	}
	for _, member := range members {
		if _, err = tx.Exec(`INSERT INTO expense_pair_members(ledger_id,decision_id,transaction_id,role,transaction_version,facts_json,evidence_json,active) VALUES(?,?,?,?,?,?,?,1)`, s.ledgerID, id, member.TransactionID, member.Role, member.TransactionVersion, encode(member.Facts), encode(member.Evidence)); err != nil {
			return expensePairReceipt{}, expensePairUnavailable()
		}
	}
	snapshot := expensePairRevisionSnapshot{ContractVersion: 1, RevisionID: newID(), LedgerID: s.ledgerID, Operation: "confirm", ActorID: s.actorID, Reason: reason, CreatedAt: timestamp, CreatorID: s.actorID, DecisionCreatedAt: timestamp, Receipt: receipt, Members: members}
	if err = s.expensePairWriteRevision(tx, snapshot); err != nil {
		return expensePairReceipt{}, err
	}
	return s.expensePairFinish(tx, key, requestHash, receipt)
}

// Withdrawal changes metadata membership only. It neither restores nor rewrites
// a member's financial fields, and cannot bypass active-state corruption checks.
func (s *Store) withdrawExpensePair(in expensePairWithdrawInput, key string) (expensePairReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !associationAccountingID(s.actorID) {
		return expensePairReceipt{}, problem("unauthenticated", "请先登录")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return expensePairReceipt{}, expensePairUnavailable()
	}
	defer tx.Rollback()
	if err = s.expensePairAuthorize(tx); err != nil {
		return expensePairReceipt{}, err
	}
	reason, validReason := expensePairReason(in.Reason)
	if !expensePairKey(key) || !validReason || in.LedgerVersion < 0 || in.DecisionVersion < 1 || !associationAccountingID(in.DecisionID) {
		return expensePairReceipt{}, expensePairInvalid()
	}
	in.Reason = reason
	requestHash := hash(encode([]any{"expense-pair-withdraw-v1", s.ledgerID, s.actorID, in}))
	replayed, err := s.expensePairReplay(tx, key, requestHash)
	if err != nil {
		return expensePairReceipt{}, err
	}
	decisions, err := s.loadExpensePairLedger(tx)
	if err != nil {
		return expensePairReceipt{}, err
	}
	if replayed != nil {
		d, ok := decisions[replayed.ID]
		if !ok || d.Status != "retracted" || len(d.Revisions) != 2 || d.Revisions[1].Receipt != *replayed || replayed.ID != in.DecisionID || in.DecisionVersion != 1 || in.LedgerVersion == math.MaxInt64 || replayed.LedgerVersion != in.LedgerVersion+1 || d.Revisions[1].ActorID != s.actorID || d.Revisions[1].Reason != reason {
			return expensePairReceipt{}, expensePairUnavailable()
		}
		return *replayed, nil
	}
	version, err := s.expensePairLedgerVersion(tx)
	if err != nil {
		return expensePairReceipt{}, err
	}
	if version != in.LedgerVersion {
		return expensePairReceipt{}, expensePairConflict()
	}
	d, ok := decisions[in.DecisionID]
	if !ok {
		return expensePairReceipt{}, problem("not_found", "找不到此账本的关联决策")
	}
	if d.Status != "active" || d.Version != in.DecisionVersion {
		return expensePairReceipt{}, expensePairConflict()
	}
	nextVersion, err := s.expensePairReserveVersion(tx, version)
	if err != nil {
		return expensePairReceipt{}, err
	}
	timestamp := now()
	updated, err := tx.Exec(`UPDATE expense_pair_decisions SET status='retracted',version=version+1,updated_at=? WHERE ledger_id=? AND id=? AND status='active' AND version=?`, timestamp, s.ledgerID, d.ID, d.Version)
	if err != nil {
		return expensePairReceipt{}, expensePairUnavailable()
	}
	changed, err := updated.RowsAffected()
	if err != nil || changed != 1 {
		return expensePairReceipt{}, expensePairConflict()
	}
	updated, err = tx.Exec(`UPDATE expense_pair_members SET active=0 WHERE ledger_id=? AND decision_id=? AND active=1`, s.ledgerID, d.ID)
	if err != nil {
		return expensePairReceipt{}, expensePairUnavailable()
	}
	changed, err = updated.RowsAffected()
	if err != nil || changed != 2 {
		return expensePairReceipt{}, expensePairUnavailable()
	}
	d.Version, d.Status = 2, "retracted"
	receipt := expensePairReceiptFor(d, nextVersion)
	snapshot := expensePairRevisionSnapshot{ContractVersion: 1, RevisionID: newID(), LedgerID: s.ledgerID, Operation: "withdraw", ActorID: s.actorID, Reason: reason, CreatedAt: timestamp, CreatorID: d.CreatorID, DecisionCreatedAt: d.CreatedAt, Receipt: receipt, Members: expensePairSnapshotMembers(d.Members, 0)}
	if err = s.expensePairWriteRevision(tx, snapshot); err != nil {
		return expensePairReceipt{}, err
	}
	return s.expensePairFinish(tx, key, requestHash, receipt)
}
