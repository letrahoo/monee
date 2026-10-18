package ledger

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

func TestNativeEvidenceStoreRetainsDistinctRawColumnNames(t *testing.T) {
	for _, keys := range [][2]string{{"extra", "EXTRA"}, {"scope", "ſcope"}} {
		t.Run(keys[0]+"-"+keys[1], func(t *testing.T) {
			csv := strings.TrimSuffix(nativeHeader, "\n") + "," + keys[0] + "," + keys[1] + "\n" + strings.TrimSuffix(nativeExpense, "\n") + ",a,b\n"
			s := testStore(t)
			p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
			if len(p.Errors) != 0 || len(p.Rows) != 1 || p.SourceDocument.Records[0].Raw[keys[0]] != "a" || p.SourceDocument.Records[0].Raw[keys[1]] != "b" {
				t.Fatal("fixture parser did not retain legal distinct raw keys", p)
			}
			again, e := s.PreviewAlipay("repeat.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
			if e != nil || !reflect.DeepEqual(again.SourceDocument, p.SourceDocument) {
				t.Fatal("valid raw map keys treated as struct aliases or dropped", again, e)
			}
		})
	}
}

func TestNativeEvidenceStoreSparseXLSXBlankCellRepeat(t *testing.T) {
	raw, e := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	input, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		t.Fatal(e)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	removed := false
	cell := regexp.MustCompile(`(?s)<c\b[^>]*\br="J2"[^>]*>.*?</c>`)
	for _, entry := range input.File {
		r, e := entry.Open()
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		if entry.Name == "xl/worksheets/sheet1.xml" {
			next := cell.ReplaceAll(data, nil)
			removed = !bytes.Equal(data, next)
			data = next
		}
		w, e := writer.Create(entry.Name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if e = writer.Close(); e != nil {
		t.Fatal(e)
	}
	if !removed {
		t.Fatal("fixture lacks expected merchant-order cell")
	}
	doc := ingestion.ParseWeChatXLSX(output.Bytes(), "synthetic-wallet")
	if len(doc.Issues) != 0 || len(doc.Records) == 0 {
		t.Fatal(doc.Issues)
	}
	if _, present := doc.Records[0].Raw["商户单号"]; present {
		t.Fatal("sparse source fixture did not omit blank cell")
	}
	s := testStore(t)
	content := base64.StdEncoding.EncodeToString(output.Bytes())
	p, e := s.PreviewWeChat("synthetic-sparse.xlsx", content, "synthetic-wallet")
	if e != nil || len(p.Errors) != 0 {
		t.Fatal(p, e)
	}
	again, e := s.PreviewWeChat("repeat.xlsx", content, "synthetic-wallet")
	if e != nil || !reflect.DeepEqual(p.SourceDocument, again.SourceDocument) || !reflect.DeepEqual(p.Pending, again.Pending) {
		t.Fatal("current sparse-XLSX parser output falsely drifted", again, e)
	}
	if _, present := again.SourceDocument.Records[0].Raw["商户单号"]; present {
		t.Fatal("guard backfilled missing original source cell")
	}
}
