package ledger

import (
	"encoding/base64"
	"os"
	"reflect"
	"testing"
)

// Keep the planned real-UI input executable even when GUI interaction is not
// available. This test is not a substitute for authenticated runtime acceptance.
func TestReviewItemMetadataAllPendingSyntheticFixture(t *testing.T) {
	contents, err := os.ReadFile("../../../fixtures/synthetic/review-metadata-pending.csv")
	if err != nil {
		t.Fatal(err)
	}
	s := testStore(t)
	before, err := s.Dashboard("2026-10", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PreviewAlipay("review-metadata-pending.csv", base64.StdEncoding.EncodeToString(contents), "synthetic-source-metadata")
	if err != nil || len(p.Errors) != 0 || len(p.Rows) != 0 || len(p.Pending) != 2 || p.NewCount != 0 || p.AlreadyCommitted {
		t.Fatal(p, err)
	}
	q, err := s.ReviewQueue(1, "alipay")
	if err != nil || q.TotalCount != 2 || len(q.Items) != 2 {
		t.Fatal(q, err)
	}
	if q.Items[0].Line != 2 || q.Items[1].Line != 3 || q.Items[0].ImportID != p.ID || q.Items[1].ImportID != p.ID {
		t.Fatal("expected both distinct fixture source lines", q.Items)
	}
	for _, item := range q.Items {
		m, err := s.ReviewItemMetadata(item.ImportID, item.Line)
		if err != nil || !m.ReadOnly || m.Actions == nil || len(m.Actions) != 0 || m.Facts.SourceAccount != "synthetic-source-metadata" {
			t.Fatal(m, err)
		}
		switch item.Line {
		case 2:
			if m.Classification != "special" || !metadataHas(m, "special_source") || !metadataHas(m, "amount_invalid") || item.Amount != "12.345" {
				t.Fatal("original refund lost to overridden amount reason", m, item)
			}
		case 3:
			if m.Classification != "unknown" || !metadataHas(m, "same_merchant_refund") {
				t.Fatal("same-merchant original became eligible", m)
			}
		default:
			t.Fatal("unexpected source line", item.Line)
		}
		repeated, err := s.ReviewItemMetadata(item.ImportID, item.Line)
		if err != nil || !reflect.DeepEqual(repeated, m) {
			t.Fatal("unstable metadata GET", repeated, err)
		}
	}
	after, err := s.Dashboard("2026-10", "", 1)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("all-pending preview/metadata changed finance", before, after, err)
	}
}
