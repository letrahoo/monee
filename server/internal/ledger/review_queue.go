package ledger

import (
	"encoding/json"
	"strconv"
)

// ReviewQueue exposes retained source questions, not financial transactions.
// Rows are intentionally not deduplicated across files: each is separate evidence.
type ReviewQueue struct {
	Items      []ReviewQueueItem `json:"items"`
	Page       int               `json:"page"`
	PageSize   int               `json:"pageSize"`
	TotalCount int               `json:"totalCount"`
	Format     string            `json:"format"`
}
type ReviewQueueItem struct {
	ID            string            `json:"id"`
	ImportID      string            `json:"importId"`
	Filename      string            `json:"filename"`
	Line          int               `json:"line"`
	Format        string            `json:"format"`
	SourceAccount string            `json:"sourceAccount"`
	BatchState    string            `json:"batchState"`
	Date          string            `json:"date"`
	Merchant      string            `json:"merchant"`
	Amount        string            `json:"amount"`
	Reason        string            `json:"reason"`
	Raw           map[string]string `json:"raw"`
}

// All retained native batches are eligible, including all-pending previews and
// failures with recoverable source rows. File-level parse errors have no pending
// row and remain visible through import history. Undo never deletes questions.
const reviewQueueFrom = ` FROM imports i JOIN json_each(i.preview_json,'$.pending') p
 WHERE i.ledger_id=? AND json_extract(i.preview_json,'$.format') IN ('alipay','wechat')
 AND (?='' OR json_extract(i.preview_json,'$.format')=?)
 AND NOT EXISTS (SELECT 1 FROM source_records sr WHERE sr.import_id=i.id AND sr.line=json_extract(p.value,'$.line'))`

func (s *Store) ReviewQueue(page int, format string) (ReviewQueue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := ReviewQueue{Items: []ReviewQueueItem{}, Page: page, PageSize: PageSize, Format: format}
	tx, e := s.db.Begin()
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = s.authorize(tx, false); e != nil {
		return out, e
	}
	if page < 1 || page > 1_000_000 {
		return out, problem("invalid", "分页参数无效")
	}
	if format != "" && format != "alipay" && format != "wechat" {
		return out, problem("invalid", "来源格式须为支付宝或微信")
	}
	if e = tx.QueryRow("SELECT COUNT(*)"+reviewQueueFrom, s.ledgerID, format, format).Scan(&out.TotalCount); e != nil {
		return out, e
	}
	// Extract only the requested source rows instead of returning the entire
	// source document or private raw-file envelope for each result.
	rows, e := tx.Query(`SELECT i.id,i.filename,json_extract(p.value,'$.line'),json_extract(i.preview_json,'$.format'),COALESCE(json_extract(i.preview_json,'$.sourceAccount'),''),
 CASE WHEN COALESCE((SELECT json_extract(payload_json,'$.state') FROM change_log
 WHERE ledger_id=i.ledger_id AND entity_type='import' AND entity_id=i.id
 AND operation IN ('import_undo','import_restore') ORDER BY sequence DESC LIMIT 1),'committed')='undone' THEN 'undone'
 WHEN i.committed_at IS NOT NULL THEN 'committed'
 WHEN COALESCE(json_array_length(i.preview_json,'$.errors'),0)>0 THEN 'failed' ELSE 'preview' END,
 COALESCE(json_extract(p.value,'$.date'),''),COALESCE(json_extract(p.value,'$.merchant'),''),
 COALESCE(json_extract(p.value,'$.amount'),''),COALESCE(json_extract(p.value,'$.reason'),''),
 COALESCE((SELECT json_extract(r.value,'$.raw') FROM json_each(i.preview_json,'$.sourceDocument.records') r
 WHERE json_extract(r.value,'$.line')=json_extract(p.value,'$.line') LIMIT 1),'{}')`+reviewQueueFrom+`
 ORDER BY i.created_at DESC,i.id DESC,CAST(json_extract(p.value,'$.line') AS INTEGER),CAST(p.key AS INTEGER) LIMIT ? OFFSET ?`, s.ledgerID, format, format, PageSize, (page-1)*PageSize)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var item ReviewQueueItem
		var raw string
		if e = rows.Scan(&item.ImportID, &item.Filename, &item.Line, &item.Format, &item.SourceAccount, &item.BatchState, &item.Date, &item.Merchant, &item.Amount, &item.Reason, &raw); e != nil {
			return out, e
		}
		item.ID = item.ImportID + ":" + strconv.Itoa(item.Line)
		item.Raw = map[string]string{}
		if e = json.Unmarshal([]byte(raw), &item.Raw); e != nil {
			return out, e
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}
