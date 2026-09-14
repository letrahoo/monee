package ledger

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxCSVBytes = 2 << 20
const MaxRows = 1000
const PageSize = 50

type Problem struct{ Code, Message string }

func (e *Problem) Error() string         { return e.Message }
func problem(code, message string) error { return &Problem{code, message} }

type Input struct {
	Date       string `json:"date"`
	Type       string `json:"type"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	Merchant   string `json:"merchant"`
	Category   string `json:"category"`
	Source     string `json:"source"`
	Account    string `json:"account"`
	ExternalID string `json:"externalId"`
	Note       string `json:"note"`
}

type Transaction struct {
	ID          string `json:"id"`
	Date        string `json:"date"`
	Type        string `json:"type"`
	AmountMinor string `json:"amountMinor"`
	Currency    string `json:"currency"`
	Merchant    string `json:"merchant"`
	Category    string `json:"category"`
	Source      string `json:"source"`
	Account     string `json:"account"`
	ExternalID  string `json:"externalId"`
	Note        string `json:"note"`
	Version     int64  `json:"version"`
}

type CategoryTotal struct {
	Name        string `json:"name"`
	AmountMinor string `json:"amountMinor"`
}
type Dashboard struct {
	LedgerID      string          `json:"ledgerId"`
	Version       int64           `json:"version"`
	Month         string          `json:"month"`
	Today         string          `json:"today"`
	Months        []string        `json:"months"`
	IncomeMinor   string          `json:"incomeMinor"`
	ExpenseMinor  string          `json:"expenseMinor"`
	SourceCount   int             `json:"sourceCount"`
	TotalCount    int             `json:"totalCount"`
	FilteredCount int             `json:"filteredCount"`
	Page          int             `json:"page"`
	PageSize      int             `json:"pageSize"`
	Categories    []CategoryTotal `json:"categories"`
	Transactions  []Transaction   `json:"transactions"`
}
type PreviewRow struct {
	Line    int         `json:"line"`
	Record  Transaction `json:"record"`
	Status  string      `json:"status"`
	Message string      `json:"message"`
}
type Preview struct {
	ID               string       `json:"id"`
	Filename         string       `json:"filename"`
	LedgerVersion    int64        `json:"ledgerVersion"`
	Rows             []PreviewRow `json:"rows"`
	NewCount         int          `json:"newCount"`
	DuplicateCount   int          `json:"duplicateCount"`
	SimilarCount     int          `json:"similarCount"`
	Errors           []string     `json:"errors"`
	AlreadyCommitted bool         `json:"alreadyCommitted"`
}
type CommitResult struct {
	ImportID string `json:"importId"`
	Added    int    `json:"added"`
	Skipped  int    `json:"skipped"`
	Month    string `json:"month"`
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func hash(text string) string { sum := sha256.Sum256([]byte(text)); return hex.EncodeToString(sum[:]) }
func encode(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

var amountPattern = regexp.MustCompile(`^[0-9]{1,11}(\.[0-9]{1,2})?$`)
var monthPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

func parseAmount(value string) (int64, error) {
	if !amountPattern.MatchString(value) {
		return 0, problem("invalid", "金额须为正数，最多两位小数，不支持千分位或科学计数法")
	}
	parts := strings.SplitN(value, ".", 2)
	fraction := "00"
	if len(parts) == 2 {
		fraction = (parts[1] + "0")[:2]
	}
	n, err := strconv.ParseInt(parts[0]+fraction, 10, 64)
	if err != nil || n <= 0 || n > 1_000_000_000_000 {
		return 0, problem("invalid", "金额须大于 0 且不超过 100 亿元")
	}
	return n, nil
}

func normalize(in Input) (Transaction, error) {
	t := Transaction{Date: strings.TrimSpace(in.Date), Type: strings.TrimSpace(in.Type), Currency: strings.TrimSpace(in.Currency), Merchant: strings.TrimSpace(in.Merchant), Category: strings.TrimSpace(in.Category), Source: strings.TrimSpace(in.Source), Account: strings.TrimSpace(in.Account), ExternalID: strings.TrimSpace(in.ExternalID), Note: strings.TrimSpace(in.Note), Version: 1}
	date, err := time.Parse("2006-01-02", t.Date)
	if err != nil || date.Year() < 1900 || date.Year() > 2200 {
		return t, problem("invalid", "日期须为 1900–2200 年的 YYYY-MM-DD")
	}
	if t.Type == "支出" {
		t.Type = "expense"
	}
	if t.Type == "收入" {
		t.Type = "income"
	}
	if t.Type != "expense" && t.Type != "income" {
		return t, problem("invalid", "当前只支持收入或支出；转账、退款、还款须等待相应功能")
	}
	if t.Currency != "CNY" {
		return t, problem("invalid", "当前仅支持明确标注 CNY 的人民币账单")
	}
	minor, err := parseAmount(strings.TrimSpace(in.Amount))
	if err != nil {
		return t, err
	}
	if t.Type == "expense" {
		minor = -minor
	}
	t.AmountMinor = strconv.FormatInt(minor, 10)
	if t.Merchant == "" || t.Source == "" {
		return t, problem("invalid", "商户和来源不能为空")
	}
	if t.Category == "" {
		t.Category = "未分类"
	}
	if t.Account == "" {
		t.Account = "待核实资金账户"
	}
	for _, s := range []string{t.Merchant, t.Category, t.Source, t.Account, t.ExternalID} {
		if len([]rune(s)) > 200 || strings.ContainsAny(s, "\x00\r\n") {
			return t, problem("invalid", "字段过长或含有不支持的换行/控制字符")
		}
	}
	if len([]rune(t.Note)) > 1000 || strings.ContainsRune(t.Note, 0) {
		return t, problem("invalid", "备注最多 1000 字")
	}
	return t, nil
}

func identity(t Transaction) string {
	if t.ExternalID == "" {
		return ""
	}
	return hash(encode([]string{t.Source, t.Account, t.ExternalID}))
}
func fingerprint(t Transaction) string { t.ID = ""; t.Version = 1; return hash(encode(t)) }
func similarity(t Transaction) string {
	return hash(encode([]string{t.Date, t.Type, t.AmountMinor, t.Currency, t.Merchant}))
}
