package ledger

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
)

// Compare logical schema and rows, not SQLite file bytes (VACUUM changes page
// layout). Row order is immaterial; duplicates and SQLite value types remain.
func expensePairUpgradeDigest(q interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}) ([32]byte, error) {
	var zero [32]byte
	rows, err := q.Query("SELECT name,type,tbl_name,sql FROM sqlite_schema ORDER BY name,type")
	if err != nil {
		return zero, err
	}
	var schema [][][]byte
	var tables []string
	for rows.Next() {
		var name, kind, table string
		var ddl interface{}
		if err = rows.Scan(&name, &kind, &table, &ddl); err != nil {
			rows.Close()
			return zero, err
		}
		text := ""
		if ddl != nil {
			text = ddl.(string)
		}
		schema = append(schema, [][]byte{[]byte(name), []byte(kind), []byte(table), []byte(text)})
		if kind == "table" {
			tables = append(tables, name)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	all := [][]byte{}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return zero, err
	}
	all = append(all, encoded)
	for _, table := range tables {
		rows, err = q.Query(`SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`)
		if err != nil {
			return zero, err
		}
		cols, e := rows.Columns()
		if e != nil {
			rows.Close()
			return zero, e
		}
		records := []string{}
		for rows.Next() {
			values := make([]interface{}, len(cols))
			ptr := make([]interface{}, len(cols))
			for i := range values {
				ptr[i] = &values[i]
			}
			if e = rows.Scan(ptr...); e != nil {
				rows.Close()
				return zero, e
			}
			// Tag dynamic types to distinguish BLOB from TEXT and integers from strings.
			typed := make([]interface{}, 0, len(values)*2)
			for _, v := range values {
				kind := "null"
				switch v.(type) {
				case int64:
					kind = "integer"
				case float64:
					kind = "real"
				case string:
					kind = "text"
					// Encode bytes, not JSON text: SQLite permits invalid UTF-8.
					v = []byte(v.(string))
				case []byte:
					kind = "blob"
				case bool:
					kind = "bool"
				}
				typed = append(typed, kind, v)
			}
			b, e := json.Marshal(typed)
			if e != nil {
				rows.Close()
				return zero, e
			}
			records = append(records, string(b))
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return zero, e
		}
		sort.Strings(records)
		columnBytes := make([][]byte, len(cols))
		for i, col := range cols {
			columnBytes[i] = []byte(col)
		}
		b, e := json.Marshal([]interface{}{[]byte(table), columnBytes, records})
		if e != nil {
			return zero, e
		}
		all = append(all, b)
	}
	data, err := json.Marshal(all)
	if err != nil {
		return zero, err
	}
	return sha256.Sum256(data), nil
}
