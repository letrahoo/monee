package ledger

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/redaction"
)

func redactionProjector(t *testing.T) *redaction.Projector {
	t.Helper()
	p, err := redaction.New(bytes.Repeat([]byte{32}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportRedactionPreviewIsReadOnlyPrivateAndStable(t *testing.T) {
	s := testStore(t)
	invalidAmount := strings.Replace(strings.Replace(nativeExpense, "12.34", "1.001", 1), "pay-1", "pay-bad", 1)
	invalidDate := strings.Replace(strings.Replace(nativeExpense, "2026-09-15", "1899-09-15", 1), "pay-1", "pay-date", 1)
	raw := nativeHeader + nativeExpense + nativeRefund + invalidAmount + invalidDate
	p := alipayPreview(t, s, []byte(raw), "private-wallet-13800000000")
	projector := redactionProjector(t)
	before := dashboard(t, s)
	preview, err := s.ImportRedactionPreview(p.ID, projector)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.LocalOnly || preview.ImportID != p.ID || preview.IncludedCount != 2 || len(preview.Excluded) != 2 || len(preview.Treatments) != 2 {
		t.Fatal("wrong preview counts", preview)
	}
	if preview.Excluded[0].Line != 4 || preview.Excluded[1].Line != 5 {
		t.Fatal("lost source positions", preview.Excluded)
	}
	encoded := string(preview.Payload)
	for _, forbidden := range []string{"private-wallet", "13800000000", "合成咖啡店", "合成退款商户", "早餐", "退货", "银行卡A", "pay-1", "order-1", "10:20:30", "alipay.csv", p.ID, "1.001", "1899"} {
		if strings.Contains(encoded, forbidden) {
			t.Errorf("leaked private/invalid field %q", forbidden)
		}
	}
	var payload struct {
		Records []struct{ Date, AmountMinor, Direction, Source, Currency string }
	}
	if err := json.Unmarshal(preview.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Records) != 2 || payload.Records[0].AmountMinor != "1234" || payload.Records[0].Direction != "expense" || payload.Records[1].Direction != "unknown" {
		t.Fatal("altered financial evidence", payload)
	}
	again, err := s.ImportRedactionPreview(p.ID, projector)
	if err != nil || !bytes.Equal(preview.Payload, again.Payload) {
		t.Fatal("preview unstable", err)
	}
	after := dashboard(t, s)
	if before.Version != after.Version || after.TotalCount != 0 || count(t, s, "source_records") != 0 {
		t.Fatal("preview changed ledger")
	}
	// Committing doesn't change the projection: this is source evidence, not a
	// projection of mutable live transactions.
	if _, err := s.Commit(p.ID, p.LedgerVersion, false); err != nil {
		t.Fatal(err)
	}
	committed, err := s.ImportRedactionPreview(p.ID, projector)
	if err != nil || !bytes.Equal(preview.Payload, committed.Payload) {
		t.Fatal("commit changed source projection", err)
	}
}

func TestRedactionPreviewExcludesInvalidAndRejectsIncompleteSources(t *testing.T) {
	s := testStore(t)
	projector := redactionProjector(t)
	p := alipayPreview(t, s, []byte(nativeHeader+strings.Replace(nativeExpense, "12.34", "1.001", 1)), "wallet")
	result, err := s.ImportRedactionPreview(p.ID, projector)
	if err != nil || result.IncludedCount != 0 || len(result.Excluded) != 1 || !strings.Contains(string(result.Payload), `"records":[]`) {
		t.Fatal(result, err)
	}
	if _, err := s.ImportRedactionPreview(p.ID, nil); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := s.ImportRedactionPreview("missing", projector); err == nil {
		t.Fatal("missing batch accepted")
	}
	broken := alipayPreview(t, s, []byte(nativeHeader+nativeExpense+"bad,row\n"), "wallet")
	if _, err := s.ImportRedactionPreview(broken.ID, projector); err == nil {
		t.Fatal("partial malformed file accepted")
	}
	standard, err := s.Preview("standard.csv", "date,type,amount,currency,merchant,source\n2026-09-17,expense,1.00,CNY,synthetic,synthetic\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportRedactionPreview(standard.ID, projector); err == nil {
		t.Fatal("unrecognized source guessed")
	}
}

func TestRedactionPreviewWeChatCSVAndXLSX(t *testing.T) {
	for _, format := range []string{"csv", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			raw, err := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review." + format)
			if err != nil {
				t.Fatal(err)
			}
			s := testStore(t)
			p, err := s.PreviewWeChat("private-name."+format, base64.StdEncoding.EncodeToString(raw), "private-wallet")
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.ImportRedactionPreview(p.ID, redactionProjector(t))
			if err != nil || result.IncludedCount != 3 || len(result.Excluded) != 1 {
				t.Fatal(result, err)
			}
			var data struct {
				Records []struct{ Source, Direction, AmountMinor string }
			}
			if err = json.Unmarshal(result.Payload, &data); err != nil {
				t.Fatal(err)
			}
			if data.Records[0].Source != "wechat" || data.Records[0].AmountMinor != "1234" || data.Records[0].Direction != "expense" || data.Records[1].Direction != "unknown" || data.Records[2].Direction != "unknown" {
				t.Fatal(data)
			}
			if strings.Contains(string(result.Payload), "private-name") || strings.Contains(string(result.Payload), "private-wallet") {
				t.Fatal("private source leaked")
			}
		})
	}
}
