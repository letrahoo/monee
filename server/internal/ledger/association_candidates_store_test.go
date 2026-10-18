package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func associationTestCreate(t *testing.T, s *Store, source, key string) Transaction {
	t.Helper()
	in := testInput()
	in.Source = source
	in.Account = "synthetic-account-not-returned"
	in.ExternalID = "synthetic-private-trace-" + key
	in.Note = "synthetic-note-not-returned"
	r, e := s.Create(in, "association-"+key)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func associationTestSnapshot(t *testing.T, s *Store) map[string][][]any {
	t.Helper()
	rows, e := s.db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			t.Fatal(e)
		}
		names = append(names, name)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	out := map[string][][]any{}
	for _, name := range names {
		r, e := s.db.Query(`SELECT * FROM "` + strings.ReplaceAll(name, `"`, `""`) + `"`)
		if e != nil {
			t.Fatal(e)
		}
		columns, e := r.Columns()
		if e != nil {
			t.Fatal(e)
		}
		var data [][]any
		for r.Next() {
			v := make([]any, len(columns))
			p := make([]any, len(columns))
			for i := range v {
				p[i] = &v[i]
			}
			if e = r.Scan(p...); e != nil {
				t.Fatal(e)
			}
			data = append(data, v)
		}
		if e = r.Err(); e != nil {
			t.Fatal(e)
		}
		r.Close()
		out[name] = data
	}
	return out
}
func associationTestProblem(t *testing.T, e error, code string) {
	t.Helper()
	var p *Problem
	if !errors.As(e, &p) || p.Code != code {
		t.Fatalf("want %s got %v", code, e)
	}
}

func TestAssociationCandidatesStoreReadOnlyHintsNotEconomicIdentity(t *testing.T) {
	s := testStore(t)
	a := associationTestCreate(t, s, "支付宝", "base")
	b := associationTestCreate(t, s, "银行卡", "other")
	before := associationTestSnapshot(t, s)
	dBefore := dashboard(t, s)
	for i := 0; i < 2; i++ {
		p, e := s.TransactionAssociationCandidates(a.ID, 1)
		if e != nil {
			t.Fatal(e)
		}
		if p.ContractVersion != 1 || !p.ReadOnly || p.Basis != "current_transaction_similarity" || p.SourceRolesVerified || p.Actions == nil || len(p.Actions) != 0 || p.Base.ID != a.ID || p.Base.SourceDisplay != a.Source || p.TotalCount != 1 || len(p.Items) != 1 || p.Items[0].Transaction.ID != b.ID || p.Items[0].Transaction.AmountMinor != "-1234" || !reflect.DeepEqual(p.Items[0].Reasons, []string{"current_projection_match", "different_source_display"}) {
			t.Fatal("bad readonly discovery contract", p)
		}
		body, e := json.Marshal(p)
		if e != nil {
			t.Fatal(e)
		}
		for _, secret := range []string{"synthetic-private-trace", "synthetic-account-not-returned", "synthetic-note-not-returned", `"externalId"`, `"account"`, `"note"`, `"category"`, `"raw"`, `"confidence"`, `"confirmed"`, `"allowedConfirm"`} {
			if strings.Contains(string(body), secret) {
				t.Fatal("narrow DTO leaked or overstated", secret)
			}
		}
	}
	if after := associationTestSnapshot(t, s); !reflect.DeepEqual(before, after) {
		t.Fatal("readonly lookup changed application tables")
	}
	after := dashboard(t, s)
	if !reflect.DeepEqual(dBefore, after) || after.TotalCount != 2 || after.ExpenseMinor != "2468" {
		t.Fatal("similarity query counted only once without a decision", after)
	}
}

func TestAssociationCandidatesStorePagingAndNoWinner(t *testing.T) {
	s := testStore(t)
	base := associationTestCreate(t, s, "合成来源A", "base")
	var ids []string
	for i := 0; i < 51; i++ {
		r := associationTestCreate(t, s, "合成来源B", fmt.Sprintf("candidate-%03d", i))
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	before := associationTestSnapshot(t, s)
	var seen []string
	for page := 1; page <= 3; page++ {
		p, e := s.TransactionAssociationCandidates(base.ID, page)
		if e != nil || p.Page != page || p.PageSize != 50 || p.TotalCount != 51 || p.LedgerVersion != 52 || p.SourceRolesVerified || p.Items == nil || len(p.Actions) != 0 {
			t.Fatal("bad paged snapshot", p, e)
		}
		want := 50
		if page == 2 {
			want = 1
		}
		if page == 3 {
			want = 0
		}
		if len(p.Items) != want {
			t.Fatal("bad page length", len(p.Items), want)
		}
		for _, item := range p.Items {
			seen = append(seen, item.Transaction.ID)
		}
	}
	if !reflect.DeepEqual(ids, seen) {
		t.Fatal("candidate order unstable or missing/duplicated")
	}
	again, e := s.TransactionAssociationCandidates(base.ID, 1)
	if e != nil || again.Items[0].Transaction.ID != ids[0] {
		t.Fatal(again, e)
	}
	if !reflect.DeepEqual(before, associationTestSnapshot(t, s)) {
		t.Fatal("paging mutated ledger/source/versions")
	}
	for _, page := range []int{-1, 0, 1000001} {
		_, e := s.TransactionAssociationCandidates(base.ID, page)
		associationTestProblem(t, e, "invalid")
	}
}

func TestAssociationCandidatesStoreExactFieldsAndInvalidProjection(t *testing.T) {
	cases := map[string]func(*Input){"date": func(in *Input) { in.Date = "2026-09-15" }, "amount": func(in *Input) { in.Amount = "12.35" }, "merchant": func(in *Input) { in.Merchant = "中文商户另一店" }, "income": func(in *Input) { in.Type = "income" }, "same source different funding account": func(in *Input) { in.Source = "合成来源A"; in.Account = "another account" }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			base := associationTestCreate(t, s, "合成来源A", "base")
			in := testInput()
			in.Source = "合成来源B"
			change(&in)
			if _, e := s.Create(in, "association-other"); e != nil {
				t.Fatal(e)
			}
			p, e := s.TransactionAssociationCandidates(base.ID, 1)
			if e != nil || p.TotalCount != 0 || len(p.Items) != 0 {
				t.Fatal("nonexact fields matched", p, e)
			}
		})
	}
	for name, value := range map[string]any{"source": "　", "source-trailing": "valid ", "source-control": "valid\x00hidden", "version": int64(0), "amount-real": -1234.5} {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			base := associationTestCreate(t, s, "合成来源A", "base")
			bad := associationTestCreate(t, s, "合成来源B", "other")
			column := strings.Split(name, "-")[0]
			if column == "amount" {
				column = "amount_minor"
			}
			if _, e := s.db.Exec("UPDATE transactions SET "+column+"=? WHERE id=?", value, bad.ID); e != nil {
				t.Fatal(e)
			}
			p, e := s.TransactionAssociationCandidates(base.ID, 1)
			if e != nil || p.TotalCount != 0 || len(p.Items) != 0 {
				t.Fatal("invalid projection entered count or page", name, p, e)
			}
			_, e = s.TransactionAssociationCandidates(bad.ID, 1)
			associationTestProblem(t, e, "invalid")
		})
	}
}

