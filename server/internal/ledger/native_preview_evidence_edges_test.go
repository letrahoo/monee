package ledger

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestNativeEvidenceStoreSourceFailureAndDynamicErrors(t *testing.T) {
	t.Run("static failure without records", func(t *testing.T) {
		s := testStore(t)
		csv := "not,a,native,header\nsynthetic,broken,bill,input\n"
		p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
		if len(p.Errors) == 0 || p.SourceDocument == nil || len(p.SourceDocument.Records) != 0 || p.SourceAccount != "" {
			t.Fatal("fixture is not a source parse failure", p)
		}
		again, e := s.PreviewAlipay("renamed.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
		if e != nil || again.ID != p.ID || again.Filename != p.Filename || !reflect.DeepEqual(again.Errors, p.Errors) || !reflect.DeepEqual(again.SourceDocument, p.SourceDocument) {
			t.Fatal("same retained source failure changed", again, e)
		}
		if _, e = s.Commit(again.ID, again.LedgerVersion, false); e == nil {
			t.Fatal("source failure became bookable")
		}
	})
	t.Run("current dedup conflict can clear", func(t *testing.T) {
		s := testStore(t)
		csv := nativeHeader + nativeExpense
		p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
		p.Rows[0].Status = "conflict"
		p.Rows[0].Message = "相同来源流水号的数据不一致"
		p.Errors = []string{fmt.Sprintf("第 %d 行：%s", p.Rows[0].Line, p.Rows[0].Message)}
		p.NewCount = 0
		if _, e := s.db.Exec("UPDATE imports SET preview_json=? WHERE id=?", encode(p), p.ID); e != nil {
			t.Fatal(e)
		}
		again, e := s.PreviewAlipay("refresh.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
		if e != nil || len(again.Errors) != 0 || again.NewCount != 1 || again.Rows[0].Status != "new" {
			t.Fatal("dynamic conflict was permanently frozen", again, e)
		}
		if r, e := s.Commit(again.ID, again.LedgerVersion, false); e != nil || r.Added != 1 {
			t.Fatal(r, e)
		}
	})
}

func TestNativeEvidenceStoreRejectsCorruptUndoState(t *testing.T) {
	for _, payload := range []string{`null`, `{}`, `{"state":"invented","changes":{}}`, `{"state":"committed","changes":{}}`, `{"state":"undone","ſtate":"undone","changes":{}}`, `{"state":"undone","changes":{"synthetic":{"version":0,"deletedAt":"bad"}}}`} {
		t.Run(payload, func(t *testing.T) {
			s := testStore(t)
			csv := nativeHeader + nativeExpense
			p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
			if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
				t.Fatal(e)
			}
			u, e := s.PreviewImportUndo(p.ID, false)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.UndoImport(p.ID, u.LedgerVersion); e != nil {
				t.Fatal(e)
			}
			if _, e = s.db.Exec("UPDATE change_log SET payload_json=? WHERE entity_type='import' AND entity_id=? AND operation='import_undo'", payload, p.ID); e != nil {
				t.Fatal(e)
			}
			nativeEvidenceConflict(t, s, func() error {
				_, e := s.PreviewAlipay("repeat.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
				return e
			})
		})
	}
	t.Run("unknown payload version", func(t *testing.T) {
		s := testStore(t)
		csv := nativeHeader + nativeExpense
		p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
		if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
			t.Fatal(e)
		}
		u, e := s.PreviewImportUndo(p.ID, false)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.UndoImport(p.ID, u.LedgerVersion); e != nil {
			t.Fatal(e)
		}
		if _, e = s.db.Exec("UPDATE change_log SET payload_version=2 WHERE entity_type='import' AND entity_id=? AND operation='import_undo'", p.ID); e != nil {
			t.Fatal(e)
		}
		nativeEvidenceConflict(t, s, func() error {
			_, e := s.PreviewAlipay("repeat.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
			return e
		})
	})
}

func TestNativeEvidenceStoreRepeatWeChatSources(t *testing.T) {
	for _, name := range []string{"ordinary-and-review.csv", "ordinary-and-review.xlsx"} {
		t.Run(name, func(t *testing.T) {
			raw, e := os.ReadFile("../../../fixtures/synthetic/wechat/" + name)
			if e != nil {
				t.Fatal(e)
			}
			s := testStore(t)
			content := base64.StdEncoding.EncodeToString(raw)
			p, e := s.PreviewWeChat(name, content, "synthetic-wallet")
			if e != nil || p.SourceDocument == nil {
				t.Fatal(p, e)
			}
			again, e := s.PreviewWeChat("renamed"+filepath.Ext(name), content, "synthetic-wallet")
			if e != nil || again.ID != p.ID || again.Filename != p.Filename || !reflect.DeepEqual(again.SourceDocument, p.SourceDocument) || !reflect.DeepEqual(again.Pending, p.Pending) {
				t.Fatal("WeChat source changed on repeat", again, e)
			}
		})
	}
}

func TestNativeEvidenceStoreIndependentConnectionCommitRace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-race.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	other, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	for i := 0; i < 6; i++ {
		csv := nativeHeader + strings.ReplaceAll(nativeExpense, "pay-1", fmt.Sprintf("synthetic-race-%d", i))
		p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
		var wg sync.WaitGroup
		wg.Add(2)
		start := make(chan struct{})
		var pe, ce error
		var replay Preview
		go func() {
			defer wg.Done()
			<-start
			replay, pe = other.PreviewAlipay("same.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
		}()
		go func() { defer wg.Done(); <-start; _, ce = s.Commit(p.ID, p.LedgerVersion, true) }()
		close(start)
		wg.Wait()
		if pe != nil && !strings.Contains(strings.ToLower(pe.Error()), "locked") && !strings.Contains(strings.ToLower(pe.Error()), "busy") {
			var problem *Problem
			if !errors.As(pe, &problem) || problem.Code != "conflict" {
				t.Fatal("unexpected preview race failure", pe)
			}
		}
		if ce != nil {
			if _, ce = s.Commit(p.ID, p.LedgerVersion, true); ce != nil {
				t.Fatal("commit not recoverable after race", ce)
			}
		}
		if pe == nil && replay.ID != p.ID {
			t.Fatal("race created a second source batch")
		}
		before := nativeEvidenceTables(t, s)
		again, e := other.PreviewAlipay("after.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
		if e != nil || !again.AlreadyCommitted {
			t.Fatal("committed source became uncommitted", again, e)
		}
		if after := nativeEvidenceTables(t, s); !reflect.DeepEqual(before, after) {
			t.Fatal("postcommit repeat overwrote evidence")
		}
	}
	if got := count(t, s, "transactions"); got != 6 {
		t.Fatal("concurrent refresh created duplicates or lost commits", got)
	}
	if count(t, s, "imports") != 6 || count(t, s, "source_records") != 6 {
		t.Fatal("concurrent refresh duplicated source batches or associations")
	}
	d := dashboard(t, s)
	if d.TotalCount != 6 || d.GrossExpenseMinor != "7404" || d.ExpenseMinor != "7404" || d.IncomeMinor != "0" || d.RefundMinor != "0" || d.Version != 6 {
		t.Fatal("concurrent preview/commit changed expected active finance or version", d)
	}
}
