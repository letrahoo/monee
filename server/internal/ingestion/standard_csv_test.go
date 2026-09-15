package ingestion

import (
	"strings"
	"testing"
)

const header = "date,type,amount,currency,merchant,source,account,external_id,note\n"

func TestEvidencePreservesSourceAndMultilinePosition(t *testing.T) {
	text := "\uFEFF" + header + "2026-09-01,refund,1.001,CNY,商户,支付宝,卡A,order-1,\"第一行\n第二行\"\n2026-09-02,expense,12.30,CNY,商户,支付宝,卡A,order-2,\n"
	d := ParseStandardCSV(text)
	if len(d.Issues) != 0 || len(d.Records) != 2 {
		t.Fatalf("parse: %+v", d)
	}
	first := d.Records[0]
	if first.Fields.Amount != "1.001" || first.Fields.Type != "refund" || first.Fields.Note != "第一行\n第二行" {
		t.Fatal("source silently rewritten", first)
	}
	if first.Line != 2 || first.Ordinal != 1 || d.Records[1].Line != 4 || d.Records[1].Ordinal != 2 {
		t.Fatal("lost source position", d.Records)
	}
	if d.ContentHash != ParseStandardCSV(text).ContentHash || d.ContentHash == ParseStandardCSV(strings.TrimPrefix(text, "\uFEFF")).ContentHash {
		t.Fatal("hash is not original bytes")
	}
}

func TestIdentifierScopeAndMissingValues(t *testing.T) {
	id := Identifier{Kind: "payment_trace", Scope: "issuer/account", Value: "same-id"}
	key, err := id.MatchKey("ledger-a")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := id.MatchKey("ledger-b")
	if key == other {
		t.Fatal("cross-ledger identity collision")
	}
	for _, changed := range []Identifier{{"merchant_order", id.Scope, id.Value}, {id.Kind, "other-account", id.Value}, {id.Kind, id.Scope, "different"}} {
		next, _ := changed.MatchKey("ledger-a")
		if next == key {
			t.Fatal("identifier namespace collapsed")
		}
	}
	if _, err = id.MatchKey(""); err == nil {
		t.Fatal("missing ledger accepted")
	}
	if _, err = (Identifier{Kind: "trace"}).MatchKey("ledger-a"); err == nil {
		t.Fatal("incomplete identity accepted")
	}
	d := ParseStandardCSV(header + "2026-01-01,expense,1,CNY,商户,来源,,,\n")
	if len(d.Issues) != 0 || len(d.Records[0].Identifiers) != 0 {
		t.Fatal("invented external identity")
	}
}

func TestStructuralErrorsAndLimits(t *testing.T) {
	for name, text := range map[string]string{
		"unknown":   "date,type,amount,currency,merchant,source,surprise\n",
		"duplicate": "date,type,amount,currency,merchant,source,date\n",
		"missing":   "date,type\n",
		"columns":   header + "2026-01-01,expense\n",
		"quote":     header + "\"unfinished",
		"encoding":  header + string([]byte{0xff}),
		"size":      strings.Repeat("a", MaxBytes+1),
		"rows":      header + strings.Repeat("2026-01-01,expense,1,CNY,商户,来源,账户,id,\n", MaxRows+1),
	} {
		t.Run(name, func(t *testing.T) {
			d := ParseStandardCSV(text)
			if len(d.Issues) == 0 {
				t.Fatal("invalid document accepted")
			}
			if len(d.Records) > MaxRows {
				t.Fatal("row limit exceeded")
			}
		})
	}
}

func TestValidRowsAfterMalformedRecordKeepOrdinal(t *testing.T) {
	d := ParseStandardCSV(header + "bad,row\n2026-01-01,expense,1,CNY,商户,来源,账户,id,\n")
	if len(d.Issues) != 1 || len(d.Records) != 1 || d.Records[0].Ordinal != 2 || d.Records[0].Line != 3 {
		t.Fatal("lost issue/occurrence", d)
	}
}