func TestAssociationCandidatesStoreCorrectionAndAnnotationPolicy(t *testing.T) {
	s := testStore(t)
	base := associationTestCreate(t, s, "合成来源A", "base")
	candidate := associationTestCreate(t, s, "合成来源B", "other")
	annotated, e := s.Annotate(candidate.ID, AnnotationInput{Version: 1, Category: "合成备注分类", Note: "synthetic annotation"})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.TransactionAssociationCandidates(base.ID, 1)
	if e != nil || p.TotalCount != 1 || p.Items[0].Transaction.Version != annotated.Version {
		t.Fatal("annotation mistaken for financial correction", p, e)
	}
	corrected, e := s.Correct(candidate.ID, CorrectionInput{Version: annotated.Version, Type: "expense", AmountMinor: "1235", Reason: "合成更正"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Correct(candidate.ID, CorrectionInput{Version: corrected.Version, Type: "expense", AmountMinor: "1234", Reason: "合成改回同金额"}); e != nil {
		t.Fatal(e)
	}
	p, e = s.TransactionAssociationCandidates(base.ID, 1)
	if e != nil || p.TotalCount != 0 {
		t.Fatal("known corrected projection returned as eligible hint", p, e)
	}
	_, e = s.TransactionAssociationCandidates(candidate.ID, 1)
	associationTestProblem(t, e, "invalid")
	if _, e = s.db.Exec("UPDATE change_log SET payload_json='synthetic bad JSON' WHERE entity_type='transaction' AND entity_id=? AND operation='correct'", candidate.ID); e != nil {
		t.Fatal(e)
	}
	p, e = s.TransactionAssociationCandidates(base.ID, 1)
	if e != nil || p.TotalCount != 0 {
		t.Fatal("bad correction payload became absence of history", p, e)
	}
}

func TestAssociationCandidatesStoreRefundAndSoftDeletePolicy(t *testing.T) {
	for _, softDeleteRefund := range []bool{false, true} {
		t.Run(fmt.Sprint(softDeleteRefund), func(t *testing.T) {
			s := testStore(t)
			base := associationTestCreate(t, s, "合成来源A", "base")
			other := associationTestCreate(t, s, "合成来源B", "other")
			refund, e := s.CreateRefund(other.ID, RefundInput{Version: 1, Date: "2026-09-15", Amount: "1.00", Currency: "CNY", Account: "合成退款账户"}, "association-refund")
			if e != nil {
				t.Fatal(e)
			}
			if softDeleteRefund {
				if _, e = s.db.Exec("UPDATE transactions SET deleted_at=? WHERE id=?", now(), refund.ID); e != nil {
					t.Fatal(e)
				}
			}
			p, e := s.TransactionAssociationCandidates(base.ID, 1)
			if e != nil || p.TotalCount != 0 {
				t.Fatal("refund dependency was ignored", p, e)
			}
			_, e = s.TransactionAssociationCandidates(other.ID, 1)
			associationTestProblem(t, e, "invalid")
			_, e = s.TransactionAssociationCandidates(refund.ID, 1)
			if softDeleteRefund {
				associationTestProblem(t, e, "not_found")
			} else {
				associationTestProblem(t, e, "invalid")
			}
		})
	}
	t.Run("deleted base and candidate", func(t *testing.T) {
		s := testStore(t)
		base := associationTestCreate(t, s, "合成来源A", "base")
		other := associationTestCreate(t, s, "合成来源B", "other")
		if _, e := s.db.Exec("UPDATE transactions SET deleted_at=? WHERE id=?", now(), other.ID); e != nil {
			t.Fatal(e)
		}
		p, e := s.TransactionAssociationCandidates(base.ID, 1)
		if e != nil || p.TotalCount != 0 {
			t.Fatal(p, e)
		}
		_, e = s.TransactionAssociationCandidates(other.ID, 1)
		associationTestProblem(t, e, "not_found")
	})
}
