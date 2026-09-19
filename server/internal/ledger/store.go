package ledger

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/letrahoo/monee/server/internal/storage"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001.sql
var schema string

//go:embed migrations/002.sql
var schema2 string

type Store struct {
	db                 *sql.DB
	mu                 *sync.Mutex
	ledgerID, deviceID string
	actorID            string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, mu: &sync.Mutex{}}
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version > storage.SchemaVersion {
		return fail(problem("future_schema", "数据库来自更新版本，拒绝以旧程序写入"))
	}
	if version > 0 && version < storage.SchemaVersion {
		if err = storage.Snapshot(path, path+".backup-before-v"+strconv.Itoa(storage.SchemaVersion)+"-"+newID()); err != nil {
			return fail(err)
		}
	}
	if version == 0 {
		tx, e := db.Begin()
		if e != nil {
			return fail(e)
		}
		defer tx.Rollback()
		if _, e = tx.Exec(schema); e != nil {
			tx.Rollback()
			return fail(e)
		}
		if _, e = tx.Exec("INSERT INTO ledgers VALUES(?, 'CNY','Asia/Shanghai',0,?,?)", newID(), newID(), now()); e != nil {
			tx.Rollback()
			return fail(e)
		}
		if e = tx.Commit(); e != nil {
			return fail(e)
		}
	}
	if version < 2 {
		tx, e := db.Begin()
		if e != nil {
			return fail(e)
		}
		if _, e = tx.Exec(storage.IdentitySchema + schema2); e != nil {
			tx.Rollback()
			return fail(e)
		}
		if e = tx.Commit(); e != nil {
			return fail(e)
		}
	}
	if version < 3 {
		if err = migrateRefundSchema(db); err != nil {
			return fail(err)
		}
	}
	if version < 4 {
		if err = migrateTransferSchema(db); err != nil {
			return fail(err)
		}
	}
	if err = db.QueryRow("SELECT id,device_id FROM ledgers ORDER BY created_at,id LIMIT 1").Scan(&s.ledgerID, &s.deviceID); err != nil {
		return fail(err)
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) version() (int64, error) {
	var v int64
	err := s.db.QueryRow("SELECT version FROM ledgers WHERE id=?", s.ledgerID).Scan(&v)
	return v, err
}

