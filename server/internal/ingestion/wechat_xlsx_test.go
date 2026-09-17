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

func editWeChatPackage(t *testing.T, raw []byte, edit func(map[string][]byte)) []byte {
	t.Helper()
	r, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		t.Fatal(e)
	}
	parts := map[string][]byte{}
	for _, f := range r.File {
		reader, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		parts[f.Name], e = io.ReadAll(reader)
		reader.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	edit(parts)
	var result bytes.Buffer
	w := zip.NewWriter(&result)
	for name, data := range parts {
		writer, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = writer.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	return result.Bytes()
}

func TestWeChatRejectsIncompleteOrAmbiguousWorkbook(t *testing.T) {
	const workbook = "xl/workbook.xml"
	const relations = "xl/_rels/workbook.xml.rels"
	const contentTypes = "[Content_Types].xml"
	const sheet = "xl/worksheets/sheet1.xml"
	replace := func(part, old, next string) func(map[string][]byte) {
		return func(parts map[string][]byte) {
			parts[part] = bytes.Replace(parts[part], []byte(old), []byte(next), 1)
		}
	}
	for name, edit := range map[string]func(map[string][]byte){
		"missing workbook":                               func(p map[string][]byte) { delete(p, workbook) },
		"missing relationship file":                      func(p map[string][]byte) { delete(p, relations) },
		"missing content types":                          func(p map[string][]byte) { delete(p, contentTypes) },
		"missing sheet part":                             func(p map[string][]byte) { delete(p, sheet) },
		"corrupt workbook":                               func(p map[string][]byte) { p[workbook] = []byte("<workbook>") },
		"corrupt relationships":                          func(p map[string][]byte) { p[relations] = []byte("<Relationships>") },
		"missing sheet relationship":                     replace(workbook, `r:id="rId1"`, `r:id="missing"`),
		"unnamespaced sheet relationship":                replace(workbook, `r:id="rId1"`, `id="rId1"`),
		"missing relationship target":                    replace(relations, `Target="worksheets/sheet1.xml"`, ``),
		"misdirected relationship":                       replace(relations, `Target="worksheets/sheet1.xml"`, `Target="worksheets/missing.xml"`),
		"external relationship":                          replace(relations, `Target="worksheets/sheet1.xml"`, `Target="worksheets/sheet1.xml" TargetMode="External"`),
		"remote relationship":                            replace(relations, `Target="worksheets/sheet1.xml"`, `Target="https://example.invalid/sheet1.xml"`),
		"wrong relationship type":                        replace(relations, `/relationships/worksheet"`, `/relationships/styles"`),
		"wrong worksheet content type":                   replace(contentTypes, `application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml`, `application/xml`),
		"undeclared second worksheet":                    func(p map[string][]byte) { p["xl/worksheets/other.xml"] = p[sheet] },
		"unreferenced worksheet relation":                replace(relations, `</Relationships>`, `<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`),
		"duplicate relationship ID":                      replace(relations, `</Relationships>`, `<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`),
		"extra worksheet outside conventional directory": replace(contentTypes, `</Types>`, `<Override PartName="/other.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`),
		"duplicate worksheet content type":               replace(contentTypes, `</Types>`, `<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`),
		"declared second worksheet with arbitrary part name": func(p map[string][]byte) {
			replace(workbook, `</sheets>`, `<sheet name="Second" sheetId="2" r:id="rId2"/></sheets>`)(p)
			replace(relations, `</Relationships>`, `<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/other.xml"/></Relationships>`)(p)
			replace(contentTypes, `</Types>`, `<Override PartName="/xl/worksheets/other.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`)(p)
			p["xl/worksheets/other.xml"] = p[sheet]
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := ParseWeChatXLSX(editWeChatPackage(t, wechatFixture(t), edit), "wallet")
			if len(d.Issues) == 0 || len(d.Records) != 0 {
				t.Fatal("ambiguous package must be rejected before reading any rows", d)
			}
		})
	}
}

func TestWeChatAcceptsPackageAbsoluteWorksheetTarget(t *testing.T) {
	raw := editWeChatPackage(t, wechatFixture(t), func(parts map[string][]byte) {
		const name = "xl/_rels/workbook.xml.rels"
		parts[name] = bytes.Replace(parts[name], []byte(`Target="worksheets/sheet1.xml"`), []byte(`Target="/xl/worksheets/sheet1.xml" TargetMode="Internal"`), 1)
	})
	d := ParseWeChatXLSX(raw, "wallet")
	if len(d.Issues) != 0 || len(d.Records) != 4 {
		t.Fatal("valid package-absolute worksheet relationship rejected", d)
	}
}
