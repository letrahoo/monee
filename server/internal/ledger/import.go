package ledger

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

// Extraction is shared by source adapters; financial normalization remains here.
var headers = ingestion.StandardColumns()

func parseCSV(text string) ([]PreviewRow, []string) {
	document := ingestion.ParseStandardCSV(text)
	result := []PreviewRow{}
	issues := []string{}
	for _, issue := range document.Issues {
		issues = append(issues, issue.Message)
	}
	for _, row := range document.Records {
		f := row.Fields
		t, err := normalize(Input{Date: f.Date, Type: f.Type, Amount: f.Amount, Currency: f.Currency, Merchant: f.Merchant, Category: f.Category, Source: f.Source, Account: f.Account, ExternalID: f.ExternalID, Note: f.Note})
		if err != nil {
			issues = append(issues, fmt.Sprintf("第 %d 行：%s", row.Line, err))
			if len(issues) >= 20 {
				break
			}
			continue
		}
		t.ID = newID()
		result = append(result, PreviewRow{Line: row.Line, Record: t, Status: "new"})
	}
	return result, issues
}

func (s *Store) Preview(filename, text string) (Preview, error) {
	return s.preview(filename, text, nil)
}

func (s *Store) preview(filename, text string, native *ingestion.Document) (Preview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.authorize(s.db, true); err != nil {
		return Preview{}, err
	}
	p := Preview{Filename: filepath.Base(filename), Rows: []PreviewRow{}, Errors: []string{}}
	if p.Filename == "" || p.Filename == "." {
		p.Filename = "import.csv"
	}
	if len([]rune(p.Filename)) > 200 {
		return p, problem("invalid", "文件名过长")
	}
	if native == nil {
		p.Rows, p.Errors = parseCSV(text)
	} else {
		p.Format = "alipay"
		if native.Parser == "wechat-xlsx" {
			p.Format = "wechat"
		}
		p.SourceDocument = native
		if len(native.Records) > 0 {
			p.SourceAccount = native.Records[0].Fields.Account
		}
		p.Rows, p.Pending = s.nativeRows(*native)
		for _, issue := range native.Issues {
			p.Errors = append(p.Errors, fmt.Sprintf("第 %d 行：%s", issue.Line, issue.Message))
		}
	}
	if len(p.Errors) > 0 {
		if native != nil {
			return s.retainFailedPreview(p, text)
		}
		return p, nil
	}
	var err error
	if p.LedgerVersion, err = s.version(); err != nil {
		return p, err
	}
	contentHash := hash(text)
	parserVersion := 1
	if native != nil {
		parserVersion = 2
	}
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
		originalFingerprint := ""
		if key != "" {
			if previous, ok := seenIDs[key]; ok {
				duplicate = previous
				found = true
			} else {
				duplicate, err = scanRecord(s.db.QueryRow("SELECT "+transactionColumns+" FROM transactions WHERE ledger_id=? AND identity_key=?", s.ledgerID, key))
				if err == nil {
					found = true
					if err = s.db.QueryRow("SELECT fingerprint FROM transactions WHERE ledger_id=? AND id=?", s.ledgerID, duplicate.ID).Scan(&originalFingerprint); err != nil {
						return p, err
					}
				} else if !errors.Is(err, sql.ErrNoRows) {
					return p, err
				}
			}
		}
		if found {
			if originalFingerprint == "" {
				originalFingerprint = fingerprint(duplicate)
			}
			if fingerprint(t) != originalFingerprint {
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
		if native != nil {
			return s.retainFailedPreview(p, text)
		}
		return p, nil
	}
	_, err = s.db.Exec(`INSERT INTO imports(id,ledger_id,filename,content_hash,raw_csv,parser_version,ledger_version,preview_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)
 ON CONFLICT(ledger_id,content_hash) DO UPDATE SET ledger_version=excluded.ledger_version,preview_json=excluded.preview_json`, p.ID, s.ledgerID, p.Filename, contentHash, text, parserVersion, p.LedgerVersion, encode(p), now())
	if err == nil && s.actorID != "" {
		_, err = s.db.Exec("UPDATE imports SET created_by=COALESCE(created_by,?) WHERE id=? AND ledger_id=?", s.actorID, p.ID, s.ledgerID)
	}
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
	if err = s.authorize(tx, true); err != nil {
		return result, err
	}
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
	if len(p.Rows) == 0 {
		return result, problem("invalid", "本批没有可入账记录，待核实记录已保留")
	}
	result.Pending = len(p.Pending)
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
	if err = s.change(tx, "import", id, map[string]any{"result": result, "sourceLinks": links, "parserVersion": func() int {
		if p.SourceDocument != nil {
			return 2
		}
		return 1
	}()}); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

// Failed native imports remain inspectable and can never be committed. Preserve
// the original envelope and error locations without creating financial records.
func (s *Store) retainFailedPreview(p Preview, raw string) (Preview, error) {
	version, err := s.version()
	if err != nil {
		return p, err
	}
	p.LedgerVersion = version
	err = s.db.QueryRow("SELECT id FROM imports WHERE ledger_id=? AND content_hash=?", s.ledgerID, hash(raw)).Scan(&p.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	if p.ID == "" {
		p.ID = newID()
	}
	_, err = s.db.Exec(`INSERT INTO imports(id,ledger_id,filename,content_hash,raw_csv,parser_version,ledger_version,preview_json,created_at) VALUES(?,?,?,?,?,2,?,?,?)
    ON CONFLICT(ledger_id,content_hash) DO UPDATE SET preview_json=excluded.preview_json,ledger_version=excluded.ledger_version WHERE imports.committed_at IS NULL`, p.ID, s.ledgerID, p.Filename, hash(raw), raw, version, encode(p), now())
	if err == nil && s.actorID != "" {
		_, err = s.db.Exec("UPDATE imports SET created_by=COALESCE(created_by,?) WHERE ledger_id=? AND id=?", s.actorID, s.ledgerID, p.ID)
	}
	return p, err
}