func (s *Store) change(tx *sql.Tx, entityType, id string, payload any) error {
	_, err := tx.Exec("INSERT INTO change_log(change_id,ledger_id,device_id,entity_id,entity_type,base_version,entity_version,operation,payload_version,payload_json,created_at,actor_id) VALUES(?,?,?,?,?,0,1,'create',1,?,?,?)", newID(), s.ledgerID, s.deviceID, id, entityType, encode(payload), now(), nullActor(s.actorID))
	return err
}
func (s *Store) account(tx *sql.Tx, kind, name string) (string, error) {
	var id string
	err := tx.QueryRow("SELECT id FROM accounts WHERE ledger_id=? AND kind=? AND name=?", s.ledgerID, kind, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = newID()
	timestamp := now()
	_, err = tx.Exec("INSERT INTO accounts(id,ledger_id,name,kind,created_at,updated_at) VALUES(?,?,?,?,?,?)", id, s.ledgerID, name, kind, timestamp, timestamp)
	if err != nil {
		return "", err
	}
	err = s.change(tx, "account", id, map[string]any{"id": id, "ledgerId": s.ledgerID, "name": name, "kind": kind, "version": 1, "createdAt": timestamp, "updatedAt": timestamp, "deletedAt": nil})
	return id, err
}
func (s *Store) insert(tx *sql.Tx, t Transaction) error {
	minor, err := strconv.ParseInt(t.AmountMinor, 10, 64)
	if err != nil {
		return err
	}
	fund, err := s.account(tx, "clearing", t.Account)
	if err != nil {
		return err
	}
	categoryType, categoryName, fundMinor := categoryKind(t), t.Category, minor
	if t.Type == "transfer" {
		categoryType, categoryName, fundMinor = "clearing", t.ToAccount, -minor
	}
	category, err := s.account(tx, categoryType, categoryName)
	if err != nil {
		return err
	}
	var key any
	if identity(t) != "" {
		key = identity(t)
	}
	timestamp := now()
	columns := "id,ledger_id,occurred_on,kind,amount_minor,currency,merchant,category,source,account,external_id,note,identity_key,fingerprint,similarity_key,version,device_id,created_at,updated_at"
	values := "?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?"
	args := []any{t.ID, s.ledgerID, t.Date, t.Type, minor, t.Currency, t.Merchant, t.Category, t.Source, t.Account, t.ExternalID, t.Note, key, fingerprint(t), similarity(t), s.deviceID, timestamp, timestamp}
	if t.Type == "refund" {
		columns += ",refund_of"
		values += ",?"
		args = append(args, t.RefundOf)
	}
	if t.Type == "transfer" {
		columns += ",to_account"
		values += ",?"
		args = append(args, t.ToAccount)
	}
	_, err = tx.Exec("INSERT INTO transactions("+columns+") VALUES("+values+")", args...)
	if err != nil {
		return err
	}
	postings := []map[string]any{}
	for _, p := range []struct {
		account string
		amount  int64
	}{{fund, fundMinor}, {category, -fundMinor}} {
		id := newID()
		if _, err = tx.Exec("INSERT INTO postings VALUES(?,?,?,?,?)", id, t.ID, p.account, p.amount, "CNY"); err != nil {
			return err
		}
		postings = append(postings, map[string]any{"id": id, "accountId": p.account, "amountMinor": strconv.FormatInt(p.amount, 10), "currency": "CNY"})
	}
	var balance int64
	if err = tx.QueryRow("SELECT SUM(amount_minor) FROM postings WHERE transaction_id=?", t.ID).Scan(&balance); err != nil {
		return err
	}
	if balance != 0 {
		return problem("unbalanced", "分录不平衡，操作已取消")
	}
	if s.actorID != "" {
		if _, err = tx.Exec("UPDATE transactions SET created_by=? WHERE id=?", s.actorID, t.ID); err != nil {
			return err
		}
	}
	return s.change(tx, "transaction", t.ID, map[string]any{"transaction": t, "postings": postings, "ledgerId": s.ledgerID, "createdAt": timestamp, "updatedAt": timestamp, "deletedAt": nil})
}

func (s *Store) Create(in Input, key string) (Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := normalize(in)
	if err != nil {
		return t, err
	}
	if len(key) < 16 || len(key) > 128 || strings.ContainsAny(key, "\x00\n\r") {
		return t, problem("invalid", "缺少有效的幂等请求标识")
	}
	requestHash := fingerprint(t)
	tx, err := s.db.Begin()
	if err != nil {
		return t, err
	}
	defer tx.Rollback()
	if err = s.authorize(tx, true); err != nil {
		return t, err
	}
	var previousHash, previousJSON string
	err = tx.QueryRow("SELECT request_hash,response_json FROM idempotency WHERE ledger_id=? AND key=?", s.ledgerID, key).Scan(&previousHash, &previousJSON)
	if err == nil {
		if previousHash != requestHash {
			return t, problem("conflict", "该请求标识已用于另一笔数据")
		}
		err = json.Unmarshal([]byte(previousJSON), &t)
		return t, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return t, err
	}
	if identity(t) != "" {
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM transactions WHERE ledger_id=? AND identity_key=?", s.ledgerID, identity(t)).Scan(&count); err != nil {
			return t, err
		}
		if count > 0 {
			return t, problem("conflict", "该来源流水号已入账，请通过导入预览检查重复")
		}
	}
	t.ID = newID()
	if err = s.insert(tx, t); err != nil {
		return t, err
	}
	if _, err = tx.Exec("UPDATE ledgers SET version=version+1 WHERE id=?", s.ledgerID); err != nil {
		return t, err
	}
	if _, err = tx.Exec("INSERT INTO idempotency VALUES(?,?,?,?,?)", s.ledgerID, key, requestHash, encode(t), now()); err != nil {
		return t, err
	}
	return t, tx.Commit()
}

const transactionColumns = `id,occurred_on,kind,amount_minor,currency,merchant,category,source,account,external_id,note,version,COALESCE(refund_of,''),COALESCE(to_account,'')`

