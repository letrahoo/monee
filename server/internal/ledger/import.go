package ledger

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var headers = []string{"date", "type", "amount", "currency", "merchant", "category", "source", "account", "external_id", "note"}

func parseCSV(text string) ([]PreviewRow, []string) {
	result := []PreviewRow{}
	issues := []string{}
	if len(text) > MaxCSVBytes || !utf8.ValidString(text) || strings.ContainsRune(text, '\uFFFD') {
		return result, []string{"请使用不超过 2 MiB 的 UTF-8 CSV 文件"}
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, "\uFEFF")))
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return result, []string{"文件为空或 CSV 表头无效"}
	}
	positions := map[string]int{}
	for index, value := range head {
		value = strings.TrimSpace(value)
		known := false
		for _, h := range headers {
			if h == value {
				known = true
			}
		}
		if !known {
			return result, []string{"不支持的表头：" + value + "。请使用标准 CSV 模板"}
		}
		if _, exists := positions[value]; exists {
			return result, []string{"重复的表头：" + value}
		}
		positions[value] = index
	}
	for _, h := range []string{"date", "type", "amount", "currency", "merchant", "source"} {
		if _, ok := positions[h]; !ok {
			return result, []string{"缺少必要表头：" + h}
		}
	}
	for count := 0; ; count++ {
		fields, e := r.Read()
		if e == io.EOF {
			break
		}
		if count >= MaxRows {
			return result, []string{"单次最多导入 1000 行，请拆分文件"}
		}
		if e != nil {
			return result, []string{"CSV 格式错误：" + e.Error()}
		}
		line, _ := r.FieldPos(0)
		if len(fields) != len(head) {
			issues = append(issues, fmt.Sprintf("第 %d 行：列数与表头不一致", line))
			if len(issues) >= 20 {
				break
			}
			continue
		}
		get := func(name string) string {
			if index, ok := positions[name]; ok {
				return fields[index]
			}
			return ""
		}
		t, e := normalize(Input{get("date"), get("type"), get("amount"), get("currency"), get("merchant"), get("category"), get("source"), get("account"), get("external_id"), get("note")})
		if e != nil {
			issues = append(issues, fmt.Sprintf("第 %d 行：%s", line, e))
			if len(issues) >= 20 {
				break
			}
			continue
		}
		t.ID = newID()
		result = append(result, PreviewRow{Line: line, Record: t, Status: "new"})
	}
	if len(result) == 0 && len(issues) == 0 {
		issues = append(issues, "CSV 中没有账单记录")
	}
	return result, issues
}

