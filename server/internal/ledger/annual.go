package ledger

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

// AnnualReport describes recorded activity, not completeness of source coverage.
type AnnualReport struct {
	LedgerID          string             `json:"ledgerId"`
	Version           int64              `json:"version"`
	Year              int                `json:"year"`
	Today             string             `json:"today"`
	Years             []int              `json:"years"`
	IncomeMinor       string             `json:"incomeMinor"`
	GrossExpenseMinor string             `json:"grossExpenseMinor"`
	RefundMinor       string             `json:"refundMinor"`
	ExpenseMinor      string             `json:"expenseMinor"`
	SurplusMinor      string             `json:"surplusMinor"`
	TotalCount        int                `json:"totalCount"`
	Months            []YearMonthSummary `json:"months"`
	Categories        []CategoryTotal    `json:"categories"`
}

type YearMonthSummary struct {
	Month             string          `json:"month"`
	HasRecords        bool            `json:"hasRecords"`
	TotalCount        int             `json:"totalCount"`
	IncomeMinor       string          `json:"incomeMinor"`
	GrossExpenseMinor string          `json:"grossExpenseMinor"`
	RefundMinor       string          `json:"refundMinor"`
	ExpenseMinor      string          `json:"expenseMinor"`
	SurplusMinor      string          `json:"surplusMinor"`
	Categories        []CategoryTotal `json:"categories"`
}

type annualTotals struct {
	income, gross, refund int64
	count                 int
	categories            map[string]*annualTotals
}

func annualOverflow() error { return problem("overflow", "统计金额超出可表示范围") }
func annualAdd(dst *int64, n int64) error {
	if n < 0 || *dst > math.MaxInt64-n {
		return annualOverflow()
	}
	*dst += n
	return nil
}
func annualSubtract(a, b int64) (int64, error) {
	if (b > 0 && a < math.MinInt64+b) || (b < 0 && a > math.MaxInt64+b) {
		return 0, annualOverflow()
	}
	return a - b, nil
}
func (a *annualTotals) add(kind, category string, minor int64) error {
	a.count++
	var err error
	switch kind {
	case "income":
		if minor <= 0 {
			return problem("invalid", "收入记录金额无效")
		}
		return annualAdd(&a.income, minor)
	case "expense":
		if minor == math.MinInt64 {
			return annualOverflow()
		}
		if minor >= 0 {
			return problem("invalid", "消费记录金额无效")
		}
		err = annualAdd(&a.gross, -minor)
	case "refund":
		if minor <= 0 {
			return problem("invalid", "退款记录金额无效")
		}
		err = annualAdd(&a.refund, minor)
	default:
		// Future transfers and other non-income/expense records count as activity only.
		return nil
	}
	if err != nil {
		return err
	}
	if a.categories == nil {
		a.categories = map[string]*annualTotals{}
	}
	c := a.categories[category]
	if c == nil {
		c = &annualTotals{}
		a.categories[category] = c
	}
	if kind == "expense" {
		return annualAdd(&c.gross, -minor)
	}
	return annualAdd(&c.refund, minor)
}
func (a *annualTotals) summary(month string) (YearMonthSummary, error) {
	out := YearMonthSummary{Month: month, HasRecords: a.count > 0, TotalCount: a.count, IncomeMinor: strconv.FormatInt(a.income, 10), GrossExpenseMinor: strconv.FormatInt(a.gross, 10), RefundMinor: strconv.FormatInt(a.refund, 10), Categories: []CategoryTotal{}}
	net, err := annualSubtract(a.gross, a.refund)
	if err != nil {
		return out, err
	}
	surplus, err := annualSubtract(a.income, net)
	if err != nil {
		return out, err
	}
	out.ExpenseMinor = strconv.FormatInt(net, 10)
	out.SurplusMinor = strconv.FormatInt(surplus, 10)
	for name, c := range a.categories {
		net, err := annualSubtract(c.gross, c.refund)
		if err != nil {
			return out, err
		}
		out.Categories = append(out.Categories, CategoryTotal{Name: name, AmountMinor: strconv.FormatInt(net, 10), GrossExpenseMinor: strconv.FormatInt(c.gross, 10), RefundMinor: strconv.FormatInt(c.refund, 10)})
	}
	sort.Slice(out.Categories, func(i, j int) bool {
		x, y := out.Categories[i], out.Categories[j]
		a, _ := strconv.ParseInt(x.GrossExpenseMinor, 10, 64)
		b, _ := strconv.ParseInt(y.GrossExpenseMinor, 10, 64)
		if a == b {
			return x.Name < y.Name
		}
		return a > b
	})
	return out, nil
}

