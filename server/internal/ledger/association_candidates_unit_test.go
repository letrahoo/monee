package ledger

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func associationUnitProjection() associationProjection {
	return associationProjection{transaction: AssociationTransaction{ID: "synthetic-base", Version: 1, Date: "2026-09-15", Type: "expense", AmountMinor: "-1234", Currency: "CNY", Merchant: "合成商户", SourceDisplay: "合成来源甲"}, amountInteger: true}
}
func TestAssociationUnitEligibility(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*associationProjection)
	}{
		{"not integer storage", func(p *associationProjection) { p.amountInteger = false }},
		{"missing id", func(p *associationProjection) { p.transaction.ID = "" }},
		{"zero version", func(p *associationProjection) { p.transaction.Version = 0 }},
		{"negative version", func(p *associationProjection) { p.transaction.Version = -1 }},
		{"income", func(p *associationProjection) { p.transaction.Type = "income"; p.transaction.AmountMinor = "1234" }},
		{"refund", func(p *associationProjection) { p.transaction.Type = "refund" }},
		{"transfer", func(p *associationProjection) { p.transaction.Type = "transfer" }},
		{"currency", func(p *associationProjection) { p.transaction.Currency = "USD" }},
		{"refund of", func(p *associationProjection) { p.refundOf = "synthetic-parent" }},
		{"any refund", func(p *associationProjection) { p.hasRefund = true }},
		{"known correction", func(p *associationProjection) { p.corrected = true }},
		{"merchant empty", func(p *associationProjection) { p.transaction.Merchant = "" }},
		{"source empty", func(p *associationProjection) { p.transaction.SourceDisplay = "" }},
		{"merchant trim", func(p *associationProjection) { p.transaction.Merchant = " 合成商户" }},
		{"source unicode trim", func(p *associationProjection) { p.transaction.SourceDisplay = "合成来源\u3000" }},
		{"merchant nul", func(p *associationProjection) { p.transaction.Merchant = "合成\x00商户" }},
		{"source control", func(p *associationProjection) { p.transaction.SourceDisplay = "合成\u0085来源" }},
		{"bad utf8", func(p *associationProjection) { p.transaction.SourceDisplay = string([]byte{0xff}) }},
	}
	base := associationUnitProjection()
	if !associationEligible(base) {
		t.Fatal("valid projection rejected")
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.mutate(&p)
			before := p
			if associationEligible(p) {
				t.Fatal("accepted unsupported projection")
			}
			if p != before {
				t.Fatal("helper changed input")
			}
		})
	}
	// Annotation versions are not correction evidence, and arbitrary source labels
	// or legacy-looking names never become verified source roles.
	for _, source := range []string{"手动", "ai-financial:synthetic", "银行卡", "支付宝", "source with internal space"} {
		p := base
		p.transaction.Version = 9
		p.transaction.SourceDisplay = source
		if !associationEligible(p) {
			t.Fatal("annotation/label falsely excluded", source)
		}
	}
}
func TestAssociationUnitCanonicalAmounts(t *testing.T) {
	for _, value := range []string{"-1", "-1234", "-1000000000000"} {
		p := associationUnitProjection()
		p.transaction.AmountMinor = value
		if !associationEligible(p) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"0", "-0", "1", "+1", "-01", "-1.0", "-1e2", " -1", "-1 ", "", "-1000000000001", "-9223372036854775808", "-9223372036854775809", "9223372036854775808"} {
		p := associationUnitProjection()
		p.transaction.AmountMinor = value
		if associationEligible(p) {
			t.Fatal("noncanonical/out-of-range", value)
		}
	}
}
func TestAssociationUnitCanonicalDatesAndDisplays(t *testing.T) {
	for _, date := range []string{"1900-01-01", "2200-12-31", "2024-02-29"} {
		p := associationUnitProjection()
		p.transaction.Date = date
		if !associationEligible(p) {
			t.Fatal(date)
		}
	}
	for _, date := range []string{"1899-12-31", "2201-01-01", "2026-02-29", "2026-9-15", "2026-09-5", "2026-09-15T00:00:00Z", "2026-09-15 ", ""} {
		p := associationUnitProjection()
		p.transaction.Date = date
		if associationEligible(p) {
			t.Fatal(date)
		}
	}
	for _, value := range []string{"", " ", "\u3000", "\u00a0", " leading", "trailing\u3000", "line\nbreak", "tab\tvalue", "nul\x00value", "control\x7fvalue", strings.Repeat("中", 201)} {
		if associationDisplayValid(value) {
			t.Fatal("invalid display accepted", value)
		}
	}
	for _, value := range []string{"合成商户", strings.Repeat("中", 200), "source internal space", "a' OR 1=1 --"} {
		if !associationDisplayValid(value) {
			t.Fatal("valid literal rejected", value)
		}
	}
}
func TestAssociationUnitExactCurrentMatch(t *testing.T) {
	base := associationUnitProjection()
	candidate := base
	candidate.transaction.ID = "synthetic-candidate"
	candidate.transaction.SourceDisplay = "合成来源乙"
	candidate.transaction.Version = 8
	if !associationMatches(base, candidate) {
		t.Fatal("matching candidate rejected")
	}
	for _, mutate := range []func(*associationProjection){
		func(p *associationProjection) { p.transaction.ID = base.transaction.ID },
		func(p *associationProjection) { p.transaction.Date = "2026-09-16" },
		func(p *associationProjection) { p.transaction.Type = "income" },
		func(p *associationProjection) { p.transaction.AmountMinor = "-1235" },
		func(p *associationProjection) { p.transaction.Currency = "USD" },
		func(p *associationProjection) { p.transaction.Merchant = "合成商户甲" },
		func(p *associationProjection) { p.transaction.SourceDisplay = base.transaction.SourceDisplay },
		func(p *associationProjection) { p.hasRefund = true },
		func(p *associationProjection) { p.corrected = true },
	} {
		p := candidate
		mutate(&p)
		if associationMatches(base, p) {
			t.Fatal("inexact or excluded projection matched", p)
		}
	}
	invalidBase := base
	invalidBase.corrected = true
	if associationMatches(invalidBase, candidate) {
		t.Fatal("invalid base matched")
	}
	// Exact display comparison is not provider recognition or Unicode normalization.
	base.transaction.SourceDisplay = "source"
	candidate.transaction.SourceDisplay = "SOURCE"
	if !associationMatches(base, candidate) {
		t.Fatal("case-sensitive distinct display was folded")
	}
}
func TestAssociationUnitNarrowDTO(t *testing.T) {
	tx := associationUnitProjection().transaction
	out := AssociationCandidates{ContractVersion: 1, ReadOnly: true, Basis: "current_transaction_similarity", Actions: []string{}, Base: tx, Items: []AssociationCandidate{{Transaction: tx, Reasons: []string{"current_projection_match", "different_source_display"}}}, Page: 1, PageSize: 50, TotalCount: 1}
	bytes, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	var root map[string]json.RawMessage
	if e = json.Unmarshal(bytes, &root); e != nil {
		t.Fatal(e)
	}
	expected := []string{"contractVersion", "readOnly", "basis", "sourceRolesVerified", "actions", "ledgerVersion", "base", "items", "page", "pageSize", "totalCount"}
	if len(root) != len(expected) {
		t.Fatal("root shape", string(bytes))
	}
	for _, key := range expected {
		if _, ok := root[key]; !ok {
			t.Fatal("missing root key", key)
		}
	}
	var narrow map[string]any
	if e = json.Unmarshal(root["base"], &narrow); e != nil {
		t.Fatal(e)
	}
	if len(narrow) != 8 {
		t.Fatal(narrow)
	}
	for _, key := range []string{"id", "version", "date", "type", "amountMinor", "currency", "merchant", "sourceDisplay"} {
		if _, ok := narrow[key]; !ok {
			t.Fatal("missing narrow field", key)
		}
	}
	for _, forbidden := range []string{"account", "note", "category", "externalId", "refundOf", "raw", "token", "confidence", "allowedConfirm"} {
		if _, ok := root[forbidden]; ok {
			t.Fatal("root leak", forbidden)
		}
		if _, ok := narrow[forbidden]; ok {
			t.Fatal("transaction leak", forbidden)
		}
	}
	if string(root["actions"]) != "[]" || string(root["sourceRolesVerified"]) != "false" {
		t.Fatal(string(bytes))
	}
	out.Items = []AssociationCandidate{}
	empty, _ := json.Marshal(out)
	if strings.Contains(string(empty), `"items":null`) {
		t.Fatal("null items")
	}
}

