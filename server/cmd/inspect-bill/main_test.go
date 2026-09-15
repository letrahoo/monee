package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

func TestPrivateBundleAndFailureEvidence(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "input.csv")
	out := filepath.Join(dir, "bundle")
	raw := []byte("支付宝交易明细\n交易时间,交易分类,交易对方,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号\n2026-09-01 10:00:00,退款,合成商户,测试,收入,12.34,余额,退款成功,trace-1,order-1\n")
	if err := os.WriteFile(src, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(src, "account-a", out); err != nil {
		t.Fatal(err)
	}
	if err := run(src, "account-a", out); err == nil {
		t.Fatal("overwrote existing bundle")
	}
	saved, _ := os.ReadFile(filepath.Join(out, "source.csv"))
	if string(saved) != string(raw) {
		t.Fatal("source bytes changed")
	}
	var d ingestion.Document
	b, _ := os.ReadFile(filepath.Join(out, "evidence.json"))
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Records) != 1 || d.Records[0].Raw["交易状态"] != "退款成功" {
		t.Fatal("refund evidence lost")
	}
	for _, name := range []string{"", "source.csv", "evidence.json", "README.txt"} {
		s, err := os.Stat(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if s.Mode().Perm()&0077 != 0 {
			t.Fatal("private output accessible to other users")
		}
	}
	if err := os.WriteFile(src, []byte("unsupported format"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "failed")
	if err := run(src, "a", bad); err == nil {
		t.Fatal("reported malformed file as success")
	}
	b, _ = os.ReadFile(filepath.Join(bad, "evidence.json"))
	if err := json.Unmarshal(b, &d); err != nil || len(d.Issues) == 0 {
		t.Fatal("failure evidence not retained")
	}
}