// Annual reads authorization, ledger version and activity in one SQLite snapshot.
// Refunds reduce expenses in their own occurrence month, never income.
func (s *Store) Annual(year string) (AnnualReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
	if year == "" {
		year = today[:4]
	}
	out := AnnualReport{LedgerID: s.ledgerID, Today: today, Years: []int{}, Months: []YearMonthSummary{}, Categories: []CategoryTotal{}}
	if len(year) != 4 {
		return out, problem("invalid", "年份须为 1900–2200 年的 YYYY")
	}
	for _, c := range year {
		if c < '0' || c > '9' {
			return out, problem("invalid", "年份须为 1900–2200 年的 YYYY")
		}
	}
	y, err := strconv.Atoi(year)
	if err != nil || y < 1900 || y > 2200 {
		return out, problem("invalid", "年份须为 1900–2200 年的 YYYY")
	}
	out.Year = y
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = s.authorize(tx, false); err != nil {
		return out, err
	}
	if err = tx.QueryRow("SELECT version FROM ledgers WHERE id=?", s.ledgerID).Scan(&out.Version); err != nil {
		return out, err
	}
	current, _ := strconv.Atoi(today[:4])
	years := map[int]bool{current: true, y: true}
	rows, err := tx.Query("SELECT DISTINCT substr(occurred_on,1,4) FROM transactions WHERE ledger_id=? AND deleted_at IS NULL", s.ledgerID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var recorded int
		if err = rows.Scan(&recorded); err != nil {
			rows.Close()
			return out, err
		}
		years[recorded] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for recorded := range years {
		out.Years = append(out.Years, recorded)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out.Years)))
	rows, err = tx.Query("SELECT occurred_on,kind,amount_minor,category FROM transactions WHERE ledger_id=? AND substr(occurred_on,1,4)=? AND deleted_at IS NULL ORDER BY occurred_on,id", s.ledgerID, year)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	total := annualTotals{}
	var months [12]annualTotals
	for rows.Next() {
		var date, kind, category string
		var minor int64
		if err = rows.Scan(&date, &kind, &minor, &category); err != nil {
			return out, err
		}
		parsed, e := time.Parse("2006-01-02", date)
		if e != nil {
			return out, problem("invalid", "账本记录日期无效")
		}
		if err = total.add(kind, category, minor); err != nil {
			return out, err
		}
		if err = months[int(parsed.Month())-1].add(kind, category, minor); err != nil {
			return out, err
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	summary, err := total.summary("")
	if err != nil {
		return out, err
	}
	out.IncomeMinor, out.GrossExpenseMinor, out.RefundMinor = summary.IncomeMinor, summary.GrossExpenseMinor, summary.RefundMinor
	out.ExpenseMinor, out.SurplusMinor, out.TotalCount, out.Categories = summary.ExpenseMinor, summary.SurplusMinor, summary.TotalCount, summary.Categories
	for i := range months {
		m, e := months[i].summary(fmt.Sprintf("%04d-%02d", y, i+1))
		if e != nil {
			return out, e
		}
		out.Months = append(out.Months, m)
	}
	return out, nil
}