// A synthetic Scan implementation checks storage typing without any database.
type associationUnitRow struct {
	amount any
	kind   string
}

func (r associationUnitRow) Scan(dest ...any) error {
	values := []any{"synthetic", int64(1), "2026-09-15", "expense", r.amount, r.kind, "CNY", "合成商户", "来源", "", false, false}
	for i, value := range values {
		switch target := dest[i].(type) {
		case *string:
			*target = value.(string)
		case *int64:
			*target = value.(int64)
		case *bool:
			*target = value.(bool)
		case *any:
			*target = value
		}
	}
	return nil
}
func TestAssociationUnitStorageAmountType(t *testing.T) {
	valid, e := scanAssociationProjection(associationUnitRow{int64(-1234), "integer"})
	if e != nil || !associationEligible(valid) {
		t.Fatal(valid, e)
	}
	for _, row := range []associationUnitRow{{float64(-1234), "real"}, {float64(-1234.5), "real"}, {"-1234", "text"}, {int64(-1234), "real"}, {nil, "null"}} {
		p, e := scanAssociationProjection(row)
		if e != nil || associationEligible(p) || p.transaction.AmountMinor != "" {
			t.Fatal("noninteger storage accepted/canonicalized", p, e)
		}
	}
	if !reflect.DeepEqual(valid.transaction, AssociationTransaction{ID: "synthetic", Version: 1, Date: "2026-09-15", Type: "expense", AmountMinor: "-1234", Currency: "CNY", Merchant: "合成商户", SourceDisplay: "来源"}) {
		t.Fatal(valid)
	}
}
