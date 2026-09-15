// inspect-bill creates a private source-evidence bundle. It has no ledger access.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

func run(source, account, out string) error {
	if source == "" || account == "" || out == "" {
		return fmt.Errorf("-source, -account and -out are required")
	}
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, ingestion.MaxBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > ingestion.MaxBytes {
		return fmt.Errorf("file exceeds 2 MiB")
	}
	d := ingestion.ParseAlipayCSV(raw, account)
	encoded, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	// Exclusive directory creation prevents overwriting a previous evidence bundle.
	if err = os.Mkdir(out, 0700); err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(out)
		}
	}()
	if err = os.WriteFile(filepath.Join(out, "source.csv"), raw, 0600); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(out, "evidence.json"), append(encoded, '\n'), 0600); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(out, "README.txt"), []byte("支付宝来源解析包：不是入账结果。\n原文件和字段包含私有信息，不得提交 Git 或上传模型。\n收支、状态、退款、转账和付款方式尚未经过账本规则确认；所有记录只作来源证据。\n解析错误保存在 evidence.json，发生错误不代表完整解析成功。\n"), 0600); err != nil {
		return err
	}
	complete = true
	if len(d.Issues) > 0 {
		return fmt.Errorf("inspection incomplete: %d issues; private evidence retained in output directory", len(d.Issues))
	}
	fmt.Printf("Extracted %d source records (%s). No ledger changed.\n", len(d.Records), d.Encoding)
	return nil
}
func main() {
	source := flag.String("source", "", "native Alipay CSV")
	account := flag.String("account", "", "stable local source account alias")
	out := flag.String("out", "", "new private output directory")
	flag.Parse()
	if err := run(*source, *account, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
