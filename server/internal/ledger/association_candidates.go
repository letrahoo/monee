package ledger

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// AssociationTransaction is an explicitly narrow current-ledger projection.
// SourceDisplay is only a label, not a verified provider or funding-account role.
type AssociationTransaction struct {
	ID            string `json:"id"`
	Version       int64  `json:"version"`
	Date          string `json:"date"`
	Type          string `json:"type"`
	AmountMinor   string `json:"amountMinor"`
	Currency      string `json:"currency"`
	Merchant      string `json:"merchant"`
	SourceDisplay string `json:"sourceDisplay"`
}
type AssociationCandidate struct {
	Transaction AssociationTransaction `json:"transaction"`
	Reasons     []string               `json:"reasons"`
}

// AssociationCandidates supplies investigation hints, never a same-payment
// assertion, write permission, or a reduction in the ledger's financial totals.
type AssociationCandidates struct {
	ContractVersion     int                    `json:"contractVersion"`
	ReadOnly            bool                   `json:"readOnly"`
	Basis               string                 `json:"basis"`
	SourceRolesVerified bool                   `json:"sourceRolesVerified"`
	Actions             []string               `json:"actions"`
	LedgerVersion       int64                  `json:"ledgerVersion"`
	Base                AssociationTransaction `json:"base"`
	Items               []AssociationCandidate `json:"items"`
	Page                int                    `json:"page"`
	PageSize            int                    `json:"pageSize"`
	TotalCount          int                    `json:"totalCount"`
}

// These flags describe known persisted relations, not an audit-completeness
// guarantee. Even a soft-deleted refund makes this first slice conservative.
type associationProjection struct {
	transaction   AssociationTransaction
	refundOf      string
	hasRefund     bool
	corrected     bool
	amountInteger bool
}

// Every correlated lookup is ledger-scoped, including historical correction
// markers. Their presence suffices; no payload, source snapshot or trace is read.
const associationProjectionColumns = `t.id,t.version,t.occurred_on,t.kind,t.amount_minor,typeof(t.amount_minor),t.currency,t.merchant,t.source,COALESCE(t.refund_of,''),
 EXISTS(SELECT 1 FROM transactions r WHERE r.ledger_id=t.ledger_id AND r.refund_of=t.id AND r.kind='refund'),
 EXISTS(SELECT 1 FROM change_log c WHERE c.ledger_id=t.ledger_id AND c.entity_type='transaction' AND c.entity_id=t.id AND c.operation='correct')`

func scanAssociationProjection(row interface{ Scan(...any) error }) (associationProjection, error) {
	var out associationProjection
	var rawAmount any
	var amountType string
	t := &out.transaction
	err := row.Scan(&t.ID, &t.Version, &t.Date, &t.Type, &rawAmount, &amountType, &t.Currency, &t.Merchant, &t.SourceDisplay, &out.refundOf, &out.hasRefund, &out.corrected)
	if amount, ok := rawAmount.(int64); ok && amountType == "integer" {
		out.amountInteger = true
		t.AmountMinor = strconv.FormatInt(amount, 10)
	}
	return out, err
}

// TransactionAssociationCandidates uses one read-only transaction for membership,
// the ledger version, base, relation exclusions, count and ordered page contents.
func (s *Store) TransactionAssociationCandidates(id string, page int) (AssociationCandidates, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := AssociationCandidates{ContractVersion: 1, ReadOnly: true, Basis: "current_transaction_similarity", SourceRolesVerified: false, Actions: []string{}, Items: []AssociationCandidate{}, Page: page, PageSize: 50}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = s.authorize(tx, false); err != nil {
		return out, err
	}
	if page < 1 || page > 1_000_000 {
		return out, problem("invalid", "分页参数无效")
	}
	if err = tx.QueryRow(`SELECT version FROM ledgers WHERE id=?`, s.ledgerID).Scan(&out.LedgerVersion); err != nil {
		return out, err
	}
	base, err := scanAssociationProjection(tx.QueryRow(`SELECT `+associationProjectionColumns+` FROM transactions t WHERE t.ledger_id=? AND t.id=? AND t.deleted_at IS NULL`, s.ledgerID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return out, problem("not_found", "找不到该交易")
	}
	if err != nil {
		return out, err
	}
	if !associationEligible(base) {
		return out, problem("invalid", "当前仅支持未更正且无退款记录的普通人民币支出相似排查")
	}
	out.Base = base.transaction
	amount, _ := strconv.ParseInt(out.Base.AmountMinor, 10, 64)
	rows, err := tx.Query(`SELECT `+associationProjectionColumns+` FROM transactions t
 WHERE t.ledger_id=? AND t.id<>? AND t.deleted_at IS NULL
 AND t.occurred_on=? AND t.kind=? AND t.amount_minor=? AND t.currency=? AND t.merchant=? AND t.source<>?
 ORDER BY t.id ASC`, s.ledgerID, id, out.Base.Date, out.Base.Type, amount, out.Base.Currency, out.Base.Merchant, out.Base.SourceDisplay)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	// Streaming keeps only the requested fifty items. The exact same eligibility
	// predicate drives count and page, including Unicode/canonical label checks.
	offset := (page - 1) * out.PageSize
	for rows.Next() {
		candidate, err := scanAssociationProjection(rows)
		if err != nil {
			return out, err
		}
		if !associationMatches(base, candidate) {
			continue
		}
		if out.TotalCount >= offset && len(out.Items) < out.PageSize {
			out.Items = append(out.Items, AssociationCandidate{Transaction: candidate.transaction, Reasons: []string{"current_projection_match", "different_source_display"}})
		}
		out.TotalCount++
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	return out, nil
}

func associationEligible(p associationProjection) bool {
	t := p.transaction
	if !p.amountInteger || t.ID == "" || t.Version < 1 || t.Type != "expense" || t.Currency != "CNY" || p.refundOf != "" || p.hasRefund || p.corrected {
		return false
	}
	amount, err := strconv.ParseInt(t.AmountMinor, 10, 64)
	if err != nil || amount >= 0 || amount < -1_000_000_000_000 || strconv.FormatInt(amount, 10) != t.AmountMinor {
		return false
	}
	date, err := time.Parse("2006-01-02", t.Date)
	if err != nil || date.Year() < 1900 || date.Year() > 2200 || date.Format("2006-01-02") != t.Date {
		return false
	}
	return associationDisplayValid(t.Merchant) && associationDisplayValid(t.SourceDisplay)
}
func associationDisplayValid(value string) bool {
	if !utf8.ValidString(value) || value == "" || strings.TrimSpace(value) != value || len([]rune(value)) > 200 {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}
func associationMatches(base, candidate associationProjection) bool {
	if !associationEligible(base) || !associationEligible(candidate) {
		return false
	}
	a, b := base.transaction, candidate.transaction
	return a.ID != b.ID && a.Date == b.Date && a.Type == b.Type && a.AmountMinor == b.AmountMinor && a.Currency == b.Currency && a.Merchant == b.Merchant && a.SourceDisplay != b.SourceDisplay
}
