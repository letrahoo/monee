package ingestion

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func wechatFixture(t *testing.T) []byte {
	t.Helper()
	b, e := os.ReadFile("../../../fixtures/synthetic/wechat/ordinary-and-review.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func rewriteXLSX(t *testing.T, raw []byte, mutate func(string, string) string, extra bool) []byte {
	t.Helper()
	in, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, f := range in.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		a, _ := w.Create(f.Name)
		a.Write([]byte(mutate(f.Name, string(b))))
	}
	if extra {
		a, _ := w.Create("xl/worksheets/sheet2.xml")
		a.Write([]byte("<worksheet/>"))
	}
	w.Close()
	return out.Bytes()
}
func TestWeChatPreservesEvidenceAndDecimalStrings(t *testing.T) {
	d := ParseWeChatXLSX(wechatFixture(t), "wallet-a")
	if len(d.Issues) != 0 || len(d.Records) != 4 {
		t.Fatal(d.Issues)
	}
	r := d.Records[0]
	if r.Line != 2 || r.Fields.Amount != "12.34" || r.Identifiers[0].Kind != "wechat_payment_trace" || r.Raw["备注"] != "合成验收" {
		t.Fatal(r)
	}
	if d.Records[3].Fields.Amount != "1.001" {
		t.Fatal("rounded source amount")
	}
	a, _ := r.Identifiers[0].MatchKey("ledger-a")
	other := ParseWeChatXLSX(wechatFixture(t), "wallet-b")
	b, _ := other.Records[0].Identifiers[0].MatchKey("ledger-a")
	if a == b {
		t.Fatal("wallet identity leaked")
	}
}
func TestWeChatRejectsCorruptAmbiguousAndFormulaFiles(t *testing.T) {
	raw := wechatFixture(t)
	for name, change := range map[string]func(string, string) string{
		"formula": func(n, s string) string { return strings.Replace(s, "<is><t>12.34</t></is>", "<f>1+1</f><v>2</v>", 1) },
		"numeric-id": func(n, s string) string {
			return strings.Replace(s, `<c r="I2" t="inlineStr"><is><t>wx-test-001</t></is></c>`, `<c r="I2"><v>12345678901234567890</v></c>`, 1)
		},
		"missing-header": func(n, s string) string { return strings.Replace(s, "当前状态", "未知状态", 1) },
		"duplicate-cell": func(n, s string) string { return strings.Replace(s, `r="B2"`, `r="A2"`, 1) },
		"corrupt": func(n, s string) string {
			if strings.Contains(n, "sheet1") {
				return "<broken>"
			}
			return s
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := ParseWeChatXLSX(rewriteXLSX(t, raw, change, false), "wallet")
			if len(d.Issues) == 0 {
				t.Fatal("accepted", name)
			}
		})
	}
	d := ParseWeChatXLSX(rewriteXLSX(t, raw, func(n, s string) string { return s }, true), "wallet")
	if len(d.Issues) == 0 {
		t.Fatal("multiple sheets accepted")
	}
	for _, raw := range [][]byte{[]byte("not xlsx"), bytes.Repeat([]byte("x"), MaxBytes+1)} {
		if len(ParseWeChatXLSX(raw, "wallet").Issues) == 0 {
			t.Fatal("invalid file accepted")
		}
	}
}
