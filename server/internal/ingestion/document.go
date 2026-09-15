// Package ingestion extracts source evidence without granting authority to write
// a ledger. Only the ledger layer validates and commits financial transactions.
package ingestion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const MaxBytes = 2 << 20
const MaxRows = 1000

// Fields preserves the source's decimal and date strings. Parsers must not round
// amounts or guess the financial meaning of a transfer, refund or repayment.
type Fields struct {
	Date       string `json:"date"`
	Type       string `json:"type"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	Merchant   string `json:"merchant"`
	Category   string `json:"category"`
	Source     string `json:"source"`
	Account    string `json:"account"`
	ExternalID string `json:"externalId"`
	Note       string `json:"note"`
}

// Identifier namespaces must be explicit: merchant order and payment trace
// numbers are not interchangeable. Scope identifies the issuing source/account.
type Identifier struct {
	Kind  string `json:"kind"`
	Scope string `json:"scope"`
	Value string `json:"value"`
}

// MatchKey is internal, not a privacy transformation. Never send it or source
// identifiers to a model as a substitute for the separate redaction boundary.
func (id Identifier) MatchKey(ledgerID string) (string, error) {
	if ledgerID == "" || id.Kind == "" || id.Scope == "" || id.Value == "" {
		return "", errors.New("incomplete source identifier")
	}
	data, err := json.Marshal([]string{ledgerID, id.Kind, id.Scope, id.Value})
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

type Record struct {
	Ordinal     int          `json:"ordinal"`
	Line        int          `json:"line"`
	Fields      Fields       `json:"fields"`
	Identifiers []Identifier `json:"identifiers"`
	// Raw preserves native column names and values, including status, payment
	// method and unknown columns. It is private evidence, never model input.
	Raw map[string]string `json:"raw,omitempty"`
}

type Issue struct {
	Line    int    `json:"line"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Document hashes the original bytes, including BOM and line endings. A row's
// ordinal identifies an occurrence in this document, never an economic event.
// Records are extracted evidence, not an assertion of validity or completeness.
// Any Issues block the existing standard-CSV commit path.
type Document struct {
	ContentHash   string   `json:"contentHash"`
	Parser        string   `json:"parser"`
	ParserVersion int      `json:"parserVersion"`
	Records       []Record `json:"records"`
	Issues        []Issue  `json:"issues"`
	Encoding      string   `json:"encoding,omitempty"`
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