func (s *Store) Preview(filename, text string) (Preview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := Preview{Filename: filepath.Base(filename), Rows: []PreviewRow{}, Errors: []string{}}
	if p.Filename == "" || p.Filename == "." {
		p.Filename = "import.csv"
	}
	if len([]rune(p.Filename)) > 200 {
		return p, problem("invalid", "文件名过长")
	}
	p.Rows, p.Errors = parseCSV(text)
	if len(p.Errors) > 0 {
		return p, nil
	}
	var err error
	if p.LedgerVersion, err = s.version(); err != nil {
		return p, err
	}
	contentHash := hash(text)
	var previousJSON string
	var committed sql.NullString
	err = s.db.QueryRow("SELECT id,preview_json,committed_at FROM imports WHERE ledger_id=? AND content_hash=?", s.ledgerID, contentHash).Scan(&p.ID, &previousJSON, &committed)
	if err == nil && committed.Valid {
		err = json.Unmarshal([]byte(previousJSON), &p)
		p.AlreadyCommitted = true
		return p, err
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	if p.ID == "" {
		p.ID = newID()
	}
	seenIDs := map[string]Transaction{}
	seenSimilar := map[string]bool{}
	for index := range p.Rows {
		row := &p.Rows[index]
		t := row.Record
		key := identity(t)
		var duplicate Transaction
		found := false
		if key != "" {
			if previous, ok := seenIDs[key]; ok {
				duplicate = previous
				found = true
			} else {
				duplicate, err = scanRecord(s.db.QueryRow("SELECT "+transactionColumns+" FROM transactions WHERE ledger_id=? AND identity_key=?", s.ledgerID, key))
				if err == nil {
					found = true
				} else if !errors.Is(err, sql.ErrNoRows) {
					return p, err
				}
			}
		}
		if found {
			if fingerprint(t) != fingerprint(duplicate) {
				row.Status = "conflict"
				row.Message = "相同来源流水号的数据不一致"
				p.Errors = append(p.Errors, fmt.Sprintf("第 %d 行：%s", row.Line, row.Message))
			} else {
				row.Status = "duplicate"
				row.Message = "相同来源流水已入账或在文件内重复，将跳过"
				row.Record.ID = duplicate.ID
				p.DuplicateCount++
			}
			continue
		}
		var similarCount int
		if err = s.db.QueryRow("SELECT COUNT(*) FROM transactions WHERE ledger_id=? AND similarity_key=? AND deleted_at IS NULL", s.ledgerID, similarity(t)).Scan(&similarCount); err != nil {
			return p, err
		}
		if similarCount > 0 || seenSimilar[similarity(t)] {
			row.Status = "similar"
			row.Message = "日期、金额、商户相同，可能与另一来源重复"
			p.SimilarCount++
		}
		p.NewCount++
		seenSimilar[similarity(t)] = true
		if key != "" {
			seenIDs[key] = t
		}
	}
	if len(p.Errors) > 0 {
		return p, nil
	}
	_, err = s.db.Exec(`INSERT INTO imports(id,ledger_id,filename,content_hash,raw_csv,parser_version,ledger_version,preview_json,created_at) VALUES(?,?,?,?,?,1,?,?,?)
 ON CONFLICT(ledger_id,content_hash) DO UPDATE SET ledger_version=excluded.ledger_version,preview_json=excluded.preview_json`, p.ID, s.ledgerID, p.Filename, contentHash, text, p.LedgerVersion, encode(p), now())
	return p, err
}

func (s *Store) Commit(id string, version int64, confirmSimilar bool) (CommitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := CommitResult{ImportID: id}
	tx, err := s.db.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var previewJSON string
	var previousResult sql.NullString
	err = tx.QueryRow("SELECT preview_json,result_json FROM imports WHERE id=? AND ledger_id=?", id, s.ledgerID).Scan(&previewJSON, &previousResult)
	if errors.Is(err, sql.ErrNoRows) {
		return result, problem("not_found", "找不到导入预览，请重新预览")
	}
	if err != nil {
		return result, err
	}
	if previousResult.Valid {
		err = json.Unmarshal([]byte(previousResult.String), &result)
		return result, err
	}
	var p Preview
	if err = json.Unmarshal([]byte(previewJSON), &p); err != nil {
		return result, err
	}
	var current int64
	if err = tx.QueryRow("SELECT version FROM ledgers WHERE id=?", s.ledgerID).Scan(&current); err != nil {
		return result, err
	}
	if current != version || p.LedgerVersion != version {
		return result, problem("conflict", "账本已变化，请重新预览后确认")
	}
	if len(p.Errors) > 0 {
		return result, problem("invalid", "预览存在错误，无法入账")
	}
	if p.SimilarCount > 0 && !confirmSimilar {
		return result, problem("conflict", "请先核实疑似重复，确认它们是独立交易")
	}
	links := []map[string]any{}
	for _, row := range p.Rows {
		disposition := "added"
		if row.Status == "duplicate" {
			result.Skipped++
			disposition = "duplicate"
		} else {
			if err = s.insert(tx, row.Record); err != nil {
				return result, err
			}
			result.Added++
		}
		if row.Record.Date[:7] > result.Month {
			result.Month = row.Record.Date[:7]
		}
		linkID := newID()
		if _, err = tx.Exec("INSERT INTO source_records VALUES(?,?,?,?,?,?)", linkID, id, row.Line, row.Record.ID, encode(row.Record), disposition); err != nil {
			return result, err
		}
		links = append(links, map[string]any{"id": linkID, "line": row.Line, "transactionId": row.Record.ID, "disposition": disposition})
	}
	if _, err = tx.Exec("UPDATE imports SET result_json=?,committed_at=? WHERE id=?", encode(result), now(), id); err != nil {
		return result, err
	}
	if _, err = tx.Exec("UPDATE ledgers SET version=version+1 WHERE id=?", s.ledgerID); err != nil {
		return result, err
	}
	if err = s.change(tx, "import", id, map[string]any{"result": result, "sourceLinks": links, "parserVersion": 1}); err != nil {
		return result, err
	}
	return result, tx.Commit()
}
