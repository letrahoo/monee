package ledger

import (
	"database/sql"
	"reflect"
	"strings"
	"unicode/utf8"
)

func expensePairReason(reason string) (string, bool) {
	reason = strings.TrimSpace(reason)
	return reason, utf8.ValidString(reason) && reason != "" && len([]rune(reason)) <= 1000 && !strings.ContainsRune(reason, 0)
}

func expensePairInteger(raw any, kind string) (int64, bool) {
	value, ok := raw.(int64)
	return value, ok && kind == "integer"
}

func expensePairSnapshotMembers(members []expensePairMemberSnapshot, active int) []expensePairMemberSnapshot {
	out := append([]expensePairMemberSnapshot{}, members...)
	for i := range out {
		out[i].Active = active
	}
	return out
}

func expensePairReceiptFor(d expensePairStoredDecision, ledgerVersion int64) expensePairReceipt {
	return expensePairReceipt{ID: d.ID, Version: d.Version, Status: d.Status, LedgerVersion: ledgerVersion, PrimaryID: d.Members[0].TransactionID, SecondaryID: d.Members[1].TransactionID}
}

// This is a strict private metadata loader, not an effective financial reader.
// Every decision in the current ledger is checked before a new metadata write.
// Unknown objects in another ledger are not read or used to disclose evidence.
func (s *Store) loadExpensePairLedger(tx *sql.Tx) (map[string]expensePairStoredDecision, error) {
	var ledgerVersionRaw any
	var ledgerVersionKind string
	if err := tx.QueryRow(`SELECT version,typeof(version) FROM ledgers WHERE id=?`, s.ledgerID).Scan(&ledgerVersionRaw, &ledgerVersionKind); err != nil {
		return nil, expensePairUnavailable()
	}
	ledgerVersion, ok := expensePairInteger(ledgerVersionRaw, ledgerVersionKind)
	if !ok || ledgerVersion < 0 {
		return nil, expensePairUnavailable()
	}
	var orphan bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM expense_pair_members m LEFT JOIN expense_pair_decisions d ON d.ledger_id=m.ledger_id AND d.id=m.decision_id WHERE m.ledger_id=? AND d.id IS NULL)
	OR EXISTS(SELECT 1 FROM expense_pair_revisions r LEFT JOIN expense_pair_decisions d ON d.ledger_id=r.ledger_id AND d.id=r.decision_id WHERE r.ledger_id=? AND d.id IS NULL)
	OR EXISTS(SELECT 1 FROM change_log c LEFT JOIN expense_pair_decisions d ON d.ledger_id=c.ledger_id AND d.id=c.entity_id WHERE c.ledger_id=? AND c.entity_type='expense_pair_decision' AND d.id IS NULL)`, s.ledgerID, s.ledgerID, s.ledgerID).Scan(&orphan); err != nil || orphan {
		return nil, expensePairUnavailable()
	}
	rows, err := tx.Query(`SELECT id,status,version,typeof(version),contract_version,typeof(contract_version),created_by,created_at,updated_at FROM expense_pair_decisions WHERE ledger_id=? ORDER BY id`, s.ledgerID)
	if err != nil {
		return nil, expensePairUnavailable()
	}
	roots := []expensePairStoredDecision{}
	for rows.Next() {
		var d expensePairStoredDecision
		var rawVersion, rawContract any
		var versionKind, contractKind string
		if err = rows.Scan(&d.ID, &d.Status, &rawVersion, &versionKind, &rawContract, &contractKind, &d.CreatorID, &d.CreatedAt, &d.UpdatedAt); err != nil {
			rows.Close()
			return nil, expensePairUnavailable()
		}
		version, vok := expensePairInteger(rawVersion, versionKind)
		contract, cok := expensePairInteger(rawContract, contractKind)
		if !associationAccountingID(d.ID) || !associationAccountingID(d.CreatorID) || !vok || !cok || contract != 1 || (d.Status != "active" && d.Status != "retracted") || (d.Status == "active" && version != 1) || (d.Status == "retracted" && version != 2) || !nativeEvidenceTimestamp(d.CreatedAt) || !nativeEvidenceTimestamp(d.UpdatedAt) {
			rows.Close()
			return nil, expensePairUnavailable()
		}
		d.Version = version
		roots = append(roots, d)
	}
	rowErr, closeErr := rows.Err(), rows.Close()
	if rowErr != nil || closeErr != nil {
		return nil, expensePairUnavailable()
	}
	out := map[string]expensePairStoredDecision{}
	activeMembers := map[string]bool{}
	for _, root := range roots {
		d, err := s.loadExpensePairDecision(tx, root, ledgerVersion)
		if err != nil {
			return nil, expensePairUnavailable()
		}
		if d.Status == "active" {
			for _, member := range d.Members {
				if activeMembers[member.TransactionID] {
					return nil, expensePairUnavailable()
				}
				activeMembers[member.TransactionID] = true
			}
		}
		out[d.ID] = d
	}
	return out, nil
}

func (s *Store) loadExpensePairDecision(tx *sql.Tx, d expensePairStoredDecision, ledgerVersion int64) (expensePairStoredDecision, error) {
	var creatorExists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM app_users WHERE id=?)`, d.CreatorID).Scan(&creatorExists); err != nil || !creatorExists {
		return d, expensePairUnavailable()
	}
	rows, err := tx.Query(`SELECT role,transaction_id,transaction_version,typeof(transaction_version),facts_json,evidence_json,active,typeof(active) FROM expense_pair_members WHERE ledger_id=? AND decision_id=? ORDER BY role`, s.ledgerID, d.ID)
	if err != nil {
		return d, expensePairUnavailable()
	}
	d.Members = []expensePairMemberSnapshot{}
	for rows.Next() {
		var member expensePairMemberSnapshot
		var rawVersion, rawActive any
		var versionKind, activeKind, factsJSON, evidenceJSON string
		if err = rows.Scan(&member.Role, &member.TransactionID, &rawVersion, &versionKind, &factsJSON, &evidenceJSON, &rawActive, &activeKind); err != nil {
			rows.Close()
			return d, expensePairUnavailable()
		}
		version, vok := expensePairInteger(rawVersion, versionKind)
		active, aok := expensePairInteger(rawActive, activeKind)
		if !vok || version < 1 || !aok || (active != 0 && active != 1) || !associationAccountingID(member.TransactionID) || !expensePairCanonical(factsJSON, &member.Facts) || !expensePairCanonical(evidenceJSON, &member.Evidence) {
			rows.Close()
			return d, expensePairUnavailable()
		}
		member.TransactionVersion, member.Active = version, int(active)
		d.Members = append(d.Members, member)
	}
	rowErr, closeErr := rows.Err(), rows.Close()
	if rowErr != nil || closeErr != nil || len(d.Members) != 2 || d.Members[0].Role != "primary" || d.Members[1].Role != "secondary" || d.Members[0].TransactionID == d.Members[1].TransactionID {
		return d, expensePairUnavailable()
	}
	active := 0
	if d.Status == "active" {
		active = 1
	}
	for _, member := range d.Members {
		if member.Active != active || member.Facts.Transaction.ID != member.TransactionID || member.Facts.Transaction.Version != member.TransactionVersion || !expensePairFactsValid(s.ledgerID, member.Facts) {
			return d, expensePairUnavailable()
		}
		if err = s.expensePairVerifyHistoricalEvidence(tx, member); err != nil {
			return d, expensePairUnavailable()
		}
		if d.Status == "active" {
			current, currentErr := s.expensePairReadMember(tx, member.TransactionID)
			if currentErr != nil || current != member.Facts {
				return d, expensePairUnavailable()
			}
			evidence, evidenceErr := s.expensePairReadEvidence(tx, current.Transaction)
			if evidenceErr != nil || !reflect.DeepEqual(evidence, member.Evidence) {
				return d, expensePairUnavailable()
			}
		}
	}
	if _, err = planAssociationExpensePairProjection(s.ledgerID, 0, expensePairInput(d.Members[0].Facts), expensePairInput(d.Members[1].Facts), d.Members[0].TransactionID); err != nil {
		return d, expensePairUnavailable()
	}
	rows, err = tx.Query(`SELECT revision_id,version,typeof(version),operation,actor_id,reason,snapshot_json,contract_version,typeof(contract_version),created_at FROM expense_pair_revisions WHERE ledger_id=? AND decision_id=? ORDER BY version`, s.ledgerID, d.ID)
	if err != nil {
		return d, expensePairUnavailable()
	}
	d.Revisions = []expensePairRevisionSnapshot{}
	for rows.Next() {
		var id, operation, actor, reason, snapshotJSON, createdAt, versionKind, contractKind string
		var rawVersion, rawContract any
		var snapshot expensePairRevisionSnapshot
		if err = rows.Scan(&id, &rawVersion, &versionKind, &operation, &actor, &reason, &snapshotJSON, &rawContract, &contractKind, &createdAt); err != nil {
			rows.Close()
			return d, expensePairUnavailable()
		}
		version, vok := expensePairInteger(rawVersion, versionKind)
		contract, cok := expensePairInteger(rawContract, contractKind)
		canonicalReason, validReason := expensePairReason(reason)
		index := int64(len(d.Revisions)) + 1
		wantOperation, status, memberActive := "confirm", "active", 1
		if index == 2 {
			wantOperation, status, memberActive = "withdraw", "retracted", 0
		}
		if !vok || !cok || contract != 1 || version != index || index > d.Version || !associationAccountingID(id) || !associationAccountingID(actor) || !validReason || canonicalReason != reason || operation != wantOperation || !nativeEvidenceTimestamp(createdAt) || !expensePairCanonical(snapshotJSON, &snapshot) {
			rows.Close()
			return d, expensePairUnavailable()
		}
		expectedReceipt := expensePairReceipt{ID: d.ID, Version: index, Status: status, LedgerVersion: snapshot.Receipt.LedgerVersion, PrimaryID: d.Members[0].TransactionID, SecondaryID: d.Members[1].TransactionID}
		if snapshot.ContractVersion != 1 || snapshot.RevisionID != id || snapshot.LedgerID != s.ledgerID || snapshot.Operation != operation || snapshot.ActorID != actor || snapshot.Reason != reason || snapshot.CreatedAt != createdAt || snapshot.CreatorID != d.CreatorID || snapshot.DecisionCreatedAt != d.CreatedAt || snapshot.Receipt != expectedReceipt || snapshot.Receipt.LedgerVersion < 1 || snapshot.Receipt.LedgerVersion > ledgerVersion || !expensePairMembersEqual(snapshot.Members, expensePairSnapshotMembers(d.Members, memberActive)) {
			rows.Close()
			return d, expensePairUnavailable()
		}
		if index == 1 && (actor != d.CreatorID || createdAt != d.CreatedAt) {
			rows.Close()
			return d, expensePairUnavailable()
		}
		if index == 2 && snapshot.Receipt.LedgerVersion <= d.Revisions[0].Receipt.LedgerVersion {
			rows.Close()
			return d, expensePairUnavailable()
		}
		d.Revisions = append(d.Revisions, snapshot)
	}
	rowErr, closeErr = rows.Err(), rows.Close()
	if rowErr != nil || closeErr != nil || int64(len(d.Revisions)) != d.Version || d.UpdatedAt != d.Revisions[len(d.Revisions)-1].CreatedAt {
		return d, expensePairUnavailable()
	}
	for _, revision := range d.Revisions {
		var actorExists bool
		if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM app_users WHERE id=?)`, revision.ActorID).Scan(&actorExists); err != nil || !actorExists {
			return d, expensePairUnavailable()
		}
	}
	if err = s.expensePairVerifyAudit(tx, d); err != nil {
		return d, err
	}
	return d, nil
}

func (s *Store) expensePairVerifyAudit(tx *sql.Tx, d expensePairStoredDecision) error {
	rows, err := tx.Query(`SELECT base_version,typeof(base_version),entity_version,typeof(entity_version),operation,payload_version,typeof(payload_version),payload_json,created_at,COALESCE(actor_id,'') FROM change_log WHERE ledger_id=? AND entity_type='expense_pair_decision' AND entity_id=? ORDER BY entity_version,sequence`, s.ledgerID, d.ID)
	if err != nil {
		return expensePairUnavailable()
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		if index >= len(d.Revisions) {
			return expensePairUnavailable()
		}
		var baseRaw, versionRaw, payloadRaw any
		var baseKind, versionKind, payloadKind, operation, raw, createdAt, actor string
		if err = rows.Scan(&baseRaw, &baseKind, &versionRaw, &versionKind, &operation, &payloadRaw, &payloadKind, &raw, &createdAt, &actor); err != nil {
			return expensePairUnavailable()
		}
		base, bok := expensePairInteger(baseRaw, baseKind)
		version, vok := expensePairInteger(versionRaw, versionKind)
		payloadVersion, pok := expensePairInteger(payloadRaw, payloadKind)
		r := d.Revisions[index]
		if !bok || !vok || !pok || base != int64(index) || version != int64(index+1) || payloadVersion != 1 || operation != "expense_pair_"+r.Operation || raw != encode(r) || createdAt != r.CreatedAt || actor != r.ActorID {
			return expensePairUnavailable()
		}
		index++
	}
	if rows.Err() != nil || index != len(d.Revisions) {
		return expensePairUnavailable()
	}
	return nil
}