func scanRecord(row interface{ Scan(...any) error }) (Transaction, error) {
	var t Transaction
	var minor int64
	err := row.Scan(&t.ID, &t.Date, &t.Type, &minor, &t.Currency, &t.Merchant, &t.Category, &t.Source, &t.Account, &t.ExternalID, &t.Note, &t.Version, &t.RefundOf, &t.ToAccount)
	t.AmountMinor = strconv.FormatInt(minor, 10)
	return t, err
}
func (s *Store) Dashboard(month, query string, page int) (Dashboard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
	if month == "" {
		month = today[:7]
	}
	d := Dashboard{LedgerID: s.ledgerID, Month: month, Today: today, Months: []string{}, IncomeMinor: "0", ExpenseMinor: "0", Page: page, PageSize: PageSize, Categories: []CategoryTotal{}, Transactions: []Transaction{}}
	if !monthPattern.MatchString(month) || month[:4] < "1900" || month[:4] > "2200" {
		return d, problem("invalid", "月份须为 YYYY-MM")
	}
	if page < 1 || page > 100000 || len([]rune(query)) > 100 {
		return d, problem("invalid", "分页或搜索条件无效")
	}
	var err error
	if err = s.authorize(s.db, false); err != nil {
		return d, err
	}
	if d.Version, err = s.version(); err != nil {
		return d, err
	}
	rows, err := s.db.Query("SELECT DISTINCT substr(occurred_on,1,7) FROM transactions WHERE ledger_id=? AND deleted_at IS NULL ORDER BY 1 DESC", s.ledgerID)
	if err != nil {
		return d, err
	}
	months := map[string]bool{month: true, today[:7]: true}
	for rows.Next() {
		var m string
		if err = rows.Scan(&m); err != nil {
			rows.Close()
			return d, err
		}
		months[m] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	for m := range months {
		d.Months = append(d.Months, m)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(d.Months)))
	rows, err = s.db.Query("SELECT "+transactionColumns+" FROM transactions WHERE ledger_id=? AND substr(occurred_on,1,7)=? AND deleted_at IS NULL ORDER BY occurred_on DESC,created_at DESC,id", s.ledgerID, month)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	var income, expense, refunds int64
	sources := map[string]bool{}
	categories := map[string]int64{}
	categoryRefunds := map[string]int64{}
	query = strings.ToLower(strings.TrimSpace(query))
	for rows.Next() {
		t, e := scanRecord(rows)
		if e != nil {
			return d, e
		}
		minor, _ := strconv.ParseInt(t.AmountMinor, 10, 64)
		if t.Type == "transfer" {
			// A positive magnitude is not income: both legs are funding accounts.
			// Keep the event in the month/list without adding it to any totals.
		} else if t.Type == "refund" {
			if minor <= 0 || refunds > math.MaxInt64-minor {
				return d, problem("overflow", "统计金额超出可表示范围")
			}
			refunds += minor
			categoryRefunds[t.Category] += minor
		} else if minor > 0 {
			if income > math.MaxInt64-minor {
				return d, problem("overflow", "统计金额超出可表示范围")
			}
			income += minor
		} else {
			if expense > math.MaxInt64+minor {
				return d, problem("overflow", "统计金额超出可表示范围")
			}
			expense -= minor
			categories[t.Category] -= minor
		}
		d.TotalCount++
		sources[t.Source] = true
		if query == "" || strings.Contains(strings.ToLower(strings.Join([]string{t.Merchant, t.Category, t.Source, t.Account, t.ToAccount, t.Note}, "\n")), query) {
			if d.FilteredCount >= (page-1)*PageSize && d.FilteredCount < page*PageSize {
				d.Transactions = append(d.Transactions, t)
			}
			d.FilteredCount++
		}
	}
	if err = rows.Err(); err != nil {
		return d, err
	}
	d.IncomeMinor = strconv.FormatInt(income, 10)
	d.GrossExpenseMinor = strconv.FormatInt(expense, 10)
	d.RefundMinor = strconv.FormatInt(refunds, 10)
	d.ExpenseMinor = strconv.FormatInt(expense-refunds, 10)
	d.SourceCount = len(sources)
	for name := range categoryRefunds {
		if _, exists := categories[name]; !exists {
			categories[name] = 0
		}
	}
	for name, minor := range categories {
		d.Categories = append(d.Categories, CategoryTotal{Name: name, AmountMinor: strconv.FormatInt(minor-categoryRefunds[name], 10), GrossExpenseMinor: strconv.FormatInt(minor, 10), RefundMinor: strconv.FormatInt(categoryRefunds[name], 10)})
	}
	sort.Slice(d.Categories, func(i, j int) bool {
		a, _ := strconv.ParseInt(d.Categories[i].AmountMinor, 10, 64)
		b, _ := strconv.ParseInt(d.Categories[j].AmountMinor, 10, 64)
		if a == b {
			return d.Categories[i].Name < d.Categories[j].Name
		}
		return a > b
	})
	return d, nil
}
