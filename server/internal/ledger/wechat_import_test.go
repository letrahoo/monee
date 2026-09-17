package ledger

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

func TestWeChatNativePreviewCommitHistory(t *testing.T) {
	raw, e := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	s := testStore(t)
	content := base64.StdEncoding.EncodeToString(raw)
	p, e := s.PreviewWeChat("wechat.xlsx", content, "wallet-a")
	if e != nil || len(p.Errors) > 0 || p.NewCount != 1 || len(p.Pending) != 3 || p.Format != "wechat" {
		t.Fatal(p, e)
	}
	if count(t, s, "transactions") != 0 {
		t.Fatal("preview wrote money")
	}
	result, e := s.Commit(p.ID, p.LedgerVersion, false)
	if e != nil || result.Added != 1 || result.Pending != 3 {
		t.Fatal(result, e)
	}
	d := dashboard(t, s)
	if d.ExpenseMinor != "1234" || d.IncomeMinor != "0" {
		t.Fatal(d)
	}
	repeat, e := s.PreviewWeChat("renamed.xlsx", content, "wallet-a")
	if e != nil || !repeat.AlreadyCommitted || repeat.ID != p.ID {
		t.Fatal(repeat, e)
	}
	detail, e := s.ImportDetail(p.ID)
	if e != nil || len(detail.Evidence) != 1 || len(detail.Preview.SourceDocument.Records) != 4 {
		t.Fatal(detail, e)
	}
	other, e := s.PreviewWeChat("wechat.xlsx", content, "wallet-b")
	if e != nil || other.DuplicateCount != 0 || other.SimilarCount != 1 {
		t.Fatal(other, e)
	}
}

func TestFailedNativeImportIsRetainedButNeverBookable(t *testing.T) {
	s := testStore(t)
	p, e := s.PreviewWeChat("broken.xlsx", base64.StdEncoding.EncodeToString([]byte("broken")), "wallet")
	if e != nil || p.ID == "" || len(p.Errors) == 0 {
		t.Fatal(p, e)
	}
	if _, e = s.Commit(p.ID, p.LedgerVersion, false); e == nil {
		t.Fatal("failed batch committed")
	}
	h, e := s.ImportHistory(1)
	if e != nil || h.TotalCount != 1 || h.Imports[0].Errors == 0 {
		t.Fatal(h, e)
	}
	detail, e := s.ImportDetail(p.ID)
	if e != nil || len(detail.Preview.Errors) == 0 {
		t.Fatal(detail, e)
	}
	if count(t, s, "transactions") != 0 {
		t.Fatal("failed batch wrote money")
	}
	again, e := s.PreviewWeChat("renamed.xlsx", base64.StdEncoding.EncodeToString([]byte("broken")), "wallet")
	if e != nil || again.ID != p.ID {
		t.Fatal(again, e)
	}
}

