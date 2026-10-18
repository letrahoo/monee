package ledger

// Private metadata capability only. No normal route or financial reader calls
// this lifecycle, and the schema-five Open/startup fence remains in force.
type expensePairConfirmInput struct {
	LedgerVersion          int64  `json:"ledgerVersion"`
	PrimaryTransactionID   string `json:"primaryTransactionId"`
	SecondaryTransactionID string `json:"secondaryTransactionId"`
	PrimaryVersion         int64  `json:"primaryVersion"`
	SecondaryVersion       int64  `json:"secondaryVersion"`
	Reason                 string `json:"reason"`
}

type expensePairWithdrawInput struct {
	LedgerVersion   int64  `json:"ledgerVersion"`
	DecisionID      string `json:"decisionId"`
	DecisionVersion int64  `json:"decisionVersion"`
	Reason          string `json:"reason"`
}

type expensePairReceipt struct {
	ID            string `json:"id"`
	Version       int64  `json:"version"`
	Status        string `json:"status"`
	LedgerVersion int64  `json:"ledgerVersion"`
	PrimaryID     string `json:"primaryId"`
	SecondaryID   string `json:"secondaryId"`
}

type expensePairAccountingDocument struct {
	LedgerID         string `json:"ledgerId"`
	TransactionID    string `json:"transactionId"`
	Version          int64  `json:"version"`
	AmountMinor      int64  `json:"amountMinor"`
	FundPostingID    string `json:"fundPostingId"`
	FundAccountID    string `json:"fundAccountId"`
	ExpensePostingID string `json:"expensePostingId"`
	ExpenseAccountID string `json:"expenseAccountId"`
}

type expensePairFactsDocument struct {
	ContractVersion int                           `json:"contractVersion"`
	Transaction     Transaction                   `json:"transaction"`
	Accounting      expensePairAccountingDocument `json:"accounting"`
}

type expensePairNativeAnchor struct {
	SourceRecordID        string      `json:"sourceRecordId"`
	ImportID              string      `json:"importId"`
	Line                  int         `json:"line"`
	Ordinal               int         `json:"ordinal"`
	ImportContentHash     string      `json:"importContentHash"`
	EnvelopeParserVersion int         `json:"envelopeParserVersion"`
	DocumentContentHash   string      `json:"documentContentHash"`
	DocumentParser        string      `json:"documentParser"`
	DocumentParserVersion int         `json:"documentParserVersion"`
	PreviewHash           string      `json:"previewHash"`
	NormalizedHash        string      `json:"normalizedHash"`
	CommittedAt           string      `json:"committedAt"`
	Original              Transaction `json:"original"`
}

type expensePairSourceOccurrence struct {
	SourceRecordID        string `json:"sourceRecordId"`
	ImportID              string `json:"importId"`
	Line                  int    `json:"line"`
	Disposition           string `json:"disposition"`
	NormalizedHash        string `json:"normalizedHash"`
	ImportContentHash     string `json:"importContentHash"`
	EnvelopeParserVersion int    `json:"envelopeParserVersion"`
	CommittedAt           string `json:"committedAt"`
}

type expensePairEvidenceDocument struct {
	ContractVersion int                           `json:"contractVersion"`
	Anchor          expensePairNativeAnchor       `json:"anchor"`
	Occurrences     []expensePairSourceOccurrence `json:"occurrences"`
}

type expensePairMemberSnapshot struct {
	Role               string                      `json:"role"`
	TransactionID      string                      `json:"transactionId"`
	TransactionVersion int64                       `json:"transactionVersion"`
	Active             int                         `json:"active"`
	Facts              expensePairFactsDocument    `json:"facts"`
	Evidence           expensePairEvidenceDocument `json:"evidence"`
}

type expensePairRevisionSnapshot struct {
	ContractVersion   int                         `json:"contractVersion"`
	RevisionID        string                      `json:"revisionId"`
	LedgerID          string                      `json:"ledgerId"`
	Operation         string                      `json:"operation"`
	ActorID           string                      `json:"actorId"`
	Reason            string                      `json:"reason"`
	CreatedAt         string                      `json:"createdAt"`
	CreatorID         string                      `json:"creatorId"`
	DecisionCreatedAt string                      `json:"decisionCreatedAt"`
	Receipt           expensePairReceipt          `json:"receipt"`
	Members           []expensePairMemberSnapshot `json:"members"`
}

type expensePairStoredDecision struct {
	ID        string
	Version   int64
	Status    string
	CreatorID string
	CreatedAt string
	UpdatedAt string
	Members   []expensePairMemberSnapshot
	Revisions []expensePairRevisionSnapshot
}

func expensePairUnavailable() error {
	return problem("expense_pair_unavailable", "关联决策资料不完整或已变化，操作未执行")
}

func expensePairUnsupported() error {
	return problem("unsupported", "当前私有能力仅支持原始来源完整且未更正、无退款的同账户普通支出")
}

func expensePairConflict() error {
	return problem("conflict", "账本、交易或关联决策已变化，请重新核实")
}

func expensePairInvalid() error {
	return problem("invalid", "关联决策请求参数无效")
}
