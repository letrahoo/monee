package ledger

import (
	"encoding/base64"
	"testing"
)

func TestNativeEvidenceStoreCommittedRejectsMissingOrCorruptSource(t *testing.T) {
	for name, mutate := range map[string]func(*Preview){
		"normalized amount conflict":       func(p *Preview) { p.Rows[0].Record.AmountMinor = "-999" },
		"unexpected financial association": func(p *Preview) { p.Rows[0].Record.RefundOf = "synthetic-other-transaction" },
		"source document missing":          func(p *Preview) { p.SourceDocument = nil },
		"unsupported original parser":      func(p *Preview) { p.SourceDocument.ParserVersion = 999 },
		"source records missing":           func(p *Preview) { p.SourceDocument.Records = nil },
		"format missing":                   func(p *Preview) { p.Format = "" },
		"source account missing":           func(p *Preview) { p.SourceAccount = "" },
		"original document hash bad":       func(p *Preview) { p.SourceDocument.ContentHash = "synthetic-secret" },
	} {
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			csv := nativeHeader + nativeExpense
			p := alipayPreview(t, s, []byte(csv), "synthetic-wallet")
			if _, e := s.Commit(p.ID, p.LedgerVersion, false); e != nil {
				t.Fatal(e)
			}
			mutate(&p)
			if _, e := s.db.Exec("UPDATE imports SET preview_json=? WHERE id=?", encode(p), p.ID); e != nil {
				t.Fatal(e)
			}
			nativeEvidenceConflict(t, s, func() error {
				_, e := s.PreviewAlipay("repeat.csv", base64.StdEncoding.EncodeToString([]byte(csv)), "synthetic-wallet")
				return e
			})
		})
	}
}
