package ledger

import (
	"database/sql"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DashboardFilters only narrows detail rows, never the financial month summary.
type DashboardFilters struct{ Type, AccountID, Category string }

var dashboardAccountIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validateDashboardFilters(f DashboardFilters, query string) error {
	switch f.Type {
	case "", "income", "expense", "refund", "transfer":
	default:
		return problem("invalid", "交易类型筛选无效")
	}
	for _, value := range []string{f.Type, f.AccountID, query} {
		if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
			return problem("invalid", "筛选条件不能包含控制字符")
		}
	}
	if f.AccountID != "" && !dashboardAccountIDPattern.MatchString(f.AccountID) {
		return problem("invalid", "账户筛选须使用有效账户 ID")
	}
	// Exact category selectors must remain usable for legacy labels accepted by
	// normalize (for example internal tabs). SQL binds the literal value.
	if !utf8.ValidString(f.Category) || strings.ContainsAny(f.Category, "\x00\r\n") {
		return problem("invalid", "分类含有不支持的换行或控制字符")
	}
	if utf8.RuneCountInString(f.Category) > 200 {
		return problem("invalid", "分类筛选过长")
	}
	return nil
}

// Read actual postings rather than the transaction's historical account label.
// DISTINCT/sets also permit either clearing side of future transfer postings.
func (s *Store) dashboardAccountOptions(tx *sql.Tx, month, accountID string) (map[string]bool, []DashboardAccountOption, error) {
	rows, err := tx.Query(`SELECT DISTINCT t.id,a.id,a.name FROM transactions t
 JOIN postings p ON p.transaction_id=t.id JOIN accounts a ON a.id=p.account_id
 WHERE t.ledger_id=? AND a.ledger_id=t.ledger_id AND substr(t.occurred_on,1,7)=?
 AND t.deleted_at IS NULL AND a.deleted_at IS NULL AND a.kind='clearing'
 ORDER BY a.name,a.id,t.id`, s.ledgerID, month)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	matches := map[string]bool{}
	options := []DashboardAccountOption{}
	seen := map[string]bool{}
	for rows.Next() {
		var transactionID string
		var option DashboardAccountOption
		if err = rows.Scan(&transactionID, &option.ID, &option.Name); err != nil {
			return nil, nil, err
		}
		if !seen[option.ID] {
			seen[option.ID] = true
			options = append(options, option)
		}
		if strings.EqualFold(option.ID, accountID) {
			matches[transactionID] = true
		}
	}
	return matches, options, rows.Err()
}
