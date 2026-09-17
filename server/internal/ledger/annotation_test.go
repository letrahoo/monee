package ledger

import (
	"strings"
	"testing"
)

func TestAnnotationHistoryConflictAndImportIdentity(t *testing.T) {
	s := testStore(t)
	p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense), "wallet")
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	original := p.Rows[0].Record
	updated, e := s.Annotate(original.ID, AnnotationInput{Version: original.Version, Category: "早餐", Note: "已核实"})
	if e != nil || updated.Version != 2 || updated.AmountMinor != original.AmountMinor {
		t.Fatal(updated, e)
	}
	d := dashboard(t, s)
	if d.ExpenseMinor != "1234" || d.Categories[0].Name != "早餐" {
		t.Fatal(d)
	}
	if _, e = s.Annotate(original.ID, AnnotationInput{Version: 1, Category: "晚餐"}); e == nil {
		t.Fatal("stale update accepted")
	}
	h, e := s.AnnotationHistory(original.ID)
	if e != nil || len(h) != 1 || h[0].Before.Category == h[0].After.Category {
		t.Fatal(h, e)
	}
	var balance int
	if e = s.db.QueryRow("SELECT sum(amount_minor) FROM postings WHERE transaction_id=?", original.ID).Scan(&balance); e != nil || balance != 0 {
		t.Fatal(balance, e)
	}
	again := alipayPreview(t, s, []byte("账单元信息\n"+nativeHeader+nativeExpense), "wallet")
	if len(again.Errors) > 0 || again.DuplicateCount != 1 {
		t.Fatal("annotation broke dedup", again)
	}
	if _, e = s.Commit(again.ID, again.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	if dashboard(t, s).Transactions[0].Category != "早餐" {
		t.Fatal("import overwrote annotation")
	}
}

func TestAnnotationPreservesSupportedFieldLengths(t *testing.T) {
	s := testStore(t)
	note := strings.Repeat("注", 1000)
	p := alipayPreview(t, s, []byte(nativeHeader+strings.Replace(nativeExpense, "早餐", note, 1)), "wallet")
	if len(p.Errors) > 0 || len(p.Rows) != 1 {
		t.Fatal("valid source note rejected", p)
	}
	if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
		t.Fatal(e)
	}
	original := p.Rows[0].Record
	category := strings.Repeat("类", 200)
	updated, e := s.Annotate(original.ID, AnnotationInput{Version: original.Version, Category: category, Note: original.Note})
	if e != nil || updated.Category != category || updated.Note != note {
		t.Fatal("category-only edit must preserve the full valid source note", updated, e)
	}
	updated, e = s.Annotate(updated.ID, AnnotationInput{Version: updated.Version, Category: updated.Category, Note: "一行\n另一行"})
	if e != nil || updated.Category != category || updated.Note != "一行\n另一行" {
		t.Fatal("note-only edit must preserve a valid category and allow multiline notes", updated, e)
	}
	if dashboard(t, s).ExpenseMinor != "1234" {
		t.Fatal("annotation changed the financial total")
	}
}

func TestAnnotationRejectsUnsupportedFieldsWithoutMutation(t *testing.T) {
	for name, input := range map[string]AnnotationInput{
		"category too long": {Category: strings.Repeat("类", 201), Note: "早餐"},
		"note too long":     {Category: "餐饮美食", Note: strings.Repeat("注", 1001)},
		"category NUL":      {Category: "早\x00餐", Note: "早餐"},
		"category CR":       {Category: "早\r餐", Note: "早餐"},
		"category LF":       {Category: "早\n餐", Note: "早餐"},
		"note NUL":          {Category: "餐饮美食", Note: "早\x00餐"},
	} {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			p := alipayPreview(t, s, []byte(nativeHeader+nativeExpense), "wallet")
			if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
				t.Fatal(e)
			}
			original := p.Rows[0].Record
			before := dashboard(t, s)
			input.Version = original.Version
			if _, e := s.Annotate(original.ID, input); e == nil {
				t.Fatal("invalid annotation accepted")
			}
			after := dashboard(t, s)
			if after.Version != before.Version || after.Transactions[0] != original {
				t.Fatal("rejected annotation mutated transaction or ledger")
			}
			history, e := s.AnnotationHistory(original.ID)
			if e != nil || len(history) != 0 {
				t.Fatal("rejected annotation left a revision", history, e)
			}
		})
	}
}
