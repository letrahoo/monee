package ledger

import (
	"database/sql"
	"encoding/json"
	"errors"
)

type ImportSummary struct {
	Undone        bool          `json:"undone"`
	Errors        int           `json:"errors"`
	Pending       int           `json:"pending"`
	ID            string        `json:"id"`
	Filename      string        `json:"filename"`
	ParserVersion int           `json:"parserVersion"`
	CreatedAt     string        `json:"createdAt"`
	CommittedAt   *string       `json:"committedAt"`
	Result        *CommitResult `json:"result"`
}

type ImportHistory struct {
	Imports    []ImportSummary `json:"imports"`
	Page       int             `json:"page"`
	PageSize   int             `json:"pageSize"`
	TotalCount int             `json:"totalCount"`
}

type SourceEvidence struct {
	ID            string      `json:"id"`
	Line          int         `json:"line"`
	TransactionID string      `json:"transactionId"`
	Disposition   string      `json:"disposition"`
	Record        Transaction `json:"record"`
}

type ImportDetail struct {
	ImportSummary
	Preview  Preview          `json:"preview"`
	Evidence []SourceEvidence `json:"evidence"`
}

func scanImport(row interface{ Scan(...any) error }) (ImportSummary, error) {
	var item ImportSummary
	var committed, result sql.NullString
	err := row.Scan(&item.ID, &item.Filename, &item.ParserVersion, &item.CreatedAt, &committed, &result, &item.Errors, &item.Pending, &item.Undone)
	if err != nil {
		return item, err
	}
	if committed.Valid {
		item.CommittedAt = &committed.String
	}
	if result.Valid {
		item.Result = &CommitResult{}
		err = json.Unmarshal([]byte(result.String), item.Result)
	}
	return item, err
}

const importSummaryColumns = "id,filename,parser_version,created_at,committed_at,result_json,COALESCE(json_array_length(preview_json,'$.errors'),0),COALESCE(json_array_length(preview_json,'$.pending'),0),COALESCE((SELECT json_extract(payload_json,'$.state')='undone' FROM change_log WHERE ledger_id=imports.ledger_id AND entity_type='import' AND entity_id=imports.id AND operation IN ('import_undo','import_restore') ORDER BY sequence DESC LIMIT 1),0)"

func (s *Store) ImportHistory(page int) (ImportHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := ImportHistory{Imports: []ImportSummary{}, Page: page, PageSize: PageSize}
	if err := s.authorize(s.db, false); err != nil {
		return out, err
	}
	if page < 1 || page > 1_000_000 {
		return out, problem("invalid", "分页参数无效")
	}
	// Read count and rows from a single snapshot, including when another
	// connection updates the database between queries.
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = s.authorize(tx, false); err != nil {
		return out, err
	}
	if err = tx.QueryRow("SELECT COUNT(*) FROM imports WHERE ledger_id=?", s.ledgerID).Scan(&out.TotalCount); err != nil {
		return out, err
	}
	rows, err := tx.Query("SELECT "+importSummaryColumns+" FROM imports WHERE ledger_id=? ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", s.ledgerID, PageSize, (page-1)*PageSize)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		item, e := scanImport(rows)
		if e != nil {
			return out, e
		}
		out.Imports = append(out.Imports, item)
	}
	return out, rows.Err()
}

// ImportDetail exposes normalized source evidence to ledger members only.
// raw_csv and the original file are deliberately not part of this response.
func (s *Store) ImportDetail(id string) (ImportDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := ImportDetail{Evidence: []SourceEvidence{}}
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = s.authorize(tx, false); err != nil {
		return out, err
	}
	out.ImportSummary, err = scanImport(tx.QueryRow("SELECT "+importSummaryColumns+" FROM imports WHERE ledger_id=? AND id=?", s.ledgerID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return out, problem("not_found", "找不到此账本的导入记录")
	}
	if err != nil {
		return out, err
	}
	var preview string
	if err = tx.QueryRow("SELECT preview_json FROM imports WHERE ledger_id=? AND id=?", s.ledgerID, id).Scan(&preview); err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(preview), &out.Preview); err != nil {
		return out, err
	}
	out.Preview.AlreadyCommitted = out.CommittedAt != nil
	out.Preview.Undone = out.Undone
	rows, err := tx.Query(`SELECT sr.id,sr.line,sr.transaction_id,sr.disposition,sr.normalized_json
 FROM source_records sr JOIN imports i ON i.id=sr.import_id
 WHERE i.ledger_id=? AND i.id=? ORDER BY sr.line,sr.id`, s.ledgerID, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e SourceEvidence
		var record string
		if err = rows.Scan(&e.ID, &e.Line, &e.TransactionID, &e.Disposition, &record); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(record), &e.Record); err != nil {
			return out, err
		}
		out.Evidence = append(out.Evidence, e)
	}
	return out, rows.Err()
}

type TransactionSource struct {
	ImportID    string `json:"importId"`
	Filename    string `json:"filename"`
	Line        int    `json:"line"`
	Disposition string `json:"disposition"`
}

func (s *Store) TransactionSources(id string) ([]TransactionSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []TransactionSource{}
	if e := s.authorize(s.db, false); e != nil {
		return out, e
	}
	var found int
	if e := s.db.QueryRow("SELECT COUNT(*) FROM transactions WHERE ledger_id=? AND id=?", s.ledgerID, id).Scan(&found); e != nil {
		return out, e
	}
	if found == 0 {
		return out, problem("not_found", "找不到该交易")
	}
	rows, e := s.db.Query(`SELECT i.id,i.filename,sr.line,sr.disposition FROM source_records sr JOIN imports i ON i.id=sr.import_id WHERE i.ledger_id=? AND sr.transaction_id=? ORDER BY i.created_at DESC,i.id,sr.line LIMIT 100`, s.ledgerID, id)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var row TransactionSource
		if e = rows.Scan(&row.ImportID, &row.Filename, &row.Line, &row.Disposition); e != nil {
			return out, e
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