func TestWeChatCSVCommitRepeatAndCrossContainerIdentity(t *testing.T) {
	csvBytes, err := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.csv")
	if err != nil {
		t.Fatal(err)
	}
	xlsxBytes, err := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, csvFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("csv-first-%t", csvFirst), func(t *testing.T) {
			s := testStore(t)
			first, second := csvBytes, xlsxBytes
			if !csvFirst {
				first, second = second, first
			}
			preview := func(raw []byte) Preview {
				// Filename is intentionally unrelated to the container.
				p, err := s.PreviewWeChat("bill.dat", base64.StdEncoding.EncodeToString(raw), "wallet")
				if err != nil || len(p.Errors) != 0 || p.Format != "wechat" || len(p.Pending) != 3 {
					t.Fatal(p, err)
				}
				return p
			}
			p := preview(first)
			if p.NewCount != 1 || count(t, s, "transactions") != 0 {
				t.Fatal(p)
			}
			result, err := s.Commit(p.ID, p.LedgerVersion, false)
			if err != nil || result.Added != 1 || result.Pending != 3 {
				t.Fatal(result, err)
			}
			if retry := preview(first); retry.ID != p.ID || !retry.AlreadyCommitted {
				t.Fatal("file idempotency lost", retry)
			}
			p2 := preview(second)
			if p2.ID == p.ID || p2.NewCount != 0 || p2.DuplicateCount != 1 {
				t.Fatal("container changed payment identity", p2)
			}
			result, err = s.Commit(p2.ID, p2.LedgerVersion, false)
			if err != nil || result.Added != 0 || result.Skipped != 1 || count(t, s, "transactions") != 1 {
				t.Fatal(result, err)
			}
			if repeated, err := s.Commit(p2.ID, p2.LedgerVersion, false); err != nil || repeated != result {
				t.Fatal(repeated, err)
			}
			if d := dashboard(t, s); d.ExpenseMinor != "1234" || d.IncomeMinor != "0" {
				t.Fatal(d)
			}
			for i, id := range []string{p.ID, p2.ID} {
				detail, err := s.ImportDetail(id)
				if err != nil || len(detail.Preview.SourceDocument.Records) != 4 {
					t.Fatal(detail, err)
				}
				var rawEnvelope string
				if err := s.db.QueryRow("SELECT raw_csv FROM imports WHERE id=?", id).Scan(&rawEnvelope); err != nil {
					t.Fatal(err)
				}
				var envelope struct{ Format, Account, Content string }
				if err := json.Unmarshal([]byte(rawEnvelope), &envelope); err != nil {
					t.Fatal(err)
				}
				expectedRaw := first
				if i == 1 {
					expectedRaw = second
				}
				if envelope.Account != "wallet" || envelope.Content != base64.StdEncoding.EncodeToString(expectedRaw) {
					t.Fatal("raw evidence changed")
				}
				if bytes.Equal(expectedRaw, xlsxBytes) && envelope.Format != "wechat-xlsx-v1" {
					t.Fatal("existing XLSX envelope changed", envelope.Format)
				}
				if bytes.Equal(expectedRaw, csvBytes) && (envelope.Format != "wechat-csv-v1" || detail.Preview.SourceDocument.Records[0].Line != 3) {
					t.Fatal("CSV evidence changed")
				}
			}
			other, err := s.PreviewWeChat("bill.csv", base64.StdEncoding.EncodeToString(csvBytes), "another-wallet")
			if err != nil || other.DuplicateCount != 0 || other.SimilarCount != 1 {
				t.Fatal(other, err)
			}
			changed := bytes.Replace(csvBytes, []byte("12.34"), []byte("12.35"), 1)
			conflict, err := s.PreviewWeChat("bill.csv", base64.StdEncoding.EncodeToString(changed), "wallet")
			if err != nil || len(conflict.Errors) == 0 {
				t.Fatal("changed payment silently accepted", conflict, err)
			}
			if _, err := s.Commit(conflict.ID, conflict.LedgerVersion, false); err == nil {
				t.Fatal("conflicting payment committed")
			}
		})
	}
}

func TestWeChatDamagedCSVRetainsEvidenceWithoutBookingPartialRows(t *testing.T) {
	s := testStore(t)
	raw, err := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.csv")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("unrecognized footer\n")...)
	p, err := s.PreviewWeChat("bill.xlsx", base64.StdEncoding.EncodeToString(raw), "wallet")
	if err != nil || len(p.Errors) == 0 || p.ID == "" || p.Format != "wechat" {
		t.Fatal(p, err)
	}
	if _, err := s.Commit(p.ID, p.LedgerVersion, false); err == nil {
		t.Fatal("partial file committed")
	}
	detail, err := s.ImportDetail(p.ID)
	if err != nil || detail.Preview.SourceDocument.Parser != "wechat-csv" || len(detail.Preview.SourceDocument.Records) != 4 {
		t.Fatal(detail, err)
	}
	if count(t, s, "transactions") != 0 || count(t, s, "source_records") != 0 {
		t.Fatal("failed CSV wrote booked evidence")
	}
}

func TestWeChatExistingXLSXBatchRemainsIdempotent(t *testing.T) {
	s := testStore(t)
	raw, err := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	content := base64.StdEncoding.EncodeToString(raw)
	// Seed exactly the envelope and extraction used before CSV support.
	envelope := encode(struct{ Format, Account, Content string }{"wechat-xlsx-v1", "wallet", content})
	document := ingestion.ParseWeChatXLSX(raw, "wallet")
	old, err := s.preview("old.xlsx", envelope, &document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit(old.ID, old.LedgerVersion, false); err != nil {
		t.Fatal(err)
	}
	current, err := s.PreviewWeChat("renamed.csv", content, "wallet")
	if err != nil || current.ID != old.ID || !current.AlreadyCommitted {
		t.Fatal("existing batch identity changed", current, err)
	}
	if count(t, s, "imports") != 1 || count(t, s, "transactions") != 1 {
		t.Fatal("old import duplicated")
	}
}
