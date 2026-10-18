package ledger

import (
	"strings"
	"testing"
)

func TestReviewItemMetadataUnicodeSemanticDuplicate(t *testing.T) {
	p, snapshot := metadataFixture()
	snapshot.previewJSON = strings.Replace(encode(p), `"sourceAccount":"synthetic-wallet"`, `"sourceAccount":"wrong-wallet","ſourceAccount":"synthetic-wallet"`, 1)
	out, err := evaluateReviewItemMetadata(p.ID, 2, snapshot)
	if err != nil || !metadataHas(out, "snapshot_invalid") || out.Classification != "unknown" {
		t.Fatalf("Unicode-equivalent source keys must fail closed: %+v, %v", out, err)
	}
	_, snapshot = metadataFixture()
	snapshot.committed = true
	snapshot.eventVersion = 1
	snapshot.eventOperation = "import_undo"
	snapshot.eventJSON = `{"state":"committed","ſtate":"undone","changes":{}}`
	out, err = evaluateReviewItemMetadata(p.ID, 2, snapshot)
	if err != nil || !metadataHas(out, "batch_event_invalid") || out.BatchState != "unknown" {
		t.Fatalf("Unicode-equivalent event keys must fail closed: %+v, %v", out, err)
	}
}

func TestReviewItemMetadataJSONUsesUnicodeSimpleFold(t *testing.T) {
	for _, raw := range []string{`{"key":"a","Key":"b"}`, `{"key":"a","KEY":"b"}`} {
		var target struct {
			Key string `json:"key"`
		}
		if reviewMetadataJSON(raw, &target) {
			t.Fatalf("accepted equivalent key aliases: %s", raw)
		}
	}
	var target struct {
		Sigma string `json:"σ"`
	}
	if reviewMetadataJSON(`{"σ":"a","ς":"b"}`, &target) {
		t.Fatal("accepted equivalent sigma aliases")
	}
}
