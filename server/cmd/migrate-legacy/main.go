// migrate-legacy prepares private CSV candidates and a complete source archive.
// It deliberately has no database, session, authentication or commit option.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/letrahoo/monee/server/internal/ledger"
)

func run(source, out string) error {
	if source == "" || out == "" {
		return fmt.Errorf("both -snapshot and -out are required; output must be a new private directory")
	}
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 64<<20+1))
	if err != nil {
		return err
	}
	if len(raw) > 64<<20 {
		return fmt.Errorf("snapshot exceeds 64 MiB")
	}
	plan, err := ledger.PrepareLegacy(raw)
	if err != nil {
		return err
	}
	// Exclusive creation prevents accidental overwrite of a previous bundle.
	if err = os.Mkdir(out, 0700); err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(out)
		}
	}()
	write := func(name string, b []byte) error { return os.WriteFile(filepath.Join(out, name), b, 0600) }
	if err = write("source-snapshot.json", raw); err != nil {
		return err
	}
	for _, file := range plan.Files {
		if err = write(file.Name, []byte(file.CSV)); err != nil {
			return err
		}
	}
	manifest, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if err = write("manifest.json", append(manifest, '\n')); err != nil {
		return err
	}
	instructions := `迁移候选包（不是已入账账本）

source-snapshot.json 保留全部旧版流水、来源证据、去重信息和事件分析。
manifest.json 按旧流水 ID 记录 candidate（可预览）、review（需核实）、archive（保留未入账）。
候选只含普通人民币消费；旧版收入、退款及疑似退款关联不自动转换。
旧版排除的来源与流水沿用为隔离记录，不表示已认定应永久排除。

登录当前 Monee，逐个选择 candidates-*.csv，预览后按现有确认流程入账。
遇到疑似重复必须逐笔核实，不能为了迁移而统一确认独立交易。
每批确认后再预览下一批。重跑相同快照使用稳定来源 ID，原流水不会重复入账。
旧快照内容变更后可能发生身份冲突，须核实，不能修改 ID 绕过。
未来导入原生账单时仍需核查与这些历史流水的重叠。

金额总计只代表候选消费子集，不是旧版消费总额、净支出或资产余额。
保留该私有目录以追溯原始证据。不得提交 Git、上传公共站点或放入 Web 静态目录。
当前没有应用内撤销，确认前应核实预览。
`
	if err = write("README.txt", []byte(instructions)); err != nil {
		return err
	}
	complete = true
	fmt.Printf("Prepared %d candidates in %d files; %d review; %d archived. No ledger was changed.\n", plan.Candidates, len(plan.Files), plan.Review, plan.Archived)
	return nil
}
func main() {
	source := flag.String("snapshot", "", "legacy product-snapshot.json")
	out := flag.String("out", "", "new private output directory")
	flag.Parse()
	if err := run(*source, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
