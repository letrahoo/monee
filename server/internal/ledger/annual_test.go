package ledger

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func annualCreate(t *testing.T, s *Store, date, kind, amount, category string) Transaction {
	t.Helper()
	in := testInput()
	in.Date = date
	in.Type = kind
	in.Amount = amount
	in.Category = category
	out, err := s.Create(in, newID())
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func annualRead(t *testing.T, s *Store, year string) AnnualReport {
	t.Helper()
	out, err := s.Annual(year)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestAnnualEmptyAndStrictYear(t *testing.T) {
	s := testStore(t)
	out := annualRead(t, s, "")
	current, _ := strconv.Atoi(out.Today[:4])
	if out.Year != current || out.TotalCount != 0 || out.ExpenseMinor != "0" || out.SurplusMinor != "0" || len(out.Months) != 12 || out.Categories == nil || !reflect.DeepEqual(out.Years, []int{current}) {
		t.Fatal(out)
	}
	for i, m := range out.Months {
		if m.Month != fmt.Sprintf("%04d-%02d", current, i+1) || m.HasRecords || m.TotalCount != 0 || m.Categories == nil || m.IncomeMinor != "0" || m.GrossExpenseMinor != "0" || m.RefundMinor != "0" || m.ExpenseMinor != "0" || m.SurplusMinor != "0" {
			t.Fatal(m)
		}
	}
	for _, y := range []string{"1899", "2201", "026", "20260", " 2026", "2026 ", "+2026", "2e03", "２０２６", "2026-01"} {
		_, err := s.Annual(y)
		var p *Problem
		if !errors.As(err, &p) || p.Code != "invalid" {
			t.Fatalf("%q: %v", y, err)
		}
	}
	for _, y := range []string{"1900", "2200"} {
		if annualRead(t, s, y).Year == 0 {
			t.Fatal(y)
		}
	}
}
func TestAnnualRefundMonthsCategoriesAndDashboard(t *testing.T) {
	s := testStore(t)
	old := annualCreate(t, s, "2025-12-31", "expense", "100", "旧消费")
	if _, err := s.CreateRefund(old.ID, RefundInput{Version: 1, Date: "2026-01-02", Amount: "100", Currency: "CNY"}, newID()); err != nil {
		t.Fatal(err)
	}
	original := annualCreate(t, s, "2026-02-01", "expense", "100", "甲")
	if _, err := s.CreateRefund(original.ID, RefundInput{Version: 1, Date: "2026-02-02", Amount: "100", Currency: "CNY"}, newID()); err != nil {
		t.Fatal(err)
	}
	annualCreate(t, s, "2026-03-01", "expense", "80", "乙")
	annualCreate(t, s, "2026-03-02", "expense", "80", "丙")
	annualCreate(t, s, "2026-03-03", "income", "200", "工资")
	out := annualRead(t, s, "2026")
	if out.TotalCount != 6 || out.IncomeMinor != "20000" || out.GrossExpenseMinor != "26000" || out.RefundMinor != "20000" || out.ExpenseMinor != "6000" || out.SurplusMinor != "14000" {
		t.Fatal(out)
	}
	if !out.Months[1].HasRecords || out.Months[1].ExpenseMinor != "0" || out.Months[1].TotalCount != 2 || out.Months[3].HasRecords {
		t.Fatal(out.Months)
	}
	names := []string{}
	for _, c := range out.Categories {
		names = append(names, c.Name)
	}
	// Gross sort puts fully refunded 甲 first; refund-only 旧消费 remains visible.
	want := []string{"甲", "丙", "乙", "旧消费"}
	if !reflect.DeepEqual(names, want) {
		t.Fatal(names)
	}
	for _, m := range out.Months {
		d, err := s.Dashboard(m.Month, "", 1)
		if err != nil || d.IncomeMinor != m.IncomeMinor || d.GrossExpenseMinor != m.GrossExpenseMinor || d.RefundMinor != m.RefundMinor || d.ExpenseMinor != m.ExpenseMinor || d.TotalCount != m.TotalCount {
			t.Fatal(m, d, err)
		}
		for _, c := range m.Categories {
			found := false
			for _, dc := range d.Categories {
				if dc == c {
					found = true
				}
			}
			if !found {
				t.Fatal(c, d.Categories)
			}
		}
	}
	if prior := annualRead(t, s, "2025"); prior.ExpenseMinor != "10000" || prior.RefundMinor != "0" {
		t.Fatal(prior)
	}
	for i := 1; i < len(out.Years); i++ {
		if out.Years[i-1] <= out.Years[i] {
			t.Fatal(out.Years)
		}
	}
}
func TestAnnualImportUndoAndUncommittedExcluded(t *testing.T) {
	s := testStore(t)
	preview(t, s, sampleCSV)
	if annualRead(t, s, "2026").TotalCount != 0 {
		t.Fatal("preview included")
	}
	p := committedUndoFixture(t, s, sampleCSV)
	before := annualRead(t, s, "2026")
	u, err := s.PreviewImportUndo(p.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UndoImport(p.ID, u.LedgerVersion); err != nil {
		t.Fatal(err)
	}
	after := annualRead(t, s, "2026")
	if before.TotalCount != 3 || after.TotalCount != 0 || after.GrossExpenseMinor != "0" || after.Version <= before.Version {
		t.Fatal(before, after)
	}
	restore, err := s.PreviewImportUndo(p.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RestoreImport(p.ID, restore.LedgerVersion); err != nil {
		t.Fatal(err)
	}
	if annualRead(t, s, "2026").TotalCount != 3 {
		t.Fatal("restore missing")
	}
}
func TestAnnualRejectsOverflowAndFractionalAmounts(t *testing.T) {
	cases := []struct {
		name    string
		amounts []int64
		kind    string
	}{
		{"income", []int64{math.MaxInt64, 1}, "income"},
		{"expense", []int64{-math.MaxInt64, -1}, "expense"},
		{"minimum", []int64{math.MinInt64}, "expense"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			for i, n := range tc.amounts {
				r := annualCreate(t, s, fmt.Sprintf("2026-%02d-01", i+1), tc.kind, "1", "合成")
				if _, err := s.db.Exec("UPDATE transactions SET amount_minor=? WHERE id=?", n, r.ID); err != nil {
					t.Fatal(err)
				}
			}
			_, err := s.Annual("2026")
			var p *Problem
			if !errors.As(err, &p) || p.Code != "overflow" {
				t.Fatal(err)
			}
		})
	}
	t.Run("surplus", func(t *testing.T) {
		s := testStore(t)
		old := annualCreate(t, s, "2025-12-31", "expense", "1", "合成")
		refund, err := s.CreateRefund(old.ID, RefundInput{Version: 1, Date: "2026-01-01", Amount: "1", Currency: "CNY"}, newID())
		if err != nil {
			t.Fatal(err)
		}
		income := annualCreate(t, s, "2026-02-01", "income", "1", "工资")
		if _, err = s.db.Exec("UPDATE transactions SET amount_minor=? WHERE id IN (?,?)", int64(math.MaxInt64), refund.ID, income.ID); err != nil {
			t.Fatal(err)
		}
		_, err = s.Annual("2026")
		var p *Problem
		if !errors.As(err, &p) || p.Code != "overflow" {
			t.Fatal(err)
		}
	})
	t.Run("fractional", func(t *testing.T) {
		s := testStore(t)
		r := annualCreate(t, s, "2026-01-01", "income", "1", "合成")
		if _, err := s.db.Exec("UPDATE transactions SET amount_minor=1.5 WHERE id=?", r.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Annual("2026"); err == nil {
			t.Fatal("rounded fractional amount")
		}
	})
}
func TestAnnualPendingSourceRowsExcluded(t *testing.T) {
	s := testStore(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense+nativeRefund), "synthetic-annual-wallet")
	if len(p.Pending) != 1 {
		t.Fatal(p.Pending)
	}
	if _, err := s.Commit(p.ID, p.LedgerVersion, false); err != nil {
		t.Fatal(err)
	}
	out := annualRead(t, s, "2026")
	if out.TotalCount != 1 || out.RefundMinor != "0" {
		t.Fatal(out)
	}
	q, err := s.ReviewQueue(1, "")
	if err != nil || q.TotalCount != 1 {
		t.Fatal(q, err)
	}
}
func TestAnnualYearChoicesExcludeDeletedOnlyYears(t *testing.T) {
	s := testStore(t)
	r := annualCreate(t, s, "1901-01-01", "expense", "1", "合成")
	annualCreate(t, s, "1902-01-01", "income", "1", "合成")
	if _, err := s.db.Exec("UPDATE transactions SET deleted_at=? WHERE id=?", now(), r.ID); err != nil {
		t.Fatal(err)
	}
	out := annualRead(t, s, "1903")
	current, _ := strconv.Atoi(out.Today[:4])
	if !reflect.DeepEqual(out.Years, []int{current, 1903, 1902}) {
		t.Fatal(out.Years)
	}
}
func TestAnnualMonthlySurplusOverflowDespiteSafeAnnual(t *testing.T) {
	s := testStore(t)
	old := annualCreate(t, s, "2025-12-31", "expense", "1", "合成")
	refund, err := s.CreateRefund(old.ID, RefundInput{Version: 1, Date: "2026-01-01", Amount: "1", Currency: "CNY"}, newID())
	if err != nil {
		t.Fatal(err)
	}
	income := annualCreate(t, s, "2026-01-01", "income", "1", "工资")
	expense := annualCreate(t, s, "2026-02-01", "expense", "1", "合成")
	if _, err = s.db.Exec("UPDATE transactions SET amount_minor=? WHERE id=?", int64(math.MaxInt64), income.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE transactions SET amount_minor=CASE WHEN id=? THEN 1 ELSE -1 END WHERE id IN (?,?)", refund.ID, refund.ID, expense.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.Annual("2026")
	var p *Problem
	if !errors.As(err, &p) || p.Code != "overflow" {
		t.Fatal(err)
	}
}
func TestAnnualSnapshotVersionAndAmountsWithIndependentWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.db")
	reader, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	record := annualCreate(t, reader, "2026-01-01", "income", "1", "工资")
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	done := make(chan error, 1)
	go func() {
		for version := int64(2); version <= 60; version++ {
			tx, e := writer.db.Begin()
			if e != nil {
				done <- e
				return
			}
			if _, e = tx.Exec("UPDATE transactions SET amount_minor=? WHERE id=?", version*100, record.ID); e != nil {
				tx.Rollback()
				done <- e
				return
			}
			if _, e = tx.Exec("UPDATE ledgers SET version=? WHERE id=?", version, reader.ledgerID); e != nil {
				tx.Rollback()
				done <- e
				return
			}
			if e = tx.Commit(); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 60; i++ {
		out, e := reader.Annual("2026")
		if e != nil {
			t.Error(e)
			break
		}
		expected := strconv.FormatInt(out.Version*100, 10)
		if out.IncomeMinor != expected || out.Months[0].IncomeMinor != expected {
			t.Errorf("mixed snapshot: version=%d annual=%s month=%s", out.Version, out.IncomeMinor, out.Months[0].IncomeMinor)
			break
		}
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
func TestAnnualFutureTransferIsOnlyActivity(t *testing.T) {
	// The current schema cannot store transfers yet; exercise the aggregation rule
	// without relaxing production constraints.
	total := annualTotals{}
	if err := total.add("transfer", "合成", math.MinInt64); err != nil {
		t.Fatal(err)
	}
	m, err := total.summary("2026-01")
	if err != nil || !m.HasRecords || m.TotalCount != 1 || m.IncomeMinor != "0" || m.ExpenseMinor != "0" || m.SurplusMinor != "0" || len(m.Categories) != 0 {
		t.Fatal(m, err)
	}
}
