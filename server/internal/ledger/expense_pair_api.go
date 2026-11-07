package ledger

import (
	"database/sql"
	"sort"
	"strconv"
)

// These public commands require the offline-upgraded capability in the same
// authorized transaction. Normal schema5 startup still cannot enable them.
type ExpensePairConfirmInput = expensePairConfirmInput
type ExpensePairWithdrawInput = expensePairWithdrawInput
type ExpensePairReceipt = expensePairReceipt

type ExpensePairPreviewInput struct {
	LedgerVersion          int64  `json:"ledgerVersion"`
	PrimaryTransactionID   string `json:"primaryTransactionId"`
	SecondaryTransactionID string `json:"secondaryTransactionId"`
	PrimaryVersion         int64  `json:"primaryVersion"`
	SecondaryVersion       int64  `json:"secondaryVersion"`
}

type ExpensePairPreview struct {
	LedgerVersion        int64  `json:"ledgerVersion"`
	PrimaryID            string `json:"primaryId"`
	SecondaryID          string `json:"secondaryId"`
	PrimaryVersion       int64  `json:"primaryVersion"`
	SecondaryVersion     int64  `json:"secondaryVersion"`
	Currency             string `json:"currency"`
	PairGrossBeforeMinor string `json:"pairGrossBeforeMinor"`
	PairGrossAfterMinor  string `json:"pairGrossAfterMinor"`
	PairGrossDeltaMinor  string `json:"pairGrossDeltaMinor"`
}

type ExpensePairDetail struct {
	LedgerVersion int64                `json:"ledgerVersion"`
	Decisions     []ExpensePairReceipt `json:"decisions"`
}

func expensePairRequireCapability(tx *sql.Tx) error {
	var version int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return expensePairUnavailable()
	}
	if version != 6 {
		return problem("expense_pair_not_enabled", "当前账本尚未启用消费关联，请勿重复提交")
	}
	return nil
}

func (s *Store) ConfirmExpensePair(in ExpensePairConfirmInput, key string) (ExpensePairReceipt, error) {
	return s.confirmExpensePairCommand(in, key, true)
}
func (s *Store) WithdrawExpensePair(in ExpensePairWithdrawInput, key string) (ExpensePairReceipt, error) {
	return s.withdrawExpensePairCommand(in, key, true)
}

// Preview is a fresh, authorized read, not a reservation. Confirm rechecks all
// versions and evidence; the client must explicitly choose the primary again.
func (s *Store) PreviewExpensePair(in ExpensePairPreviewInput) (ExpensePairPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out ExpensePairPreview
	tx, err := s.db.Begin()
	if err != nil {
		return out, expensePairUnavailable()
	}
	defer tx.Rollback()
	if err = s.expensePairAuthorize(tx); err != nil {
		return out, err
	}
	if err = expensePairRequireCapability(tx); err != nil {
		return out, err
	}
	if in.LedgerVersion < 0 || in.PrimaryVersion < 1 || in.SecondaryVersion < 1 || !associationAccountingID(in.PrimaryTransactionID) || !associationAccountingID(in.SecondaryTransactionID) || in.PrimaryTransactionID == in.SecondaryTransactionID {
		return out, expensePairInvalid()
	}
	decisions, err := s.loadExpensePairLedger(tx)
	if err != nil {
		return out, err
	}
	version, err := s.expensePairLedgerVersion(tx)
	if err != nil {
		return out, err
	}
	if version != in.LedgerVersion || expensePairHasActiveMember(decisions, in.PrimaryTransactionID) || expensePairHasActiveMember(decisions, in.SecondaryTransactionID) {
		return out, expensePairConflict()
	}
	primary, err := s.expensePairReadMember(tx, in.PrimaryTransactionID)
	if err != nil {
		return out, err
	}
	secondary, err := s.expensePairReadMember(tx, in.SecondaryTransactionID)
	if err != nil {
		return out, err
	}
	if primary.Transaction.Version != in.PrimaryVersion || secondary.Transaction.Version != in.SecondaryVersion {
		return out, expensePairConflict()
	}
	if !expensePairFactsValid(s.ledgerID, primary) || !expensePairFactsValid(s.ledgerID, secondary) {
		return out, expensePairUnavailable()
	}
	projection, err := planAssociationExpensePairProjection(s.ledgerID, version, expensePairInput(primary), expensePairInput(secondary), in.PrimaryTransactionID)
	if err != nil {
		return out, expensePairUnsupported()
	}
	if _, err = s.expensePairReadEvidence(tx, primary.Transaction); err != nil {
		return out, err
	}
	if _, err = s.expensePairReadEvidence(tx, secondary.Transaction); err != nil {
		return out, err
	}
	return ExpensePairPreview{LedgerVersion: version, PrimaryID: in.PrimaryTransactionID, SecondaryID: in.SecondaryTransactionID, PrimaryVersion: in.PrimaryVersion, SecondaryVersion: in.SecondaryVersion, Currency: "CNY", PairGrossBeforeMinor: strconv.FormatInt(projection.PairGrossBeforeMinor, 10), PairGrossAfterMinor: strconv.FormatInt(projection.PairGrossAfterMinor, 10), PairGrossDeltaMinor: strconv.FormatInt(projection.PairGrossDeltaMinor, 10)}, nil
}

// Detail is available to viewers and returns receipts, never frozen raw source
// documents or private evidence envelopes. History makes withdrawal discoverable.
func (s *Store) ExpensePairs(transactionID string) (ExpensePairDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty ExpensePairDetail
	tx, err := s.db.Begin()
	if err != nil {
		return empty, expensePairUnavailable()
	}
	defer tx.Rollback()
	if err = s.authorize(tx, false); err != nil {
		return empty, err
	}
	if err = expensePairRequireCapability(tx); err != nil {
		return empty, err
	}
	if !associationAccountingID(transactionID) {
		return empty, expensePairInvalid()
	}
	var exists bool
	if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM transactions WHERE ledger_id=? AND id=?)", s.ledgerID, transactionID).Scan(&exists); err != nil {
		return empty, expensePairUnavailable()
	}
	if !exists {
		return empty, problem("not_found", "交易不存在")
	}
	decisions, err := s.loadExpensePairLedger(tx)
	if err != nil {
		return empty, err
	}
	version, err := s.expensePairLedgerVersion(tx)
	if err != nil {
		return empty, err
	}
	out := ExpensePairDetail{LedgerVersion: version, Decisions: []ExpensePairReceipt{}}
	for _, d := range decisions {
		for _, m := range d.Members {
			if m.TransactionID == transactionID {
				out.Decisions = append(out.Decisions, d.Revisions[len(d.Revisions)-1].Receipt)
				break
			}
		}
	}
	sort.Slice(out.Decisions, func(i, j int) bool { return out.Decisions[i].ID < out.Decisions[j].ID })
	return out, nil
}
