// Package redaction projects validated local evidence into a minimal cloud
// payload. It never authorizes posting, resolves identities, or calls a model.
package redaction

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

const Version = "monee-redaction-v1"
const maxRecords = 1000

// Input contains local, private values. Callers must normalize financial fields
// using ledger rules before projection; this module does not reinterpret money.
// RecordID is a stable source-occurrence ID, not a filename or row number alone.
type Input struct {
	RecordID    string
	Date        string // YYYY-MM-DD; precise times are intentionally excluded.
	AmountMinor string // Positive integer, never a floating-point amount.
	Currency    string
	Direction   string // income, expense, or unknown (not an accounting decision).
	Source      string // Canonical enum below, never an arbitrary imported label.
	Account     string
	Merchant    string
	Note        string
	Identifiers []ingestion.Identifier
}

// Projector owns a copy of a caller-provided secret. Production key generation,
// secure persistence, rotation and authorization belong at the service boundary.
type Projector struct{ key []byte }

func New(key []byte) (*Projector, error) {
	if len(key) < 32 {
		return nil, errors.New("redaction key must contain at least 32 bytes")
	}
	return &Projector{key: append([]byte(nil), key...)}, nil
}

// Preview retains exactly the JSON bytes intended for a future provider call.
// Bytes returns a defensive copy; callers must display and send these same bytes
// rather than reconstructing a request from private Input fields.
type Preview struct {
	payload    []byte
	treatments []Treatment
}

func (p Preview) Bytes() []byte           { return append([]byte(nil), p.payload...) }
func (p Preview) Treatments() []Treatment { return append([]Treatment(nil), p.treatments...) }

// Treatment is local inspection metadata, with no original text or identifiers.
type Treatment struct {
	RecordRef       string `json:"recordRef"`
	Merchant        string `json:"merchant"`
	Note            string `json:"note"`
	Account         string `json:"account"`
	IdentifierCount int    `json:"identifierCount"`
}

type payload struct {
	Version string   `json:"version"`
	Records []record `json:"records"`
}

type record struct {
	Ref         string       `json:"ref"`
	Date        string       `json:"date"`
	AmountMinor string       `json:"amountMinor"`
	Currency    string       `json:"currency"`
	Direction   string       `json:"direction"`
	Source      string       `json:"source"`
	AccountRef  string       `json:"accountRef,omitempty"`
	MerchantRef string       `json:"merchantRef,omitempty"`
	Identifiers []identifier `json:"identifiers"`
}

type identifier struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

var minorPattern = regexp.MustCompile(`^[1-9][0-9]{0,12}$`)
var sources = map[string]bool{
	"alipay": true, "wechat": true, "jd": true, "cmb": true,
	"abc": true, "bank_of_shanghai": true, "icbc": true, "other": true,
}
var identifierKinds = map[string]bool{
	"alipay_payment_trace": true, "wechat_payment_trace": true,
	"merchant_order": true, "source_transaction": true,
}

// Project fails the whole request on invalid normalized fields or unknown
// namespaces. No partially redacted payload is returned on an error. IDs retain
// equality only for an identical kind, authority scope and value in one ledger.
// Matching pseudonyms are evidence references, never a deduplication verdict.
func (p *Projector) Project(ledgerID string, inputs []Input) (Preview, error) {
	if p == nil || len(p.key) < 32 {
		return Preview{}, errors.New("redaction projector is not initialized")
	}
	if ledgerID == "" || len(ledgerID) > 256 {
		return Preview{}, errors.New("invalid ledger scope")
	}
	if len(inputs) == 0 || len(inputs) > maxRecords {
		return Preview{}, errors.New("invalid redaction batch size")
	}
	result := payload{Version: Version, Records: make([]record, 0, len(inputs))}
	treatments := make([]Treatment, 0, len(inputs))
	seen := make(map[string]bool)
	for i, input := range inputs {
		fail := func(field string) (Preview, error) { return Preview{}, fmt.Errorf("record %d: invalid %s", i+1, field) }
		if input.RecordID == "" || len(input.RecordID) > 1024 || seen[input.RecordID] {
			return fail("record reference")
		}
		seen[input.RecordID] = true
		date, err := time.Parse("2006-01-02", input.Date)
		if err != nil || date.Format("2006-01-02") != input.Date {
			return fail("date")
		}
		if !minorPattern.MatchString(input.AmountMinor) {
			return fail("amount")
		}
		if _, err := strconv.ParseInt(input.AmountMinor, 10, 64); err != nil {
			return fail("amount")
		}
		if input.Currency != "CNY" {
			return fail("currency")
		}
		if input.Direction != "income" && input.Direction != "expense" && input.Direction != "unknown" {
			return fail("direction")
		}
		if !sources[input.Source] {
			return fail("source")
		}
		if len(input.Account) > 8192 || len(input.Merchant) > 8192 || len(input.Note) > 65536 {
			return fail("private text size")
		}
		if len(input.Identifiers) > 16 {
			return fail("identifier count")
		}
		row := record{Ref: p.alias(ledgerID, "record", input.RecordID), Date: input.Date, AmountMinor: input.AmountMinor, Currency: input.Currency, Direction: input.Direction, Source: input.Source, Identifiers: []identifier{}}
		treatment := Treatment{RecordRef: row.Ref, Merchant: "absent", Note: "absent", Account: "absent"}
		if input.Merchant != "" {
			row.MerchantRef = p.alias(ledgerID, "merchant", input.Merchant)
			treatment.Merchant = "pseudonymized"
		}
		if input.Account != "" {
			row.AccountRef = p.alias(ledgerID, "account", input.Source, input.Account)
			treatment.Account = "pseudonymized"
		}
		// Free text is separately handled: omission is deliberate. Regex replacement
		// cannot reliably remove personal names, addresses or prompt instructions.
		if input.Note != "" {
			treatment.Note = "omitted_unstructured_text"
		}
		seenIDs := make(map[string]bool)
		for _, id := range input.Identifiers {
			if !identifierKinds[id.Kind] || id.Scope == "" || id.Value == "" || len(id.Scope) > 8192 || len(id.Value) > 8192 {
				return fail("identifier")
			}
			if (id.Kind == "alipay_payment_trace" && input.Source != "alipay") || (id.Kind == "wechat_payment_trace" && input.Source != "wechat") {
				return fail("identifier source")
			}
			ref := p.alias(ledgerID, "identifier", id.Kind, id.Scope, id.Value)
			if !seenIDs[ref] {
				row.Identifiers = append(row.Identifiers, identifier{Kind: id.Kind, Ref: ref})
				seenIDs[ref] = true
			}
		}
		treatment.IdentifierCount = len(row.Identifiers)
		result.Records = append(result.Records, row)
		treatments = append(treatments, treatment)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return Preview{}, errors.New("cannot serialize redacted payload")
	}
	return Preview{payload: encoded, treatments: treatments}, nil
}

func (p *Projector) alias(ledgerID, domain string, parts ...string) string {
	// JSON array framing prevents ambiguous concatenations and cross-domain reuse.
	framed, _ := json.Marshal(append([]string{Version, ledgerID, domain}, parts...))
	mac := hmac.New(sha256.New, p.key)
	_, _ = mac.Write(framed)
	return "r1_" + hex.EncodeToString(mac.Sum(nil))
}
